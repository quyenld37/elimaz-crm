package catalog_test

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
)

const testBaseURL = "https://anh.elimaz.test"

func testView() catalog.ImageView { return catalog.ImageView{BaseURL: testBaseURL, Preset: 1} }

// image tạo tệp ảnh sản phẩm ready (12 biến thể v1) của tenant e.tenant.
func (e *catalogEnv) image() uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.must(func(tx pgx.Tx) error {
		id = filestest.Image(e.t, tx, e.tenant, "product_image")
		return nil
	})
	return id
}

func (e *catalogEnv) must(fn func(tx pgx.Tx) error) {
	e.t.Helper()
	if err := e.app(fn); err != nil {
		e.t.Fatal(err)
	}
}

func (e *catalogEnv) version(id uuid.UUID) int64 {
	e.t.Helper()
	p, err := e.get(id)
	if err != nil {
		e.t.Fatal(err)
	}
	return p.Version
}

func (e *catalogEnv) attach(productID, fileID uuid.UUID, color *string) (catalog.Image, error) {
	var img catalog.Image
	err := e.app(func(tx pgx.Tx) error {
		p, err := catalog.GetProduct(e.t.Context(), tx, e.seller, productID)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		img, err = e.writer().AttachImage(e.t.Context(), tx, productID, p.Version, fileID, color, e.actor)
		return err //nolint:wrapcheck // test
	})
	return img, err
}

func (e *catalogEnv) mustAttach(productID, fileID uuid.UUID, color *string) catalog.Image {
	e.t.Helper()
	img, err := e.attach(productID, fileID, color)
	if err != nil {
		e.t.Fatalf("AttachImage: %v", err)
	}
	return img
}

func (e *catalogEnv) images(productID uuid.UUID) []catalog.Image {
	e.t.Helper()
	var out []catalog.Image
	e.must(func(tx pgx.Tx) error {
		var err error
		out, err = catalog.ProductImages(e.t.Context(), tx, e.seller, productID, testView())
		return err //nolint:wrapcheck // test
	})
	return out
}

func (e *catalogEnv) reorder(productID uuid.UUID, version int64, ids []uuid.UUID) error {
	return e.app(func(tx pgx.Tx) error {
		return e.writer().ReorderImages(e.t.Context(), tx, productID, version, ids, e.actor)
	})
}

func (e *catalogEnv) deleteImage(productID, imageID uuid.UUID) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := e.writer().DeleteImage(e.t.Context(), tx, productID, imageID, e.actor)
		return err //nolint:wrapcheck // test
	})
}

func (e *catalogEnv) restoreImage(productID, imageID uuid.UUID) error {
	return e.app(func(tx pgx.Tx) error {
		_, err := e.writer().RestoreImage(e.t.Context(), tx, productID, imageID, e.actor)
		return err //nolint:wrapcheck // test
	})
}

func (e *catalogEnv) viewRows(productID uuid.UUID) int {
	e.t.Helper()
	var n int
	e.must(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), "SELECT count(*) FROM catalog.product_images_v WHERE product_id = $1", productID).Scan(&n)
	})
	return n
}

// checkReady: mọi ảnh ready, 12 đường dẫn bắt đầu testBaseURL/img/…/v1/, vị trí 1..n.
func checkReady(t *testing.T, imgs []catalog.Image) {
	t.Helper()
	for i, img := range imgs {
		if img.Status != "ready" || len(img.Variants) != 12 || img.Position != i+1 {
			t.Errorf("ảnh %d sai: status %s, %d biến thể, vị trí %d", i, img.Status, len(img.Variants), img.Position)
		}
		for _, v := range img.Variants {
			if !strings.HasPrefix(v.URL, testBaseURL+"/img/") || !strings.Contains(v.URL, "/v1/") {
				t.Errorf("đường dẫn sai: %s", v.URL)
			}
		}
	}
}

// withTwoImages: môi trường mới, mẫu DAM01 có hai ảnh không màu.
func withTwoImages(t *testing.T) (*catalogEnv, catalog.Product, catalog.Image, catalog.Image) {
	t.Helper()
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	return e, p, e.mustAttach(p.ID, e.image(), nil), e.mustAttach(p.ID, e.image(), nil)
}

