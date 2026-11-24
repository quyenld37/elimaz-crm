package content

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/content/internal/store"
	"elimaz.com.vn/crm/internal/platform/events"
)

// TopicLooksChanged là sự kiện mỗi lần lưu danh sách bộ phối (aggregate home_looks, mã = tenant, Seq = revision mới).
// Consumer webcache làm mới bộ đệm bộ phối của website.
const TopicLooksChanged = "content.looks.changed"

const (
	aggregateLooks   = "home_looks"
	looksSignalTopic = "content.looks"
)

// LooksChanged là payload của TopicLooksChanged (chỉ số, A16).
type LooksChanged struct {
	Revision int `json:"revision"`
}

// LookSet là danh sách bộ phối của shop theo thứ tự. Chưa lưu lần nào: rỗng, Revision 0, UpdatedBy nil.
type LookSet struct {
	Revision  int
	Looks     []Look
	UpdatedBy *uuid.UUID
	UpdatedAt *time.Time
}

// AuditFields là bản tóm tắt cho nhật ký thao tác: revision và từng bộ (mã, tên, trang chủ, số món).
func (s LookSet) AuditFields() map[string]any {
	items := make([]map[string]any, 0, len(s.Looks))
	for _, l := range s.Looks {
		items = append(items, map[string]any{"id": l.ID, "name": l.Name, "onHome": l.OnHome, "items": len(l.Items)})
	}
	return map[string]any{"revision": s.Revision, "looks": items}
}

// LookChange là danh sách trước và sau một lần lưu.
type LookChange struct {
	Before, After LookSet
}

// GetLooks đọc danh sách bộ phối của tenant của tx (không khóa).
func GetLooks(ctx context.Context, tx pgx.Tx) (LookSet, error) {
	q := store.New(tx)
	var s LookSet
	head, err := q.GetLookSet(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return LookSet{}, fmt.Errorf("đọc danh sách bộ phối: %w", err)
	default:
		s = LookSet{Revision: int(head.Revision), UpdatedBy: head.UpdatedBy, UpdatedAt: head.UpdatedAt}
	}
	rows, err := q.ListLooks(ctx)
	if err != nil {
		return LookSet{}, fmt.Errorf("đọc bộ phối: %w", err)
	}
	items, err := q.ListLookItems(ctx)
	if err != nil {
		return LookSet{}, fmt.Errorf("đọc món của bộ phối: %w", err)
	}
	byLook := map[uuid.UUID][]LookItem{}
	for _, it := range items {
		byLook[it.LookID] = append(byLook[it.LookID], LookItem{ProductID: it.ProductID, Color: it.Color, X: it.XPct, Y: it.YPct})
	}
	s.Looks = make([]Look, 0, len(rows))
	for _, r := range rows {
		l := Look{
			ID: r.ID, OnHome: r.OnHome, Name: r.Name, Title: r.Title, Headline: r.Headline, Story: r.Story,
			PhotoFileID: r.PhotoFileID, CategoryIDs: r.CategoryIds, ModelHeightCM: int(r.ModelHeightCm.Int32),
			ModelSize: r.ModelSize, StartsAt: r.StartsAt, EndsAt: r.EndsAt, Items: byLook[r.ID],
		}
		if l.Items == nil {
			l.Items = []LookItem{}
		}
		s.Looks = append(s.Looks, l)
	}
	return s, nil
}

// SaveLooks thay cả danh sách bộ phối của tenant bằng list (chuẩn hóa, kiểm bằng ValidateLooks trên dữ liệu đọc trong
// tx) khi revision khớp: khóa dòng look_sets (ADR-46 nhóm 12d; chưa có thì tạo revision 0), lệch →
// ErrRevisionConflict; giới hạn trang chủ kiểm sau khi khóa; revision + 1, phát TopicLooksChanged và báo hiệu màn hình
// trong tx. Gán lại mọi kết quả mỗi lần chạy (RetryTx có thể chạy lại).
func (w Writer) SaveLooks(ctx context.Context, tx pgx.Tx, list []Look, revision int, a Actor, now time.Time) (LookChange, error) {
	if w.Events == nil || w.Queue == nil {
		return LookChange{}, fmt.Errorf("lưu bộ phối: Writer thiếu Events, Queue: %w", events.ErrInvalidEvent)
	}
	q := store.New(tx)
	if err := q.EnsureLookSet(ctx, a.TenantID); err != nil {
		return LookChange{}, fmt.Errorf("tạo danh sách bộ phối: %w", err)
	}
	rev, err := q.LockLookSet(ctx)
	if err != nil {
		return LookChange{}, fmt.Errorf("khóa danh sách bộ phối: %w", err)
	}
	if int(rev) != revision {
		return LookChange{}, fmt.Errorf("lưu bộ phối revision %d (hiện %d): %w", revision, rev, ErrRevisionConflict)
	}
	before, err := GetLooks(ctx, tx)
	if err != nil {
		return LookChange{}, err
	}
	ls := NormalizeLooks(list)
	ref, err := LoadLookRefData(ctx, tx, ls, nil, catalog.ImageView{})
	if err != nil {
		return LookChange{}, err
	}
	if err := ValidateLooks(ls, ref, before.Looks); err != nil {
		return LookChange{}, err
	}
	if err := writeLooks(ctx, q, a.TenantID, ls); err != nil {
		return LookChange{}, err
	}
	head, err := q.BumpLookSet(ctx, store.BumpLookSetParams{UpdatedBy: a.UserID, UpdatedAt: now, TenantID: a.TenantID})
	if err != nil {
		return LookChange{}, fmt.Errorf("tăng revision bộ phối: %w", err)
	}
	after := LookSet{Revision: int(head.Revision), Looks: ls, UpdatedBy: head.UpdatedBy, UpdatedAt: head.UpdatedAt}
	if err := w.listChanged(ctx, tx, a.TenantID, after.Revision, aggregateLooks, TopicLooksChanged, looksSignalTopic,
		LooksChanged{Revision: after.Revision}); err != nil {
		return LookChange{}, err
	}
	return LookChange{Before: before, After: after}, nil
}

