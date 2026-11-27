package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Đơn nháp hết hạn (kế hoạch M2, Task M2-21; Q8): lịch order.expire_drafts cho từng tenant. M5 Task 202: mỗi 15 giây
// (tối thiểu của platform.schedules), mỗi lần tối đa 500 đơn — suất flash sale hết hạn giữ phải quay lại sớm.
const (
	// ExpireDraftsSchedule là tên lịch hủy đơn nháp quá hạn (platform.schedules).
	ExpireDraftsSchedule = "order.expire_drafts"
	expireDraftsEvery    = 15 * time.Second
	expireDraftsBatch    = 500
)

// ExpireDrafts hủy (lý do draft_expired, người làm system) các đơn nháp của tenant trong tx có draft_expires_at ≤ now,
// tối đa limit đơn theo mã đơn tăng dần, trong tx của bên gọi; đơn đang bị khóa (nhân viên đang chốt, sửa) để kỳ sau.
// Mỗi đơn như Cancel: nhả hàng, trả lượt mã, trả suất flash sale, lịch sử, order.cancelled. Trả số đơn đã hủy. limit ≤
// 0 → ErrInvalidInput. Đơn không hủy được ở bước kiểm (ErrInvalidTransition: hàng đã rời kho, nhiều dòng flash) bị bỏ
// qua, vẫn draft, không chặn các đơn khác của lô (M5 Task 202). Cả lô hủy chung một lần (savepoint) để giữ thứ tự khóa
// ADR-46 trên mọi đơn (hủy từng đơn sẽ khóa số dư của đơn trước rồi mới khóa đợt flash của đơn sau); lô lỗi nghiệp vụ →
// lùi savepoint, hủy từng đơn (expireOneByOne), bỏ qua đơn lỗi. Lỗi tạm, ctx hủy → trả lỗi (kỳ sau chạy lại).
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
		if errors.Is(err, ErrInvalidTransition) {
			continue
		}
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
	err = trySavepoint(ctx, tx, func(sp pgx.Tx) error {
		return w.cancelAll(ctx, sp, plans, CancelDraftExpired, "", a, now)
	})
	if err == nil {
		return len(plans), nil
	}
	if !skippable(ctx, err) {
		return 0, fmt.Errorf("hủy đơn nháp quá hạn: %w", err)
	}
	return w.expireOneByOne(ctx, tx, plans, a, now)
}

// expireOneByOne là đường dự phòng khi cả lô lỗi nghiệp vụ (vd. suất flash của một đơn lệch claims hay đơn vị do sửa tay
// dữ liệu; đối chiếu đêm báo): hủy từng đơn trong savepoint riêng, đơn lỗi nghiệp vụ bị bỏ qua (vẫn draft, không chặn
// đơn khác, kế hoạch M5 Task 202). Mã giảm của mọi đơn đã khóa trước (ExpireDrafts); thứ tự khóa giữa các đơn không còn
// giữ được như cả lô, deadlock hiếm gặp → lỗi tạm, trả lỗi, kỳ sau chạy lại. Trả số đơn đã hủy.
func (w Writer) expireOneByOne(ctx context.Context, tx pgx.Tx, plans []cancelPlan, a Actor, now time.Time) (int, error) {
	n := 0
	for _, p := range plans {
		err := trySavepoint(ctx, tx, func(sp pgx.Tx) error {
			return w.cancelAll(ctx, sp, []cancelPlan{p}, CancelDraftExpired, "", a, now)
		})
		switch {
		case err == nil:
			n++
		case !skippable(ctx, err):
			return 0, fmt.Errorf("hủy đơn nháp quá hạn %s: %w", p.o.ID, err)
		}
	}
	return n, nil
}

// errSavepoint đánh dấu lỗi của chính savepoint (mở, lùi): transaction hỏng, không bỏ qua được.
var errSavepoint = errors.New("savepoint hỏng")

// trySavepoint chạy fn trong savepoint của tx; lỗi của fn trả nguyên (đã lùi về savepoint), lỗi của savepoint bọc
// errSavepoint.
func trySavepoint(ctx context.Context, tx pgx.Tx, fn func(pgx.Tx) error) error {
	var fnErr error
	err := db.Savepoint(ctx, tx, func(sp pgx.Tx) error {
		fnErr = fn(sp)
		return fnErr
	})
	if err != nil && err != fnErr { //nolint:errorlint // Savepoint trả nguyên lỗi của fn khi lùi được; khác là lỗi của savepoint
		return fmt.Errorf("%w: %w", errSavepoint, err)
	}
	return fnErr
}

// skippable: lỗi nghiệp vụ của một lần hủy (đã lùi về savepoint) mà lô bỏ qua được; lỗi tạm (deadlock, chờ khóa),
// ctx đã hủy, savepoint hỏng thì không.
func skippable(ctx context.Context, err error) bool {
	if _, transient := db.TransientCode(err); transient {
		return false
	}
	return ctx.Err() == nil && !errors.Is(err, errSavepoint)
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

// Handle hủy tối đa 500 đơn nháp quá hạn; kết quả {"expired": n}.
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