func imageIDs(imgs []catalog.Image) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(imgs))
	for _, i := range imgs {
		out = append(out, i.ID)
	}
	return out
}

// TestAttachImage_OrderAndColor (KH02): mẫu 2 màu, gắn 3 ảnh (một ảnh gắn màu "Đỏ") → đúng thứ tự, mỗi ảnh 12 đường
// dẫn; sắp lại → thứ tự mới; product_images_v có 3 dòng; mỗi lần đổi tăng version mẫu, phát product.updated, audit.
func TestAttachImage_OrderAndColor(t *testing.T) {
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	f1, f2, f3 := e.image(), e.image(), e.image()
	i1 := e.mustAttach(p.ID, f1, nil)
	i2 := e.mustAttach(p.ID, f2, ptr("Đỏ"))
	i3 := e.mustAttach(p.ID, f3, nil)
	got := e.images(p.ID)
	if !slices.Equal(imageIDs(got), []uuid.UUID{i1.ID, i2.ID, i3.ID}) {
		t.Fatalf("thứ tự sau khi gắn sai: %v", imageIDs(got))
	}
	checkReady(t, got)
	if got[1].Color == nil || *got[1].Color != "Đỏ" || got[0].Color != nil || got[1].FileID != f2 {
		t.Errorf("màu hoặc tệp của ảnh sai: %+v", got)
	}
	if n := e.viewRows(p.ID); n != 3 {
		t.Errorf("product_images_v: mong 3 dòng, có %d", n)
	}
	v := e.version(p.ID)
	if v != 4 {
		t.Errorf("version mẫu sau 3 lần gắn: mong 4, có %d", v)
	}
	if err := e.reorder(p.ID, v, []uuid.UUID{i3.ID, i1.ID, i2.ID}); err != nil {
		t.Fatalf("ReorderImages: %v", err)
	}
	if ids := imageIDs(e.images(p.ID)); !slices.Equal(ids, []uuid.UUID{i3.ID, i1.ID, i2.ID}) {
		t.Errorf("thứ tự sau khi sắp lại sai: %v", ids)
	}
	if n := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = 'product.updated' AND aggregate_id = $1", p.ID.String()); n != 5 {
		t.Errorf("product.updated: mong 5 (tạo + 3 gắn + sắp), có %d", n)
	}
	if n := e.count("SELECT count(*) FROM identity.audit_logs WHERE action LIKE 'catalog.image.%' AND entity_id = $1", p.ID.String()); n != 4 {
		t.Errorf("audit ảnh: mong 4, có %d", n)
	}
	if err := e.reorder(p.ID, v, []uuid.UUID{i1.ID, i2.ID, i3.ID}); !errors.Is(err, catalog.ErrVersionConflict) {
		t.Errorf("sắp lại bằng version cũ: mong ErrVersionConflict, có %v", err)
	}
}

