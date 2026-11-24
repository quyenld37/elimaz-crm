package web

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries/webcatalog"
)

// Bộ phối cho website (kế hoạch M5, Task 454). Bộ đệm SpaceLooks 60 s, hai khóa: "home" (trang chủ) và "link" (mọi bộ
// mở được bằng link, trang /bo-phoi đọc từ đây). Hiện/ẩn đổi theo giờ, tồn, mẫu, ảnh mà không có sự kiện riêng;
// content.looks.changed làm mới sớm hơn (webcache).
const (
	looksTTL     = 60 * time.Second
	looksHomeKey = "home"
	looksLinkKey = "link"
)

// lookWatched: lý do ẩn của bộ đang bật trang chủ mà website báo Telegram (watch.ts); bộ tắt, chưa tới giờ, hết giờ là
// ẩn có chủ ý.
func lookWatched() []string {
	return []string{content.HiddenProductMissing, content.LookHiddenSoldOut, content.LookHiddenPhotoMissing}
}

// HomeLooks trả bộ phối (home: trang chủ ≤ 6 kèm bộ trang chủ đang bị ẩn; không: mọi bộ mở được bằng link), có ETag;
// If-None-Match khớp → 304.
//
//nolint:ireturn // kiểu trả của operation do ogen sinh (gen.Handler)
func (c *Catalog) HomeLooks(ctx context.Context, tenant uuid.UUID, p gen.WebHomeLooksParams) (gen.WebHomeLooksRes, error) {
	raw, err := c.lookList(ctx, tenant, p.Home)
	if err != nil {
		return nil, err
	}
	tag := etag(raw)
	if matches(p.IfNoneMatch, tag) {
		return &gen.WebHomeLooksNotModified{}, nil
	}
	var out gen.WebLookList
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc bộ phối trong bộ đệm: %w", err)
	}
	return &gen.WebLookListHeaders{ETag: gen.NewOptString(tag), Response: out}, nil
}

// Look trả bộ mở được bằng link key (8 hex đầu của id, không phân biệt hoa thường); không có, đã xóa, đang ẩn →
// content.ErrLookNotFound (404).
//
//nolint:ireturn // kiểu trả của operation do ogen sinh (gen.Handler)
func (c *Catalog) Look(ctx context.Context, tenant uuid.UUID, p gen.WebLookParams) (gen.WebLookRes, error) {
	raw, err := c.lookList(ctx, tenant, false)
	if err != nil {
		return nil, err
	}
	var all gen.WebLookList
	if err := all.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc bộ phối trong bộ đệm: %w", err)
	}
	key := strings.ToLower(p.LinkKey)
	i := slices.IndexFunc(all.Items, func(l gen.WebLook) bool { return l.LinkKey == key })
	if i < 0 {
		return nil, content.ErrLookNotFound
	}
	return &all.Items[i], nil
}

// lookList đọc bản JSON của WebLookList (home hay link) qua bộ đệm.
func (c *Catalog) lookList(ctx context.Context, tenant uuid.UUID, home bool) ([]byte, error) {
	key := looksLinkKey
	if home {
		key = looksHomeKey
	}
	raw, _, err := c.rc.GetOrLoad(ctx, tenant, cache.SpaceLooks, key, looksTTL, func(ctx context.Context) ([]byte, error) {
		var out gen.WebLookList
		now := c.clock()
		err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
			var err error
			out, err = c.loadLooks(ctx, tx, now, home)
			return err
		}, db.TxAPI)
		if err != nil {
			return nil, fmt.Errorf("đọc bộ phối cho website: %w", err)
		}
		return out.MarshalJSON()
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // ReadCache đã bọc; ErrBusy giữ nguyên để đổi 503
	}
	return raw, nil
}

