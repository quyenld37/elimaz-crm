package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/queries"
)

// Mặc định khi không gửi limit: mockup danh sách tải 50 mẫu một lần; ô tìm biến thể hiện 20 dòng.
const (
	defaultProductPage = 50
	defaultVariantHits = 20
)

// ListProducts (GET /api/v1/catalog/products, catalog.read): một trang mẫu của người bán mặc định. warehouseId cần
// thêm inventory.read ở kho đó (guard chỉ kiểm catalog.read).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListProducts(ctx context.Context, params gen.ListProductsParams) (gen.ListProductsRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	f, err := productFilter(u, params)
	if err != nil {
		return nil, err
	}
	var rows []queries.ProductRow
	var next *queries.Cursor
	var covers map[uuid.UUID]catalog.Image
	err = h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		f.SellerID = seller
		var err error
		rows, next, err = queries.ListProducts(ctx, tx, f)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của queries
		}
		covers, err = h.productCovers(ctx, tx, rows)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách mẫu: %w", err)
	}
	return productPageOut(rows, next, covers), nil
}

// productCovers lấy biến thể ảnh bìa của cả trang bằng một lời gọi catalog.ResolveImages (một câu PublicVariants), theo
// mã mẫu.
func (h *handler) productCovers(ctx context.Context, tx pgx.Tx, rows []queries.ProductRow) (map[uuid.UUID]catalog.Image, error) {
	var imgs []catalog.Image
	var owners []uuid.UUID
	for _, r := range rows {
		if r.CoverImageID != nil && r.CoverFileID != nil {
			imgs = append(imgs, catalog.Image{ID: *r.CoverImageID, FileID: *r.CoverFileID})
			owners = append(owners, r.ProductID)
		}
	}
	if err := catalog.ResolveImages(ctx, tx, imgs, h.imageView()); err != nil {
		return nil, err //nolint:wrapcheck // lỗi của catalog đã bọc
	}
	out := make(map[uuid.UUID]catalog.Image, len(imgs))
	for i, img := range imgs {
		out[owners[i]] = img
	}
	return out, nil
}

// productFilter đổi tham số của listProducts thành bộ lọc (chưa có người bán). warehouseId cần inventory.read ở kho đó
// (thiếu → errForbiddenTarget); con trỏ hỏng → queries.ErrInvalidInput.
func productFilter(u identity.AuthUser, params gen.ListProductsParams) (queries.ProductFilter, error) {
	f := queries.ProductFilter{Q: params.Q.Value, Limit: int(params.Limit.Or(defaultProductPage))}
	for _, s := range params.Status {
		f.Status = append(f.Status, string(s))
	}
	if params.CategoryId.Set {
		f.CategoryID = &params.CategoryId.Value
	}
	if params.TagId.Set {
		f.TagID = &params.TagId.Value
	}
	if params.WarehouseId.Set {
		if err := canAt(u, params.WarehouseId.Value, permInvRead); err != nil {
			return f, err
		}
		f.WarehouseID = params.WarehouseId.Value
	}
	if params.Cursor.Set {
		c, err := queries.ParseCursor(params.Cursor.Value)
		if err != nil {
			return f, fmt.Errorf("đọc tham số cursor: %w", err)
		}
		f.Cursor = &c
	}
	return f, nil
}

