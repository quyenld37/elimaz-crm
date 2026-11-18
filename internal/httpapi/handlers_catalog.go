package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
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
	out, err := h.catalogWriteAsDefaultSeller(ctx, http.StatusCreated, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser, seller uuid.UUID) (any, error) {
		in.SellerID = seller
		p, err := w.CreateProduct(ctx, tx, in, u)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		return h.productWithImages(ctx, tx, p.SellerID, p.ID) // gắn ảnh trong bài viết (Task M1-44b)
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Product), nil //nolint:forcetypeassert // fn trên trả *gen.Product
}

// GetProduct (GET /api/v1/catalog/products/{id}, catalog.read): mẫu của người bán mặc định, kèm articleHtml (bài viết
// đã gắn ảnh bằng catalog.ResolveArticleImages rồi dựng bằng catalog.RenderArticleHTML — đúng các bước của
// catalog.ArticleHTML, R7, Task M1-44b) và ảnh đang hiện (Task M1-44).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetProduct(ctx context.Context, params gen.GetProductParams) (gen.GetProductRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var out *gen.Product
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		out, err = h.productWithImages(ctx, tx, seller, params.ID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("đọc mẫu: %w", err)
	}
	return out, nil
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
	out, err := h.catalogWriteAsDefaultSeller(ctx, http.StatusOK, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser, seller uuid.UUID) (any, error) {
		p, err := w.UpdateProduct(ctx, tx, seller, params.ID, req.Version, patch, u)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		return h.productWithImages(ctx, tx, p.SellerID, p.ID)
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
	out, err := h.catalogWriteAsDefaultSeller(ctx, http.StatusCreated, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser, seller uuid.UUID) (any, error) {
		v, err := w.AddVariant(ctx, tx, seller, params.ID, req.ProductVersion, in, u)
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
	out, err := h.catalogWriteAsDefaultSeller(ctx, http.StatusOK, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser, seller uuid.UUID) (any, error) {
		v, err := w.UpdateVariant(ctx, tx, seller, params.ID, req.Version, patch, u)
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

// catalogWrite chạy change trong refusableWrite với một catalog.Writer của yêu cầu (sự kiện, báo hiệu, audit do catalog
// ghi trong cùng transaction). Lỗi nghiệp vụ của catalog là câu trả lời không đổi gì (không lưu vào khóa chống trùng).
func (h *handler) catalogWrite(ctx context.Context, status int,
	change func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error),
) (any, error) {
	w := catalog.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime, RequestID: RequestID(ctx)}
	return h.refusableWrite(ctx, status, isCatalogRefusal, "ghi catalog", func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		return change(tx, w, u)
	})
}

// catalogWriteAsDefaultSeller như catalogWrite, truyền thêm người bán mặc định của shop (API chỉ làm việc với người
// bán này, ADR-29): mẫu, biến thể của người bán khác → catalog.ErrNotFound (404).
func (h *handler) catalogWriteAsDefaultSeller(ctx context.Context, status int,
	change func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser, seller uuid.UUID) (any, error),
) (any, error) {
	return h.catalogWrite(ctx, status, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		return change(tx, w, u, seller)
	})
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

// productOut đổi mẫu sang kiểu API; khối ảnh của p.Article phải đã qua catalog.ResolveArticleImages (không thì bị bỏ
// khỏi articleHtml).
func productOut(p catalog.Product, imgs []catalog.Image) (*gen.Product, error) {
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
		Status: gen.ProductStatus(p.Status), Version: p.Version, Variants: variants, Images: productImagesOut(imgs),
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

// ListCategories (GET /api/v1/catalog/categories, catalog.read): mọi danh mục của shop.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCategories(ctx context.Context) (gen.ListCategoriesRes, error) {
	var cs []catalog.Category
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		cs, err = catalog.ListCategories(ctx, tx)
		return err //nolint:wrapcheck // catalog đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc danh mục: %w", err)
	}
	out := make(gen.ListCategoriesOKApplicationJSON, 0, len(cs))
	for _, c := range cs {
		out = append(out, *categoryOut(c))
	}
	return &out, nil
}

// CreateCategory (POST /api/v1/catalog/categories, catalog.manage) → 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateCategory(ctx context.Context, req *gen.CreateCategoryRequest, _ gen.CreateCategoryParams) (gen.CreateCategoryRes, error) {
	in := catalog.CategoryInput{Name: string(req.Name), Slug: string(req.Slug.Value), Position: int32(req.Position.Value)}
	if req.ParentId.Set {
		in.ParentID = &req.ParentId.Value
	}
	out, err := h.catalogWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		c, err := w.CreateCategory(ctx, tx, in, u)
		return categoryOut(c), err
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Category), nil //nolint:forcetypeassert // fn trên trả *gen.Category
}

