// Package web là lớp HTTP mỏng của các route /api/v1/web/* (kế hoạch M5): đọc tham số đã kiểm của ogen, đọc qua bộ
// đệm cache.ReadCache (tải bằng PostgreSQL trong tenancy.WithTenant khi thiếu), dựng câu trả lời của hợp đồng. Không
// có nghiệp vụ; lỗi trả lên cho internal/httpapi đổi mã (errors_web.go, errors.go).
//
// Khi nào không dùng: route của nhân viên (internal/httpapi); không gọi Valkey trong transaction (load mở transaction
// bên trong lời gọi bộ đệm, không bao giờ ngược lại).
package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries/webcatalog"
)

// Hạn sống trong bộ đệm (kế hoạch M5 Task 190, QĐ14): sản phẩm và trang 60 s, phân loại 300 s; sự kiện sản phẩm xóa
// sớm hơn (webcache.Invalidator).
const (
	productTTL  = 60 * time.Second
	pageTTL     = 60 * time.Second
	taxonomyTTL = 300 * time.Second
	taxonomyKey = "all"
	// absent là giá trị đệm của mã mẫu không có (đã xóa, người bán ngừng): website gỡ khỏi bản sao.
	absent = "null"
)

// ErrBadQuery báo tham số đọc sai: gửi cả cursor lẫn ids, hay cursor không phải chuỗi của nextCursor.
var ErrBadQuery = errors.New("web: tham số đọc không hợp lệ")

// Catalog phục vụ webCatalogProducts, webCatalogChanges, webCatalogTaxonomy.
type Catalog struct {
	db   *db.DB
	rc   cache.ReadCache
	view catalog.ImageView
	log  *slog.Logger
}

// NewCatalog dựng Catalog đọc d qua rc; view là FILES_PUBLIC_BASE_URL và IMAGE_PRESET_VERSION (cùng cho ảnh sản phẩm
// và ảnh trong bài viết). log nil thì slog.Default.
func NewCatalog(d *db.DB, rc cache.ReadCache, view catalog.ImageView, log *slog.Logger) *Catalog {
	if log == nil {
		log = slog.Default()
	}
	return &Catalog{db: d, rc: rc, view: view, log: log}
}

// Products trả trang mẫu (cursor, limit) hoặc các mẫu theo ids, có ETag (sha256 của thân); If-None-Match khớp → 304.
// Theo ids: mỗi mẫu một khóa SpaceProduct (các mẫu thiếu trong bộ đệm tải chung một transaction); theo trang: một khóa
// SpaceProductsPage.
//
//nolint:ireturn // kiểu trả của operation do ogen sinh (gen.Handler)
func (c *Catalog) Products(ctx context.Context, tenant uuid.UUID, p gen.WebCatalogProductsParams) (gen.WebCatalogProductsRes, error) {
	var (
		page gen.WebProductPage
		err  error
	)
	if len(p.Ids) > 0 {
		if p.Cursor.Set {
			return nil, ErrBadQuery
		}
		page, err = c.byIDs(ctx, tenant, p.Ids)
	} else {
		page, err = c.page(ctx, tenant, p)
	}
	if err != nil {
		return nil, err
	}
	body, err := page.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("mã hóa trang mẫu: %w", err)
	}
	tag := etag(body)
	if matches(p.IfNoneMatch, tag) {
		return &gen.WebCatalogProductsNotModified{}, nil
	}
	return &gen.WebProductPageHeaders{ETag: gen.NewOptString(tag), Response: page}, nil
}

// page đọc một trang qua khóa p_<after>_<limit> của SpaceProductsPage.
func (c *Catalog) page(ctx context.Context, tenant uuid.UUID, p gen.WebCatalogProductsParams) (gen.WebProductPage, error) {
	after := uuid.Nil
	if s, ok := p.Cursor.Get(); ok {
		id, err := uuid.Parse(s)
		if err != nil || id == uuid.Nil {
			return gen.WebProductPage{}, ErrBadQuery
		}
		after = id
	}
	limit := int(p.Limit.Or(webcatalog.MaxPage))
	key := "p_" + strings.ReplaceAll(after.String(), "-", "") + "_" + strconv.Itoa(limit)
	raw, _, err := c.rc.GetOrLoad(ctx, tenant, cache.SpaceProductsPage, key, pageTTL, func(ctx context.Context) ([]byte, error) {
		var out gen.WebProductPage
		err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
			ps, next, err := webcatalog.Products(ctx, tx, after, limit, c.imageView())
			if err != nil {
				return err //nolint:wrapcheck // webcatalog đã bọc
			}
			out = gen.WebProductPage{Items: make([]gen.WebProduct, 0, len(ps))}
			for _, wp := range ps {
				out.Items = append(out.Items, c.product(ctx, tx, wp))
			}
			if next != uuid.Nil {
				out.NextCursor = gen.NewOptString(next.String())
			}
			return nil
		}, db.TxAPI)
		if err != nil {
			return nil, fmt.Errorf("đọc trang mẫu cho website: %w", err)
		}
		return out.MarshalJSON()
	})
	if err != nil {
		return gen.WebProductPage{}, err //nolint:wrapcheck // ReadCache đã bọc; ErrBusy giữ nguyên để đổi 503
	}
	var out gen.WebProductPage
	if err := out.UnmarshalJSON(raw); err != nil {
		return gen.WebProductPage{}, fmt.Errorf("đọc trang mẫu trong bộ đệm: %w", err)
	}
	return out, nil
}

