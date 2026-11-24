package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Tạm dừng nhận đơn web (kế hoạch M5, Task 456). Đọc cần order.read, ghi cần order.web_intake.manage (bảng route);
// website đọc {paused} qua webIntakeStatus (handlers_web_catalog.go). Audit ghi ở đây, cùng tx (order không nhập
// identity).

// GetWebIntake (GET /api/v1/web-intake, order.read).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetWebIntake(ctx context.Context) (gen.GetWebIntakeRes, error) {
	var out *gen.WebIntake
	if err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		wi, err := order.GetWebIntake(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // order đã bọc
		}
		out, err = webIntakeOut(ctx, tx, wi)
		return err
	}); err != nil {
		return nil, fmt.Errorf("đọc trạng thái nhận đơn web: %w", err)
	}
	return out, nil
}

// SetWebIntake (PUT /api/v1/web-intake, order.web_intake.manage): tạm dừng (lý do bắt buộc → 422) hay mở lại khi
// version khớp (409); audit order.web_intake.set kèm lý do; sự kiện order.web_intake.changed làm mới bộ đệm website.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetWebIntake(ctx context.Context, req *gen.SetWebIntakeRequest, _ gen.SetWebIntakeParams) (gen.SetWebIntakeRes, error) {
	w := order.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
	out, err := h.refusableWrite(ctx, http.StatusOK, isWebIntakeRefusal, "ghi trạng thái nhận đơn web",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			ch, err := w.SetWebIntake(ctx, tx, req.Paused, req.Reason.Or(""), int(req.Version),
				order.Actor{TenantID: u.TenantID, UserID: u.UserID}, h.now())
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của order
			}
			out, err := webIntakeOut(ctx, tx, ch.After)
			if err != nil {
				return nil, err
			}
			return out, h.audit(ctx, tx, u, "order.web_intake.set", "order.web_intake", u.TenantID,
				ch.Before.AuditFields(), ch.After.AuditFields())
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WebIntake), nil //nolint:forcetypeassert // fn trên trả *gen.WebIntake
}

// isWebIntakeRefusal: lỗi nghiệp vụ của SetWebIntake — trả lời, không lưu theo khóa chống trùng.
func isWebIntakeRefusal(err error) bool {
	return errors.Is(err, order.ErrReasonRequired) || errors.Is(err, order.ErrInvalidInput) ||
		errors.Is(err, order.ErrVersionConflict)
}

// webIntakeOut là trạng thái của hợp đồng nhân viên, kèm tên hiển thị của người đổi (staffNames; nhân viên không còn →
// không có tên).
func webIntakeOut(ctx context.Context, tx pgx.Tx, wi order.WebIntake) (*gen.WebIntake, error) {
	out := &gen.WebIntake{Paused: wi.Paused, Version: int32(wi.Version)} //nolint:gosec // G115: version int4 của bảng
	if wi.Reason != "" {
		out.Reason = gen.NewOptString(wi.Reason)
	}
	if wi.ChangedAt != nil {
		out.ChangedAt = gen.NewOptDateTime(*wi.ChangedAt)
	}
	if wi.ChangedBy != nil {
		out.ChangedBy = gen.NewOptUUID(*wi.ChangedBy)
		names, err := staffNames(ctx, tx, []uuid.UUID{*wi.ChangedBy})
		if err != nil {
			return nil, err
		}
		if n := names[*wi.ChangedBy]; n != "" {
			out.ChangedByName = gen.NewOptString(n)
		}
	}
	return out, nil
}
