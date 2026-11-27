package catalog_test

import (
	"errors"
	"io/fs"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/migrations"
)

func (e *catalogEnv) reorderVariants(productID uuid.UUID, version int64, ids []uuid.UUID) error {
	return e.app(func(tx pgx.Tx) error {
		return e.writer().ReorderVariants(e.t.Context(), tx, e.seller, productID, version, ids, e.actor)
	})
}

func (e *catalogEnv) mustGet(id uuid.UUID) catalog.Product {
	e.t.Helper()
	p, err := e.get(id)
	if err != nil {
		e.t.Fatalf("GetProduct: %v", err)
	}
	return p
}

func variantIDs(vs []catalog.Variant) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.ID)
	}
	return out
}

// positions: cột position của các biến thể của mẫu theo thứ tự đọc (position, id).
func (e *catalogEnv) positions(productID uuid.UUID) []int {
	e.t.Helper()
	var out []int
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), "SELECT position FROM catalog.variants WHERE product_id = $1 ORDER BY position, id", productID)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[int])
		return err //nolint:wrapcheck // test
	})
	return out
}

// TestReorderVariants (Task 513): biến thể tạo có vị trí 1..n theo thứ tự gửi; sắp lại → GetProduct trả theo thứ tự mới,
// vị trí 1..n, version mẫu +1 (version biến thể giữ nguyên), product.updated, audit catalog.variant.reorder trước/sau;
// biến thể thêm sau vào cuối; version mẫu cũ → ErrVersionConflict.
func TestReorderVariants(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	p := e.mustCreate(dam01(e.seller))
	ids := variantIDs(p.Variants)
	if got := e.positions(p.ID); !slices.Equal(got, []int{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("vị trí sau khi tạo: %v", got)
	}
	want := []uuid.UUID{ids[5], ids[0], ids[3], ids[1], ids[4], ids[2]}
	if err := e.reorderVariants(p.ID, p.Version, want); err != nil {
		t.Fatalf("ReorderVariants: %v", err)
	}
	got := e.mustGet(p.ID)
	if !slices.Equal(variantIDs(got.Variants), want) || !slices.Equal(e.positions(p.ID), []int{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("thứ tự sau khi sắp: %v, vị trí %v", variantIDs(got.Variants), e.positions(p.ID))
	}
	if got.Version != p.Version+1 {
		t.Errorf("version mẫu: mong %d, có %d", p.Version+1, got.Version)
	}
	for _, v := range got.Variants {
		if v.Version != 1 {
			t.Errorf("version biến thể %s đổi thành %d", v.SKU, v.Version)
		}
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'product.updated' AND aggregate_id = $1 AND seq = 2`,
		p.ID.String()); n != 1 {
		t.Errorf("mong 1 product.updated seq 2, có %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'catalog.variant.reorder' AND entity_id = $1
		AND before->'order'->>0 = $2 AND after->'order'->>0 = $3`, p.ID.String(), ids[0].String(), ids[5].String()); n != 1 {
		t.Errorf("mong 1 audit catalog.variant.reorder đúng trước/sau, có %d", n)
	}
	// Biến thể thêm sau vào cuối.
	var added catalog.Variant
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		added, err = e.writer().AddVariant(t.Context(), tx, e.seller, p.ID, got.Version,
			catalog.VariantInput{SKU: "DAM01-DO-XL", Color: "Đỏ", Size: "XL", Price: 350000}, e.actor)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	after := e.mustGet(p.ID)
	if last := after.Variants[len(after.Variants)-1]; last.ID != added.ID || !slices.Equal(e.positions(p.ID), []int{1, 2, 3, 4, 5, 6, 7}) {
		t.Errorf("biến thể mới phải ở cuối, vị trí 7: %+v, vị trí %v", last, e.positions(p.ID))
	}
	if err := e.reorderVariants(p.ID, p.Version, want); !errors.Is(err, catalog.ErrVersionConflict) {
		t.Errorf("version cũ: mong ErrVersionConflict, có %v", err)
	}
}

// Danh sách không đúng tập biến thể của mẫu (cả biến thể ngừng bán) → ErrVariantOrderMismatch, không đổi gì; mẫu không có
// hay của người bán khác → ErrNotFound.
func TestReorderVariants_Invalid(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	p := e.mustCreate(dam01(e.seller))
	other := e.mustCreate(catalog.ProductInput{SellerID: e.seller, Code: "AO01", Name: "Áo", Variants: []catalog.VariantInput{
		{SKU: "AO01-DEN-S", Color: "Đen", Size: "S", Price: 100000},
	}})
	ids := variantIDs(p.Variants)
	if _, err := e.updateVariant(ids[2], 1, catalog.VariantPatch{Status: ptr(catalog.VariantDiscontinued)}); err != nil {
		t.Fatal(err)
	}
	cur := e.mustGet(p.ID)
	cases := map[string][]uuid.UUID{
		"thiếu":             ids[:5],
		"thiếu ngừng bán":   {ids[0], ids[1], ids[3], ids[4], ids[5]},
		"thừa":              append(slices.Clone(ids), uuid.New()),
		"trùng":             {ids[0], ids[0], ids[1], ids[2], ids[3], ids[4]},
		"biến thể mẫu khác": {ids[0], ids[1], ids[2], ids[3], ids[4], other.Variants[0].ID},
		"rỗng":              {},
	}
	for name, list := range cases {
		if err := e.reorderVariants(p.ID, cur.Version, list); !errors.Is(err, catalog.ErrVariantOrderMismatch) {
			t.Errorf("%s: mong ErrVariantOrderMismatch, có %v", name, err)
		}
	}
	if got := e.mustGet(p.ID); got.Version != cur.Version || !slices.Equal(variantIDs(got.Variants), ids) {
		t.Errorf("lỗi không được đổi gì: version %d, thứ tự %v", got.Version, variantIDs(got.Variants))
	}
	if err := e.reorderVariants(uuid.New(), 1, ids); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("mẫu không có: mong ErrNotFound, có %v", err)
	}
	wrong := uuid.New()
	if err := e.app(func(tx pgx.Tx) error {
		return e.writer().ReorderVariants(t.Context(), tx, wrong, p.ID, cur.Version, ids, e.actor)
	}); !errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("người bán khác: mong ErrNotFound, có %v", err)
	}
	reversed := slices.Clone(ids)
	slices.Reverse(reversed)
	if err := e.reorderVariants(p.ID, cur.Version, reversed); err != nil {
		t.Errorf("đủ tập (kể cả ngừng bán): %v", err)
	}
}

