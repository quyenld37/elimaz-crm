package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/conversation"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// metricPresenceKeys: counter số dòng idempotency_keys do nhịp tim sinh ra (mỗi nhịp tim đã lưu một dòng; kế hoạch M4
// Q9: xem lại khi bảng vượt 5 triệu dòng).
const metricPresenceKeys = "idempotency_keys_created"

// GetPresence (GET /api/v1/conversations/{id}/presence, conversation.read trên Page của hội thoại): người khác đang
// xem, gõ, lên đơn còn hạn, trừ người hỏi (Task 163). Không có quyền trên Page hay không có hội thoại → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetPresence(ctx context.Context, params gen.GetPresenceParams) (gen.GetPresenceRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var items []conversation.PresenceEntry
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		var err error
		items, err = conversation.ListPresence(ctx, tx, u, params.ID, h.now())
		return err //nolint:wrapcheck // conversation đã bọc
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("đọc người có mặt: %w", err)
	}
	out := &gen.PresenceList{Items: make([]gen.PresenceEntry, 0, len(items))}
	for _, it := range items {
		out.Items = append(out.Items, gen.PresenceEntry{
			UserId: it.UserID, DisplayName: it.DisplayName, State: gen.PresenceState(it.State),
		})
	}
	return out, nil
}

// SetPresence (POST /api/v1/conversations/{id}/presence, conversation.read trên Page của hội thoại): nhịp tim "đang
// xem/gõ/lên đơn" hạn 30 s, mỗi lần một khóa chống trùng mới (Q9); báo hiệu conversation.presence chỉ khi trạng thái
// đổi hoặc mới xuất hiện. Không audit (không phải thay đổi nghiệp vụ). Không có quyền hay không có hội thoại → 404,
// không lưu khóa.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetPresence(ctx context.Context, req *gen.SetPresenceRequest, params gen.SetPresenceParams) (gen.SetPresenceRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var refused error
	err := WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		refused = nil // fn có thể chạy lại
		err := conversation.SetPresence(ctx, tx, h.d.Realtime, u, params.ID, conversation.PresenceState(req.State), h.now())
		switch {
		case errors.Is(err, conversation.ErrNotFound):
			refused = err
			return 0, nil, false, nil
		case err != nil:
			return 0, nil, false, err //nolint:wrapcheck // conversation đã bọc
		}
		return http.StatusNoContent, nil, true, nil
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, refused
	}
	if h.d.Metrics != nil {
		h.d.Metrics.Inc(metricPresenceKeys, "setPresence", 1)
	}
	return &gen.SetPresenceNoContent{}, nil
}
