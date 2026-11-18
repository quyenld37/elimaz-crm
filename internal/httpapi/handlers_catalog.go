package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// CreateProduct (POST /api/v1/catalog/products, catalog.manage) tạo mẫu của người bán mặc định → 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateProduct(ctx context.Context, req *gen.CreateProductRequest, _ gen.CreateProductParams) (gen.CreateProductRes, error) {
	in := catalog.ProductInput{
		Code: string(req.Code), Name: req.Name, Description: req.Description.Value, Article: rawArticle(req.Article),
		Status: catalog.ProductStatus(req.Status.Value),
	}
	for _, v := range req.Variants {
		in.Variants = append(in.Variants, catalog.VariantInput{
			SKU: string(v.Sku), Barcode: string(v.Barcode.Value), Color: string(v.Color), ColorHex: string(v.ColorHex.Value),
			Size: string(v.Size), Price: int64(v.Price), CostPrice: optMoney(v.CostPrice),
		})
	}
	out, err := h.catalogWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		in.SellerID = seller
		p, err := w.CreateProduct(ctx, tx, in, u)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		return productOut(p)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Product), nil //nolint:forcetypeassert // fn trên trả *gen.Product
}

// GetProduct (GET /api/v1/catalog/products/{id}, catalog.read): mẫu của người bán mặc định, kèm articleHtml dựng bằng
// catalog.RenderArticleHTML (R7).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetProduct(ctx context.Context, params gen.GetProductParams) (gen.GetProductRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var p catalog.Product
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của catalog
		}
		p, err = catalog.GetProduct(ctx, tx, seller, params.ID)
		return err //nolint:wrapcheck // sentinel của catalog
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("đọc mẫu: %w", err)
	}
	return productOut(p)
}

// UpdateProduct (PATCH /api/v1/catalog/products/{id}, catalog.manage) sửa mẫu khi version còn khớp.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateProduct(ctx context.Context, req *gen.UpdateProductRequest, params gen.UpdateProductParams) (gen.UpdateProductRes, error) {
	patch := catalog.ProductPatch{}
	if req.Name.Set {
		patch.Name = &req.Name.Value
	}
	if req.Description.Set {
		patch.Description = &req.Description.Value
	}
	if req.Article != nil {
		a := rawArticle(req.Article)
		patch.Article = &a
	}
	if req.Status.Set {
		s := catalog.ProductStatus(req.Status.Value)
		patch.Status = &s
	}
	out, err := h.catalogWrite(ctx, http.StatusOK, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		p, err := w.UpdateProduct(ctx, tx, params.ID, req.Version, patch, u)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		return productOut(p)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Product), nil //nolint:forcetypeassert // fn trên trả *gen.Product
}

// AddVariant (POST /api/v1/catalog/products/{id}/variants, catalog.manage) thêm biến thể khi version mẫu còn khớp → 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AddVariant(ctx context.Context, req *gen.AddVariantRequest, params gen.AddVariantParams) (gen.AddVariantRes, error) {
	in := catalog.VariantInput{
		SKU: string(req.Sku), Barcode: string(req.Barcode.Value), Color: string(req.Color),
		ColorHex: string(req.ColorHex.Value), Size: string(req.Size), Price: int64(req.Price), CostPrice: optMoney(req.CostPrice),
	}
	out, err := h.catalogWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		v, err := w.AddVariant(ctx, tx, params.ID, req.ProductVersion, in, u)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		return variantOut(v), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Variant), nil //nolint:forcetypeassert // fn trên trả *gen.Variant
}

// UpdateVariant (PATCH /api/v1/catalog/variants/{id}, catalog.manage) sửa biến thể khi version còn khớp; costPrice
// null bỏ giá nhập.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateVariant(ctx context.Context, req *gen.UpdateVariantRequest, params gen.UpdateVariantParams) (gen.UpdateVariantRes, error) {
	patch := catalog.VariantPatch{
		SKU: optString(req.Sku.Set, string(req.Sku.Value)), Barcode: optString(req.Barcode.Set, string(req.Barcode.Value)),
		Color: optString(req.Color.Set, string(req.Color.Value)), ColorHex: optString(req.ColorHex.Set, string(req.ColorHex.Value)),
		Size: optString(req.Size.Set, string(req.Size.Value)),
	}
	if req.Price.Set {
		p := int64(req.Price.Value)
		patch.Price = &p
	}
	switch {
	case req.CostPrice.Set && req.CostPrice.Null:
		patch.ClearCostPrice = true
	case req.CostPrice.Set:
		c := req.CostPrice.Value
		patch.CostPrice = &c
	}
	if req.Status.Set {
		s := string(req.Status.Value)
		patch.Status = &s
	}
	out, err := h.catalogWrite(ctx, http.StatusOK, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		v, err := w.UpdateVariant(ctx, tx, params.ID, req.Version, patch, u)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		return variantOut(v), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Variant), nil //nolint:forcetypeassert // fn trên trả *gen.Variant
}

