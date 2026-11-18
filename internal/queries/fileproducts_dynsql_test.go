package queries_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/queries"
	"elimaz.com.vn/crm/internal/testkit"
)

// attachImage gắn tệp file làm ảnh thứ pos của mẫu product (dòng catalog.product_images bằng câu ghi thẳng: gói
// queries chỉ đọc, đường ghi ảnh có test riêng ở catalog).
func attachImage(t *testing.T, tx pgx.Tx, tenant, product, file uuid.UUID, pos int) {
	t.Helper()
	if _, err := tx.Exec(t.Context(), `INSERT INTO catalog.product_images (tenant_id, product_id, file_id, position)
		VALUES ($1, $2, $3, $4)`, tenant, product, file, pos); err != nil {
		t.Fatal(err)
	}
}

// FileProducts (TP12, danh sách tệp mất): tệp gắn một mẫu → mã mẫu; tệp gắn hai mẫu → mã nhỏ nhất; ảnh đã xóa, tệp
// không gắn mẫu → không có; tenant khác → không thấy; danh sách rỗng → rỗng, không lỗi.
func TestFileProducts(t *testing.T) {
	s := testkit.OpenStaff(t)
	a, b := s.Tenant(t, "A"), s.Tenant(t, "B")
	var one, two, removed, loose, other uuid.UUID
	var pA, pB uuid.UUID
	s.Exec(t, a, func(tx pgx.Tx) error {
		seller, _ := catalogtest.Sellers(t, tx, a)
		pA, pB = catalogtest.Product(t, tx, a, seller, "DAM01"), catalogtest.Product(t, tx, a, seller, "AO02")
		one, two, removed, loose = filestest.Image(t, tx, a, "product_image"), filestest.Image(t, tx, a, "product_image"),
			filestest.Image(t, tx, a, "product_image"), filestest.Image(t, tx, a, "product_image")
		attachImage(t, tx, a, pA, one, 1)
		attachImage(t, tx, a, pA, two, 2)
		attachImage(t, tx, a, pB, two, 1)
		attachImage(t, tx, a, pB, removed, 2)
		_, err := tx.Exec(t.Context(), "UPDATE catalog.product_images SET deleted_at = now() WHERE file_id = $1", removed)
		return err //nolint:wrapcheck // test
	})
	s.Exec(t, b, func(tx pgx.Tx) error {
		seller, _ := catalogtest.Sellers(t, tx, b)
		pb := catalogtest.Product(t, tx, b, seller, "DAM01")
		other = filestest.Image(t, tx, b, "product_image")
		attachImage(t, tx, b, pb, other, 1)
		return nil
	})
	var got map[uuid.UUID]queries.FileProduct
	s.Exec(t, a, func(tx pgx.Tx) error {
		var err error
		got, err = queries.FileProducts(t.Context(), tx, []uuid.UUID{one, two, removed, loose, other})
		return err //nolint:wrapcheck // test
	})
	want := map[uuid.UUID]queries.FileProduct{one: {pA, "DAM01"}, two: {pB, "AO02"}}
	if len(got) != len(want) || got[one] != want[one] || got[two] != want[two] {
		t.Fatalf("FileProducts = %+v, muốn %+v", got, want)
	}
	s.Exec(t, a, func(tx pgx.Tx) error {
		var err error
		got, err = queries.FileProducts(t.Context(), tx, nil)
		return err //nolint:wrapcheck // test
	})
	if len(got) != 0 {
		t.Fatalf("danh sách rỗng: %+v", got)
	}
}
