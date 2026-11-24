package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/httpapi/web"
	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Bộ phối của nhân viên (kế hoạch M5, Task 454): đọc, xem trước content.read, lưu content.manage (bảng route). Audit
// content.look.save ghi ở đây, cùng tx với lần lưu (QĐ38: hành động content.look.*).

// GetLooks (GET /api/v1/content/looks).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetLooks(ctx context.Context) (gen.GetLooksRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var out *gen.LookList
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		set, err := content.GetLooks(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // content đã bọc
		}
		out, err = h.lookList(ctx, tx, set)
		return err
	}, db.TxAPI)
	if err != nil {
		return nil, err //nolint:wrapcheck // lỗi của fn trả nguyên
	}
	return out, nil
}

// PreviewLooks (POST /api/v1/content/looks/preview): kiểm luật cả danh sách như lưu (422), bỏ các bộ chưa đủ kèm mã
// lỗi lưu sẽ trả (Task 488), rồi tính hiện/ẩn lúc này của các bộ còn lại, kèm mẫu, màu và ảnh của các món; không ghi gì.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) PreviewLooks(ctx context.Context, req *gen.PreviewLooksRequest) (gen.PreviewLooksRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	ls := content.NormalizeLooks(looksIn(req.Looks))
	now := h.now()
	var out *gen.LookPreview
	err := tenancy.WithTenant(ctx, h.d.DB, u.TenantID, func(tx pgx.Tx) error {
		saved, err := content.GetLooks(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // content đã bọc
		}
		ref, err := web.LoadLookRefs(ctx, tx, ls, h.imageView())
		if err != nil {
			return err //nolint:wrapcheck // web, content đã bọc
		}
		ok, skipped, err := content.PreviewLooks(ls, ref, saved.Looks)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của content
		}
		out = &gen.LookPreview{
			Statuses: lookStatusesOut(ok, content.LookStatuses(ok, ref, now)), Products: lookProductsOut(ref),
			Photos: lookPhotosOut(ref), Skipped: lookSkipsOut(skipped),
		}
		return nil
	}, db.TxAPI)
	if err != nil {
		return nil, err //nolint:wrapcheck // lỗi của fn trả nguyên
	}
	return out, nil
}

// SaveLooks (PUT /api/v1/content/looks): thay cả danh sách khi revision khớp; lỗi luật không lưu vào khóa chống trùng
// (gửi lại sau khi sửa).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SaveLooks(ctx context.Context, req *gen.SaveLooksRequest, _ gen.SaveLooksParams) (gen.SaveLooksRes, error) {
	save := func(tx pgx.Tx, u identity.AuthUser) (any, error) { return h.saveLooksTx(ctx, tx, u, req) }
	out, err := h.refusableWrite(ctx, http.StatusOK, isContentRefusal, "lưu bộ phối", save)
	if err != nil {
		return nil, err
	}
	return out.(*gen.LookList), nil //nolint:forcetypeassert // saveLooksTx trả *gen.LookList
}

// saveLooksTx: lưu, audit content.look.save, dựng câu trả lời — trong transaction của refusableWrite.
func (h *handler) saveLooksTx(ctx context.Context, tx pgx.Tx, u identity.AuthUser, req *gen.SaveLooksRequest) (*gen.LookList, error) {
	w := content.Writer{Realtime: h.d.Realtime, Events: h.d.Events, Queue: queue.NewQueue()}
	a := content.Actor{TenantID: u.TenantID, UserID: u.UserID}
	ch, err := w.SaveLooks(ctx, tx, looksIn(req.Looks), int(req.Revision), a, h.now())
	if err != nil {
		return nil, err //nolint:wrapcheck // sentinel của content
	}
	before, after := ch.Before.AuditFields(), ch.After.AuditFields()
	if err := h.audit(ctx, tx, u, "content.look.save", "content.looks", u.TenantID, before, after); err != nil {
		return nil, err
	}
	return h.lookList(ctx, tx, ch.After)
}

// WebHomeLooks (GET /api/v1/web/content/looks, Task 454) → 200 kèm ETag, hoặc 304.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebHomeLooks(ctx context.Context, params gen.WebHomeLooksParams) (gen.WebHomeLooksRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	return h.d.webCatalog.HomeLooks(ctx, u.TenantID, params) //nolint:wrapcheck // lỗi đổi mã ở errors.go
}

// WebLook (GET /api/v1/web/content/looks/{linkKey}, Task 454) → bộ mở được bằng link, hoặc 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebLook(ctx context.Context, params gen.WebLookParams) (gen.WebLookRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	return h.d.webCatalog.Look(ctx, u.TenantID, params) //nolint:wrapcheck // lỗi đổi mã ở errors.go
}

// lookList dựng câu trả lời của nhân viên: danh sách, hiện/ẩn lúc này, mẫu và ảnh của các món, danh mục đang dùng.
func (h *handler) lookList(ctx context.Context, tx pgx.Tx, set content.LookSet) (*gen.LookList, error) {
	ref, err := web.LoadLookRefs(ctx, tx, set.Looks, h.imageView())
	if err != nil {
		return nil, err //nolint:wrapcheck // web, content đã bọc
	}
	out := &gen.LookList{
		Revision: int32(set.Revision), //nolint:gosec // G115: revision int4
		Looks:    make([]gen.Look, 0, len(set.Looks)), Statuses: lookStatusesOut(set.Looks, content.LookStatuses(set.Looks, ref, h.now())),
		Products: lookProductsOut(ref), Photos: lookPhotosOut(ref), Categories: categoriesOut(ref.Categories),
	}
	if err := stampInto(ctx, tx, set.UpdatedBy, set.UpdatedAt, &out.UpdatedBy, &out.UpdatedByName, &out.UpdatedAt); err != nil {
		return nil, err
	}
	for _, l := range set.Looks {
		out.Looks = append(out.Looks, lookOut(l))
	}
	return out, nil
}

