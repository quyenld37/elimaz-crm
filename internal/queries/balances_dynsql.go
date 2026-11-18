package queries

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// maxBalanceMatches là số biến thể khớp BalanceFilter.Q tối đa (giới hạn của catalog.search_variants).
const maxBalanceMatches = 100

// BalanceCursor là vị trí sau dòng cuối của một trang danh sách tồn (thứ tự variant_id giảm: biến thể mới tạo trước).
type BalanceCursor struct {
	VariantID uuid.UUID
}

// String mã hóa con trỏ thành chuỗi mờ cho API (base64url của mã biến thể).
func (c BalanceCursor) String() string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.VariantID.String()))
}

// ParseBalanceCursor đọc chuỗi do BalanceCursor.String tạo. Hỏng → ErrInvalidInput.
func ParseBalanceCursor(s string) (BalanceCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return BalanceCursor{}, fmt.Errorf("đọc con trỏ tồn: %w", ErrInvalidInput)
	}
	id, err := uuid.Parse(string(raw))
	if err != nil {
		return BalanceCursor{}, fmt.Errorf("đọc con trỏ tồn: %w", ErrInvalidInput)
	}
	return BalanceCursor{VariantID: id}, nil
}

// BalanceFilter là bộ lọc của ListBalances. SellerID, WarehouseID bắt buộc (bên gọi đã kiểm quyền xem kho). Q: tìm
// theo barcode, SKU, mã mẫu (tiền tố), tên không dấu qua catalog.search_variants (tối đa 100 biến thể khớp). OnlyShort:
// chỉ dòng có thể bán âm (thiếu so với phần giữ, Q8). Cursor: trang sau. Limit 1–100.
type BalanceFilter struct {
	SellerID    uuid.UUID
	WarehouseID uuid.UUID
	Q           string
	OnlyShort   bool
	Cursor      *BalanceCursor
	Limit       int
}

// BalanceRow là số dư một biến thể ở một kho. Available = OnHand − Damaged − Held − InTransit (có thể âm).
// ProductStatus: draft, active, discontinued; VariantStatus: active, discontinued.
type BalanceRow struct {
	VariantID     uuid.UUID
	SKU           string
	Barcode       string
	ProductCode   string
	ProductName   string
	Color         string
	Size          string
	ProductStatus string
	VariantStatus string
	OnHand        int64
	Held          int64
	InTransit     int64
	Damaged       int64
	Available     int64
}

// ListBalances trả một trang số dư (biến thể đã có dòng số dư) của người bán f.SellerID ở kho f.WarehouseID, biến thể
// mới tạo trước, và con trỏ trang sau (nil khi hết). Chạy trong tenancy.WithTenant. Lỗi: ErrInvalidInput.
func ListBalances(ctx context.Context, tx pgx.Tx, f BalanceFilter) ([]BalanceRow, *BalanceCursor, error) {
	sql, args, err := balancesSQL(f)
	if err != nil {
		return nil, nil, err
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách tồn: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[BalanceRow])
	if err != nil {
		return nil, nil, fmt.Errorf("đọc danh sách tồn: %w", err)
	}
	var next *BalanceCursor
	if len(out) > f.Limit {
		out = out[:f.Limit]
		next = &BalanceCursor{VariantID: out[len(out)-1].VariantID}
	}
	return out, next, nil
}

// balancesSQL dựng câu của ListBalances (lấy Limit + 1 dòng để biết còn trang sau). Thứ tự variant_id giảm đi theo
// index stock_balances_warehouse (tenant_id, warehouse_id, variant_id) nên trang đầu và trang sau dừng sớm.
func balancesSQL(f BalanceFilter) (string, []any, error) {
	if f.Limit < 1 || f.Limit > MaxListLimit {
		return "", nil, fmt.Errorf("giới hạn %d: %w", f.Limit, ErrInvalidInput)
	}
	if f.WarehouseID == uuid.Nil || f.SellerID == uuid.Nil {
		return "", nil, fmt.Errorf("thiếu kho hoặc người bán: %w", ErrInvalidInput)
	}
	args := []any{f.SellerID, f.WarehouseID}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	var b strings.Builder
	b.WriteString(`SELECT a.variant_id, v.sku, v.barcode, v.product_code, v.product_name, v.color, v.size, v.product_status,
	v.variant_status, a.on_hand::bigint, a.held::bigint, a.in_transit::bigint, a.damaged::bigint, a.available::bigint
FROM inventory.stock_available_v AS a
JOIN catalog.variants_v AS v ON v.variant_id = a.variant_id AND v.seller_id = a.seller_id
WHERE a.seller_id = $1 AND a.warehouse_id = $2`)
	if f.OnlyShort {
		b.WriteString("\n\tAND a.available < 0")
	}
	if q := strings.TrimSpace(f.Q); q != "" || f.Q != "" {
		folded, err := foldQuery(f.Q)
		if err != nil {
			return "", nil, err
		}
		b.WriteString("\n\tAND a.variant_id IN (SELECT s.variant_id FROM catalog.search_variants($1, " + arg(folded) + ", " +
			arg(q) + ", " + strconv.Itoa(maxBalanceMatches) + ") AS s)")
	}
	if f.Cursor != nil {
		b.WriteString("\n\tAND a.variant_id < " + arg(f.Cursor.VariantID) + "::uuid")
	}
	b.WriteString("\nORDER BY a.variant_id DESC\nLIMIT " + strconv.Itoa(f.Limit+1))
	return b.String(), args, nil
}