// UpdateCategory (PATCH /api/v1/catalog/categories/{id}, catalog.manage): có trường parentId là đổi cha (null → gốc).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) UpdateCategory(ctx context.Context, req *gen.UpdateCategoryRequest, params gen.UpdateCategoryParams) (gen.UpdateCategoryRes, error) {
	patch := catalog.CategoryPatch{
		Name: optString(req.Name.Set, string(req.Name.Value)), Slug: optString(req.Slug.Set, string(req.Slug.Value)),
		Move: req.ParentId.Set,
	}
	if req.Position.Set {
		p := int32(req.Position.Value)
		patch.Position = &p
	}
	if req.ParentId.Set && !req.ParentId.Null {
		patch.ParentID = &req.ParentId.Value
	}
	out, err := h.catalogWrite(ctx, http.StatusOK, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		c, err := w.UpdateCategory(ctx, tx, params.ID, req.Version, patch, u)
		return categoryOut(c), err
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Category), nil //nolint:forcetypeassert // fn trên trả *gen.Category
}

// ArchiveCategory (POST /api/v1/catalog/categories/{id}/archive, catalog.manage).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ArchiveCategory(ctx context.Context, req *gen.VersionRequest, params gen.ArchiveCategoryParams) (gen.ArchiveCategoryRes, error) {
	out, err := h.catalogWrite(ctx, http.StatusOK, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		c, err := w.ArchiveCategory(ctx, tx, params.ID, req.Version, u)
		return categoryOut(c), err
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Category), nil //nolint:forcetypeassert // fn trên trả *gen.Category
}

// ListAttributes (GET /api/v1/catalog/attributes, catalog.read): thuộc tính kèm giá trị.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListAttributes(ctx context.Context) (gen.ListAttributesRes, error) {
	var as []catalog.Attribute
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		as, err = catalog.ListAttributes(ctx, tx)
		return err //nolint:wrapcheck // catalog đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc thuộc tính: %w", err)
	}
	out := make(gen.ListAttributesOKApplicationJSON, 0, len(as))
	for _, a := range as {
		out = append(out, *attributeOut(a))
	}
	return &out, nil
}

// CreateAttribute (POST /api/v1/catalog/attributes, catalog.manage) → 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateAttribute(ctx context.Context, req *gen.CreateAttributeRequest, _ gen.CreateAttributeParams) (gen.CreateAttributeRes, error) {
	in := catalog.AttributeInput{Code: string(req.Code), Name: string(req.Name), Position: int32(req.Position.Value)}
	out, err := h.catalogWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		a, err := w.CreateAttribute(ctx, tx, in, u)
		return attributeOut(a), err
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Attribute), nil //nolint:forcetypeassert // fn trên trả *gen.Attribute
}

// AddAttributeValue (POST /api/v1/catalog/attributes/{id}/values, catalog.manage) → 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AddAttributeValue(ctx context.Context, req *gen.AddAttributeValueRequest, params gen.AddAttributeValueParams) (gen.AddAttributeValueRes, error) {
	in := catalog.AttributeValueInput{Value: string(req.Value), Position: int32(req.Position.Value)}
	out, err := h.catalogWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		v, err := w.AddAttributeValue(ctx, tx, params.ID, in, u)
		return attributeValueOut(v), err
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.AttributeValue), nil //nolint:forcetypeassert // fn trên trả *gen.AttributeValue
}

// ListTags (GET /api/v1/catalog/tags, catalog.read).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListTags(ctx context.Context) (gen.ListTagsRes, error) {
	var ts []catalog.Tag
	if err := h.catalogRead(ctx, func(tx pgx.Tx) (err error) {
		ts, err = catalog.ListTags(ctx, tx)
		return err //nolint:wrapcheck // catalog đã bọc
	}); err != nil {
		return nil, fmt.Errorf("đọc nhãn: %w", err)
	}
	out := make(gen.ListTagsOKApplicationJSON, 0, len(ts))
	for _, t := range ts {
		out = append(out, gen.Tag{ID: t.ID, Code: t.Code, Name: t.Name})
	}
	return &out, nil
}

// CreateTag (POST /api/v1/catalog/tags, catalog.manage) → 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateTag(ctx context.Context, req *gen.CreateTagRequest, _ gen.CreateTagParams) (gen.CreateTagRes, error) {
	in := catalog.TagInput{Code: string(req.Code), Name: string(req.Name)}
	out, err := h.catalogWrite(ctx, http.StatusCreated, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser) (any, error) {
		t, err := w.CreateTag(ctx, tx, in, u)
		return &gen.Tag{ID: t.ID, Code: t.Code, Name: t.Name}, err //nolint:wrapcheck // sentinel của catalog
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Tag), nil //nolint:forcetypeassert // fn trên trả *gen.Tag
}