func TestReorderImages_Invalid(t *testing.T) {
	e, p, a, b := withTwoImages(t)
	for name, ids := range map[string][]uuid.UUID{
		"thiếu":    {a.ID},
		"thừa":     {a.ID, b.ID, uuid.New()},
		"trùng":    {a.ID, a.ID},
		"mã lạ":    {a.ID, uuid.New()},
		"rỗng":     {},
		"trùng đủ": {a.ID, b.ID, b.ID},
		"đảo + lạ": {b.ID, uuid.New()},
	} {
		if err := e.reorder(p.ID, e.version(p.ID), ids); !errors.Is(err, catalog.ErrInvalidInput) {
			t.Errorf("%s: mong ErrInvalidInput, có %v", name, err)
		}
	}
	if err := e.deleteImage(p.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.reorder(p.ID, e.version(p.ID), []uuid.UUID{b.ID, a.ID}); !errors.Is(err, catalog.ErrInvalidInput) {
		t.Errorf("sắp lại kèm ảnh đã xóa: mong ErrInvalidInput, có %v", err)
	}
	if err := e.reorder(p.ID, e.version(p.ID), []uuid.UUID{b.ID}); err != nil {
		t.Errorf("sắp lại đúng ảnh còn lại: %v", err)
	}
}

// Gắn tệp chưa xử lý xong → ErrFileNotReady; tệp của tenant khác → ErrNotFound; tệp mục đích catalog_import →
// ErrFileNotReady; màu không có ở biến thể → ErrInvalidInput; mẫu của người bán khác hay không có → ErrNotFound.
func TestAttachImage_Refused(t *testing.T) {
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	var processing, imported, deleted uuid.UUID
	e.must(func(tx pgx.Tx) error {
		processing = filestest.Uploaded(t, tx, e.tenant, "product_image")
		imported = filestest.Image(t, tx, e.tenant, "catalog_import")
		return nil
	})
	deleted = e.image()
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE platform.files SET deleted_at = now() WHERE id = $1", deleted)
		return err //nolint:wrapcheck // test
	})
	other := e.staff.Tenant(t, "B")
	var foreign uuid.UUID
	e.staff.Exec(t, other, func(tx pgx.Tx) error {
		foreign = filestest.Image(t, tx, other, "product_image")
		return nil
	})
	cases := []struct {
		name  string
		file  uuid.UUID
		color *string
		want  error
	}{
		{"tệp đang xử lý", processing, nil, catalog.ErrFileNotReady},
		{"tệp nhập Excel", imported, nil, catalog.ErrFileNotReady},
		{"tệp tenant khác", foreign, nil, catalog.ErrNotFound},
		{"tệp không có", uuid.New(), nil, catalog.ErrNotFound},
		{"tệp đã xóa", deleted, nil, catalog.ErrNotFound},
		{"màu không có", e.image(), ptr("Tím"), catalog.ErrInvalidInput},
		{"màu rỗng", e.image(), ptr("  "), catalog.ErrInvalidInput},
	}
	for _, c := range cases {
		if _, err := e.attach(p.ID, c.file, c.color); !errors.Is(err, c.want) {
			t.Errorf("%s: mong %v, có %v", c.name, c.want, err)
		}
	}
	if n := e.viewRows(p.ID); n != 0 {
		t.Errorf("gắn bị từ chối mà vẫn có %d dòng", n)
	}
	// Mẫu của người bán khác (ADR-29) và mẫu không có.
	err = e.app(func(tx pgx.Tx) error {
		_, err := e.writer().AttachImage(t.Context(), tx, uuid.New(), 1, e.image(), nil, e.actor)
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("mẫu không có: mong ErrNotFound, có %v", err)
	}
	var imgs []catalog.Image
	err = e.app(func(tx pgx.Tx) error {
		var err error
		imgs, err = catalog.ProductImages(t.Context(), tx, e.partnerSeller(), p.ID, testView())
		return err //nolint:wrapcheck // test
	})
	if !errors.Is(err, catalog.ErrNotFound) || imgs != nil {
		t.Errorf("ảnh của mẫu người bán khác: mong ErrNotFound, có %v", err)
	}
}

