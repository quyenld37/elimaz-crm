package content_test

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"elimaz.com.vn/crm/internal/modules/content"
)

// lookRefs là dữ liệu tham chiếu cố định cho test luật bộ phối: mẫu pA (đang bán, còn hàng, danh mục catA), pB (đang
// bán, còn hàng, catB), pOut (đang bán, hết hàng), pStop (ngừng bán); ảnh photoA (của pA, đã xử lý), photoB (của pB),
// photoPending (của pA, chưa xử lý), photoOther (của mẫu ngoài bộ).
type lookRefs struct {
	now                                             time.Time
	pA, pB, pOut, pStop, pOther, catA, catB, catOld uuid.UUID
	photoA, photoB, photoPending, photoOther        uuid.UUID
	ref                                             content.LookRefData
}

func newLookRefs() lookRefs {
	r := lookRefs{now: time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)}
	for _, p := range []*uuid.UUID{
		&r.pA, &r.pB, &r.pOut, &r.pStop, &r.pOther, &r.catA, &r.catB, &r.catOld,
		&r.photoA, &r.photoB, &r.photoPending, &r.photoOther,
	} {
		*p = uuid.New()
	}
	r.ref = content.LookRefData{
		Products: map[uuid.UUID]content.LookProduct{
			r.pA:     {ID: r.pA, Name: "Sơ mi dài tay nhún vai", CategoryID: r.catA, OnSale: true, InStock: true},
			r.pB:     {ID: r.pB, Name: "Quần suông", CategoryID: r.catB, OnSale: true, InStock: true},
			r.pOut:   {ID: r.pOut, Name: "Váy hết hàng", CategoryID: r.catB, OnSale: true},
			r.pStop:  {ID: r.pStop, Name: "Áo ngừng bán", CategoryID: r.catA, InStock: true},
			r.pOther: {ID: r.pOther, Name: "Mẫu khác", CategoryID: r.catA, OnSale: true, InStock: true},
		},
		Images: []content.LookImage{
			{ProductID: r.pA, FileID: r.photoA, Ready: true},
			{ProductID: r.pB, FileID: r.photoB, Ready: true},
			{ProductID: r.pA, FileID: r.photoPending},
			{ProductID: r.pOther, FileID: r.photoOther, Ready: true},
		},
		Categories: map[uuid.UUID]content.CategoryRef{r.catA: {ID: r.catA, Name: "Áo"}, r.catB: {ID: r.catB, Name: "Quần"}},
	}
	return r
}

// look là một bộ hợp lệ, lên trang chủ: pA + pB, ảnh photoA.
func (r lookRefs) look() content.Look {
	return content.Look{
		ID: uuid.New(), OnHome: true, Name: "Đi làm ngày thường", PhotoFileID: r.photoA,
		Items: []content.LookItem{{ProductID: r.pA, X: 50, Y: 30}, {ProductID: r.pB, X: 50, Y: 70}},
	}
}

// many trả n bộ hợp lệ, home bộ đầu lên trang chủ.
func (r lookRefs) many(n, home int) []content.Look {
	out := make([]content.Look, n)
	for i := range out {
		out[i] = r.look()
		out[i].OnHome = i < home
	}
	return out
}

