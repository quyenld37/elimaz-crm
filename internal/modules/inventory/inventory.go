// Package inventory là kho, số dư tồn theo (biến thể, kho), sổ tồn chỉ ghi thêm và giữ hàng theo dòng đơn (kế hoạch
// M1, Task M1-28; spec 05 mục 2 "Giữ hàng và sổ tồn", 04 mục 5.5, ADR-46).
//
// Có thể bán = on_hand − damaged − held − in_transit. Mọi thay đổi số dư là một câu UPDATE có điều kiện
// (movement.go, applyDelta) kèm một dòng sổ cùng transaction; không câu nào đọc số dư ra Go rồi ghi lại.
//
// Thứ tự khóa (docs/adr/0046-thu-tu-khoa.md nhóm 11): stock_balances theo (variant_id, warehouse_id) tăng dần, rồi
// stock_reservations theo id tăng dần. Reserve cập nhật số dư theo thứ tự rồi chèn giữ hàng (dòng mới); Release,
// StartTransit, Consume khóa số dư trước rồi mới khóa và đổi dòng giữ hàng.
//
// Phần công khai: hàm đọc (Availability, DefaultWarehouse, ListWarehouses), tạo kho mặc định (EnsureDefaultWarehouse, chủ bảng) và
// Writer (mọi đường đổi tồn: tự ghi sổ, sự kiện stock.changed và báo hiệu inventory.balance trong tx của bên gọi).
// Đối chiếu tồn hằng đêm (reconcile.go): Reconcile, lịch ReconcileSchedule, handler và lệnh cảnh báo lệch sổ; chỉ
// báo lệch, không sửa số.
//
// Khi nào không dùng: module khác không đọc bảng inventory.* (đọc view inventory.*_v); không sửa số dư bằng SQL ngoài
// gói này (mất sổ và sự kiện); test của module khác nạp tồn bằng invtest.SeedStock.
package inventory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// DefaultWarehouseCode là mã kho chính của tenant trong dữ liệu khởi tạo.
const DefaultWarehouseCode = "MAIN"

const defaultWarehouseName = "Kho chính"

// Tác nhân ghi sổ (cột actor_type).
const (
	ActorStaff    = "staff"
	ActorSystem   = "system"
	ActorCarrier  = "carrier"
	ActorCustomer = "customer"
)

// ReserveLine là một dòng giữ hàng: dòng đơn, biến thể, kho, số lượng (> 0), hạn giữ (nil: không hạn).
type ReserveLine struct {
	OrderItemID, VariantID, WarehouseID uuid.UUID
	Qty                                 int32
	ExpiresAt                           *time.Time
}

// Actor là người hay hệ thống gây ra thay đổi. Type: staff | system | carrier | customer; ID = uuid.Nil → NULL.
type Actor struct {
	Type string
	ID   uuid.UUID
}

func (a Actor) valid() bool {
	switch a.Type {
	case ActorStaff, ActorSystem, ActorCarrier, ActorCustomer:
		return true
	}
	return false
}

// Balance là số dư của một biến thể ở một kho.
type Balance struct {
	VariantID, WarehouseID                      uuid.UUID
	OnHand, Held, InTransit, Damaged, Available int32
}

// Writer là đường đổi tồn: mỗi lần đổi một dòng số dư (trong tx của bên gọi) ghi kèm dòng sổ, sự kiện stock.changed
// (Events.Emit qua Queue) và báo hiệu inventory.balance (Realtime nil → bỏ qua: báo hiệu được phép mất). Dựng mới
// cho mỗi yêu cầu; không giữ trạng thái nên chạy lại được trong db.RetryTx.
type Writer struct {
	Events   *events.Registry
	Queue    queue.JobQueue
	Realtime realtime.Bus
}

// Availability trả số dư của từng biến thể trong variantIDs ở kho warehouseID (bỏ trùng, theo variant_id tăng dần);
// biến thể chưa có dòng số dư trả số 0.
func Availability(ctx context.Context, tx pgx.Tx, warehouseID uuid.UUID, variantIDs []uuid.UUID) ([]Balance, error) {
	ids := slices.Clone(variantIDs)
	slices.SortFunc(ids, compareUUID)
	ids = slices.Compact(ids)
	rows, err := store.New(tx).ListAvailability(ctx, store.ListAvailabilityParams{WarehouseID: warehouseID, VariantIds: ids})
	if err != nil {
		return nil, fmt.Errorf("đọc số dư: %w", err)
	}
	out := make([]Balance, 0, len(ids))
	for _, id := range ids {
		b := Balance{VariantID: id, WarehouseID: warehouseID}
		if i := slices.IndexFunc(rows, func(r store.ListAvailabilityRow) bool { return r.VariantID == id }); i >= 0 {
			r := rows[i]
			b.OnHand, b.Held, b.InTransit, b.Damaged, b.Available = r.OnHand, r.Held, r.InTransit, r.Damaged, r.Available
		}
		out = append(out, b)
	}
	return out, nil
}

// Warehouse là một kho của tenant. Status: active | closed.
type Warehouse struct {
	ID     uuid.UUID
	Code   string
	Name   string
	Status string
}

// ListWarehouses trả mọi kho của tenant trong ngữ cảnh của tx (cả kho đã đóng), kho MAIN trước rồi theo mã. Bên gọi
// tự lọc theo phạm vi quyền của người xem.
func ListWarehouses(ctx context.Context, tx pgx.Tx) ([]Warehouse, error) {
	rows, err := store.New(tx).ListWarehouses(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách kho: %w", err)
	}
	out := make([]Warehouse, 0, len(rows))
	for _, r := range rows {
		out = append(out, Warehouse{ID: r.ID, Code: r.Code, Name: r.Name, Status: r.Status})
	}
	return out, nil
}

// DefaultWarehouse trả kho MAIN của tenant trong ngữ cảnh của tx; chưa có → ErrNoWarehouse.
func DefaultWarehouse(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	id, err := store.New(tx).GetWarehouseByCode(ctx, DefaultWarehouseCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNoWarehouse
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc kho mặc định: %w", err)
	}
	return id, nil
}

// EnsureDefaultWarehouse tạo kho MAIN "Kho chính" do sellerID vận hành nếu tenant chưa có, và trả mã kho. Chạy bằng
// chủ bảng (seed-dev; crm_app không ghi được warehouses), trong tenancy.WithTenant của tenantID. Chạy lại được.
func EnsureDefaultWarehouse(ctx context.Context, tx pgx.Tx, tenantID, sellerID uuid.UUID) (uuid.UUID, error) {
	if err := store.New(tx).InsertWarehouseIfMissing(ctx, store.InsertWarehouseIfMissingParams{
		TenantID: tenantID, OperatedBySellerID: sellerID, Code: DefaultWarehouseCode, Name: defaultWarehouseName,
	}); err != nil {
		return uuid.Nil, fmt.Errorf("ghi kho mặc định: %w", err)
	}
	return DefaultWarehouse(ctx, tx)
}

// compareUUID so hai uuid như PostgreSQL (16 byte theo thứ tự), để thứ tự khóa trong Go trùng ORDER BY của SQL.
func compareUUID(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) }

// compareKey so (variant_id, warehouse_id).
func compareKey(av, aw, bv, bw uuid.UUID) int {
	if c := compareUUID(av, bv); c != 0 {
		return c
	}
	return compareUUID(aw, bw)
}
