package channeltest

import (
	"context"
	"errors"
	"iter"
	"slices"
	"sync"
	"time"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Lỗi mẫu của ConnectorAdapter, mỗi lỗi một lớp ClassifyError (test giả lập lỗi của Graph khi kết nối Page).
var (
	ErrConnectNotSent   = errors.New("channeltest: chưa gửi, thử lại được")
	ErrConnectAuth      = errors.New("channeltest: token hỏng")
	ErrConnectPermanent = errors.New("channeltest: yêu cầu sai")
	ErrConnectUnknown   = errors.New("channeltest: không rõ đã gửi chưa")
)

// Tên thao tác của ConnectorAdapter (Call.Op, FailNext).
const (
	OpExchange    = "exchange"
	OpListPages   = "list_pages"
	OpPageToken   = "page_token"
	OpSubscribe   = "subscribe"
	OpUnsubscribe = "unsubscribe"
	OpHealth      = "health"
	OpBackfill    = "backfill"
	OpCommentPoll = "comment_poll"
)

// FakePage là một Page token thấy được: Token là token Page Meta trả khi xin bằng token đó.
type FakePage struct {
	PageID, Name, Token     string
	CanMessage, CanModerate bool
}

// Call là một lần ConnectorAdapter được gọi: thao tác, Page, token đã dùng, trường đăng ký.
type Call struct {
	Op, PageID, Token string
	Fields            []string
	// Since là mốc đọc của OpBackfill, OpCommentPoll.
	Since time.Time
}

// ConnectorAdapter là adapter kênh giả có channel.Connector (đổi mã đăng nhập, liệt kê Page, lấy token Page, đăng ký và
// hủy webhook) cho test kết nối Page, trạng thái trong bộ nhớ, an toàn khi dùng từ nhiều goroutine. Chỉ hiện thực Key,
// HealthCheck, ClassifyError và Connector; phương thức khác của channel.MessagingAdapter (nhúng nil) không được gọi
// trong test kết nối (gọi thì panic).
type ConnectorAdapter struct {
	channel.MessagingAdapter

	key string

	mu         sync.Mutex
	codes      map[string]string                  // mã đăng nhập → token người dùng
	pages      map[string]map[string]FakePage     // token → mã Page → Page
	revoked    map[string]bool                    // token đã thu hồi
	subscribed map[string][]string                // mã Page → trường đã đăng ký
	failNext   map[string][]error                 // thao tác → lỗi lần gọi tới
	backfill   map[string][]webhooks.InboundEvent // mã Page → sự kiện Backfill trả
	comments   map[string][]webhooks.InboundEvent // mã Page → sự kiện PollPageComments trả
	calls      []Call
}

// NewConnectorAdapter dựng adapter giả khóa key (thường "facebook").
func NewConnectorAdapter(key string) *ConnectorAdapter {
	return &ConnectorAdapter{
		key: key, codes: map[string]string{}, pages: map[string]map[string]FakePage{}, revoked: map[string]bool{},
		subscribed: map[string][]string{}, failNext: map[string][]error{}, backfill: map[string][]webhooks.InboundEvent{},
		comments: map[string][]webhooks.InboundEvent{},
	}
}

// Key là khóa kênh của adapter giả.
func (a *ConnectorAdapter) Key() string { return a.key }

// AddCode cho mã đăng nhập code đổi ra token người dùng userToken.
func (a *ConnectorAdapter) AddCode(code, userToken string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.codes[code] = userToken
}

// AddPages cho token (người dùng hay system user) thấy các Page ps.
func (a *ConnectorAdapter) AddPages(token string, ps ...FakePage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pages[token] == nil {
		a.pages[token] = map[string]FakePage{}
	}
	for _, p := range ps {
		a.pages[token][p.PageID] = p
	}
}

// RevokeToken thu hồi token: lời gọi sau bằng token đó trả ErrConnectAuth.
func (a *ConnectorAdapter) RevokeToken(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.revoked[token] = true
}

// FailNext làm lần gọi op tới trả err (gọi nhiều lần: lần lượt từng lỗi).
func (a *ConnectorAdapter) FailNext(op string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failNext[op] = append(a.failNext[op], err)
}

// Calls trả các lần gọi thao tác op (rỗng: mọi thao tác).
func (a *ConnectorAdapter) Calls(op string) []Call {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.DeleteFunc(slices.Clone(a.calls), func(c Call) bool { return op != "" && c.Op != op })
}

// Subscribed trả trường đang đăng ký của Page pageID (nil: chưa đăng ký).
func (a *ConnectorAdapter) Subscribed(pageID string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.subscribed[pageID])
}

// begin ghi lần gọi và trả lỗi đã hẹn của op (nếu có); a.mu đang giữ.
func (a *ConnectorAdapter) begin(c Call) error {
	a.calls = append(a.calls, c)
	if errs := a.failNext[c.Op]; len(errs) > 0 {
		a.failNext[c.Op] = errs[1:]
		return errs[0]
	}
	if a.revoked[c.Token] {
		return ErrConnectAuth
	}
	return nil
}

// ExchangeCode đổi mã đăng nhập ra token người dùng; mã lạ → ErrConnectPermanent.
func (a *ConnectorAdapter) ExchangeCode(_ context.Context, code string) (logging.Secret, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.begin(Call{Op: OpExchange}); err != nil {
		return "", err
	}
	tok, ok := a.codes[code]
	if !ok {
		return "", ErrConnectPermanent
	}
	return logging.Secret(tok), nil
}