func TestValidateLooks(t *testing.T) {
	t.Parallel()
	r := newLookRefs()
	tp := func(t time.Time) *time.Time { return &t }
	one := func(edit func(*content.Look)) []content.Look {
		l := r.look()
		edit(&l)
		return []content.Look{l}
	}
	many := r.many
	dup := r.look()
	cases := []struct {
		name string
		list []content.Look
		want error
	}{
		{"hợp lệ", one(func(*content.Look) {}), nil},
		{"rỗng", nil, nil},
		{"500 bộ, 6 trang chủ", many(500, 6), nil},
		{"501 bộ", many(501, 0), content.ErrTooManyLooks},
		{"7 bộ trang chủ", many(7, 7), content.ErrTooManyHomeLooks},
		{"7 bộ, 6 trang chủ", many(7, 6), nil},
		{"0 món", one(func(l *content.Look) { l.Items = nil }), content.ErrLookItems},
		{"4 món", one(func(l *content.Look) {
			l.Items = append(l.Items, content.LookItem{ProductID: r.pOut, X: 1, Y: 1}, content.LookItem{ProductID: r.pStop, X: 2, Y: 2})
		}), content.ErrLookItems},
		{"3 món", one(func(l *content.Look) { l.Items = append(l.Items, content.LookItem{ProductID: r.pOut, X: 1, Y: 1}) }), nil},
		{"món trùng", one(func(l *content.Look) { l.Items[1].ProductID = r.pA }), content.ErrLookItems},
		{"món không mã", one(func(l *content.Look) { l.Items[1].ProductID = uuid.Nil }), content.ErrLookItems},
		{"chấm 120 %", one(func(l *content.Look) { l.Items[0].X = 120 }), content.ErrLookDot},
		{"chấm âm", one(func(l *content.Look) { l.Items[1].Y = -0.5 }), content.ErrLookDot},
		{"chấm NaN", one(func(l *content.Look) { l.Items[1].Y = math.NaN() }), content.ErrLookDot},
		{"chấm biên 0 và 100", one(func(l *content.Look) { l.Items[0].X, l.Items[0].Y = 0, 100 }), nil},
		{"ảnh của mẫu ngoài bộ", one(func(l *content.Look) { l.PhotoFileID = r.photoOther }), content.ErrLookPhoto},
		{"ảnh không có", one(func(l *content.Look) { l.PhotoFileID = uuid.New() }), content.ErrLookPhoto},
		{"thiếu ảnh", one(func(l *content.Look) { l.PhotoFileID = uuid.Nil }), content.ErrLookPhoto},
		{"ảnh của món thứ hai", one(func(l *content.Look) { l.PhotoFileID = r.photoB }), nil},
		{"ảnh đang xử lý vẫn lưu", one(func(l *content.Look) { l.PhotoFileID = r.photoPending }), nil},
		{"giờ ngược", one(func(l *content.Look) { l.StartsAt, l.EndsAt = tp(r.now), tp(r.now.Add(-time.Hour)) }), content.ErrLookWindow},
		{"giờ bằng nhau", one(func(l *content.Look) { l.StartsAt, l.EndsAt = tp(r.now), tp(r.now) }), content.ErrLookWindow},
		{"chỉ giờ kết thúc", one(func(l *content.Look) { l.EndsAt = tp(r.now) }), nil},
		{"mã rỗng", one(func(l *content.Look) { l.ID = uuid.Nil }), content.ErrInvalidLook},
		{"mã trùng", []content.Look{dup, dup}, content.ErrInvalidLook},
		{"link key trùng", func() []content.Look {
			a, b := r.look(), r.look()
			b.ID = uuid.MustParse(a.ID.String()[:8] + "-0000-4000-8000-000000000000")
			return []content.Look{a, b}
		}(), content.ErrInvalidLook},
		{"tên rỗng", one(func(l *content.Look) { l.Name = "" }), content.ErrInvalidLook},
		{"tên 28 ký tự có dấu", one(func(l *content.Look) { l.Name = strings.Repeat("ộ", 28) }), nil},
		{"tên 29", one(func(l *content.Look) { l.Name = strings.Repeat("a", 29) }), content.ErrInvalidLook},
		{"tiêu đề 61", one(func(l *content.Look) { l.Title = strings.Repeat("a", 61) }), content.ErrInvalidLook},
		{"dòng dẫn 91", one(func(l *content.Look) { l.Headline = strings.Repeat("a", 91) }), content.ErrInvalidLook},
		{"câu chuyện 221", one(func(l *content.Look) { l.Story = strings.Repeat("a", 221) }), content.ErrInvalidLook},
		{"câu chuyện 220", one(func(l *content.Look) { l.Story = strings.Repeat("ữ", 220) }), nil},
		{"màu 61", one(func(l *content.Look) { l.Items[0].Color = strings.Repeat("a", 61) }), content.ErrInvalidLook},
		{"cỡ 7", one(func(l *content.Look) { l.ModelSize = "XXXXXXL" }), content.ErrInvalidLook},
		{"cao 129", one(func(l *content.Look) { l.ModelHeightCM = 129 }), content.ErrInvalidLook},
		{"cao 201", one(func(l *content.Look) { l.ModelHeightCM = 201 }), content.ErrInvalidLook},
		{"cao 130, 200", []content.Look{
			func() content.Look { l := r.look(); l.ModelHeightCM = 130; return l }(),
			func() content.Look { l := r.look(); l.ModelHeightCM = 200; return l }(),
		}, nil},
		{"7 danh mục", one(func(l *content.Look) {
			for range 7 {
				l.CategoryIDs = append(l.CategoryIDs, uuid.New())
			}
		}), content.ErrInvalidLook},
	}
	for _, c := range cases {
		err := content.ValidateLooks(content.NormalizeLooks(c.list), r.ref, nil)
		if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: lỗi %v, muốn %v", c.name, err, c.want)
		}
	}
}

