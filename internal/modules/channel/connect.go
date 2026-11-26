package channel

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Kết nối và ngắt kết nối Page (kế hoạch M4, Task 149; 02 mục 2). Gọi kênh (đổi mã, liệt kê Page, lấy token Page) ở
// ExchangeCode, FetchPageToken, ngoài mọi transaction; Writer.Connect, Writer.Disconnect chỉ ghi trong tx của bên gọi và
// ghi lệnh đăng ký/hủy đăng ký webhook (subscribe.go) để worker gọi kênh sau commit.

var (
	// ErrPageNotAccessible báo token không quản trị được Page (Page không thuộc tài sản của token, thiếu quyền
	// MESSAGING hay MODERATE, kênh từ chối token). Adapter trả lỗi này từ Connector.PageToken khi token không thấy Page.
	ErrPageNotAccessible = errors.New("channel: không quản trị được Page này")
	// ErrPageConnectedElsewhere báo Page đã nối với một shop khác (tuyến webhook thuộc tenant khác).
	ErrPageConnectedElsewhere = errors.New("channel: Page đã nối với shop khác")
	// ErrConnectUnavailable báo gọi kênh khi kết nối gặp lỗi tạm (mạng, quá tải, không rõ): chưa lưu gì, thử lại sau.
	ErrConnectUnavailable = errors.New("channel: chưa gọi được kênh, thử lại sau")
)

// routeKind: loại tuyến webhook (platform.tenant_routes.kind) của kênh kết nối được.
func routeKind(channel string) (string, bool) {
	if channel == "facebook" {
		return "fb_page", true
	}
	return "", false
}

// connector trả adapter khóa key có Connector; không có → ErrNoConnector.
func connector(reg *Registry, key string) (MessagingAdapter, Connector, error) { //nolint:ireturn // adapter là interface của kênh
	if reg == nil {
		return nil, nil, ErrNoConnector
	}
	a, ok := reg.Get(key)
	if !ok {
		return nil, nil, ErrNoConnector
	}
	c, ok := a.(Connector)
	if !ok {
		return nil, nil, ErrNoConnector
	}
	return a, c, nil
}

// CanConnect báo kênh key có adapter có Connector trong reg (bật kết nối Page).
func CanConnect(reg *Registry, key string) bool {
	_, _, err := connector(reg, key)
	return err == nil
}

// refused: lớp lỗi kênh từ chối hẳn (token, quyền, yêu cầu sai), không phải lỗi tạm.
func refused(a MessagingAdapter, err error) bool {
	switch a.ClassifyError(err) {
	case ErrorAuth, ErrorPolicy, ErrorPermanent:
		return true
	default:
		return false
	}
}

// OAuthLogin là kết quả một lần đổi mã đăng nhập: token người dùng, các Page người dùng quản trị và mã người dùng app
// (lưu dạng băm vào phiên để xóa theo yêu cầu của Meta).
type OAuthLogin struct {
	Token     logging.Secret
	Pages     []PageInfo
	AppUserID string
}

// ExchangeCode đổi mã đăng nhập code (kênh key) ra token người dùng, mã người dùng app và danh sách Page người dùng quản
// trị. Kênh từ chối (mã sai, hết hạn, token hỏng) → ErrOAuthStateInvalid; lỗi tạm → ErrConnectUnavailable; không có
// Connector → ErrNoConnector. Gọi mạng: không chạy trong transaction.
func ExchangeCode(ctx context.Context, reg *Registry, key, code string) (OAuthLogin, error) {
	a, c, err := connector(reg, key)
	if err != nil {
		return OAuthLogin{}, err
	}
	tok, err := c.ExchangeCode(ctx, code)
	if err != nil {
		return OAuthLogin{}, connectError(a, err, ErrOAuthStateInvalid, "đổi mã đăng nhập")
	}
	uid, err := c.AppUserID(ctx, tok)
	if err != nil {
		return OAuthLogin{}, connectError(a, err, ErrOAuthStateInvalid, "đọc mã người dùng")
	}
	pages, err := c.ListPages(ctx, tok)
	if err != nil {
		return OAuthLogin{}, connectError(a, err, ErrOAuthStateInvalid, "liệt kê Page")
	}
	return OAuthLogin{Token: tok, Pages: pages, AppUserID: uid}, nil
}

// FetchPageToken lấy token Page pageID bằng token (người dùng hay system user) và kiểm Page có đủ MESSAGING, MODERATE.
// Không thấy Page, thiếu quyền, kênh từ chối token → ErrPageNotAccessible; lỗi tạm → ErrConnectUnavailable. Gọi mạng.
func FetchPageToken(ctx context.Context, reg *Registry, key string, token logging.Secret, pageID string) (logging.Secret, PageInfo, error) {
	a, c, err := connector(reg, key)
	if err != nil {
		return "", PageInfo{}, err
	}
	pt, info, err := c.PageToken(ctx, token, pageID)
	switch {
	case errors.Is(err, ErrPageNotAccessible):
		return "", PageInfo{}, ErrPageNotAccessible
	case err != nil:
		return "", PageInfo{}, connectError(a, err, ErrPageNotAccessible, "lấy token Page")
	case !info.CanMessage || !info.CanModerate || pt == "":
		return "", PageInfo{}, ErrPageNotAccessible
	}
	return pt, info, nil
}

// connectError đổi lỗi gọi kênh: kênh từ chối → refusal; khác → ErrConnectUnavailable (kèm lỗi gốc, không token).
func connectError(a MessagingAdapter, err, refusal error, what string) error {
	if refused(a, err) {
		return fmt.Errorf("%s: %w", what, refusal)
	}
	return fmt.Errorf("%s: %w: %w", what, ErrConnectUnavailable, err)
}

// ConnectInput là một lần nối Page: kênh, mã Page, tên, người bán own của shop (bên gọi đọc catalog.DefaultSeller),
// token Page và loại token (page_user, system_user).
type ConnectInput struct {
	Channel, PageID, Name string
	SellerID              uuid.UUID
	Token                 logging.Secret
	TokenKind             string
}

// Connect nối Page in trong tx của bên gọi (ADR-46: tuyến nhóm 1 → tài khoản nhóm 3 → jobs nhóm 13): ghi tuyến
// webhook (Page của shop khác → ErrPageConnectedElsewhere; tx hỏng, bên gọi lùi), thêm hoặc cập nhật tài khoản (mới →
// read_only, active; đã có → giữ send_mode, về active, xóa lý do), lưu token mã hóa, audit channel.connected hay
// channel.reconnected (không token), báo hiệu channel.status, ghi lệnh subscribe_page. Không tăng version.
func (w Writer) Connect(ctx context.Context, tx pgx.Tx, kr *fieldcrypt.Keyring, in ConnectInput, actor identity.AuthUser) (ChannelAccount, error) {
	kind, ok := routeKind(in.Channel)
	switch {
	case !ok:
		return ChannelAccount{}, ErrNoConnector
	case kr == nil:
		return ChannelAccount{}, ErrNoKeyring
	case in.Token == "":
		return ChannelAccount{}, ErrInvalidCredentials
	case in.TokenKind != TokenPageUser && in.TokenKind != TokenSystemUser:
		return ChannelAccount{}, ErrInvalidTokenKind
	}
	if err := tenancy.PutRoute(ctx, tx, kind, in.PageID); err != nil {
		if errors.Is(err, tenancy.ErrRouteTaken) {
			return ChannelAccount{}, ErrPageConnectedElsewhere
		}
		return ChannelAccount{}, fmt.Errorf("ghi tuyến Page: %w", err)
	}
	r, err := store.New(tx).UpsertAccount(ctx, store.UpsertAccountParams{
		TenantID: actor.TenantID, SellerID: in.SellerID, Channel: in.Channel, ExternalID: in.PageID, Name: in.Name,
	})
	if err != nil {
		return ChannelAccount{}, fmt.Errorf("ghi tài khoản kênh: %w", err)
	}
	if err := StoreCredentials(ctx, tx, kr, r.ID, in.Token, in.TokenKind); err != nil {
		return ChannelAccount{}, err
	}
	action := "channel.reconnected"
	if r.Inserted {
		action = "channel.connected"
	}
	return w.record(ctx, tx, actor, r.ID, action, SubscribePageKind, func(a ChannelAccount) map[string]any {
		return map[string]any{"channel": a.Channel, "external_id": a.ExternalID, "token_kind": a.TokenKind, "status": a.Status}
	})
}

// Disconnect ngắt Page id trong tx của bên gọi: khóa dòng, status = disconnected (giữ tuyến: tin tới sau vẫn được
// lưu), audit channel.disconnected, báo hiệu, ghi lệnh unsubscribe_page; token chỉ bị xóa khi lệnh đó kết thúc. Đã
// disconnected → trả tài khoản, không ghi gì. Không có → ErrAccountNotFound.
func (w Writer) Disconnect(ctx context.Context, tx pgx.Tx, id uuid.UUID, actor identity.AuthUser) (ChannelAccount, error) {
	r, err := store.New(tx).LockAccount(ctx, id)
	if err != nil {
		return ChannelAccount{}, notFound(err, "khóa tài khoản kênh")
	}
	if cur := accountFrom(accountRow(r)); cur.Status == StatusDisconnected {
		return cur, nil
	}
	if _, err := MarkStatus(ctx, tx, id, StatusDisconnected, ""); err != nil {
		return ChannelAccount{}, err
	}
	return w.record(ctx, tx, actor, id, "channel.disconnected", UnsubscribePageKind, func(a ChannelAccount) map[string]any {
		return map[string]any{"status": a.Status}
	})
}

// record đọc lại tài khoản id sau khi ghi, ghi audit action của nhân viên actor (after(a) không bao giờ có token), báo
// hiệu channel.status và ghi lệnh kind cho tài khoản.
func (w Writer) record(ctx context.Context, tx pgx.Tx, actor identity.AuthUser, id uuid.UUID, action, kind string,
	after func(ChannelAccount) map[string]any,
) (ChannelAccount, error) {
	a, err := Account(ctx, tx, id)
	if err != nil {
		return ChannelAccount{}, err
	}
	if err := identity.RecordAudit(ctx, tx, w.auditEntry(actor, action, id, nil, after(a))); err != nil {
		return ChannelAccount{}, fmt.Errorf("ghi audit %s: %w", action, err)
	}
	if err := w.signal(ctx, tx, a); err != nil {
		return ChannelAccount{}, err
	}
	if err := enqueueAccountCommand(ctx, tx, a, kind); err != nil {
		return ChannelAccount{}, err
	}
	return a, nil
}

// WebhookFields là 11 trường webhook của Page mà CRM đăng ký (04 mục 4; Task 148 kiểm đủ trong HealthCheck).
func WebhookFields() []string {
	return []string{
		"messages", "message_echoes", "messaging_postbacks", "message_reads", "message_deliveries", "message_reactions",
		"message_edits", "messaging_referrals", "messaging_policy_enforcement", "feed", "live_videos",
	}
}