// Hai người sắp lại cùng version cùng lúc: đúng một người thành công; vị trí cuối cùng vẫn là 1..n.
func TestReorderVariants_Concurrent(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	p := e.mustCreate(dam01(e.seller))
	ids := variantIDs(p.Variants)
	oneWinner(t, func(i int) error {
		order := slices.Clone(ids)
		if i%2 == 0 {
			slices.Reverse(order)
		}
		return e.reorderVariants(p.ID, p.Version, order)
	})
	if got := e.positions(p.ID); !slices.Equal(got, []int{1, 2, 3, 4, 5, 6}) {
		t.Errorf("vị trí sau khi sắp đồng thời: %v", got)
	}
}

// oneWinner chạy try(0..7) cùng lúc (cùng version): đúng một lần thành công, các lần khác ErrVersionConflict.
func oneWinner(t *testing.T, try func(i int) error) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Go(func() { errs[i] = try(i) })
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
		t.Errorf("mong đúng 1 lần thành công, có %d", ok)
	}
}

// backfillSQL: đoạn điền vị trí cho dòng có sẵn của migration 0533 (từ NO FORCE tới FORCE ROW LEVEL SECURITY).
func backfillSQL(t *testing.T) string {
	t.Helper()
	raw, err := fs.ReadFile(migrations.FS, "0533_catalog_variant_position.sql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	start := strings.Index(s, "ALTER TABLE catalog.variants NO FORCE ROW LEVEL SECURITY;")
	const end = "ALTER TABLE catalog.variants FORCE ROW LEVEL SECURITY;"
	stop := strings.Index(s, end)
	if start < 0 || stop < start {
		t.Fatal("không thấy đoạn điền vị trí trong 0533")
	}
	return s[start : stop+len(end)]
}

// TestVariantPositionBackfill (migration 0533): dòng có sẵn nhận vị trí 1..n theo (created_at, id) trong từng mẫu, mỗi
// tenant riêng; RLS bị ép lại sau khi điền.
func TestVariantPositionBackfill(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	p := e.mustCreate(dam01(e.seller))
	q := e.mustCreate(catalog.ProductInput{SellerID: e.seller, Code: "AO01", Name: "Áo", Variants: []catalog.VariantInput{
		{SKU: "AO01-DEN-S", Color: "Đen", Size: "S", Price: 100000},
		{SKU: "AO01-DEN-M", Color: "Đen", Size: "M", Price: 100000},
	}})
	ids := variantIDs(p.Variants)
	// Như dữ liệu trước 0533: vị trí 0; ids[4] tạo sớm nhất, ids[1], ids[2] cùng mốc (hòa → theo id).
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), `UPDATE catalog.variants SET position = 0, created_at = '2026-01-02'`); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `UPDATE catalog.variants SET created_at = '2026-01-01' WHERE id = $1`, ids[4])
		return err //nolint:wrapcheck // test
	})
	if _, err := e.staff.Owner.Pool.Exec(t.Context(), backfillSQL(t)); err != nil {
		t.Fatalf("chạy đoạn điền của 0533: %v", err)
	}
	want := []uuid.UUID{ids[4], ids[0], ids[1], ids[2], ids[3], ids[5]}
	got := e.mustGet(p.ID)
	if !slices.Equal(variantIDs(got.Variants), want) || !slices.Equal(e.positions(p.ID), []int{1, 2, 3, 4, 5, 6}) {
		t.Errorf("mẫu 1 sau khi điền: %v, vị trí %v", variantIDs(got.Variants), e.positions(p.ID))
	}
	if got := e.positions(q.ID); !slices.Equal(got, []int{1, 2}) {
		t.Errorf("mẫu 2 đánh số riêng: %v", got)
	}
	var forced bool
	if err := e.staff.Owner.Pool.QueryRow(t.Context(),
		"SELECT relforcerowsecurity FROM pg_class WHERE oid = 'catalog.variants'::regclass").Scan(&forced); err != nil || !forced {
		t.Errorf("RLS phải được ép lại: %v, %v", forced, err)
	}
}