// Bộ đã lưu không đổi ảnh và món thì không kiểm lại ảnh (ảnh sản phẩm bị xóa sau đó không chặn lưu cả danh sách, Q2);
// đổi ảnh hay đổi món thì kiểm.
func TestValidateLooks_UnchangedPhotoNotRechecked(t *testing.T) {
	t.Parallel()
	r := newLookRefs()
	saved := r.look()
	saved.PhotoFileID = uuid.New() // ảnh đã bị xóa khỏi mẫu sau lần lưu trước
	prev := []content.Look{saved}

	same := saved
	same.Name = "Đổi tên"
	if err := content.ValidateLooks([]content.Look{same}, r.ref, prev); err != nil {
		t.Fatalf("bộ cũ giữ ảnh và món: %v", err)
	}
	swapped := saved
	swapped.Items = []content.LookItem{{ProductID: r.pA, X: 1, Y: 1}}
	if err := content.ValidateLooks([]content.Look{swapped}, r.ref, prev); !errors.Is(err, content.ErrLookPhoto) {
		t.Fatalf("đổi món: %v, muốn ErrLookPhoto", err)
	}
	newPhoto := saved
	newPhoto.PhotoFileID = r.photoOther
	if err := content.ValidateLooks([]content.Look{newPhoto}, r.ref, prev); !errors.Is(err, content.ErrLookPhoto) {
		t.Fatalf("đổi ảnh: %v, muốn ErrLookPhoto", err)
	}
}

// Chuẩn hóa: cắt khoảng trắng, chấm làm tròn 2 chữ số, danh mục bỏ trùng, mảng nil thành rỗng; không sửa bản gốc.
func TestNormalizeLooks(t *testing.T) {
	t.Parallel()
	r := newLookRefs()
	in := r.look()
	in.Name, in.Title, in.ModelSize = "  Họp  ", " Tiêu đề ", " S "
	in.Items[0].X, in.Items[0].Y, in.Items[0].Color = 33.3349, 66.666, " Đen "
	in.CategoryIDs = []uuid.UUID{r.catA, r.catB, r.catA}
	orig := in.Items[0]
	out := content.NormalizeLooks([]content.Look{in})[0]
	if out.Name != "Họp" || out.Title != "Tiêu đề" || out.ModelSize != "S" || out.Items[0].Color != "Đen" {
		t.Errorf("chữ: %+v", out)
	}
	if out.Items[0].X != 33.33 || out.Items[0].Y != 66.67 {
		t.Errorf("chấm: %v, %v", out.Items[0].X, out.Items[0].Y)
	}
	if len(out.CategoryIDs) != 2 || out.CategoryIDs[0] != r.catA || out.CategoryIDs[1] != r.catB {
		t.Errorf("danh mục: %v", out.CategoryIDs)
	}
	if in.Items[0] != orig || len(in.CategoryIDs) != 3 {
		t.Error("NormalizeLooks sửa bản gốc")
	}
	empty := content.NormalizeLooks([]content.Look{{ID: uuid.New()}})[0]
	if empty.CategoryIDs == nil || empty.Items == nil {
		t.Error("mảng nil phải thành rỗng")
	}
}

