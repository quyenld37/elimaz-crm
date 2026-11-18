package catalog

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/catalog/internal/barcode"
	"elimaz.com.vn/crm/internal/modules/catalog/internal/store"
	"elimaz.com.vn/crm/internal/modules/identity"
)

var (
	barcodePattern  = regexp.MustCompile(`^[0-9A-Za-z-]{4,32}$`)
	colorHexPattern = regexp.MustCompile(`^#[0-9A-F]{6}$`)
)

// barcodeAttempts: số lần sinh lại khi mã tự sinh trùng một barcode nhân viên đã gõ tay.
const barcodeAttempts = 5

// AddVariant thêm biến thể v vào mẫu productID khi version của mẫu còn khớp; version mẫu tăng 1 (Seq của
// product.updated). Lỗi: ErrNotFound, ErrVersionConflict, ErrInvalidInput, ErrInvalidPrice, ErrSKUTaken,
// ErrBarcodeTaken, ErrDuplicateOption.
func (w Writer) AddVariant(ctx context.Context, tx pgx.Tx, productID uuid.UUID, productVersion int64, v VariantInput,
	actor identity.AuthUser,
) (Variant, error) {
	if err := w.check(); err != nil {
		return Variant{}, err
	}
	nv, err := normalizeVariant(v)
	if err != nil {
		return Variant{}, err
	}
	p, err := lockProduct(ctx, tx, productID, productVersion)
	if err != nil {
		return Variant{}, err
	}
	created, err := insertVariant(ctx, tx, actor.TenantID, p.SellerID, productID, nv)
	if err != nil {
		return Variant{}, err
	}
	if err := w.bumpProduct(ctx, tx, actor.TenantID, p); err != nil {
		return Variant{}, err
	}
	if err := w.audit(ctx, tx, actor, "catalog.variant.create", "catalog.variant", created.ID, nil, variantAudit(created)); err != nil {
		return Variant{}, err
	}
	return created, nil
}

// UpdateVariant sửa biến thể id khi version còn khớp; version biến thể và version mẫu tăng 1. Khóa mẫu rồi biến thể
// (ADR-46 nhóm 4). Giá đổi → price.changed (Seq = version mới của biến thể); luôn product.updated. Audit ghi before,
// after (gồm price). Lỗi: ErrNotFound, ErrVersionConflict, ErrInvalidInput, ErrInvalidPrice, ErrSKUTaken,
// ErrBarcodeTaken, ErrDuplicateOption.
func (w Writer) UpdateVariant(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, patch VariantPatch,
	actor identity.AuthUser,
) (Variant, error) {
	if err := w.check(); err != nil {
		return Variant{}, err
	}
	q := store.New(tx)
	productID, err := q.GetVariantProductID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Variant{}, ErrNotFound
	}
	if err != nil {
		return Variant{}, fmt.Errorf("đọc mẫu của biến thể: %w", err)
	}
	p, err := lockProductAnyVersion(ctx, tx, productID)
	if err != nil {
		return Variant{}, err
	}
	row, err := q.LockVariant(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Variant{}, ErrNotFound
	}
	if err != nil {
		return Variant{}, fmt.Errorf("khóa biến thể: %w", err)
	}
	before := variantFromRow(store.ListProductVariantsRow(row))
	if before.Version != version {
		return Variant{}, ErrVersionConflict
	}
	after, err := applyVariantPatch(before, patch)
	if err != nil {
		return Variant{}, err
	}
	after.Version, err = q.UpdateVariant(ctx, store.UpdateVariantParams{
		ID: id, Version: version, Sku: after.SKU, Barcode: after.Barcode, Color: after.Color,
		ColorHex: optText(after.ColorHex), Size: after.Size, Price: after.Price, CostPrice: after.CostPrice,
		Status: after.Status,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Variant{}, ErrVersionConflict // không xảy ra khi đã giữ khóa dòng
	}
	if err != nil {
		return Variant{}, fmt.Errorf("sửa biến thể: %w", mapWriteError(err))
	}
	if err := w.bumpProduct(ctx, tx, actor.TenantID, p); err != nil {
		return Variant{}, err
	}
	if after.Price != before.Price {
		if err := w.priceChanged(ctx, tx, actor.TenantID, after); err != nil {
			return Variant{}, err
		}
	}
	if err := w.audit(ctx, tx, actor, "catalog.variant.update", "catalog.variant", id, variantAudit(before), variantAudit(after)); err != nil {
		return Variant{}, err
	}
	return after, nil
}

