package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

// Bài viết của comment (Task 154): bài mới được ghi kind unknown trong tx của comment và xếp lệnh chỉ đọc
// command.fb.fetch_post; lệnh đọc bài qua channel.PostFetcher ngoài transaction, OnDone ghi loại, đường dẫn, đoạn
// trích, giờ đăng. Payload lệnh chỉ có mã (token đọc từ dòng tài khoản lúc chạy).

const (
	// FetchPostKind là lệnh đọc bài viết chứa comment.
	FetchPostKind = "command.fb.fetch_post"

	fetchPostExpiry  = time.Hour
	fetchPostTimeout = 30 * time.Second
	// postSnippetRunes là độ dài đoạn trích nội dung bài.
	postSnippetRunes = 120

	// Loại bài (conversation.posts.kind).
	postKindPost, postKindLive, postKindUnknown = "post", "live", "unknown"

	// Lý do (last_error) của lệnh; không chứa token, nội dung.
	reasonBadPayload  = "bad_payload"
	reasonNoAccount   = "account_missing"
	reasonNoCreds     = "no_credentials"
	reasonNoFetcher   = "no_post_fetcher"
	reasonReadFailed  = "read_failed"
	reasonCredsBroken = "credentials_unreadable"
)

// fetchPostPayload là payload của lệnh đọc bài.
type fetchPostPayload struct {
	AccountID  uuid.UUID `json:"accountId"`
	PostID     uuid.UUID `json:"postId"`
	ExternalID string    `json:"externalId"`
}

// fetchPostResult là Outcome.Result của lệnh: thông tin bài (Missing: bài không còn trên kênh).
type fetchPostResult struct {
	Kind        string     `json:"kind,omitempty"`
	Permalink   string     `json:"permalink,omitempty"`
	Snippet     string     `json:"snippet,omitempty"`
	CreatedTime *time.Time `json:"createdTime,omitempty"`
	Missing     bool       `json:"missing,omitempty"`
}

// canFetchPosts cho biết adapter của nguồn source đọc được bài.
func canFetchPosts(r *channel.Registry, source string) bool {
	if r == nil {
		return false
	}
	a, ok := r.Get(source)
	if !ok {
		return false
	}
	_, ok = a.(channel.PostFetcher)
	return ok
}

// ensurePost trả mã bài ext của Page acc; chưa có thì tạo (unknown) và xếp lệnh đọc bài khi kênh đọc được bài.
func (h *handler) ensurePost(ctx context.Context, tx pgx.Tx, e webhooks.Inbound, acc channel.ChannelAccount, ext string,
) (uuid.UUID, error) {
	q := store.New(tx)
	id, err := q.InsertPostIfMissing(ctx, store.InsertPostIfMissingParams{ChannelAccountID: acc.ID, ExternalID: ext})
	if errors.Is(err, pgx.ErrNoRows) {
		id, err = q.FindPost(ctx, store.FindPostParams{ChannelAccountID: acc.ID, ExternalID: ext})
		if err != nil {
			return uuid.Nil, fmt.Errorf("đọc bài viết: %w", err)
		}
		return id, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi bài viết: %w", err)
	}
	if !canFetchPosts(h.d.Registry, h.source) {
		return id, nil
	}
	tenant := e.TenantID
	if _, _, err := h.d.Queue.Enqueue(ctx, tx, fetchPostKind(), queue.NewJob{
		TenantID: &tenant, Payload: fetchPostPayload{AccountID: acc.ID, PostID: id, ExternalID: ext},
		DedupeKey: "fetch_post:" + id.String(),
	}); err != nil {
		return uuid.Nil, fmt.Errorf("ghi lệnh đọc bài: %w", err)
	}
	return id, nil
}

func fetchPostKind() queue.Kind {
	return commands.CommandKind(FetchPostKind, fetchPostExpiry, fetchPostTimeout)
}

// postKind đổi status_type của kênh thành loại bài: có "live" (video live) → live; rỗng → unknown (UpsertPost giữ
// loại đã biết); còn lại → post. Bài quảng cáo (ad) chưa nhận biết được từ status_type.
func postKind(statusType string) string {
	s := strings.ToLower(strings.TrimSpace(statusType))
	switch {
	case s == "":
		return postKindUnknown
	case strings.Contains(s, "live"):
		return postKindLive
	}
	return postKindPost
}

// fetchPost là lệnh chỉ đọc command.fb.fetch_post.
type fetchPost struct {
	d   deps.Deps
	now func() time.Time
}

func newFetchPost(d deps.Deps) fetchPost {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return fetchPost{d: d, now: now}
}

func (fetchPost) Kind() queue.Kind       { return fetchPostKind() }
func (fetchPost) AutoRetryNotSent() bool { return true }