// catalogWrite chạy change trong WriteTx với một catalog.Writer của yêu cầu (sự kiện, báo hiệu, audit do catalog ghi
// trong cùng transaction). Lỗi nghiệp vụ của catalog là câu trả lời không đổi gì: tx lùi, không lưu vào khóa chống
// trùng (final = false), gửi lại cùng khóa thì xét lại; lỗi đổi sang HTTP ở errors.go.
func (h *handler) catalogWrite(ctx context.Context, status int,
	change func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error),
) (any, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	w := catalog.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime, RequestID: RequestID(ctx)}
	var out any
	var refused error
	err := WriteTx(ctx, h.d, u.TenantID, func(tx pgx.Tx) (int, any, bool, error) {
		out, refused = nil, nil // fn có thể chạy lại (ADR-46): gán lại mỗi lần
		body, err := change(tx, w, u)
		switch {
		case isCatalogRefusal(err):
			refused = err
			return 0, nil, false, nil
		case err != nil:
			return 0, nil, false, err
		}
		out = body
		return status, body, true, nil
	})
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, fmt.Errorf("ghi catalog: %w", refused)
	}
	return out, nil
}

func isCatalogRefusal(err error) bool {
	_, _, ok := catalogError(err)
	return ok
}

// rawArticle ghép các phần tử bài viết ogen đã đọc thành một mảng JSON (nil → rỗng: bài viết rỗng).
func rawArticle(items gen.ArticleInput) json.RawMessage {
	if items == nil {
		return nil
	}
	parts := make([][]byte, 0, len(items))
	for _, it := range items {
		parts = append(parts, []byte(it))
	}
	return json.RawMessage("[" + string(bytes.Join(parts, []byte(","))) + "]")
}

func optMoney(m gen.OptMoney) *int64 {
	if !m.Set {
		return nil
	}
	v := int64(m.Value)
	return &v
}

func optString(set bool, v string) *string {
	if !set {
		return nil
	}
	return &v
}

func productOut(p catalog.Product) (*gen.Product, error) {
	html, err := catalog.RenderArticleHTML(p.Article)
	if err != nil {
		return nil, fmt.Errorf("dựng HTML bài viết của mẫu %s: %w", p.ID, err)
	}
	blocks := make([]gen.ArticleBlock, 0, len(p.Article))
	for _, b := range p.Article {
		blocks = append(blocks, articleBlockOut(b))
	}
	variants := make([]gen.Variant, 0, len(p.Variants))
	for _, v := range p.Variants {
		variants = append(variants, *variantOut(v))
	}
	return &gen.Product{
		ID: p.ID, Code: p.Code, Name: p.Name, Description: p.Description, Article: blocks, ArticleHtml: html,
		Status: gen.ProductStatus(p.Status), Version: p.Version, Variants: variants,
	}, nil
}

func articleBlockOut(b catalog.ArticleBlock) gen.ArticleBlock {
	out := gen.ArticleBlock{Type: gen.ArticleBlockType(b.Type), Items: b.Items}
	if b.Text != "" {
		out.Text = gen.NewOptString(b.Text)
	}
	if b.Level != 0 {
		out.Level = gen.NewOptArticleBlockLevel(gen.ArticleBlockLevel(b.Level))
	}
	if b.FileID != nil {
		out.FileId = gen.NewOptUUID(*b.FileID)
	}
	if b.Caption != "" {
		out.Caption = gen.NewOptString(b.Caption)
	}
	return out
}

func variantOut(v catalog.Variant) *gen.Variant {
	out := &gen.Variant{
		ID: v.ID, ProductId: v.ProductID, Sku: v.SKU, Barcode: v.Barcode, Color: v.Color, Size: v.Size,
		Price: gen.Money(v.Price), Status: gen.VariantStatus(v.Status), Version: v.Version,
	}
	if v.ColorHex != "" {
		out.ColorHex = gen.NewOptString(v.ColorHex)
	}
	if v.CostPrice != nil {
		out.CostPrice = gen.NewOptMoney(gen.Money(*v.CostPrice))
	}
	return out
}