// Mỗi lý do ẩn một ca (đủ 6 lý do của website, đúng thứ tự kiểm), và ca hiện; trang link bỏ qua "disabled".
func TestLookVisibility(t *testing.T) {
	t.Parallel()
	r := newLookRefs()
	tp := func(t time.Time) *time.Time { return &t }
	mk := func(edit func(*content.Look)) content.Look {
		l := r.look()
		edit(&l)
		return l
	}
	cases := []struct {
		name, want string
		l          content.Look
		product    uuid.UUID
	}{
		{"hiện", "", mk(func(*content.Look) {}), uuid.Nil},
		{"tắt trang chủ", content.HiddenDisabled, mk(func(l *content.Look) { l.OnHome = false; l.Items[0].ProductID = r.pStop }), uuid.Nil},
		{"chưa tới giờ", content.HiddenScheduled, mk(func(l *content.Look) { l.StartsAt = tp(r.now.Add(time.Second)) }), uuid.Nil},
		{"đúng giờ bắt đầu", "", mk(func(l *content.Look) { l.StartsAt = tp(r.now) }), uuid.Nil},
		{"hết giờ", content.HiddenExpired, mk(func(l *content.Look) { l.EndsAt = tp(r.now) }), uuid.Nil},
		{"món ngừng bán", content.HiddenProductMissing, mk(func(l *content.Look) { l.Items[1].ProductID = r.pStop }), r.pStop},
		{"món không có", content.HiddenProductMissing, mk(func(l *content.Look) { l.Items = append(l.Items, content.LookItem{ProductID: uuid.Max}) }), uuid.Max},
		{"món hết hàng", content.LookHiddenSoldOut, mk(func(l *content.Look) { l.Items[1].ProductID = r.pOut }), r.pOut},
		{"ngừng bán trước hết hàng", content.HiddenProductMissing, mk(func(l *content.Look) {
			l.Items = []content.LookItem{{ProductID: r.pOut}, {ProductID: r.pStop}}
		}), r.pStop},
		{"ảnh chưa xử lý", content.LookHiddenPhotoMissing, mk(func(l *content.Look) { l.PhotoFileID = r.photoPending }), uuid.Nil},
		{"ảnh không còn của món", content.LookHiddenPhotoMissing, mk(func(l *content.Look) { l.PhotoFileID = r.photoOther }), uuid.Nil},
		{"ảnh của món thứ hai", "", mk(func(l *content.Look) { l.PhotoFileID = r.photoB }), uuid.Nil},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		shown, reason := content.LookVisibility(c.l, r.ref, r.now)
		if shown != (c.want == "") || reason != c.want {
			t.Errorf("%s: hiện %v lý do %q, muốn %q", c.name, shown, reason, c.want)
		}
		st := content.LookStatuses([]content.Look{c.l}, r.ref, r.now)[0]
		if st.Shown != shown || st.Reason != reason || st.ProductID != c.product || st.ID != c.l.ID {
			t.Errorf("%s: LookStatuses %+v, muốn món %s", c.name, st, c.product)
		}
		seen[reason] = true
	}
	for _, want := range content.LookHiddenReasons() {
		if !seen[want] {
			t.Errorf("thiếu ca cho lý do %q", want)
		}
	}
	if n := len(content.LookHiddenReasons()); n != 6 {
		t.Errorf("website có 6 lý do ẩn, có %d", n)
	}
	off := mk(func(l *content.Look) { l.OnHome = false })
	if shown, _ := content.LookVisibility(content.ForLink(off), r.ref, r.now); !shown {
		t.Error("trang link phải hiện bộ không lên trang chủ")
	}
}