// load đọc payload, tài khoản, token của j trong một transaction ngắn; out khác nil là kết quả cuối Execute trả ngay.
func (c fetchPost) load(ctx context.Context, j queue.Job,
) (p fetchPostPayload, acc channel.ChannelAccount, cred channel.Credentials, out *commands.Outcome) {
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.AccountID == uuid.Nil || p.ExternalID == "" || j.TenantID == nil {
		return p, acc, cred, &commands.Outcome{Status: commands.StatusRejected, Reason: reasonBadPayload}
	}
	var credErr error
	err := tenancy.WithTenant(ctx, c.d.DB, *j.TenantID, func(tx pgx.Tx) (err error) {
		credErr = nil // RetryTx có thể chạy lại
		if acc, err = channel.Account(ctx, tx, p.AccountID); err != nil {
			return err //nolint:wrapcheck // channel đã bọc
		}
		cred, credErr = channel.CredentialsFor(ctx, tx, c.d.Keys, p.AccountID)
		return nil
	})
	switch {
	case errors.Is(err, channel.ErrAccountNotFound):
		return p, acc, cred, &commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoAccount}
	case err != nil:
		return p, acc, cred, &commands.Outcome{Status: commands.StatusNotSent, Reason: reasonReadFailed}
	case errors.Is(credErr, channel.ErrNoCredentials):
		return p, acc, cred, &commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoCreds}
	case credErr != nil:
		return p, acc, cred, &commands.Outcome{Status: commands.StatusRejected, Reason: reasonCredsBroken}
	}
	return p, acc, cred, nil
}

// Execute đọc bài qua adapter của kênh ngoài transaction. Bài không còn (lỗi vĩnh viễn) → done Missing (bài giữ
// unknown, không dead); chưa gửi, quá tải → not_sent (tự thử lại trong 1 giờ); token, quyền, chính sách → rejected;
// còn lại → unknown (in_doubt; Lookup trả not_sent vì lệnh chỉ đọc).
func (c fetchPost) Execute(ctx context.Context, j queue.Job) (commands.Outcome, error) {
	p, acc, cred, out := c.load(ctx, j)
	if out != nil {
		return *out, nil
	}
	var ad channel.MessagingAdapter
	var ok bool
	if c.d.Registry != nil {
		ad, ok = c.d.Registry.Get(acc.Channel)
	}
	pf, isFetcher := ad.(channel.PostFetcher)
	if !ok || !isFetcher {
		return commands.Outcome{Status: commands.StatusRejected, Reason: reasonNoFetcher}, nil
	}
	info, err := pf.FetchPost(ctx, acc, cred, p.ExternalID)
	if err != nil {
		class := ad.ClassifyError(err)
		switch class {
		case channel.ErrorPermanent:
			return commands.Outcome{Status: commands.StatusDone, Result: json.RawMessage(`{"missing":true}`)}, nil
		case channel.ErrorNotSentRetry, channel.ErrorRateLimited:
			return commands.Outcome{Status: commands.StatusNotSent, Reason: string(class)}, nil
		case channel.ErrorAuth, channel.ErrorPolicy:
			return commands.Outcome{Status: commands.StatusRejected, Reason: string(class)}, nil
		default:
			return commands.Outcome{Status: commands.StatusUnknown, Reason: string(class)}, nil
		}
	}
	res := fetchPostResult{Kind: info.Kind, Permalink: info.Permalink, Snippet: info.Snippet}
	if !info.CreatedTime.IsZero() {
		t := info.CreatedTime.UTC()
		res.CreatedTime = &t
	}
	b, err := json.Marshal(res)
	if err != nil {
		return commands.Outcome{Status: commands.StatusUnknown, Reason: "encode_result"}, nil //nolint:nilerr // kiểu cố định; phòng thủ
	}
	return commands.Outcome{Status: commands.StatusDone, Result: b}, nil
}

// Lookup: lệnh chỉ đọc, chạy lại không có tác dụng phụ ngoài → luôn not_sent (về hàng chờ).
func (fetchPost) Lookup(context.Context, queue.Job) (string, error) {
	return commands.StatusNotSent, nil
}

// OnDone (trong tx đổi lệnh sang done) ghi thông tin bài: loại, đường dẫn, đoạn trích 120 ký tự, giờ đăng, mốc đọc.
// Bài không còn → chỉ ghi mốc đọc. Không có kết quả (done do tra lại) → không làm gì.
func (c fetchPost) OnDone(ctx context.Context, j queue.Job, result json.RawMessage, tx pgx.Tx) error {
	if len(result) == 0 {
		return nil
	}
	var p fetchPostPayload
	var res fetchPostResult
	if err := json.Unmarshal(j.Payload, &p); err != nil {
		return fmt.Errorf("đọc payload lệnh đọc bài: %w", err)
	}
	if err := json.Unmarshal(result, &res); err != nil {
		return fmt.Errorf("đọc kết quả lệnh đọc bài: %w", err)
	}
	snip := []rune(strings.TrimSpace(res.Snippet)) // cắt theo ký tự, không cắt giữa chữ có dấu
	if len(snip) > postSnippetRunes {
		snip = snip[:postSnippetRunes]
	}
	params := store.UpsertPostParams{
		ChannelAccountID: p.AccountID, ExternalID: p.ExternalID, Kind: postKind(res.Kind), Permalink: text(res.Permalink),
		Snippet:   text(string(snip)),
		FetchedAt: pgtype.Timestamptz{Time: c.now().UTC(), Valid: true},
	}
	if res.CreatedTime != nil {
		params.CreatedTime = pgtype.Timestamptz{Time: *res.CreatedTime, Valid: true}
	}
	if _, err := store.New(tx).UpsertPost(ctx, params); err != nil {
		return fmt.Errorf("ghi thông tin bài viết: %w", err)
	}
	return nil
}
