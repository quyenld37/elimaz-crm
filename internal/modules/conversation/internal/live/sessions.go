package live

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/access"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// Trạng thái phiên và đường nhận biết (cột status, detected_by).
const (
	StatusLive  = "live"
	StatusEnded = "ended"

	DetectedWebhook = "webhook"
	DetectedPoll    = "poll"
	DetectedManual  = "manual"
)

// Báo hiệu comment live (Global Constraints "Báo hiệu realtime" của M4: theo Page, chỉ mã phiên).
const (
	topicLiveComments = "live.comments"
	entityLiveSession = "live_session"
)

const (
	permRead    identity.Permission = "conversation.read"
	permOperate identity.Permission = "live.operate"
	// listLimit là số phiên tối đa một lần liệt kê.
	listLimit = 200
	// maxVideoDigits là độ dài tối đa mã video nhập tay.
	maxVideoDigits = 30
)

var (
	// ErrNotFound là lỗi: phiên hay Page không có, hoặc người gọi không có quyền trên Page (404, PQ04).
	ErrNotFound = access.ErrNotFound
	// ErrVideoInvalid là lỗi: chuỗi nhập tay không phải mã video hay đường dẫn video có mã (400).
	ErrVideoInvalid = errors.New("live: không tìm thấy mã video trong chuỗi nhập")
	// ErrStatusInvalid là lỗi: trạng thái lọc không phải live hay ended (400).
	ErrStatusInvalid = errors.New("live: trạng thái phiên không hợp lệ")
)

// Session là một phiên live của Page. Channel là khóa kênh của Page (nguồn sự kiện vào), chỉ bộ quét điền.
type Session struct {
	ID, TenantID, AccountID                             uuid.UUID
	Channel                                             string
	LiveVideoID, VideoID, PostExternalID, Status, Title string
	SourceMode, ActiveSource, DetectedBy                string
	StartedAt                                           time.Time
	EndedAt, LastCommentAt                              *time.Time
	ReceivedCount                                       int32
	MetaCommentCount                                    *int32
	Version                                             int32
}

func sessionFrom(r store.ConversationLiveSession) Session {
	s := Session{
		ID: r.ID, TenantID: r.TenantID, AccountID: r.ChannelAccountID, LiveVideoID: r.LiveVideoID,
		VideoID: r.VideoID.String, PostExternalID: r.PostExternalID.String, Status: r.Status, Title: r.Title.String,
		SourceMode: r.SourceMode, ActiveSource: r.ActiveSource.String, DetectedBy: r.DetectedBy, StartedAt: r.StartedAt,
		ReceivedCount: r.ReceivedCount, Version: r.Version,
	}
	if r.EndedAt.Valid {
		s.EndedAt = &r.EndedAt.Time
	}
	if r.LastCommentAt.Valid {
		s.LastCommentAt = &r.LastCommentAt.Time
	}
	if r.MetaCommentCount.Valid {
		s.MetaCommentCount = &r.MetaCommentCount.Int32
	}
	return s
}

// StateOf đổi trạng thái phiên live của kênh (Graph API: LIVE, LIVE_STOPPED, VOD, PROCESSING…) sang trạng thái phiên;
// ok = false với trạng thái chưa/không phát (lên lịch, chưa đăng) — bỏ qua.
func StateOf(meta string) (state string, ok bool) {
	switch strings.ToUpper(meta) {
	case "LIVE":
		return StatusLive, true
	case "LIVE_STOPPED", "VOD", "PROCESSING":
		return StatusEnded, true
	}
	return "", false
}

// Observation là một lần thấy phiên live của Page: qua webhook, hỏi định kỳ hay bấm tay.
type Observation struct {
	AccountID                                   uuid.UUID
	LiveVideoID, VideoID, PostExternalID, Title string
	// State: StatusLive hoặc StatusEnded.
	State, DetectedBy string
	// At: lúc phiên bắt đầu (State live) hay kết thúc (State ended).
	At time.Time
}