// lookStatusesOut: trạng thái kèm link của từng bộ (ls và ss cùng thứ tự).
func lookStatusesOut(ls []content.Look, ss []content.LookStatus) []gen.LookStatus {
	out := make([]gen.LookStatus, 0, len(ss))
	for i, s := range ss {
		g := gen.LookStatus{
			ID: s.ID, Shown: s.Shown, AutoTitle: s.AutoTitle, LinkKey: content.LookLinkKey(s.ID),
			Path: content.LookPath(s.ID, ls[i].Name),
		}
		if s.Reason != "" {
			g.Reason = gen.NewOptLookHidden(gen.LookHidden(s.Reason))
		}
		if s.ProductID != uuid.Nil {
			g.ProductId = gen.NewOptUUID(s.ProductID)
		}
		out = append(out, g)
	}
	return out
}

func lookProductsOut(ref content.LookRefData) []gen.LookProductRef {
	out := make([]gen.LookProductRef, 0, len(ref.Products))
	for _, p := range sortedValues(ref.Products, func(a, b content.LookProduct) bool { return a.Code < b.Code }) {
		g := gen.LookProductRef{
			ID: p.ID, Code: p.Code, Name: p.Name, OnSale: p.OnSale, InStock: p.InStock,
			Colors: make([]gen.LookColor, 0, len(ref.Colors[p.ID])),
		}
		for _, c := range ref.Colors[p.ID] {
			lc := gen.LookColor{Name: c.Name}
			if c.Hex != "" {
				lc.Hex = gen.NewOptString(c.Hex)
			}
			g.Colors = append(g.Colors, lc)
		}
		if p.CategoryID != uuid.Nil {
			g.CategoryId = gen.NewOptUUID(p.CategoryID)
		}
		out = append(out, g)
	}
	return out
}

// lookSkipsOut: bộ bị bỏ khi xem trước kèm mã lỗi (bảng đổi lỗi của content, cùng mã lưu sẽ trả).
func lookSkipsOut(ss []content.LookSkip) []gen.LookSkip {
	out := make([]gen.LookSkip, 0, len(ss))
	for _, s := range ss {
		_, code, _ := contentError(s.Err)
		out = append(out, gen.LookSkip{ID: s.ID, Reason: gen.LookSkipReason(code)})
	}
	return out
}

func lookPhotosOut(ref content.LookRefData) []gen.LookPhoto {
	out := make([]gen.LookPhoto, 0, len(ref.Images))
	for _, im := range ref.Images {
		g := gen.LookPhoto{
			ProductId: im.ProductID, FileId: im.FileID, Position: int32(im.Position), Ready: im.Ready, //nolint:gosec // G115: ≤ 50
			Variants: web.ImageVariantsOut(im.Variants),
		}
		if im.Color != nil {
			g.Color = gen.NewOptString(*im.Color)
		}
		out = append(out, g)
	}
	return out
}

func looksIn(in []gen.Look) []content.Look {
	out := make([]content.Look, 0, len(in))
	for _, g := range in {
		l := content.Look{
			ID: g.ID, OnHome: g.OnHome, Name: g.Name, Title: g.Title.Or(""), Headline: g.Headline.Or(""),
			Story: g.Story.Or(""), PhotoFileID: g.PhotoFileId, CategoryIDs: g.CategoryIds,
			ModelHeightCM: int(g.ModelHeightCm.Or(0)), ModelSize: g.ModelSize.Or(""), Items: make([]content.LookItem, 0, len(g.Items)),
		}
		if v, ok := g.StartsAt.Get(); ok {
			l.StartsAt = timePtr(v)
		}
		if v, ok := g.EndsAt.Get(); ok {
			l.EndsAt = timePtr(v)
		}
		for _, it := range g.Items {
			l.Items = append(l.Items, content.LookItem{ProductID: it.ProductId, Color: it.Color.Or(""), X: it.X, Y: it.Y})
		}
		out = append(out, l)
	}
	return out
}

func lookOut(l content.Look) gen.Look {
	g := gen.Look{
		ID: l.ID, OnHome: l.OnHome, Name: l.Name, Title: gen.NewOptString(l.Title), Headline: gen.NewOptString(l.Headline),
		Story: gen.NewOptString(l.Story), PhotoFileId: l.PhotoFileID, CategoryIds: append([]uuid.UUID{}, l.CategoryIDs...),
		ModelSize: gen.NewOptString(l.ModelSize), Items: make([]gen.LookItem, 0, len(l.Items)),
	}
	if l.ModelHeightCM != 0 {
		g.ModelHeightCm = gen.NewOptInt32(int32(l.ModelHeightCM)) //nolint:gosec // G115: 130–200
	}
	if l.StartsAt != nil {
		g.StartsAt = gen.NewOptDateTime(*l.StartsAt)
	}
	if l.EndsAt != nil {
		g.EndsAt = gen.NewOptDateTime(*l.EndsAt)
	}
	for _, it := range l.Items {
		g.Items = append(g.Items, gen.LookItem{ProductId: it.ProductID, Color: gen.NewOptString(it.Color), X: it.X, Y: it.Y})
	}
	return g
}