// TestVariantPositionTrigger (migration 0533): câu chèn không đặt vị trí (InsertVariant, chèn thẳng như dữ liệu thử) →
// cuối + 1 trong đúng mẫu; câu chèn có vị trí giữ nguyên vị trí đó; mẫu khác đánh số riêng.
func TestVariantPositionTrigger(t *testing.T) {
	t.Parallel()
	e := newCatalogEnv(t)
	p := e.mustCreate(dam01(e.seller))
	q := e.mustCreate(catalog.ProductInput{SellerID: e.seller, Code: "AO02", Name: "Áo", Variants: []catalog.VariantInput{
		{SKU: "AO02-DEN-S", Color: "Đen", Size: "S", Price: 100000},
	}})
	insert := func(product uuid.UUID, sku string, pos *int) {
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), `INSERT INTO catalog.variants
				(tenant_id, seller_id, product_id, sku, barcode, color, size, price, position)
				VALUES ($1, $2, $3, $4, $5, 'Đỏ', $4, 1000, coalesce($6, 0))`,
				e.tenant, e.seller, product, sku, "BC-"+sku, pos)
			return err //nolint:wrapcheck // test
		})
	}
	insert(p.ID, "THANG-1", nil)
	ten := 10
	insert(p.ID, "THANG-2", &ten)
	insert(p.ID, "THANG-3", nil)
	insert(q.ID, "THANG-4", nil)
	if got := e.positions(p.ID); !slices.Equal(got, []int{1, 2, 3, 4, 5, 6, 7, 10, 11}) {
		t.Errorf("mẫu 1: %v", got)
	}
	if got := e.positions(q.ID); !slices.Equal(got, []int{1, 2}) {
		t.Errorf("mẫu 2: %v", got)
	}
}