// VariantForSale trả biến thể variantID của người bán sellerID nếu bán được (mẫu và biến thể active, mẫu chưa xóa).
// Không có (hay của người bán, tenant khác) → ErrNotFound; có mà không bán được → ErrNotForSale (KH12).
func VariantForSale(ctx context.Context, tx pgx.Tx, sellerID, variantID uuid.UUID) (SaleVariant, error) {
	row, err := store.New(tx).GetSaleVariant(ctx, store.GetSaleVariantParams{SellerID: sellerID, VariantID: variantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return SaleVariant{}, ErrNotFound
	}
	if err != nil {
		return SaleVariant{}, fmt.Errorf("đọc biến thể để bán: %w", err)
	}
	if !row.Sellable.Bool {
		return SaleVariant{}, ErrNotForSale
	}
	return saleVariant(row.VariantID, row.SellerID, row.ProductID, row.Sku, row.ProductName, row.Color, row.Size, row.Price), nil
}

// VariantsForSale trả trong một câu các biến thể bán được của người bán sellerID trong ids (M2, M5); id không có,
// không bán được hay của người bán khác thì không có trong map.
func VariantsForSale(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]SaleVariant, error) {
	out := make(map[uuid.UUID]SaleVariant, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := store.New(tx).ListSaleVariants(ctx, store.ListSaleVariantsParams{SellerID: sellerID, VariantIds: ids})
	if err != nil {
		return nil, fmt.Errorf("đọc biến thể để bán: %w", err)
	}
	for _, r := range rows {
		out[r.VariantID] = saleVariant(r.VariantID, r.SellerID, r.ProductID, r.Sku, r.ProductName, r.Color, r.Size, r.Price)
	}
	return out, nil
}

func saleVariant(id, sellerID, productID uuid.UUID, sku, productName, color, size string, price int64) SaleVariant {
	return SaleVariant{
		ID: id, SellerID: sellerID, ProductID: productID, SKU: sku,
		Name: productName + " - " + color + " / " + size, Price: price,
	}
}

// bumpProduct tăng version của mẫu p đã khóa (biến thể của nó vừa đổi) và phát product.updated.
func (w Writer) bumpProduct(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, p Product) error {
	v, err := store.New(tx).BumpProductVersion(ctx, store.BumpProductVersionParams{ID: p.ID, Version: p.Version})
	if err != nil {
		return fmt.Errorf("tăng version mẫu: %w", err)
	}
	return w.productChanged(ctx, tx, tenantID, p.SellerID, p.ID, v)
}

// insertVariant ghi biến thể đã chuẩn hóa v; barcode rỗng → sinh EAN-13 (bỏ qua mã trùng barcode gõ tay).
func insertVariant(ctx context.Context, tx pgx.Tx, tenantID, sellerID, productID uuid.UUID, v VariantInput) (Variant, error) {
	q := store.New(tx)
	if v.Barcode == "" {
		code, err := freeBarcode(ctx, tx, q, sellerID)
		if err != nil {
			return Variant{}, err
		}
		v.Barcode = code
	}
	row, err := q.InsertVariant(ctx, store.InsertVariantParams{
		TenantID: tenantID, SellerID: sellerID, ProductID: productID, Sku: v.SKU, Barcode: v.Barcode, Color: v.Color,
		ColorHex: optText(v.ColorHex), Size: v.Size, Price: v.Price, CostPrice: v.CostPrice,
	})
	if err != nil {
		return Variant{}, fmt.Errorf("ghi biến thể: %w", mapWriteError(err))
	}
	return Variant{
		ID: row.ID, SellerID: sellerID, ProductID: productID, SKU: v.SKU, Barcode: v.Barcode, Color: v.Color,
		ColorHex: v.ColorHex, Size: v.Size, Price: v.Price, CostPrice: v.CostPrice, Status: row.Status, Version: row.Version,
	}, nil
}

func freeBarcode(ctx context.Context, tx pgx.Tx, q *store.Queries, sellerID uuid.UUID) (string, error) {
	for range barcodeAttempts {
		code, err := barcode.NextEAN13(ctx, tx)
		if err != nil {
			return "", err //nolint:wrapcheck // barcode đã bọc
		}
		taken, err := q.BarcodeExists(ctx, store.BarcodeExistsParams{SellerID: sellerID, Barcode: code})
		if err != nil {
			return "", fmt.Errorf("kiểm barcode tự sinh: %w", err)
		}
		if !taken {
			return code, nil
		}
	}
	return "", ErrBarcodeTaken
}

func listVariants(ctx context.Context, q *store.Queries, sellerID, productID uuid.UUID) ([]Variant, error) {
	rows, err := q.ListProductVariants(ctx, store.ListProductVariantsParams{SellerID: sellerID, ProductID: productID})
	if err != nil {
		return nil, fmt.Errorf("đọc biến thể của mẫu: %w", err)
	}
	out := make([]Variant, 0, len(rows))
	for _, r := range rows {
		out = append(out, variantFromRow(r))
	}
	return out, nil
}

func variantFromRow(r store.ListProductVariantsRow) Variant {
	v := Variant{
		ID: r.ID, SellerID: r.SellerID, ProductID: r.ProductID, SKU: r.Sku, Barcode: r.Barcode, Color: r.Color,
		ColorHex: r.ColorHex.String, Size: r.Size, Price: r.Price, CostPrice: r.CostPrice, Status: r.Status,
		Version: r.Version,
	}
	return v
}

// normalizeVariant kiểm và chuẩn hóa v: SKU viết hoa, màu hex viết hoa, cắt khoảng trắng màu, size, barcode.
func normalizeVariant(v VariantInput) (VariantInput, error) {
	sku, ok := normalizeCode(v.SKU)
	out := VariantInput{
		SKU: sku, Barcode: strings.TrimSpace(v.Barcode), Color: strings.TrimSpace(v.Color),
		ColorHex: strings.ToUpper(strings.TrimSpace(v.ColorHex)), Size: strings.TrimSpace(v.Size), Price: v.Price,
		CostPrice: v.CostPrice,
	}
	switch {
	case !ok, out.Barcode != "" && !barcodePattern.MatchString(out.Barcode),
		out.ColorHex != "" && !colorHexPattern.MatchString(out.ColorHex),
		!validLine(out.Color, maxOption), !validLine(out.Size, maxOption):
		return VariantInput{}, ErrInvalidInput
	case !validMoney(out.Price), out.CostPrice != nil && !validMoney(*out.CostPrice):
		return VariantInput{}, ErrInvalidPrice
	}
	return out, nil
}

func applyVariantPatch(cur Variant, p VariantPatch) (Variant, error) {
	in := VariantInput{
		SKU: cur.SKU, Barcode: cur.Barcode, Color: cur.Color, ColorHex: cur.ColorHex, Size: cur.Size,
		Price: cur.Price, CostPrice: cur.CostPrice,
	}
	for dst, src := range map[*string]*string{
		&in.SKU: p.SKU, &in.Barcode: p.Barcode, &in.Color: p.Color,
		&in.ColorHex: p.ColorHex, &in.Size: p.Size,
	} {
		if src != nil {
			*dst = *src
		}
	}
	if p.Price != nil {
		in.Price = *p.Price
	}
	if p.CostPrice != nil {
		in.CostPrice = p.CostPrice
	}
	if p.ClearCostPrice {
		in.CostPrice = nil
	}
	if p.Barcode != nil && strings.TrimSpace(*p.Barcode) == "" {
		return Variant{}, ErrInvalidInput // barcode chỉ tự sinh lúc tạo
	}
	nv, err := normalizeVariant(in)
	if err != nil {
		return Variant{}, err
	}
	status := cur.Status
	if p.Status != nil {
		status = *p.Status
	}
	if status != VariantActive && status != VariantDiscontinued {
		return Variant{}, ErrInvalidInput
	}
	return Variant{
		ID: cur.ID, SellerID: cur.SellerID, ProductID: cur.ProductID, SKU: nv.SKU, Barcode: nv.Barcode,
		Color: nv.Color, ColorHex: nv.ColorHex, Size: nv.Size, Price: nv.Price, CostPrice: nv.CostPrice, Status: status,
		Version: cur.Version,
	}, nil
}

func validMoney(x int64) bool { return x >= 0 && x <= MaxMoney }

func optText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }
