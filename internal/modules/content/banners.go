package content

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/content/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/realtime"
)

// TopicBannersChanged là sự kiện mỗi lần lưu danh sách banner (aggregate home_banners, mã = tenant, Seq = revision mới).
// Consumer webcache làm mới bộ đệm trang chủ của website.
const TopicBannersChanged = "content.banners.changed"

const (
	aggregateBanners   = "home_banners"
	bannersSignalTopic = "content.banners"
)

// BannersChanged là payload của TopicBannersChanged (chỉ số, A16).
type BannersChanged struct {
	Revision int `json:"revision"`
}

// BannerSet là danh sách banner của shop theo thứ tự hiện. Chưa lưu lần nào: rỗng, Revision 0, UpdatedBy nil.
type BannerSet struct {
	Revision  int
	Banners   []Banner
	UpdatedBy *uuid.UUID
	UpdatedAt *time.Time
}

// AuditFields là bản tóm tắt cho nhật ký thao tác: revision và từng banner (mã, loại, bật).
func (s BannerSet) AuditFields() map[string]any {
	items := make([]map[string]any, 0, len(s.Banners))
	for _, b := range s.Banners {
		items = append(items, map[string]any{"id": b.ID, "kind": b.Kind, "enabled": b.Enabled})
	}
	return map[string]any{"revision": s.Revision, "banners": items}
}

// BannerChange là danh sách trước và sau một lần lưu.
type BannerChange struct {
	Before, After BannerSet
}

// GetBanners đọc danh sách banner của tenant của tx (không khóa).
func GetBanners(ctx context.Context, tx pgx.Tx) (BannerSet, error) {
	q := store.New(tx)
	var s BannerSet
	head, err := q.GetBannerSet(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return BannerSet{}, fmt.Errorf("đọc danh sách banner: %w", err)
	default:
		s = BannerSet{Revision: int(head.Revision), UpdatedBy: head.UpdatedBy, UpdatedAt: head.UpdatedAt}
	}
	rows, err := q.ListBanners(ctx)
	if err != nil {
		return BannerSet{}, fmt.Errorf("đọc banner: %w", err)
	}
	s.Banners = make([]Banner, 0, len(rows))
	for _, r := range rows {
		s.Banners = append(s.Banners, Banner{
			ID: r.ID, Enabled: r.Enabled, Kind: r.Kind, CampaignID: val(r.CampaignID), CategoryID: val(r.CategoryID),
			Kicker: r.Kicker, Title: r.Title, CTA: r.Cta, Theme: r.Theme, StartsAt: r.StartsAt, EndsAt: r.EndsAt,
			ProductIDs: r.ProductIds, ImageDesktop: val(r.ImageDesktop), ImageMobile: val(r.ImageMobile), Alt: r.Alt,
			LinkKind: r.LinkKind.String, LinkID: val(r.LinkID),
		})
	}
	return s, nil
}

// SaveBanners thay cả danh sách của tenant bằng list (chuẩn hóa, kiểm bằng ValidateBanners trên dữ liệu đọc trong
// tx) khi revision khớp: khóa dòng home_banner_sets (ADR-46 nhóm 12d; chưa có thì tạo revision 0), lệch →
// ErrRevisionConflict; revision + 1, phát TopicBannersChanged và báo hiệu màn hình trong tx. Chỉ đọc platform.files
// (files.Status), không khóa. Gán lại mọi kết quả mỗi lần chạy (RetryTx có thể chạy lại).
func (w Writer) SaveBanners(ctx context.Context, tx pgx.Tx, list []Banner, revision int, a Actor, now time.Time) (BannerChange, error) {
	if w.Events == nil || w.Queue == nil {
		return BannerChange{}, fmt.Errorf("lưu banner: Writer thiếu Events, Queue: %w", events.ErrInvalidEvent)
	}
	q := store.New(tx)
	if err := q.EnsureBannerSet(ctx, a.TenantID); err != nil {
		return BannerChange{}, fmt.Errorf("tạo danh sách banner: %w", err)
	}
	rev, err := q.LockBannerSet(ctx)
	if err != nil {
		return BannerChange{}, fmt.Errorf("khóa danh sách banner: %w", err)
	}
	if int(rev) != revision {
		return BannerChange{}, fmt.Errorf("lưu banner revision %d (hiện %d): %w", revision, rev, ErrRevisionConflict)
	}
	before, err := GetBanners(ctx, tx)
	if err != nil {
		return BannerChange{}, err
	}
	bs := NormalizeBanners(list)
	ref, err := LoadRefData(ctx, tx, bs, nil)
	if err != nil {
		return BannerChange{}, err
	}
	if err := ValidateBanners(bs, ref); err != nil {
		return BannerChange{}, err
	}
	if err := q.DeleteBanners(ctx, a.TenantID); err != nil {
		return BannerChange{}, fmt.Errorf("xóa banner cũ: %w", err)
	}
	for i, b := range bs {
		if err := q.InsertBanner(ctx, insertParams(a.TenantID, i, b)); err != nil {
			return BannerChange{}, fmt.Errorf("ghi banner %d: %w", i+1, err)
		}
	}
	head, err := q.BumpBannerSet(ctx, store.BumpBannerSetParams{UpdatedBy: a.UserID, UpdatedAt: now, TenantID: a.TenantID})
	if err != nil {
		return BannerChange{}, fmt.Errorf("tăng revision banner: %w", err)
	}
	after := BannerSet{Revision: int(head.Revision), Banners: bs, UpdatedBy: head.UpdatedBy, UpdatedAt: head.UpdatedAt}
	if err := w.listChanged(ctx, tx, a.TenantID, after.Revision, aggregateBanners, TopicBannersChanged, bannersSignalTopic,
		BannersChanged{Revision: after.Revision}); err != nil {
		return BannerChange{}, err
	}
	return BannerChange{Before: before, After: after}, nil
}

func insertParams(tenant uuid.UUID, pos int, b Banner) store.InsertBannerParams {
	pids := b.ProductIDs
	if pids == nil {
		pids = []uuid.UUID{}
	}
	return store.InsertBannerParams{
		ID: b.ID, TenantID: tenant, Position: int32(pos), Enabled: b.Enabled, Kind: b.Kind, //nolint:gosec // G115: ≤ 8
		CampaignID: ptr(b.CampaignID), CategoryID: ptr(b.CategoryID), Kicker: b.Kicker, Title: b.Title, Cta: b.CTA,
		Theme: b.Theme, StartsAt: b.StartsAt, EndsAt: b.EndsAt, ProductIds: pids, ImageDesktop: ptr(b.ImageDesktop),
		ImageMobile: ptr(b.ImageMobile), Alt: b.Alt, LinkKind: pgtype.Text{String: b.LinkKind, Valid: b.LinkKind != ""},
		LinkID: ptr(b.LinkID),
	}
}

// listChanged phát sự kiện topic của danh sách (aggregate, mã = tenant, Seq = revision mới, payload chỉ có revision) và
// báo hiệu signal cho màn hình nhân viên (content.read) — dùng chung cho banner và bộ phối.
func (w Writer) listChanged(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, revision int, aggregate, topic, signal string, payload any) error {
	if _, _, err := w.Events.Emit(ctx, tx, w.Queue, events.Event{
		TenantID: tenant, AggregateType: aggregate, AggregateID: tenant.String(), Seq: int64(revision),
		Topic: topic, Payload: payload,
	}); err != nil {
		return fmt.Errorf("phát %s: %w", topic, err)
	}
	if w.Realtime == nil {
		return nil
	}
	if err := w.Realtime.Publish(ctx, tx, realtime.Signal{
		TenantID: tenant, Topic: signal, Requires: permRead,
		Entity: realtime.Entity{Type: aggregate, ID: tenant.String()},
	}); err != nil {
		return fmt.Errorf("báo hiệu %s: %w", signal, err)
	}
	return nil
}

// LoadRefData đọc dữ liệu mà luật banner cần cho danh sách bs (đã chuẩn hóa), trong tenant của tx: chiến dịch web chưa
// xóa, danh mục đang dùng, các mẫu được nhắc (ghim, đích), trạng thái các ảnh, và nguồn của từng banner có mẫu hiện được
// không (Pool). sold30 là mã mẫu có bán trong 30 ngày (nguồn của banner bán chạy; lớp HTTP đọc qua webcatalog.Sold30 vì
// content không đọc view của order); nil khi không cần (lưu).
func LoadRefData(ctx context.Context, tx pgx.Tx, bs []Banner, sold30 []uuid.UUID) (RefData, error) {
	q := store.New(tx)
	ref := RefData{
		Campaigns: map[uuid.UUID]CampaignRef{}, Categories: map[uuid.UUID]CategoryRef{},
		Products: map[uuid.UUID]ProductRef{}, Images: map[uuid.UUID]files.FileStatus{}, Pool: map[uuid.UUID]bool{},
	}
	cs, err := q.WebCampaigns(ctx)
	if err != nil {
		return RefData{}, fmt.Errorf("đọc chiến dịch cho banner: %w", err)
	}
	for _, c := range cs {
		ref.Campaigns[c.ID] = CampaignRef{
			ID: c.ID, Name: c.Name, Percent: int(c.Percent), Scope: c.Scope, State: c.State, StartsAt: c.StartsAt,
			EndsAt: c.EndsAt, TargetIDs: c.TargetIds,
		}
	}
	cats, err := q.ActiveCategories(ctx)
	if err != nil {
		return RefData{}, fmt.Errorf("đọc danh mục cho banner: %w", err)
	}
	for _, c := range cats {
		ref.Categories[c.CategoryID] = CategoryRef{ID: c.CategoryID, ParentID: val(c.ParentID), Name: c.Name}
	}
	if err := loadProducts(ctx, q, bs, ref); err != nil {
		return RefData{}, err
	}
	for _, b := range bs {
		for _, id := range []uuid.UUID{b.ImageDesktop, b.ImageMobile} {
			if id == uuid.Nil {
				continue
			}
			st, err := files.Status(ctx, tx, id)
			switch {
			case errors.Is(err, files.ErrNotFound):
			case err != nil:
				return RefData{}, fmt.Errorf("đọc ảnh banner: %w", err)
			default:
				ref.Images[id] = st
			}
		}
		has, err := poolHas(ctx, q, b, ref, sold30)
		if err != nil {
			return RefData{}, err
		}
		ref.Pool[b.ID] = has
	}
	return ref, nil
}

// loadProducts đọc các mẫu mà bs nhắc (ghim, đích sản phẩm của banner ảnh) vào ref.Products.
func loadProducts(ctx context.Context, q *store.Queries, bs []Banner, ref RefData) error {
	var ids []uuid.UUID
	for _, b := range bs {
		ids = append(ids, b.ProductIDs...)
		if b.LinkKind == LinkProduct && b.LinkID != uuid.Nil {
			ids = append(ids, b.LinkID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	rows, err := q.ProductsByIDs(ctx, slices.Compact(ids))
	if err != nil {
		return fmt.Errorf("đọc mẫu cho banner: %w", err)
	}
	for _, r := range rows {
		ref.Products[r.ProductID] = ProductRef{
			ID: r.ProductID, Code: r.Code, Name: r.Name, CategoryID: val(r.CategoryID), Showable: r.Showable,
		}
	}
	return nil
}

// poolHas: nguồn của banner b có ít nhất một mẫu hiện được (banner ảnh không có nguồn: false).
func poolHas(ctx context.Context, q *store.Queries, b Banner, ref RefData, sold30 []uuid.UUID) (bool, error) {
	p := store.ShowableExistsParams{CategoryIds: []uuid.UUID{}, ProductIds: []uuid.UUID{}}
	switch b.Kind {
	case KindNew:
		p.WholeShop = true
	case KindTrending:
		p.ProductIds = append(p.ProductIds, sold30...)
	case KindCategory:
		if _, ok := ref.Categories[b.CategoryID]; !ok {
			return false, nil
		}
		p.CategoryIds = ref.subtree(b.CategoryID)
	case KindCampaign:
		c, ok := ref.Campaigns[b.CampaignID]
		if !ok {
			return false, nil
		}
		switch c.Scope {
		case "all":
			p.WholeShop = true
		case "products":
			p.ProductIds = append(p.ProductIds, c.TargetIDs...)
		case "categories":
			p.CategoryIds = append(p.CategoryIds, c.TargetIDs...)
		}
	default:
		return false, nil
	}
	if !p.WholeShop && len(p.CategoryIds) == 0 && len(p.ProductIds) == 0 {
		return false, nil
	}
	found, err := q.ShowableExists(ctx, p)
	if err != nil {
		return false, fmt.Errorf("đọc nguồn của banner: %w", err)
	}
	return found, nil
}

// val: mã của cột cho phép rỗng; NULL → uuid.Nil.
func val(p *uuid.UUID) (id uuid.UUID) {
	if p != nil {
		id = *p
	}
	return id
}

func ptr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// Status là kết quả hiện/ẩn của một banner ở một thời điểm (Reason rỗng khi hiện).
type Status struct {
	ID     uuid.UUID
	Shown  bool
	Reason string
}

// Statuses trả Visibility của từng banner của bs theo thứ tự.
func Statuses(bs []Banner, ref RefData, now time.Time) []Status {
	out := make([]Status, len(bs))
	for i, b := range bs {
		shown, reason := Visibility(b, ref, now)
		out[i] = Status{ID: b.ID, Shown: shown, Reason: reason}
	}
	return out
}

// NeedsSold30 cho biết danh sách có banner bán chạy (bên gọi mới cần đọc số bán 30 ngày cho LoadRefData).
func NeedsSold30(bs []Banner) bool {
	return slices.ContainsFunc(bs, func(b Banner) bool { return b.Kind == KindTrending })
}

// Picture là đường dẫn WebP của một ảnh banner đã xử lý: Large rộng 2400, Small rộng 1200 (ảnh nhỏ hơn giữ cỡ gốc).
type Picture struct {
	Large, Small string
}

// BannerPictures trả đường dẫn ảnh (bộ files.PresetBannerV1, baseURL = FILES_PUBLIC_BASE_URL) của các tệp ids đã xử lý
// xong trong tenant của tx; tệp chưa xong, đã xóa, thiếu biến thể thì không có trong kết quả.
func BannerPictures(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, baseURL string) (map[uuid.UUID]Picture, error) {
	vs, err := files.PublicVariants(ctx, tx, ids, baseURL)
	if err != nil {
		return nil, fmt.Errorf("đọc ảnh banner: %w", err)
	}
	out := map[uuid.UUID]Picture{}
	for id, all := range vs {
		var p Picture
		for _, v := range files.SelectPreset(all, files.PresetBannerV1) {
			switch {
			case v.Format == "webp" && v.Width == 2400:
				p.Large = v.URL
			case v.Format == "webp" && v.Width == 1200:
				p.Small = v.URL
			}
		}
		if p.Large != "" && p.Small != "" {
			out[id] = p
		}
	}
	return out, nil
}