// Gắn lại tệp đang hiện → ErrAlreadyExists; gắn lại tệp đã xóa mềm → hồi lại đúng dòng đó ở cuối với màu mới (Q2).
func TestAttachImage_Again(t *testing.T) {
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	f := e.image()
	a := e.mustAttach(p.ID, f, nil)
	b := e.mustAttach(p.ID, e.image(), nil)
	if _, err := e.attach(p.ID, f, nil); !errors.Is(err, catalog.ErrAlreadyExists) {
		t.Errorf("gắn lại tệp đang hiện: mong ErrAlreadyExists, có %v", err)
	}
	if err := e.deleteImage(p.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	again := e.mustAttach(p.ID, f, ptr("Đen"))
	if again.ID != a.ID {
		t.Errorf("gắn lại tệp đã xóa phải hồi dòng cũ %s, có %s", a.ID, again.ID)
	}
	got := e.images(p.ID)
	if !slices.Equal(imageIDs(got), []uuid.UUID{b.ID, a.ID}) || got[1].Color == nil || *got[1].Color != "Đen" {
		t.Errorf("sau khi gắn lại: %+v", got)
	}
}

// TestImageSoftDeleteRestore (TP06): xóa → còn 2 (cả view); khôi phục → 3, về vị trí cũ; quá 30 ngày → ErrRestoreExpired;
// 2 dòng audit (xóa, khôi phục); xóa lại ảnh đã xóa, khôi phục ảnh đang hiện → không đổi gì.
func TestImageSoftDeleteRestore(t *testing.T) {
	e := newCatalogEnv(t)
	p, err := e.create(dam01(e.seller))
	if err != nil {
		t.Fatal(err)
	}
	i1 := e.mustAttach(p.ID, e.image(), nil)
	i2 := e.mustAttach(p.ID, e.image(), nil)
	i3 := e.mustAttach(p.ID, e.image(), nil)
	if err := e.deleteImage(p.ID, i2.ID); err != nil {
		t.Fatalf("DeleteImage: %v", err)
	}
	if ids := imageIDs(e.images(p.ID)); !slices.Equal(ids, []uuid.UUID{i1.ID, i3.ID}) || e.viewRows(p.ID) != 2 {
		t.Fatalf("sau khi xóa: %v, view %d dòng", ids, e.viewRows(p.ID))
	}
	v := e.version(p.ID)
	if err := e.deleteImage(p.ID, i2.ID); err != nil || e.version(p.ID) != v {
		t.Errorf("xóa lại ảnh đã xóa phải không đổi gì: %v, version %d→%d", err, v, e.version(p.ID))
	}
	if err := e.restoreImage(p.ID, i2.ID); err != nil {
		t.Fatalf("RestoreImage: %v", err)
	}
	if ids := imageIDs(e.images(p.ID)); !slices.Equal(ids, []uuid.UUID{i1.ID, i2.ID, i3.ID}) || e.viewRows(p.ID) != 3 {
		t.Fatalf("sau khi khôi phục: %v", ids)
	}
	v = e.version(p.ID)
	if err := e.restoreImage(p.ID, i2.ID); err != nil || e.version(p.ID) != v {
		t.Errorf("khôi phục ảnh đang hiện phải không đổi gì: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action IN ('catalog.image.delete', 'catalog.image.restore')
		AND entity_id = $1`, p.ID.String()); n != 2 {
		t.Errorf("audit xóa và khôi phục: mong 2, có %d", n)
	}
	if err := e.deleteImage(p.ID, i3.ID); err != nil {
		t.Fatal(err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE catalog.product_images SET deleted_at = now() - interval '31 days' WHERE id = $1", i3.ID)
		return err //nolint:wrapcheck // test
	})
	if err := e.restoreImage(p.ID, i3.ID); !errors.Is(err, catalog.ErrRestoreExpired) {
		t.Errorf("khôi phục sau 31 ngày: mong ErrRestoreExpired, có %v", err)
	}
	if err := e.deleteImage(p.ID, uuid.New()); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("xóa ảnh không có: mong ErrNotFound, có %v", err)
	}
	other, err := e.create(func() catalog.ProductInput { in := dam01(e.seller); in.Code = "DAM09"; prefix(&in); return in }())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.deleteImage(other.ID, i1.ID); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("xóa ảnh qua mẫu khác: mong ErrNotFound, có %v", err)
	}
}

func prefix(in *catalog.ProductInput) {
	for i := range in.Variants {
		in.Variants[i].SKU = "X" + in.Variants[i].SKU
	}
}

// Ảnh lost (TP12): ProductImages vẫn trả ảnh, status lost, không variants.
func TestProductImages_Lost(t *testing.T) {
	e, p, a, _ := withTwoImages(t)
	f := a.FileID
	e.must(func(tx pgx.Tx) error {
		filestest.MarkLost(t, tx, f)
		return nil
	})
	got := e.images(p.ID)
	if len(got) != 2 || got[0].Status != "lost" || got[0].Variants != nil || got[1].Status != "ready" || len(got[1].Variants) != 12 {
		t.Fatalf("ảnh lost sai: %+v", got)
	}
}

// Hai người sắp lại cùng version cùng lúc: đúng một người thành công (khóa dòng mẫu rồi so version).
func TestReorderImages_Concurrent(t *testing.T) {
	e, p, a, b := withTwoImages(t)
	v := e.version(p.ID)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Go(func() {
			order := []uuid.UUID{a.ID, b.ID}
			if i%2 == 0 {
				order = []uuid.UUID{b.ID, a.ID}
			}
			errs[i] = e.reorder(p.ID, v, order)
		})
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, catalog.ErrVersionConflict):
			t.Errorf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("mong đúng 1 lần sắp lại thành công, có %d", ok)
	}
}