// UpsertSession ghi một lần thấy phiên (ba đường nhận biết dùng chung, Q5): live → tạo phiên hay giữ phiên có sẵn
// (trùng (Page, live_video_id) chỉ bổ sung mã còn thiếu); ended → phiên đang live của video chuyển ended. Trạng thái
// không bao giờ đi ngược ended → live; phiên vừa chuyển ended ghi lệnh đối chiếu cuối phiên (Task 171). ok = false
// khi kết thúc một phiên chưa biết hay đã kết thúc (không làm gì).
func UpsertSession(ctx context.Context, tx pgx.Tx, o Observation) (s Session, ok bool, err error) {
	q := store.New(tx)
	if o.State == StatusEnded {
		r, err := q.EndLiveSessionByVideo(ctx, store.EndLiveSessionByVideoParams{
			EndedAt: tsOf(o.At), ChannelAccountID: o.AccountID, LiveVideoID: o.LiveVideoID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, false, nil
		}
		if err != nil {
			return Session{}, false, fmt.Errorf("kết thúc phiên live: %w", err)
		}
		s := sessionFrom(r)
		if err := enqueueReconcile(ctx, tx, s); err != nil {
			return Session{}, false, err
		}
		return s, true, nil
	}
	r, err := q.UpsertLiveSession(ctx, store.UpsertLiveSessionParams{
		ChannelAccountID: o.AccountID, LiveVideoID: o.LiveVideoID, VideoID: textOf(o.VideoID),
		PostExternalID: textOf(o.PostExternalID), Status: StatusLive, Title: textOf(o.Title), StartedAt: o.At.UTC(),
		DetectedBy: o.DetectedBy,
	})
	if err != nil {
		return Session{}, false, fmt.Errorf("ghi phiên live: %w", err)
	}
	return sessionFrom(r), true, nil
}

// IsLiveComment trả phiên live (mới nhất) chứa bài postExternalID của Page accountID: khớp mã bài, mã live hay mã
// video (cách Meta đặt mã bài của video live chưa xác minh, E4); không phải comment live → nil.
func IsLiveComment(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, postExternalID string) (*uuid.UUID, error) {
	id, err := store.New(tx).MatchLiveComment(ctx, store.MatchLiveCommentParams{ChannelAccountID: accountID, Post: postExternalID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // không phải comment live
	}
	if err != nil {
		return nil, fmt.Errorf("tìm phiên live của bài: %w", err)
	}
	return &id, nil
}

// OnComment phát báo hiệu live.comments cho phiên sessionID của Page accountID (chỉ đi khi tx commit; tầng báo hiệu
// gộp 300 ms như M0). Comment vào (Task 154) gọi sau khi ghi comment của phiên.
func OnComment(ctx context.Context, tx pgx.Tx, bus realtime.Bus, tenantID, accountID, sessionID uuid.UUID) error {
	if err := bus.Publish(ctx, tx, realtime.Signal{
		TenantID: tenantID, Topic: topicLiveComments, Requires: permRead, Scope: access.PageScope(accountID),
		Entity: realtime.Entity{Type: entityLiveSession, ID: sessionID.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu comment live: %w", err)
	}
	return nil
}

// ParseVideoRef lấy mã video từ chuỗi nhập tay: dãy số, hay đường dẫn có "/videos/<số>" (vd
// https://www.facebook.com/elimaz/videos/123456789/) hoặc tham số "v=<số>". Không có → ErrVideoInvalid.
func ParseVideoRef(in string) (string, error) {
	in = strings.TrimSpace(in)
	if isDigits(in) {
		return in, nil
	}
	for _, mark := range []string{"/videos/", "?v=", "&v="} {
		rest := in
		for {
			i := strings.Index(rest, mark)
			if i < 0 {
				break
			}
			rest = rest[i+len(mark):]
			end := strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' })
			if end < 0 {
				end = len(rest)
			}
			if id := rest[:end]; isDigits(id) {
				return id, nil
			}
		}
	}
	return "", ErrVideoInvalid
}

func isDigits(s string) bool {
	if s == "" || len(s) > maxVideoDigits {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// StartManual theo dõi tay phiên live của Page accountID theo mã video hay đường dẫn video (nút "Theo dõi live", Q5):
// cần live.operate trên Page; Page không có hay không có quyền → ErrNotFound. Video đã có phiên (mã live hay mã video
// trùng) → trả phiên đó; chưa có → phiên live mới detected_by manual, bắt đầu lúc now.
func StartManual(ctx context.Context, tx pgx.Tx, u identity.AuthUser, accountID uuid.UUID, ref string, now time.Time) (Session, error) {
	if err := access.Require(u, permOperate, accountID); err != nil {
		return Session{}, err //nolint:wrapcheck // sentinel của phần (404)
	}
	if _, err := channel.Account(ctx, tx, accountID); err != nil {
		if errors.Is(err, channel.ErrAccountNotFound) {
			return Session{}, ErrNotFound
		}
		return Session{}, fmt.Errorf("đọc Page theo dõi live: %w", err)
	}
	id, err := ParseVideoRef(ref)
	if err != nil {
		return Session{}, err
	}
	r, err := store.New(tx).FindLiveSessionByVideo(ctx, store.FindLiveSessionByVideoParams{ChannelAccountID: accountID, Video: id})
	switch {
	case err == nil:
		return sessionFrom(r), nil
	case !errors.Is(err, pgx.ErrNoRows):
		return Session{}, fmt.Errorf("tìm phiên của video: %w", err)
	}
	s, _, err := UpsertSession(ctx, tx, Observation{
		AccountID: accountID, LiveVideoID: id, VideoID: id, State: StatusLive, DetectedBy: DetectedManual, At: now,
	})
	return s, err
}

// EndManual kết thúc tay phiên id (cần live.operate trên Page của phiên) và ghi lệnh đối chiếu cuối phiên; đã kết
// thúc thì trả nguyên phiên.
func EndManual(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID, now time.Time) (Session, error) {
	q := store.New(tx)
	cur, err := q.GetLiveSession(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("đọc phiên live: %w", err)
	}
	if err := access.Require(u, permOperate, cur.ChannelAccountID); err != nil {
		return Session{}, err //nolint:wrapcheck // sentinel của phần (404)
	}
	r, err := q.EndLiveSession(ctx, store.EndLiveSessionParams{EndedAt: tsOf(now), ID: id})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if r, err = q.GetLiveSession(ctx, id); err != nil {
			return Session{}, fmt.Errorf("đọc lại phiên live: %w", err)
		}
		return sessionFrom(r), nil
	case err != nil:
		return Session{}, fmt.Errorf("kết thúc phiên live: %w", err)
	}
	s := sessionFrom(r)
	return s, enqueueReconcile(ctx, tx, s)
}

// List trả phiên trên các Page u được đọc (conversation.read), lọc trạng thái (rỗng = mọi trạng thái) và mốc bắt đầu
// từ since (nil = không lọc); mới nhất trước, tối đa 200.
func List(ctx context.Context, tx pgx.Tx, u identity.AuthUser, status string, since *time.Time) ([]Session, error) {
	if status != "" && status != StatusLive && status != StatusEnded {
		return nil, ErrStatusInvalid
	}
	all, ids := access.VisiblePages(u)
	if !all && len(ids) == 0 {
		return []Session{}, nil
	}
	p := store.ListLiveSessionsParams{AllPages: all, Ids: ids, Status: textOf(status), Lim: listLimit}
	if since != nil {
		p.Since = tsOf(*since)
	}
	rows, err := store.New(tx).ListLiveSessions(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("liệt kê phiên live: %w", err)
	}
	out := make([]Session, 0, len(rows))
	for _, r := range rows {
		out = append(out, sessionFrom(r))
	}
	return out, nil
}

func textOf(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func tsOf(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t.UTC(), Valid: true} }
