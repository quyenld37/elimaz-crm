package webcatalog

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/queries/webcatalog/internal/store"
)

// Giới hạn của còn/hết và "đã bán" (kế hoạch M5 Task 191): một lần đọc tối đa MaxAvailabilityIDs biến thể; còn 1 tới
// LowStockMax là "sắp hết"; "đã bán" tính SoldWindow gần nhất.
const (
	MaxAvailabilityIDs = 100
	LowStockMax        = 3
	SoldWindow         = 30 * 24 * time.Hour
)

// notSoldStatuses là trạng thái đơn không tính vào "đã bán" (docs/m5/doi-chieu-gia-dinh.md mục 4): nháp chưa thành
// đơn, đã hủy, đã hoàn. Trùng order.StatusDraft, StatusCancelled, StatusReturned (gói này không nhập module; test của
// internal/httpapi/web so với hằng của order).
func notSoldStatuses() []string { return []string{"draft", "cancelled", "returned"} }

// NotSoldStatuses trả bản sao notSoldStatuses (cho test so hằng).
func NotSoldStatuses() []string { return notSoldStatuses() }

// WebAvailability là còn/hết của một biến thể ở kho mặc định: Available khi có thể bán > 0; Low khi có thể bán từ 1
// tới LowStockMax. Phần đã giữ cho đơn và cho đợt flash sale không tính (FS09).
type WebAvailability struct {
	VariantID      uuid.UUID
	Available, Low bool
}

// WebSold là số đơn vị đã bán của một biến thể (kèm mẫu của nó) trong SoldWindow.
type WebSold struct {
	ProductID, VariantID uuid.UUID
	Sold                 int
}

// Availability trả còn/hết ở kho DefaultWarehouseCode của các biến thể trong variantIDs (tối đa MaxAvailabilityIDs,
// mã trùng tính một lần), theo mã tăng dần; mã không có trong tenant thì không có dòng. Tenant chưa có kho mặc định:
// mọi biến thể hết hàng.
func Availability(ctx context.Context, tx pgx.Tx, variantIDs []uuid.UUID) ([]WebAvailability, error) {
	if len(variantIDs) > MaxAvailabilityIDs {
		return nil, ErrBadLimit
	}
	if len(variantIDs) == 0 {
		return []WebAvailability{}, nil
	}
	q := store.New(tx)
	wh, err := defaultWarehouse(ctx, q)
	if err != nil {
		return nil, err
	}
	ids := slices.Clone(variantIDs)
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	rows, err := q.AvailabilityOf(ctx, store.AvailabilityOfParams{WarehouseID: wh, VariantIds: slices.Compact(ids)})
	if err != nil {
		return nil, fmt.Errorf("đọc còn/hết cho website: %w", err)
	}
	out := make([]WebAvailability, len(rows))
	for i, r := range rows {
		out[i] = WebAvailability{VariantID: r.VariantID, Available: r.Available > 0, Low: r.Available >= 1 && r.Available <= LowStockMax}
	}
	return out, nil
}

// InStock trả mã các mẫu trong productIDs có ít nhất một biến thể bán được còn có thể bán ở kho DefaultWarehouseCode
// (bộ phối "hết hàng", Task 454), theo mã tăng dần; không giới hạn số mã (bên gọi đã chặn: ≤ 3 món × 500 bộ). Tenant
// chưa có kho mặc định: không mẫu nào còn hàng.
func InStock(ctx context.Context, tx pgx.Tx, productIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(productIDs) == 0 {
		return []uuid.UUID{}, nil
	}
	q := store.New(tx)
	wh, err := defaultWarehouse(ctx, q)
	if err != nil {
		return nil, err
	}
	ids, err := q.InStockProducts(ctx, store.InStockProductsParams{WarehouseID: wh, ProductIds: productIDs})
	if err != nil {
		return nil, fmt.Errorf("đọc mẫu còn hàng cho website: %w", err)
	}
	return ids, nil
}

// Sold30 trả số đơn vị đã bán theo biến thể (kèm mã mẫu) của đơn tạo trong [now − SoldWindow, now), mọi kênh, trừ
// đơn nháp, đã hủy, đã hoàn; theo mã biến thể. Biến thể không bán được đơn vị nào thì không có dòng.
func Sold30(ctx context.Context, tx pgx.Tx, now time.Time) ([]WebSold, error) {
	rows, err := store.New(tx).Sold30(ctx, store.Sold30Params{
		Since: now.Add(-SoldWindow), Until: now, Excluded: notSoldStatuses(),
	})
	if err != nil {
		return nil, fmt.Errorf("đọc số đã bán 30 ngày cho website: %w", err)
	}
	out := make([]WebSold, len(rows))
	for i, r := range rows {
		out[i] = WebSold{ProductID: r.ProductID, VariantID: r.VariantID, Sold: int(r.Sold)}
	}
	return out, nil
}

// defaultWarehouse trả mã kho DefaultWarehouseCode; tenant chưa có kho đó → uuid.Nil (không dòng số dư nào khớp).
func defaultWarehouse(ctx context.Context, q *store.Queries) (uuid.UUID, error) {
	whs, err := q.WarehouseByCode(ctx, DefaultWarehouseCode)
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc kho mặc định cho website: %w", err)
	}
	if len(whs) == 0 {
		return uuid.Nil, nil
	}
	return whs[0], nil
}
