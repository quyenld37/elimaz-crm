package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/modules/identity"
)

// StartLiveSessionManually (POST /api/v1/channels/{id}/live-sessions, live.operate trên Page): nút "Theo dõi live" nhập
// mã video hay đường dẫn video (Q5, Task 170). Không có mã video → 400; Page không có hay không có quyền → 404. 201 kèm
// phiên (video đã có phiên → phiên đó).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) StartLiveSessionManually(ctx context.Context, req *gen.StartLiveSessionRequest,
	params gen.StartLiveSessionManuallyParams,
) (gen.StartLiveSessionManuallyRes, error) {
	return h.liveWrite(ctx, http.StatusCreated, "theo dõi live", func(tx pgx.Tx, u identity.AuthUser) (conversation.LiveSession, error) {
		return conversation.StartLiveManually(ctx, tx, u, params.ID, req.VideoIdOrUrl, h.now())
	})
}

// EndLiveSessionManually (POST /api/v1/live-sessions/{id}/end, live.operate trên Page của phiên): kết thúc theo dõi;
// đã kết thúc → 200 với phiên như cũ.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) EndLiveSessionManually(ctx context.Context, params gen.EndLiveSessionManuallyParams) (gen.EndLiveSessionManuallyRes, error) {
	return h.liveWrite(ctx, http.StatusOK, "kết thúc theo dõi live", func(tx pgx.Tx, u identity.AuthUser) (conversation.LiveSession, error) {
		return conversation.EndLiveManually(ctx, tx, u, params.ID, h.now())
	})
}

// ListLiveSessions (GET /api/v1/live-sessions?status=&since=): phiên trên các Page người gọi được đọc, mới nhất trước.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListLiveSessions(ctx context.Context, params gen.ListLiveSessionsParams) (gen.ListLiveSessionsRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var since *time.Time
	if v, ok := params.Since.Get(); ok {
		since = &v
	}
	var items []conversation.LiveSession
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		items, err = conversation.ListLiveSessions(ctx, tx, u, string(params.Status.Or("")), since)
		return err //nolint:wrapcheck // bọc ở dưới
	})
	if err != nil {
		return nil, fmt.Errorf("liệt kê phiên live: %w", err)
	}
	out := &gen.LiveSessionList{Items: make([]gen.LiveSession, 0, len(items))}
	for _, s := range items {
		out.Items = append(out.Items, *liveSessionOut(s))
	}
	return out, nil
}

// liveWrite chạy change trong refusableWrite (lỗi nghiệp vụ hộp chat không lưu theo khóa) và trả thân LiveSession.
func (h *handler) liveWrite(ctx context.Context, status int, what string,
	change func(tx pgx.Tx, u identity.AuthUser) (conversation.LiveSession, error),
) (*gen.LiveSession, error) {
	out, err := h.refusableWrite(ctx, status, isConversationRefusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		s, err := change(tx, u)
		if err != nil {
			return nil, err
		}
		return liveSessionOut(s), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.LiveSession), nil //nolint:forcetypeassert // fn trên trả *gen.LiveSession
}

func liveSessionOut(s conversation.LiveSession) *gen.LiveSession {
	out := &gen.LiveSession{
		ID: s.ID, PageId: s.AccountID, LiveVideoId: s.LiveVideoID, Status: gen.LiveSessionStatus(s.Status),
		StartedAt: s.StartedAt, DetectedBy: gen.LiveSessionDetectedBy(s.DetectedBy), ReceivedCount: s.ReceivedCount,
		Version: s.Version,
	}
	if s.VideoID != "" {
		out.VideoId = gen.NewOptString(s.VideoID)
	}
	if s.Title != "" {
		out.Title = gen.NewOptString(s.Title)
	}
	if s.EndedAt != nil {
		out.EndedAt = gen.NewOptDateTime(*s.EndedAt)
	}
	if s.MetaCommentCount != nil {
		out.MetaCommentCount = gen.NewOptInt32(*s.MetaCommentCount)
	}
	if s.ActiveSource != "" {
		out.ActiveSource = gen.NewOptString(s.ActiveSource)
	}
	if s.LastCommentAt != nil {
		out.LastCommentAt = gen.NewOptDateTime(*s.LastCommentAt)
	}
	return out
}
