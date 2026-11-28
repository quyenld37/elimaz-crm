package inventory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
)

// Tồn dự kiến của phiên kiểm kho mở sổ (kế hoạch M7, Task 541 = M7-41; ADR-48): tồn của hệ cũ (Nhanh) lúc chụp, ghi
// theo (kho, biến thể) để màn đếm hiện cột lệch. KHÔNG phải tồn: không có sổ, không số dư, không đổi stock_balances;
// tồn mở sổ vẫn là số đếm của phiên (PostOpening). Chụp lại thì số dự kiến được thay.

// maxExpected là số dự kiến lớn nhất của một biến thể (cùng giới hạn với số đếm).
const maxExpected = 1_000_000

// ExpectedStock là số dự kiến của một biến thể (SellerID là người bán của biến thể, theo khóa ngoại ghép).
type ExpectedStock struct {
	VariantID, SellerID uuid.UUID
	Expected            int32
}

// OpeningDiffRow là một dòng cột lệch: số dự kiến, số đã đếm của phiên (Counted nil: chưa đếm; Diff cũng nil), lệch
// = đếm − dự kiến.
type OpeningDiffRow struct {
	VariantID uuid.UUID
	SKU       string
	Expected  int32
	Counted   *int32
	Diff      *int32
}

// SetOpeningExpected ghi (thay nếu đã có) số dự kiến rows của kho warehouseID, nguồn Nhanh, chụp lúc takenAt, trong tx
// của bên gọi; ghi theo variant_id tăng dần (không khóa chéo với phiên khác). Trả số dòng ghi. Lỗi: ErrInvalidInput
// (số ngoài [0, 1e6], biến thể lặp, takenAt rỗng), ErrNotFound (kho hay biến thể không có trong tenant).
func SetOpeningExpected(ctx context.Context, tx pgx.Tx, tenantID, warehouseID uuid.UUID, takenAt time.Time, rows []ExpectedStock) (int, error) {
	if takenAt.IsZero() {
		return 0, fmt.Errorf("%w: thiếu giờ chụp", ErrInvalidInput)
	}
	sorted := slices.Clone(rows)
	slices.SortFunc(sorted, func(a, b ExpectedStock) int { return compareUUID(a.VariantID, b.VariantID) })
	for i, r := range sorted {
		if r.Expected < 0 || r.Expected > maxExpected || (i > 0 && sorted[i-1].VariantID == r.VariantID) {
			return 0, fmt.Errorf("%w: số dự kiến", ErrInvalidInput)
		}
	}
	q := store.New(tx)
	for _, r := range sorted {
		err := q.UpsertOpeningExpected(ctx, store.UpsertOpeningExpectedParams{
			TenantID: tenantID, WarehouseID: warehouseID, VariantID: r.VariantID, SellerID: r.SellerID,
			Expected: r.Expected, Source: "nhanh", TakenAt: takenAt.UTC(),
		})
		if pe, ok := errors.AsType[*pgconn.PgError](err); ok && pe.Code == "23503" {
			return 0, ErrNotFound
		}
		if err != nil {
			return 0, fmt.Errorf("ghi tồn dự kiến: %w", err)
		}
	}
	return len(sorted), nil
}

// OpeningDiff trả cột lệch của phiên mở sổ stocktakeID: mọi biến thể có số dự kiến ở kho của phiên, theo SKU. Phiên
// không có hay không phải phiên mở sổ → ErrNotFound.
func OpeningDiff(ctx context.Context, tx pgx.Tx, stocktakeID uuid.UUID) ([]OpeningDiffRow, error) {
	o, err := GetOpening(ctx, tx, stocktakeID)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(tx).ListOpeningDiff(ctx, store.ListOpeningDiffParams{StocktakeID: stocktakeID, WarehouseID: o.WarehouseID})
	if err != nil {
		return nil, fmt.Errorf("đọc cột lệch mở sổ: %w", err)
	}
	out := make([]OpeningDiffRow, 0, len(rows))
	for _, r := range rows {
		d := OpeningDiffRow{VariantID: r.VariantID, SKU: r.Sku, Expected: r.Expected}
		if r.Counted.Valid {
			counted, diff := r.Counted.Int32, r.Counted.Int32-r.Expected
			d.Counted, d.Diff = &counted, &diff
		}
		out = append(out, d)
	}
	return out, nil
}