// ListPages trả Page token người dùng thấy, theo mã Page.
func (a *ConnectorAdapter) ListPages(_ context.Context, userToken logging.Secret) ([]channel.PageInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.begin(Call{Op: OpListPages, Token: string(userToken)}); err != nil {
		return nil, err
	}
	var out []channel.PageInfo
	for _, p := range a.pages[string(userToken)] {
		out = append(out, channel.PageInfo{PageID: p.PageID, Name: p.Name, CanMessage: p.CanMessage, CanModerate: p.CanModerate})
	}
	slices.SortFunc(out, func(x, y channel.PageInfo) int {
		if x.PageID < y.PageID {
			return -1
		}
		return 1
	})
	return out, nil
}

// PageToken trả token Page pageID mà token thấy; không thấy → channel.ErrPageNotAccessible.
func (a *ConnectorAdapter) PageToken(_ context.Context, token logging.Secret, pageID string) (logging.Secret, channel.PageInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.begin(Call{Op: OpPageToken, PageID: pageID, Token: string(token)}); err != nil {
		return "", channel.PageInfo{}, err
	}
	p, ok := a.pages[string(token)][pageID]
	if !ok {
		return "", channel.PageInfo{}, channel.ErrPageNotAccessible
	}
	return logging.Secret(p.Token), channel.PageInfo{PageID: p.PageID, Name: p.Name, CanMessage: p.CanMessage, CanModerate: p.CanModerate}, nil
}

// Subscribe đăng ký fields cho Page của acc bằng token cred.
func (a *ConnectorAdapter) Subscribe(_ context.Context, acc channel.ChannelAccount, cred channel.Credentials, fields []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.begin(Call{Op: OpSubscribe, PageID: acc.ExternalID, Token: string(cred.AccessToken), Fields: slices.Clone(fields)}); err != nil {
		return err
	}
	a.subscribed[acc.ExternalID] = slices.Clone(fields)
	return nil
}

// Unsubscribe hủy đăng ký webhook của Page acc.
func (a *ConnectorAdapter) Unsubscribe(_ context.Context, acc channel.ChannelAccount, cred channel.Credentials) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.begin(Call{Op: OpUnsubscribe, PageID: acc.ExternalID, Token: string(cred.AccessToken)}); err != nil {
		return err
	}
	delete(a.subscribed, acc.ExternalID)
	return nil
}

// HealthCheck báo token còn hiệu lực và Page có đăng ký webhook (đủ trường) hay không.
func (a *ConnectorAdapter) HealthCheck(_ context.Context, acc channel.ChannelAccount, cred channel.Credentials) (channel.Health, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.begin(Call{Op: OpHealth, PageID: acc.ExternalID, Token: string(cred.AccessToken)}); err != nil {
		if errors.Is(err, ErrConnectAuth) {
			return channel.Health{}, nil
		}
		return channel.Health{}, err
	}
	sub := a.subscribed[acc.ExternalID]
	var missing []string
	for _, f := range channel.WebhookFields() {
		if !slices.Contains(sub, f) {
			missing = append(missing, f)
		}
	}
	return channel.Health{TokenValid: true, PermissionsOK: true, WebhookSubscribed: len(missing) == 0, MissingFields: missing}, nil
}

// ClassifyError xếp lỗi mẫu vào lớp: chưa gửi → not_sent_retry, token hỏng → auth, sai → permanent, khác → unknown.
func (*ConnectorAdapter) ClassifyError(err error) channel.ErrorClass {
	switch {
	case errors.Is(err, ErrConnectNotSent):
		return channel.ErrorNotSentRetry
	case errors.Is(err, ErrConnectAuth):
		return channel.ErrorAuth
	case errors.Is(err, ErrConnectPermanent):
		return channel.ErrorPermanent
	}
	return channel.ErrorUnknown
}

// Features trả tính năng của adapter giả: comment, nhắn riêng, ẩn comment (để danh sách kênh có tính năng).
func (*ConnectorAdapter) Features() channel.Features {
	return channel.Features{Comments: true, PrivateReply: true, HideComment: true}
}

// SetBackfill đặt sự kiện Backfill trả cho Page pageID (mọi lần gọi, không lọc theo since).
func (a *ConnectorAdapter) SetBackfill(pageID string, evs ...webhooks.InboundEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.backfill[pageID] = slices.Clone(evs)
}

// SetComments đặt sự kiện PollPageComments trả cho Page pageID.
func (a *ConnectorAdapter) SetComments(pageID string, evs ...webhooks.InboundEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.comments[pageID] = slices.Clone(evs)
}

// Backfill trả các sự kiện đã đặt bằng SetBackfill; lỗi hẹn (FailNext OpBackfill) hay token thu hồi → một lỗi sau
// cùng.
func (a *ConnectorAdapter) Backfill(_ context.Context, acc channel.ChannelAccount, cred channel.Credentials, since time.Time,
) iter.Seq2[webhooks.InboundEvent, error] {
	a.mu.Lock()
	err := a.begin(Call{Op: OpBackfill, PageID: acc.ExternalID, Token: string(cred.AccessToken), Since: since})
	evs := slices.Clone(a.backfill[acc.ExternalID])
	a.mu.Unlock()
	return func(yield func(webhooks.InboundEvent, error) bool) {
		if err != nil {
			yield(webhooks.InboundEvent{}, err)
			return
		}
		for _, e := range evs {
			if !yield(e, nil) {
				return
			}
		}
	}
}

// PollPageComments trả các sự kiện đã đặt bằng SetComments (hiện thực channel.CommentPoller).
func (a *ConnectorAdapter) PollPageComments(_ context.Context, acc channel.ChannelAccount, cred channel.Credentials, since time.Time,
) ([]webhooks.InboundEvent, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.begin(Call{Op: OpCommentPoll, PageID: acc.ExternalID, Token: string(cred.AccessToken), Since: since}); err != nil {
		return nil, err
	}
	return slices.Clone(a.comments[acc.ExternalID]), nil
}
