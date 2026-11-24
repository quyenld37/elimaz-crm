package content_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/modules/content"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
)

// inApp chạy fn bằng crm_app trong ngữ cảnh tenant của e và trả kết quả của nó.
//
//nolint:ireturn // T là kiểu cụ thể của bên gọi
func inApp[T any](e *env, fn func(tx pgx.Tx) (T, error)) (T, error) {
	var out T
	err := e.app(func(tx pgx.Tx) (err error) {
		out, err = fn(tx)
		return err
	})
	return out, err
}

func (e *env) saveLooks(list []content.Look, rev int) (content.LookChange, error) {
	a := content.Actor{TenantID: e.tenant, UserID: e.user}
	return inApp(e, func(tx pgx.Tx) (content.LookChange, error) {
		return writer().SaveLooks(e.t.Context(), tx, list, rev, a, time.Now())
	})
}

func (e *env) looks() content.LookSet {
	e.t.Helper()
	s, err := inApp(e, func(tx pgx.Tx) (content.LookSet, error) { return content.GetLooks(e.t.Context(), tx) })
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

// tenantB trả env của tenant B (cùng database) có nhân viên và người bán riêng.
func (e *env) tenantB() *env {
	e.t.Helper()
	tn := e.staff.Tenant(e.t, "B")
	b := &env{t: e.t, staff: e.staff, tenant: tn, user: e.staff.User(e.t, tn, "nv@b.test", "active")}
	e.staff.Exec(e.t, tn, func(tx pgx.Tx) error { b.seller = catalogtest.Seller(e.t, tx, tn, "ELIMAZ", "own"); return nil })
	return b
}

// piece tạo mẫu đang bán (giá 250.000, danh mục cat) kèm một ảnh sản phẩm đã xử lý; trả mã mẫu và mã tệp ảnh.
func (e *env) piece(cat uuid.UUID) (product, photo uuid.UUID) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		n := int(e.seq.Add(1))
		product = catalogtest.Product(e.t, tx, e.tenant, e.seller, fmt.Sprintf("LK%04d", n))
		catalogtest.Variant(e.t, tx, e.tenant, e.seller, product, n, 250000)
		if cat != uuid.Nil {
			if _, err := tx.Exec(e.t.Context(), `UPDATE catalog.products SET category_id = $1 WHERE id = $2`, cat, product); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		photo = filestest.Image(e.t, tx, e.tenant, "product_image")
		_, err := tx.Exec(e.t.Context(), `INSERT INTO catalog.product_images (tenant_id, product_id, file_id, color, position)
			VALUES ($1, $2, $3, 'Đen', 1)`, e.tenant, product, photo)
		return err //nolint:wrapcheck // test
	})
	return product, photo
}

// ownerExec chạy câu sql (một tham số id) bằng chủ bảng: dựng tình huống mà crm_app không được làm.
func (e *env) ownerExec(sql string, id uuid.UUID) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error { return tagErr(tx.Exec(e.t.Context(), sql, id)) })
}

func tagErr(_ pgconn.CommandTag, err error) error { return err }

func simpleLook(home bool, photo uuid.UUID, products ...uuid.UUID) content.Look {
	l := content.Look{ID: uuid.New(), OnHome: home, Name: "Đi làm", PhotoFileID: photo}
	for i, p := range products {
		l.Items = append(l.Items, content.LookItem{ProductID: p, X: 50, Y: float64(30 + 30*i)})
	}
	return l
}

