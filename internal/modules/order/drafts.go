package order

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Đơn nháp hết hạn (kế hoạch M2, Task M2-21; Q8): lịch order.expire_drafts mỗi 60 giây cho từng tenant, mỗi lần tối
// đa 200 đơn.
const (
	// ExpireDraftsSchedule là tên lịch hủy đơn nháp quá hạn (platform.schedules).
	ExpireDraftsSchedule = "order.expire_drafts"
	expireDraftsEvery    = time.Minute
	expireDraftsBatch    = 200
)

// ExpireDrafts hủy (lý do draft_expired, người làm system) các đơn nháp của tenant trong tx có draft_expires_at ≤ now,
// tối đa limit đơn theo mã đơn tăng dần, trong tx của bên gọi; đơn đang bị khóa (nhân viên đang chốt, sửa) để kỳ sau.
// Mỗi đơn như Cancel: nhả hàng, trả lượt mã, lịch sử, order.cancelled. Trả số đơn đã hủy. limit ≤ 0 →
// ErrInvalidInput. Lỗi ở một đơn → trả lỗi (bên gọi lùi cả lần; việc nền của kỳ sau chạy lại).
func (w Writer) ExpireDrafts(ctx context.Context, tx pgx.Tx, now time.Time, limit int) (int, error) {
	if err := w.wired(); err != nil {
		return 0, err
	}
	if limit <= 0 || limit > math.MaxInt32 {
		return 0, fmt.Errorf("%w: số đơn mỗi lần %d", ErrInvalidInput, limit)
	}
	now = now.UTC()
	q := store.New(tx)
	rows, err := q.LockExpiredDrafts(ctx, store.LockExpiredDraftsParams{Now: now, MaxRows: int32(limit)})
	if err != nil {
		return 0, fmt.Errorf("tìm đơn nháp quá hạn: %w", err)
	}
	// ADR-46 cho cả lần: mọi đơn đã khóa (câu trên, theo mã đơn) → mọi dòng mã khóa trước theo id tăng dần
	// (promotion.LockCoupons, cùng thứ tự với sửa đơn đổi mã) → trả lượt → số dư tồn một lần (cancelAll).
	plans := make([]cancelPlan, 0, len(rows))
	codes := make([]string, 0, len(rows))
	var tenantID uuid.UUID
	for _, r := range rows {
		o, err := lockOrder(ctx, q, r.SellerID, r.ID)
		if err != nil {
			return 0, err
		}
		cur := lockedOrder(o)
		tenantID = o.TenantID
		p, err := prepareCancel(ctx, tx, &cur)
		if err != nil {
			return 0, fmt.Errorf("hủy đơn nháp quá hạn %s: %w", r.ID, err)
		}
		code, err := promotion.OrderCouponCode(ctx, tx, r.ID)
		if err != nil {
			return 0, fmt.Errorf("mã giảm giá của đơn nháp %s: %w", r.ID, err)
		}
		plans, codes = append(plans, p), append(codes, code)
	}
	if err := promotion.LockCoupons(ctx, tx, codes); err != nil {
		return 0, fmt.Errorf("khóa mã giảm giá của đơn nháp: %w", err)
	}
	a := Actor{TenantID: tenantID, Type: inventory.ActorSystem}
	if err := w.cancelAll(ctx, tx, plans, CancelDraftExpired, "", a, now); err != nil {
		return 0, fmt.Errorf("hủy đơn nháp quá hạn: %w", err)
	}
	return len(rows), nil
}

// expireDraftsKind: việc của lịch; lỗi thì không chạy lại (kỳ sau có việc mới), quá một kỳ chưa chạy thì expired.
func expireDraftsKind() queue.Kind {
	return queue.Kind{
		Name: "task." + ExpireDraftsSchedule, Policy: queue.Policy{MaxAttempts: 1, ExpiresAfter: expireDraftsEvery},
	}
}

// expireDraftsSchedule là lịch order.expire_drafts (theo tenant).
func expireDraftsSchedule() schedules.Schedule {
	return schedules.Schedule{Name: ExpireDraftsSchedule, Every: expireDraftsEvery, PerTenant: true, Kind: expireDraftsKind()}
}

// expireDraftsHandler chạy ExpireDrafts với giờ của now (background.Deps.Clock, cùng đồng hồ với lớp HTTP) trong tx của
// việc (worker đặt WithTenant của tenant của việc).
type expireDraftsHandler struct {
	w   Writer
	now func() time.Time
}

func (expireDraftsHandler) Kind() queue.Kind { return expireDraftsKind() }

// Handle hủy tối đa 200 đơn nháp quá hạn; kết quả {"expired": n}.
func (h expireDraftsHandler) Handle(ctx context.Context, _ queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	n, err := h.w.ExpireDrafts(ctx, tx, h.now(), expireDraftsBatch)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(map[string]int{"expired": n})
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả hủy đơn nháp: %w", err)
	}
	return out, nil
}
