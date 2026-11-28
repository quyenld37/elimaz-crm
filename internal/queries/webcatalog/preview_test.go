package webcatalog_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"
)

// [MO01] Xem trước (kế hoạch M7 Task 261): mẫu nháp không có trong trang và theo mã khi không xem trước (khóa chỉ có
// web.catalog.read), có kèm Preview = true khi xem trước; mẫu đang bán, ngừng bán không bao giờ có Preview; mẫu nháp
// của người bán thứ hai cũng xem trước được (KH14).
func TestProducts_Preview(t *testing.T) {
	e := newEnv(t)
	all := e.all()
	draftA, draftP, gone := all[0], all[len(all)-1], all[1]
	e.exec("UPDATE catalog.products SET status = 'draft' WHERE id = ANY($1)", []uuid.UUID{draftA, draftP})
	e.exec("UPDATE catalog.products SET status = 'discontinued' WHERE id = $1", gone)

	ids, _, previewed := e.pagesWith(200, false)
	if len(ids) != 448 || slices.Contains(ids, draftA) || slices.Contains(ids, draftP) || !slices.Contains(ids, gone) ||
		len(previewed) != 0 {
		t.Fatalf("trang khóa thường: %d mẫu, có nháp A %v, nháp P %v, ngừng bán %v, preview %v", len(ids),
			slices.Contains(ids, draftA), slices.Contains(ids, draftP), slices.Contains(ids, gone), previewed)
	}
	ids, _, previewed = e.pagesWith(200, true)
	want := []uuid.UUID{draftA, draftP}
	slices.SortFunc(want, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
	if len(ids) != 450 || !slices.Equal(previewed, want) {
		t.Fatalf("trang xem trước: %d mẫu, preview %v, mong %v", len(ids), previewed, want)
	}

	if ps := e.byID([]uuid.UUID{draftA, gone, draftP}); len(ps) != 1 || ps[0].ID != gone || ps[0].Preview {
		t.Fatalf("theo mã, khóa thường: %+v", ps)
	}
	ps := e.byIDWith([]uuid.UUID{draftA, gone, draftP}, true)
	if len(ps) != 3 {
		t.Fatalf("theo mã, xem trước: %d mẫu", len(ps))
	}
	for _, p := range ps {
		if p.Preview != (p.ID != gone) || (p.Preview && p.Status != "draft") {
			t.Errorf("mẫu %s: status %s, preview %v", p.ID, p.Status, p.Preview)
		}
		if p.ID == draftP && p.SellerID != e.partner {
			t.Errorf("mẫu nháp của người bán thứ hai: người bán %s", p.SellerID)
		}
	}
}