// Tiêu đề tự động, dòng người mẫu, danh mục "Bán chạy cho dịp này" như website.
func TestLookDerived(t *testing.T) {
	t.Parallel()
	r := newLookRefs()
	l := r.look()
	if got := content.LookAutoTitle(l, r.ref); got != "Sơ mi dài tay nhún vai, quần suông" {
		t.Errorf("autoTitle = %q", got)
	}
	l.Items = append(l.Items, content.LookItem{ProductID: uuid.New()})
	if got := content.LookAutoTitle(l, r.ref); got != "Sơ mi dài tay nhún vai, quần suông" {
		t.Errorf("autoTitle bỏ món không có: %q", got)
	}
	lines := []struct {
		h    int
		s    string
		want string
	}{{162, "S", "Mẫu 1m62 · size S"}, {162, "", "Mẫu 1m62"}, {0, "M", "Mẫu mặc size M"}, {0, "", ""}, {200, "", "Mẫu 2m00"}, {130, "", "Mẫu 1m30"}}
	for _, c := range lines {
		if got := content.LookModelLine(c.h, c.s); got != c.want {
			t.Errorf("modelLine(%d, %q) = %q, muốn %q", c.h, c.s, got, c.want)
		}
	}
	if got := content.LookCategories(r.look(), r.ref); len(got) != 2 || got[0] != r.catA || got[1] != r.catB {
		t.Errorf("danh mục mặc định = %v, muốn danh mục của các món", got)
	}
	picked := r.look()
	picked.CategoryIDs = []uuid.UUID{r.catOld, r.catB}
	if got := content.LookCategories(picked, r.ref); len(got) != 1 || got[0] != r.catB {
		t.Errorf("danh mục chọn = %v, muốn bỏ danh mục không còn dùng", got)
	}
}

// LookSlug, LookLinkKey, LookPath đúng look-link.ts của website (bảng do website sinh:
// testdata/contracts/web-look-links.json).
func TestLookLinks_MatchWebsite(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../testdata/contracts/web-look-links.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct{ ID, Name, Slug, LinkKey, Path string }
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) < 12 {
		t.Fatalf("bảng liên kết có %d dòng", len(rows))
	}
	for _, r := range rows {
		id := uuid.MustParse(r.ID)
		if got := content.LookSlug(r.Name); got != r.Slug {
			t.Errorf("slug %q: %q, website %q", r.Name, got, r.Slug)
		}
		if got := content.LookLinkKey(id); got != r.LinkKey {
			t.Errorf("link key %s: %q, website %q", r.ID, got, r.LinkKey)
		}
		if got := content.LookPath(id, r.Name); got != r.Path {
			t.Errorf("đường dẫn %q: %q, website %q", r.Name, got, r.Path)
		}
	}
}

// Task 488: xem trước bỏ bộ chưa đủ kèm lỗi lưu sẽ trả, giữ thứ tự các bộ đủ; lỗi của cả danh sách vẫn trả lỗi.
func TestPreviewLooks_SkipsIncomplete(t *testing.T) {
	t.Parallel()
	r := newLookRefs()
	good1, good2 := r.look(), r.look()
	noName, noItems, noPhoto, badDot := r.look(), r.look(), r.look(), r.look()
	noName.Name = ""
	noItems.Items = nil
	noPhoto.PhotoFileID = uuid.Nil
	badDot.Items[0].X = 120
	ok, skipped, err := content.PreviewLooks([]content.Look{noName, good1, noItems, noPhoto, good2, badDot}, r.ref, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ok) != 2 || ok[0].ID != good1.ID || ok[1].ID != good2.ID {
		t.Errorf("bộ đủ: %+v", ok)
	}
	want := []struct {
		id  uuid.UUID
		err error
	}{
		{noName.ID, content.ErrInvalidLook},
		{noItems.ID, content.ErrLookItems},
		{noPhoto.ID, content.ErrLookPhoto},
		{badDot.ID, content.ErrLookDot},
	}
	if len(skipped) != len(want) {
		t.Fatalf("bỏ qua: %+v", skipped)
	}
	for i, w := range want {
		if skipped[i].ID != w.id || !errors.Is(skipped[i].Err, w.err) {
			t.Errorf("bỏ qua %d: %+v, muốn %v", i, skipped[i], w.err)
		}
	}
	if _, _, err := content.PreviewLooks(r.many(7, 7), r.ref, nil); !errors.Is(err, content.ErrTooManyHomeLooks) {
		t.Errorf("7 bộ trang chủ: %v", err)
	}
	dup := r.look()
	if _, _, err := content.PreviewLooks([]content.Look{dup, dup}, r.ref, nil); !errors.Is(err, content.ErrInvalidLook) {
		t.Errorf("mã trùng: %v", err)
	}
	ok, skipped, err = content.PreviewLooks(nil, r.ref, nil)
	if err != nil || len(ok) != 0 || skipped == nil || len(skipped) != 0 {
		t.Errorf("rỗng: %v %v %v", ok, skipped, err)
	}
}