// GetProductClassification (GET /api/v1/catalog/products/{id}/classification, catalog.read) trả danh mục, giá trị thuộc
// tính, nhãn đang gán và version hiện tại của mẫu (của người bán mặc định).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetProductClassification(ctx context.Context, params gen.GetProductClassificationParams) (gen.GetProductClassificationRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var pc catalog.ProductClassification
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		pc, err = catalog.GetClassification(ctx, tx, seller, params.ID)
		return err //nolint:wrapcheck // sentinel của catalog
	})
	if err != nil {
		return nil, fmt.Errorf("đọc phân loại của mẫu: %w", err)
	}
	return classificationOut(pc), nil
}

// SetProductClassification (PUT /api/v1/catalog/products/{id}/classification, catalog.manage) thay toàn bộ phân loại
// của mẫu khi version còn khớp.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetProductClassification(ctx context.Context, req *gen.SetClassificationRequest, params gen.SetProductClassificationParams) (gen.SetProductClassificationRes, error) {
	c := catalog.Classification{AttributeValueIDs: req.AttributeValueIds, TagIDs: req.TagIds}
	if req.CategoryId.Set && !req.CategoryId.Null {
		c.CategoryID = &req.CategoryId.Value
	}
	out, err := h.catalogWriteAsDefaultSeller(ctx, http.StatusOK, func(tx pgx.Tx, w catalog.Writer, u identity.AuthUser, seller uuid.UUID) (any, error) {
		pc, err := w.SetClassification(ctx, tx, seller, params.ID, req.Version, c, u)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		return classificationOut(pc), nil
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ProductClassification), nil //nolint:forcetypeassert // fn trên trả *gen.ProductClassification
}

// classificationOut đổi phân loại của mẫu sang kiểu của hợp đồng.
func classificationOut(pc catalog.ProductClassification) *gen.ProductClassification {
	res := &gen.ProductClassification{
		ProductId: pc.ProductID, Version: pc.Version, AttributeValueIds: pc.AttributeValueIDs, TagIds: pc.TagIDs,
	}
	if pc.CategoryID != nil {
		res.CategoryId = gen.NewOptUUID(*pc.CategoryID)
	}
	return res
}

// catalogRead chạy fn trong tenancy.WithTenant của người gọi: đọc dữ liệu dùng chung cả cửa hàng, không theo người bán
// hay kho (phân loại; cài đặt đơn, nguồn đơn — Task M2-15).
func (h *handler) catalogRead(ctx context.Context, fn func(tx pgx.Tx) error) error {
	u, ok := authUser(ctx)
	if !ok {
		return errNoAuthUser
	}
	return tenancy.WithTenant(ctx, h.d.DB, u.TenantID, fn, db.TxAPI) //nolint:wrapcheck // bên gọi bọc
}

func categoryOut(c catalog.Category) *gen.Category {
	out := &gen.Category{
		ID: c.ID, Name: c.Name, Slug: c.Slug, Depth: c.Depth, Position: gen.Position(c.Position),
		Status: gen.CategoryStatus(c.Status), Version: c.Version,
	}
	if c.ParentID != nil {
		out.ParentId = gen.NewOptUUID(*c.ParentID)
	}
	return out
}

func attributeOut(a catalog.Attribute) *gen.Attribute {
	values := make([]gen.AttributeValue, 0, len(a.Values))
	for _, v := range a.Values {
		values = append(values, *attributeValueOut(v))
	}
	return &gen.Attribute{ID: a.ID, Code: a.Code, Name: a.Name, Position: gen.Position(a.Position), Values: values}
}

func attributeValueOut(v catalog.AttributeValue) *gen.AttributeValue {
	return &gen.AttributeValue{ID: v.ID, AttributeId: v.AttributeID, Value: v.Value, Position: gen.Position(v.Position)}
}

// readAsDefaultSeller chạy fn trong một transaction đọc của tenant người gọi với người bán mặc định của shop (đường đọc
// của màn hình: giao diện không hiện khái niệm người bán, ADR-29).
func (h *handler) readAsDefaultSeller(ctx context.Context, u identity.AuthUser, fn func(tx pgx.Tx, seller uuid.UUID) error) error {
	return tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error { //nolint:wrapcheck // lỗi của fn trả nguyên
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của catalog
		}
		return fn(tx, seller)
	}, db.TxAPI)
}