// byIDs đọc từng mã (bỏ trùng, giữ thứ tự gửi) qua SpaceProduct; lần thiếu đầu tiên tải mọi mã còn lại trong một
// transaction, các lần thiếu sau lấy từ bản vừa tải (một lần đọc PostgreSQL cho mọi mã thiếu của yêu cầu).
func (c *Catalog) byIDs(ctx context.Context, tenant uuid.UUID, ids []uuid.UUID) (gen.WebProductPage, error) {
	ids = dedupe(ids)
	var loaded map[uuid.UUID][]byte
	out := gen.WebProductPage{Items: make([]gen.WebProduct, 0, len(ids))}
	for i, id := range ids {
		raw, _, err := c.rc.GetOrLoad(ctx, tenant, cache.SpaceProduct, id.String(), productTTL, func(ctx context.Context) ([]byte, error) {
			if loaded == nil {
				m, err := c.loadProducts(ctx, tenant, ids[i:])
				if err != nil {
					return nil, err
				}
				loaded = m
			}
			if b, ok := loaded[id]; ok {
				return b, nil
			}
			return []byte(absent), nil
		})
		if err != nil {
			return gen.WebProductPage{}, err //nolint:wrapcheck // như page
		}
		if string(raw) == absent {
			continue
		}
		var wp gen.WebProduct
		if err := wp.UnmarshalJSON(raw); err != nil {
			return gen.WebProductPage{}, fmt.Errorf("đọc mẫu trong bộ đệm: %w", err)
		}
		out.Items = append(out.Items, wp)
	}
	return out, nil
}

// loadProducts đọc ids trong một transaction và trả JSON của từng mẫu có (mã không có thì không có trong map).
func (c *Catalog) loadProducts(ctx context.Context, tenant uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]byte, error) {
	m := make(map[uuid.UUID][]byte, len(ids))
	err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
		clear(m) // RetryTx có thể chạy lại thân hàm
		ps, err := webcatalog.ProductsByID(ctx, tx, ids, c.imageView())
		if err != nil {
			return err //nolint:wrapcheck // webcatalog đã bọc
		}
		for _, wp := range ps {
			g := c.product(ctx, tx, wp)
			b, err := g.MarshalJSON()
			if err != nil {
				return fmt.Errorf("mã hóa mẫu: %w", err)
			}
			m[wp.ID] = b
		}
		return nil
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("đọc mẫu theo mã cho website: %w", err)
	}
	return m, nil
}

// product dựng mẫu của hợp đồng, kèm HTML bài viết bằng catalog.ArticleHTML trong tx đọc (R7, R12). Bài viết lỗi →
// articleHtml rỗng, log cảnh báo chỉ có mã mẫu.
func (c *Catalog) product(ctx context.Context, tx pgx.Tx, wp webcatalog.WebProduct) gen.WebProduct {
	html, err := catalog.ArticleHTML(ctx, tx, wp.Article, c.view)
	if err != nil {
		html = ""
		c.log.LogAttrs(ctx, slog.LevelWarn, "web: dựng bài viết lỗi, trả articleHtml rỗng",
			slog.String("product_id", wp.ID.String()))
	}
	g := gen.WebProduct{
		ID: wp.ID, SellerId: wp.SellerID, Code: wp.Code, Name: wp.Name, ShortDescription: wp.ShortDescription,
		ArticleHtml: html, Status: gen.ProductStatus(wp.Status), CategoryIds: wp.CategoryIDs, Tags: wp.Tags,
		Attributes: make([]gen.WebAttr, 0, len(wp.Attributes)), Images: make([]gen.WebImage, 0, len(wp.Images)),
		Variants: make([]gen.WebVariant, 0, len(wp.Variants)), UpdatedAt: wp.UpdatedAt, Version: wp.Version,
	}
	for _, a := range wp.Attributes {
		g.Attributes = append(g.Attributes, gen.WebAttr{Name: a.Name, Value: a.Value, Position: int32(a.Position)}) //nolint:gosec // G115: vị trí nhỏ
	}
	for _, im := range wp.Images {
		gi := gen.WebImage{Position: int32(im.Position), Variants: make([]gen.WebImageVariant, 0, len(im.Variants))} //nolint:gosec // G115: vị trí nhỏ
		if im.Color != nil {
			gi.Color = gen.NewOptString(*im.Color)
		}
		for _, v := range im.Variants {
			gi.Variants = append(gi.Variants, gen.WebImageVariant{Width: int32(v.Width), Format: v.Format, URL: v.URL}) //nolint:gosec // G115: chiều rộng ≤ 10.000
		}
		g.Images = append(g.Images, gi)
	}
	for _, v := range wp.Variants {
		gv := gen.WebVariant{
			ID: v.ID, Sku: v.SKU, Barcode: v.Barcode, Color: v.Color, Size: v.Size, Price: gen.Money(v.Price),
			Status: gen.VariantStatus(v.Status), Sellable: v.Sellable, Available: v.Available,
		}
		if v.WeightGrams != nil {
			gv.WeightGrams = gen.NewOptInt32(int32(*v.WeightGrams)) //nolint:gosec // G115: ≤ 50.000 (CHECK)
		}
		g.Variants = append(g.Variants, gv)
	}
	return g
}