// writeLooks xóa danh sách cũ (món xóa dây chuyền) rồi chèn ls theo thứ tự: bộ trước, món sau (thứ tự 12d).
func writeLooks(ctx context.Context, q *store.Queries, tenant uuid.UUID, ls []Look) error {
	if err := q.DeleteLooks(ctx, tenant); err != nil {
		return fmt.Errorf("xóa bộ phối cũ: %w", err)
	}
	for i, l := range ls {
		height := pgtype.Int4{Int32: int32(l.ModelHeightCM), Valid: l.ModelHeightCM != 0} //nolint:gosec // G115: 130–200
		if err := q.InsertLook(ctx, store.InsertLookParams{
			ID: l.ID, TenantID: tenant, Position: int32(i), OnHome: l.OnHome, Name: l.Name, Title: l.Title,
			Headline: l.Headline, Story: l.Story, PhotoFileID: l.PhotoFileID, CategoryIds: l.CategoryIDs,
			ModelHeightCm: height, ModelSize: l.ModelSize, StartsAt: l.StartsAt, EndsAt: l.EndsAt,
		}); err != nil {
			return fmt.Errorf("ghi bộ phối %d: %w", i+1, err)
		}
		for k, it := range l.Items {
			if err := q.InsertLookItem(ctx, store.InsertLookItemParams{
				TenantID: tenant, LookID: l.ID, ProductID: it.ProductID, Color: it.Color, XPct: it.X, YPct: it.Y,
				Position: int32(k),
			}); err != nil {
				return fmt.Errorf("ghi món %d của bộ phối %d: %w", k+1, i+1, err)
			}
		}
	}
	return nil
}

// LoadLookRefData đọc dữ liệu mà luật bộ phối cần cho ls, trong tenant của tx: các mẫu được nhắc (chưa xóa) kèm đang
// bán, ảnh đang hiện của chúng (đã xử lý và đường dẫn theo view, catalog.ResolveImages), danh mục đang dùng. inStock là
// mã mẫu còn hàng ở kho mặc định (lớp HTTP đọc qua webcatalog.InStock vì content không đọc view inventory); nil khi
// không cần (lưu).
func LoadLookRefData(ctx context.Context, tx pgx.Tx, ls []Look, inStock []uuid.UUID, view catalog.ImageView) (LookRefData, error) {
	q := store.New(tx)
	ref := LookRefData{
		Products: map[uuid.UUID]LookProduct{}, Images: []LookImage{}, Categories: map[uuid.UUID]CategoryRef{},
		Colors: map[uuid.UUID][]LookColor{},
	}
	cats, err := q.ActiveCategories(ctx)
	if err != nil {
		return LookRefData{}, fmt.Errorf("đọc danh mục cho bộ phối: %w", err)
	}
	for _, c := range cats {
		ref.Categories[c.CategoryID] = CategoryRef{ID: c.CategoryID, ParentID: val(c.ParentID), Name: c.Name}
	}
	ids := LookProductIDs(ls)
	if len(ids) == 0 {
		return ref, nil
	}
	rows, err := q.LookProducts(ctx, ids)
	if err != nil {
		return LookRefData{}, fmt.Errorf("đọc mẫu cho bộ phối: %w", err)
	}
	stocked := make(map[uuid.UUID]bool, len(inStock))
	for _, id := range inStock {
		stocked[id] = true
	}
	for _, r := range rows {
		ref.Products[r.ProductID] = LookProduct{
			ID: r.ProductID, Code: r.Code, Name: r.Name, CategoryID: val(r.CategoryID), OnSale: r.OnSale,
			InStock: stocked[r.ProductID],
		}
	}
	colors, err := q.LookProductColors(ctx, ids)
	if err != nil {
		return LookRefData{}, fmt.Errorf("đọc màu mẫu cho bộ phối: %w", err)
	}
	for _, c := range colors {
		ref.Colors[c.ProductID] = append(ref.Colors[c.ProductID], LookColor{Name: c.Color, Hex: c.ColorHex})
	}
	imgs, err := q.LookProductImages(ctx, ids)
	if err != nil {
		return LookRefData{}, fmt.Errorf("đọc ảnh mẫu cho bộ phối: %w", err)
	}
	resolved := make([]catalog.Image, len(imgs))
	for i, im := range imgs {
		resolved[i] = catalog.Image{FileID: im.FileID, Position: int(im.Position)}
		if im.Color.Valid {
			resolved[i].Color = &im.Color.String
		}
	}
	if err := catalog.ResolveImages(ctx, tx, resolved, view); err != nil {
		return LookRefData{}, fmt.Errorf("đọc biến thể ảnh cho bộ phối: %w", err)
	}
	for i, im := range imgs {
		ref.Images = append(ref.Images, LookImage{
			ProductID: im.ProductID, FileID: im.FileID, Color: resolved[i].Color, Position: int(im.Position),
			Ready: resolved[i].Status == catalog.ImageReady, Variants: resolved[i].Variants,
		})
	}
	return ref, nil
}