// loadLooks: home → bộ hiện trên trang chủ ở now theo thứ tự, và bộ đang bật trang chủ mà bị ẩn vì lý do website báo;
// không → mọi bộ hiện trên trang link (content.ForLink). Gán lại kết quả mỗi lần (RetryTx).
func (c *Catalog) loadLooks(ctx context.Context, tx pgx.Tx, now time.Time, home bool) (gen.WebLookList, error) {
	set, err := content.GetLooks(ctx, tx)
	if err != nil {
		return gen.WebLookList{}, err //nolint:wrapcheck // content đã bọc
	}
	ref, err := LoadLookRefs(ctx, tx, set.Looks, c.view)
	if err != nil {
		return gen.WebLookList{}, err
	}
	out := gen.WebLookList{Items: []gen.WebLook{}, Hidden: []gen.WebLookHidden{}}
	for _, l := range set.Looks {
		probe := l
		if !home {
			probe = content.ForLink(l)
		}
		st := content.LookStatuses([]content.Look{probe}, ref, now)[0]
		if st.Shown {
			out.Items = append(out.Items, webLook(l, ref))
			continue
		}
		if home && l.OnHome && slices.Contains(lookWatched(), st.Reason) {
			h := gen.WebLookHidden{ID: l.ID, Name: l.Name, Reason: gen.LookHidden(st.Reason)}
			if st.ProductID != uuid.Nil {
				h.ProductId = gen.NewOptUUID(st.ProductID)
				if p, ok := ref.Products[st.ProductID]; ok {
					h.ProductName = gen.NewOptString(p.Name)
				}
			}
			out.Hidden = append(out.Hidden, h)
		}
	}
	return out, nil
}

// LoadLookRefs là content.LoadLookRefData kèm danh sách mẫu còn hàng (webcatalog.InStock; content không đọc view của
// inventory) — dùng chung cho website và màn nhân viên.
func LoadLookRefs(ctx context.Context, tx pgx.Tx, ls []content.Look, view catalog.ImageView) (content.LookRefData, error) {
	stock, err := webcatalog.InStock(ctx, tx, content.LookProductIDs(ls))
	if err != nil {
		return content.LookRefData{}, err //nolint:wrapcheck // webcatalog đã bọc
	}
	ref, err := content.LoadLookRefData(ctx, tx, ls, stock, view)
	if err != nil {
		return content.LookRefData{}, err //nolint:wrapcheck // content đã bọc
	}
	return ref, nil
}

// webLook dựng bộ của hợp đồng website (bộ đang hiện: ảnh đã xử lý có trong ref).
func webLook(l content.Look, ref content.LookRefData) gen.WebLook {
	title := l.Title
	if title == "" {
		title = content.LookAutoTitle(l, ref)
	}
	out := gen.WebLook{
		ID: l.ID, LinkKey: content.LookLinkKey(l.ID), Slug: content.LookSlug(l.Name), Name: l.Name, Title: title,
		OnHome: l.OnHome, CategoryIds: content.LookCategories(l, ref), Items: make([]gen.WebLookItem, 0, len(l.Items)),
		Photo: gen.WebLookPhoto{FileId: l.PhotoFileID, Alt: l.Name + ": " + title, Variants: []gen.WebImageVariant{}},
	}
	if l.Headline != "" {
		out.Headline = gen.NewOptString(l.Headline)
	}
	if l.Story != "" {
		out.Story = gen.NewOptString(l.Story)
	}
	if m := content.LookModelLine(l.ModelHeightCM, l.ModelSize); m != "" {
		out.Model = gen.NewOptString(m)
	}
	if l.StartsAt != nil {
		out.StartsAt = gen.NewOptDateTime(*l.StartsAt)
	}
	if l.EndsAt != nil {
		out.EndsAt = gen.NewOptDateTime(*l.EndsAt)
	}
	if im, ok := ref.PhotoOf(l); ok {
		out.Photo.Variants = ImageVariantsOut(im.Variants)
	}
	for _, it := range l.Items {
		out.Items = append(out.Items, gen.WebLookItem{ProductId: it.ProductID, Color: it.Color, X: it.X, Y: it.Y})
	}
	return out
}

// ImageVariantsOut đổi biến thể ảnh của catalog sang hợp đồng (chiều rộng, định dạng, đường dẫn).
func ImageVariantsOut(vs []catalog.ImageVariant) []gen.WebImageVariant {
	out := make([]gen.WebImageVariant, 0, len(vs))
	for _, v := range vs {
		out = append(out, gen.WebImageVariant{Width: int32(v.Width), Format: v.Format, URL: v.URL}) //nolint:gosec // G115: ≤ 10.000
	}
	return out
}