// Changes trả mã mẫu đổi từ since (không đệm).
//
//nolint:ireturn // kiểu trả của operation do ogen sinh (gen.Handler)
func (c *Catalog) Changes(ctx context.Context, tenant uuid.UUID, p gen.WebCatalogChangesParams) (gen.WebCatalogChangesRes, error) {
	limit := int(p.Limit.Or(webcatalog.MaxChanges))
	var out gen.WebCatalogChanges
	err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
		ids, until, more, err := webcatalog.Changes(ctx, tx, p.Since, limit)
		if err != nil {
			return err //nolint:wrapcheck // webcatalog đã bọc
		}
		out = gen.WebCatalogChanges{ProductIds: ids, Until: until, HasMore: more}
		return nil
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("đọc thay đổi danh mục: %w", err)
	}
	return &out, nil
}

// Taxonomy trả phân loại qua khóa "all" của SpaceTaxonomy (300 s), có ETag; If-None-Match khớp → 304.
//
//nolint:ireturn // kiểu trả của operation do ogen sinh (gen.Handler)
func (c *Catalog) Taxonomy(ctx context.Context, tenant uuid.UUID, p gen.WebCatalogTaxonomyParams) (gen.WebCatalogTaxonomyRes, error) {
	raw, _, err := c.rc.GetOrLoad(ctx, tenant, cache.SpaceTaxonomy, taxonomyKey, taxonomyTTL, func(ctx context.Context) ([]byte, error) {
		var t webcatalog.WebTaxonomy
		err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
			var err error
			t, err = webcatalog.Taxonomy(ctx, tx)
			return err //nolint:wrapcheck // webcatalog đã bọc
		}, db.TxAPI)
		if err != nil {
			return nil, fmt.Errorf("đọc phân loại cho website: %w", err)
		}
		g := taxonomy(t)
		return g.MarshalJSON()
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // như page
	}
	var out gen.WebTaxonomy
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc phân loại trong bộ đệm: %w", err)
	}
	body, err := out.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("mã hóa phân loại: %w", err)
	}
	tag := etag(body)
	if matches(p.IfNoneMatch, tag) {
		return &gen.WebCatalogTaxonomyNotModified{}, nil
	}
	return &gen.WebTaxonomyHeaders{ETag: gen.NewOptString(tag), Response: out}, nil
}

func taxonomy(t webcatalog.WebTaxonomy) gen.WebTaxonomy {
	g := gen.WebTaxonomy{
		Categories: make([]gen.WebCategory, 0, len(t.Categories)), Attributes: make([]gen.WebAttribute, 0, len(t.Attributes)),
		Tags: make([]gen.WebTag, 0, len(t.Tags)),
	}
	for _, c := range t.Categories {
		gc := gen.WebCategory{ID: c.ID, Name: c.Name, Slug: c.Slug, Depth: int32(c.Depth), Position: int32(c.Position)} //nolint:gosec // G115: cấp ≤ 3, vị trí nhỏ
		if c.ParentID != nil {
			gc.ParentId = gen.NewOptUUID(*c.ParentID)
		}
		g.Categories = append(g.Categories, gc)
	}
	for _, a := range t.Attributes {
		ga := gen.WebAttribute{ID: a.ID, Code: a.Code, Name: a.Name, Position: int32(a.Position), Values: make([]gen.WebAttributeValue, 0, len(a.Values))} //nolint:gosec // G115: vị trí nhỏ
		for _, v := range a.Values {
			ga.Values = append(ga.Values, gen.WebAttributeValue{ID: v.ID, Value: v.Value, Position: int32(v.Position)}) //nolint:gosec // G115: vị trí nhỏ
		}
		g.Attributes = append(g.Attributes, ga)
	}
	for _, tg := range t.Tags {
		g.Tags = append(g.Tags, gen.WebTag{Code: tg.Code, Name: tg.Name})
	}
	return g
}

func (c *Catalog) imageView() webcatalog.ImageView {
	return webcatalog.ImageView{BaseURL: c.view.BaseURL, Preset: c.view.Preset}
}

// etag là ETag mạnh "<sha256 hex của thân>".
func etag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// matches: If-None-Match (danh sách cách nhau bằng dấu phẩy, có thể có W/, hay "*") có chứa tag.
func matches(h gen.OptString, tag string) bool {
	v, ok := h.Get()
	if !ok {
		return false
	}
	for part := range strings.SplitSeq(v, ",") {
		part = strings.TrimPrefix(strings.TrimSpace(part), "W/")
		if part == "*" || part == tag {
			return true
		}
	}
	return false
}

// dedupe bỏ mã trùng, giữ thứ tự gặp đầu.
func dedupe(ids []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}