// Lưu thay cả danh sách theo revision: chưa có → rỗng revision 0; lưu đủ trường (chuẩn hóa: cắt khoảng trắng, chấm làm
// tròn 2 chữ số) và đọc lại đúng thứ tự bộ, thứ tự món; revision cũ → ErrRevisionConflict; đổi thứ tự; xóa hết; mỗi lần
// lưu một sự kiện content.looks.changed (Seq = revision mới).
func TestSaveLooks_ReplaceByRevision(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if s := e.looks(); s.Revision != 0 || len(s.Looks) != 0 {
		t.Fatalf("chưa lưu: %+v", s)
	}
	cat := e.category("ao", uuid.Nil)
	pA, photoA := e.piece(cat)
	pB, _ := e.piece(uuid.Nil)
	start, end := time.Now().Add(-time.Hour).UTC().Truncate(time.Second), time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	full := content.Look{
		ID: uuid.New(), OnHome: true, Name: " Họp quan trọng ", Title: "Tiêu đề", Headline: "Sáng thứ Hai", Story: "Vì sao",
		PhotoFileID: photoA, CategoryIDs: []uuid.UUID{cat}, ModelHeightCM: 162, ModelSize: "S", StartsAt: &start, EndsAt: &end,
		Items: []content.LookItem{{ProductID: pB, Color: " Kem ", X: 12.345, Y: 3}, {ProductID: pA, X: 97, Y: 66.666}},
	}
	second := simpleLook(false, photoA, pA)
	switch ch, err := e.saveLooks([]content.Look{full, second}, 0); {
	case err != nil:
		t.Fatal(err)
	case ch.Before.Revision != 0, ch.After.Revision != 1, ch.After.UpdatedBy == nil, *ch.After.UpdatedBy != e.user:
		t.Fatalf("lần lưu: %+v", ch)
	}
	s := e.looks()
	if s.Revision != 1 || len(s.Looks) != 2 || s.Looks[0].ID != full.ID || s.Looks[1].ID != second.ID {
		t.Fatalf("sau lưu: %+v", s)
	}
	checkFullLook(t, s.Looks[0], full, photoA, cat)
	if s.Looks[1].OnHome || s.Looks[1].ModelHeightCM != 0 || s.Looks[1].StartsAt != nil || s.Looks[1].CategoryIDs == nil {
		t.Fatalf("bộ thứ hai: %+v", s.Looks[1])
	}
	if _, err := e.saveLooks(nil, 0); !errors.Is(err, content.ErrRevisionConflict) {
		t.Fatalf("revision cũ: %v", err)
	}
	if _, err := e.saveLooks([]content.Look{second, full}, 1); err != nil {
		t.Fatal(err)
	}
	if s := e.looks(); s.Revision != 2 || s.Looks[0].ID != second.ID {
		t.Fatalf("đổi thứ tự: %+v", s)
	}
	if _, err := e.saveLooks(nil, 2); err != nil {
		t.Fatal(err)
	}
	if s := e.looks(); s.Revision != 3 || len(s.Looks) != 0 {
		t.Fatalf("xóa hết: %+v", s)
	}
	for seq := 1; seq <= 3; seq++ {
		if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM platform.domain_events WHERE topic = $1
			AND aggregate_type = 'home_looks' AND aggregate_id = $2 AND seq = $3`, content.TopicLooksChanged,
			e.tenant.String(), seq); n != 1 {
			t.Fatalf("sự kiện seq %d: %d", seq, n)
		}
	}
}

// checkFullLook: bộ đủ trường đọc lại đúng bản đã chuẩn hóa (tên cắt khoảng trắng, màu cắt, chấm làm tròn 2 chữ số).
func checkFullLook(t *testing.T, got, sent content.Look, photo, cat uuid.UUID) {
	t.Helper()
	if got.Name != "Họp quan trọng" || got.Title != "Tiêu đề" || got.Headline != "Sáng thứ Hai" || got.Story != "Vì sao" ||
		!got.OnHome || got.PhotoFileID != photo || len(got.CategoryIDs) != 1 || got.CategoryIDs[0] != cat ||
		got.ModelHeightCM != 162 || got.ModelSize != "S" || !got.StartsAt.Equal(*sent.StartsAt) || !got.EndsAt.Equal(*sent.EndsAt) {
		t.Fatalf("bộ đọc lại: %+v", got)
	}
	want := []content.LookItem{
		{ProductID: sent.Items[0].ProductID, Color: "Kem", X: 12.35, Y: 3}, {ProductID: sent.Items[1].ProductID, X: 97, Y: 66.67},
	}
	if len(got.Items) != 2 || got.Items[0] != want[0] || got.Items[1] != want[1] {
		t.Fatalf("món đọc lại: %+v, muốn %+v", got.Items, want)
	}
}

// Kiểm khi lưu trên dữ liệu thật: ảnh của mẫu không có trong bộ, ảnh của mẫu tenant khác → ErrLookPhoto; 7 bộ trang
// chủ → ErrTooManyHomeLooks; 4 món → ErrLookItems; lần lưu bị từ chối không ghi gì. Bộ đã lưu mà ảnh bị xóa khỏi mẫu
// sau đó vẫn lưu lại được (Q2), đổi sang ảnh không thuộc món thì không.
func TestSaveLooks_ChecksReferences(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	pA, photoA := e.piece(uuid.Nil)
	pB, photoB := e.piece(uuid.Nil)
	pX, photoX := e.tenantB().piece(uuid.Nil)
	seven := make([]content.Look, 7)
	for i := range seven {
		seven[i] = simpleLook(true, photoA, pA)
	}
	for name, c := range map[string]struct {
		list []content.Look
		want error
	}{
		"ảnh của mẫu ngoài bộ": {[]content.Look{simpleLook(true, photoB, pA)}, content.ErrLookPhoto},
		"ảnh tenant khác":      {[]content.Look{simpleLook(true, photoX, pX)}, content.ErrLookPhoto},
		"7 bộ trang chủ":       {seven, content.ErrTooManyHomeLooks},
		"4 món":                {[]content.Look{simpleLook(true, photoA, pA, pB, uuid.New(), uuid.New())}, content.ErrLookItems},
	} {
		if _, err := e.saveLooks(c.list, 0); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v, muốn %v", name, err, c.want)
		}
	}
	if s := e.looks(); s.Revision != 0 || len(s.Looks) != 0 {
		t.Fatalf("lần lưu hỏng đã ghi: %+v", s)
	}
	kept := simpleLook(true, photoA, pA, pB)
	if _, err := e.saveLooks([]content.Look{kept}, 0); err != nil {
		t.Fatal(err)
	}
	e.ownerExec(`UPDATE catalog.product_images SET deleted_at = now() WHERE file_id = $1`, photoA)
	kept.Name = "Đổi tên"
	if _, err := e.saveLooks([]content.Look{kept}, 1); err != nil {
		t.Fatalf("bộ cũ giữ ảnh đã xóa: %v", err)
	}
	kept.PhotoFileID = photoX
	if _, err := e.saveLooks([]content.Look{kept}, 2); !errors.Is(err, content.ErrLookPhoto) {
		t.Fatalf("đổi ảnh sang ảnh không thuộc món: %v", err)
	}
}

// 6.1: đang có 5 bộ trang chủ, hai lần lưu song song cùng revision, mỗi lần bật thêm một bộ khác → đúng một lần thành
// công, lần kia ErrRevisionConflict; danh sách cuối có 6 bộ trang chủ.
func TestSaveLooks_ConcurrentSameRevision(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	pA, photoA := e.piece(uuid.Nil)
	base := make([]content.Look, 7)
	for i := range base {
		base[i] = simpleLook(i < 5, photoA, pA)
	}
	if _, err := e.saveLooks(base, 0); err != nil {
		t.Fatal(err)
	}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for k := range 2 {
		list := append([]content.Look(nil), base...)
		list[5+k].OnHome = true
		wg.Go(func() { _, errs[k] = e.saveLooks(list, 1) })
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, content.ErrRevisionConflict):
			conflict++
		default:
			t.Fatalf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("thành công %d, 409 %d; muốn 1 và 1", ok, conflict)
	}
	s := e.looks()
	home := 0
	for _, l := range s.Looks {
		if l.OnHome {
			home++
		}
	}
	if s.Revision != 2 || home != 6 {
		t.Fatalf("sau hai lần lưu: revision %d, %d bộ trang chủ", s.Revision, home)
	}
}

// LoadLookRefData trên dữ liệu thật: món đang bán / ngừng (status) / đã xóa / chưa giá; còn hàng theo danh sách bên gọi;
// ảnh đã xử lý có đường dẫn, ảnh đã xóa khỏi mẫu không còn; danh mục đang dùng. Lý do ẩn tính từ đó.
func TestLoadLookRefData_Visibility(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	cat := e.category("ao", uuid.Nil)
	pA, photoA := e.piece(cat)
	pB, photoB := e.piece(uuid.Nil)
	stopped, _ := e.piece(uuid.Nil)
	gone, _ := e.piece(uuid.Nil)
	free, _ := e.piece(uuid.Nil)
	lostPhoto, lost := e.piece(uuid.Nil)
	e.ownerExec(`UPDATE catalog.products SET status = 'discontinued' WHERE id = $1`, stopped)
	e.ownerExec(`UPDATE catalog.products SET deleted_at = now() WHERE id = $1`, gone)
	e.ownerExec(`UPDATE catalog.variants SET price = 0 WHERE product_id = $1`, free)
	e.ownerExec(`UPDATE catalog.product_images SET deleted_at = now() WHERE file_id = $1`, lost)
	inStock := []uuid.UUID{pA, pB, stopped, gone, free, lostPhoto}
	cases := []struct {
		name, reason string
		l            content.Look
		stock        []uuid.UUID
	}{
		{"hiện", "", simpleLook(true, photoA, pA, pB), inStock},
		{"ảnh của món thứ hai", "", simpleLook(true, photoB, pA, pB), inStock},
		{"món ngừng bán", content.HiddenProductMissing, simpleLook(true, photoA, pA, stopped), inStock},
		{"món đã xóa", content.HiddenProductMissing, simpleLook(true, photoA, pA, gone), inStock},
		{"món chưa giá", content.HiddenProductMissing, simpleLook(true, photoA, pA, free), inStock},
		{"món hết hàng", content.LookHiddenSoldOut, simpleLook(true, photoA, pA, pB), []uuid.UUID{pA}},
		{"ảnh bị xóa khỏi mẫu", content.LookHiddenPhotoMissing, simpleLook(true, lost, lostPhoto), inStock},
	}
	view := catalog.ImageView{BaseURL: "https://img.test", Preset: 1}
	for _, c := range cases {
		var ref content.LookRefData
		if err := e.app(func(tx pgx.Tx) error {
			var err error
			ref, err = content.LoadLookRefData(t.Context(), tx, []content.Look{c.l}, c.stock, view)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		if _, reason := content.LookVisibility(c.l, ref, time.Now()); reason != c.reason {
			t.Errorf("%s: lý do %q, muốn %q", c.name, reason, c.reason)
		}
		if c.name == "hiện" {
			checkRef(t, ref, cat, pA, photoA)
		}
	}
}

// checkRef: danh mục đang dùng có cat; ảnh photoA của pA đã xử lý, có đường dẫn theo FILES_PUBLIC_BASE_URL, màu Đen.
func checkRef(t *testing.T, ref content.LookRefData, cat, pA, photoA uuid.UUID) {
	t.Helper()
	if _, ok := ref.Categories[cat]; !ok {
		t.Errorf("thiếu danh mục %s", cat)
	}
	if p := ref.Products[pA]; p.Code == "" || p.Name == "" || p.CategoryID != cat || !p.OnSale || !p.InStock {
		t.Errorf("mẫu A: %+v", p)
	}
	for _, im := range ref.Images {
		if im.FileID != photoA {
			continue
		}
		if im.ProductID != pA || !im.Ready || len(im.Variants) == 0 || im.Color == nil || *im.Color != "Đen" ||
			im.Variants[0].URL[:16] != "https://img.test" {
			t.Errorf("ảnh A: %+v", im)
		}
		return
	}
	t.Errorf("thiếu ảnh %s", photoA)
}

// Tenant khác không thấy, không đụng danh sách của nhau.
func TestLooks_OtherTenant(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	pA, photoA := e.piece(uuid.Nil)
	if _, err := e.saveLooks([]content.Look{simpleLook(true, photoA, pA)}, 0); err != nil {
		t.Fatal(err)
	}
	b := e.tenantB()
	if s := b.looks(); len(s.Looks) > 0 || s.Revision > 0 {
		t.Fatalf("tenant B thấy: %+v", s)
	}
	if _, err := b.saveLooks([]content.Look{simpleLook(true, photoA, pA)}, 0); !errors.Is(err, content.ErrLookPhoto) {
		t.Fatalf("tenant B dùng ảnh của A: %v", err)
	}
	if _, err := b.saveLooks(nil, 0); err != nil {
		t.Fatal(err)
	}
	if a := e.looks(); len(a.Looks) != 1 || a.Revision != 1 {
		t.Fatalf("tenant A bị đổi: %+v", a)
	}
}