func productPageOut(rows []queries.ProductRow, next *queries.Cursor, covers map[uuid.UUID]catalog.Image) *gen.ProductPage {
	page := &gen.ProductPage{Items: make([]gen.ProductListItem, 0, len(rows))}
	for _, r := range rows {
		it := gen.ProductListItem{
			ID: r.ProductID, Code: r.Code, Name: r.Name, Status: gen.ProductStatus(r.Status), CategoryPath: r.CategoryPath,
			VariantCount: int32(r.VariantCount), UpdatedAt: r.UpdatedAt, //nolint:gosec // số biến thể của một mẫu nhỏ
		}
		if it.CategoryPath == nil {
			it.CategoryPath = []string{}
		}
		if r.CategoryID != nil {
			it.CategoryId = gen.NewOptUUID(*r.CategoryID)
		}
		if r.VariantCount > 0 {
			it.MinPrice, it.MaxPrice = gen.NewOptMoney(gen.Money(r.MinPrice)), gen.NewOptMoney(gen.Money(r.MaxPrice))
		}
		if r.Available != nil {
			it.Available = gen.NewOptInt64(*r.Available)
		}
		if c, ok := covers[r.ProductID]; ok {
			it.Cover = gen.NewOptProductCover(gen.ProductCover{
				ImageId: c.ID, Status: gen.ProductCoverStatus(c.Status), Variants: imageVariantsOut(c.Variants),
			})
		}
		page.Items = append(page.Items, it)
	}
	if next != nil {
		page.NextCursor = gen.NewOptString(next.String())
	}
	return page
}

// SearchVariants (GET /api/v1/catalog/variants/search, catalog.read): biến thể của người bán mặc định khớp q.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SearchVariants(ctx context.Context, params gen.SearchVariantsParams) (gen.SearchVariantsRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var hits []queries.VariantHit
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		hits, err = queries.SearchVariants(ctx, tx, seller, params.Q, int(params.Limit.Or(defaultVariantHits)))
		return err //nolint:wrapcheck // sentinel của queries
	})
	if err != nil {
		return nil, fmt.Errorf("tìm biến thể: %w", err)
	}
	out := make(gen.SearchVariantsOKApplicationJSON, 0, len(hits))
	for _, v := range hits {
		out = append(out, gen.VariantSearchHit{
			VariantId: v.VariantID, ProductId: v.ProductID, Sku: v.SKU, Barcode: v.Barcode, ProductCode: v.ProductCode,
			ProductName: v.ProductName, Color: v.Color, Size: v.Size, Price: gen.Money(v.Price), Sellable: v.Sellable,
		})
	}
	return &out, nil
}

// queriesError đổi lỗi của gói queries (danh sách, tìm) sang mã HTTP.
func queriesError(err error) (status int, code string, ok bool) {
	if errors.Is(err, queries.ErrInvalidInput) {
		return http.StatusBadRequest, codeInvalidInput, true
	}
	return 0, "", false
}

// ListBalances (GET /api/v1/inventory/balances, inventory.read ở đúng kho warehouseId): một trang số dư của người
// bán mặc định ở kho đó.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListBalances(ctx context.Context, params gen.ListBalancesParams) (gen.ListBalancesRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	if err := canAt(u, params.WarehouseId, permInvRead); err != nil {
		return nil, err
	}
	f := queries.BalanceFilter{
		WarehouseID: params.WarehouseId, Q: params.Q.Value, OnlyShort: params.OnlyShort.Or(false),
		Limit: int(params.Limit.Or(defaultProductPage)),
	}
	if params.Cursor.Set {
		c, err := queries.ParseBalanceCursor(params.Cursor.Value)
		if err != nil {
			return nil, fmt.Errorf("đọc tham số cursor: %w", err)
		}
		f.Cursor = &c
	}
	var rows []queries.BalanceRow
	var next *queries.BalanceCursor
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		f.SellerID = seller
		var err error
		rows, next, err = queries.ListBalances(ctx, tx, f)
		return err //nolint:wrapcheck // sentinel của queries
	})
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách tồn: %w", err)
	}
	page := &gen.BalancePage{Items: make([]gen.Balance, 0, len(rows))}
	for _, r := range rows {
		page.Items = append(page.Items, gen.Balance{
			VariantId: r.VariantID, Sku: r.SKU, Barcode: r.Barcode, ProductCode: r.ProductCode, ProductName: r.ProductName,
			Color: r.Color, Size: r.Size, ProductStatus: gen.ProductStatus(r.ProductStatus),
			VariantStatus: gen.VariantStatus(r.VariantStatus), OnHand: r.OnHand, Held: r.Held, InTransit: r.InTransit,
			Damaged: r.Damaged, Available: r.Available,
		})
	}
	if next != nil {
		page.NextCursor = gen.NewOptString(next.String())
	}
	return page, nil
}
