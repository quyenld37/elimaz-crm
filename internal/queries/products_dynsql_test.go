package queries_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/queries"
)

// Một database dựng một lần cho mọi ca (10.000 mẫu, 50.000 biến thể), kể cả danh sách tồn (balances_dynsql_test.go).
func TestProducts(t *testing.T) {
	f := newFixture(t)
	t.Run("SearchByName", func(t *testing.T) { testSearchByName(t, f) })
	t.Run("SearchBySKUPrefix", func(t *testing.T) { testSearchBySKUPrefix(t, f) })
	t.Run("SearchByBarcode", func(t *testing.T) { testSearchByBarcode(t, f) })
	t.Run("SearchByProductCode", func(t *testing.T) { testSearchByProductCode(t, f) })
	t.Run("SearchOtherTenant", func(t *testing.T) { testSearchOtherTenant(t, f) })
	t.Run("SearchOutsideTenant", func(t *testing.T) { testSearchOutsideTenant(t, f) })
	t.Run("SearchInvalid", func(t *testing.T) { testSearchInvalid(t, f) })
	t.Run("SearchSameAsBefore", func(t *testing.T) { testSearchSameAsBefore(t, f) })
	t.Run("SearchBroadName", func(t *testing.T) { testSearchBroadName(t, f) })
	t.Run("PagesAll", func(t *testing.T) { testPagesAll(t, f) })
	t.Run("ListColumns", func(t *testing.T) { testListColumns(t, f) })
	t.Run("ListFilters", func(t *testing.T) { testListFilters(t, f) })
	t.Run("ListInvalid", func(t *testing.T) { testListInvalid(t, f) })
	t.Run("CursorRoundTrip", testCursorRoundTrip)
	t.Run("Balances", func(t *testing.T) { testBalances(t, f) })
	t.Run("BalanceCursorRoundTrip", testBalanceCursorRoundTrip)
}

func search(t *testing.T, f *fixture, tenant, seller uuid.UUID, q string, limit int) []queries.VariantHit {
	t.Helper()
	var hits []queries.VariantHit
	f.inTenant(t, tenant, func(tx pgx.Tx) error {
		var err error
		hits, err = queries.SearchVariants(t.Context(), tx, seller, q, limit)
		return err //nolint:wrapcheck // test
	})
	return hits
}

func list(t *testing.T, f *fixture, tenant uuid.UUID, pf queries.ProductFilter) ([]queries.ProductRow, *queries.Cursor) {
	t.Helper()
	var rows []queries.ProductRow
	var next *queries.Cursor
	f.inTenant(t, tenant, func(tx pgx.Tx) error {
		var err error
		rows, next, err = queries.ListProducts(t.Context(), tx, pf)
		return err //nolint:wrapcheck // test
	})
	return rows, next
}

// "Đầm lụa" gõ không dấu, hoa thường lẫn lộn: mọi kết quả là mẫu "Đầm lụa …" của tenant A.
func testSearchByName(t *testing.T, f *fixture) {
	hits := search(t, f, f.tenantA, f.sellerA, "  DAM   Lụa ", 100)
	if len(hits) != 100 {
		t.Fatalf("muốn 100 dòng (giới hạn), nhận %d", len(hits))
	}
	for _, h := range hits {
		if !strings.HasPrefix(h.ProductName, "Đầm lụa mẫu ") || !strings.HasPrefix(h.ProductCode, "P") {
			t.Fatalf("dòng không khớp: %+v", h)
		}
	}
	// Từ theo thứ tự, mỗi từ là một đoạn của tên: "lua 120" ra mẫu 120 mà không ra mẫu "thun".
	hits = search(t, f, f.tenantA, f.sellerA, "dam lua mau 1200", 100)
	codes := hitCodes(hits)
	if !slices.Equal(codes, []string{"P01200"}) {
		t.Fatalf("muốn đúng mẫu P01200, nhận %v", codes)
	}
	if len(hits) != 5 {
		t.Fatalf("mẫu có 5 biến thể, nhận %d", len(hits))
	}
	if got := search(t, f, f.tenantA, f.sellerA, "lua dam", 100); len(got) != 0 {
		t.Fatalf("sai thứ tự từ phải 0 dòng, nhận %d", len(got))
	}
}

func hitCodes(hits []queries.VariantHit) []string {
	var out []string
	for _, h := range hits {
		if !slices.Contains(out, h.ProductCode) {
			out = append(out, h.ProductCode)
		}
	}
	return out
}

// "DAM01-" ra đúng ba SKU của mẫu DAM01 tenant A (không ra DAM011-S, không ra của tenant B); chữ thường cũng vậy.
func testSearchBySKUPrefix(t *testing.T, f *fixture) {
	for _, q := range []string{"DAM01-", "dam01-"} {
		hits := search(t, f, f.tenantA, f.sellerA, q, 100)
		var skus []string
		for _, h := range hits {
			skus = append(skus, h.SKU)
		}
		slices.Sort(skus)
		if !slices.Equal(skus, []string{"DAM01-L", "DAM01-M", "DAM01-S"}) {
			t.Fatalf("q %q: muốn ba SKU DAM01-, nhận %v", q, skus)
		}
	}
}

// Barcode đủ 13 số ra đúng một biến thể; một phần barcode không khớp.
func testSearchByBarcode(t *testing.T, f *fixture) {
	hits := search(t, f, f.tenantA, f.sellerA, f.barcodeOfP00042S, 100)
	if len(hits) != 1 || hits[0].SKU != "P00042-S" || hits[0].Barcode != f.barcodeOfP00042S {
		t.Fatalf("muốn đúng biến thể P00042-S, nhận %+v", hits)
	}
	if hits[0].Price != 110000 || hits[0].Color != "Đen" || hits[0].Size != "S" || hits[0].VariantID == uuid.Nil {
		t.Fatalf("cột sai: %+v", hits[0])
	}
	// Barcode chữ hoa lẫn thường: gõ đúng như in thì ra; một phần thì không.
	if hits := search(t, f, f.tenantA, f.sellerA, " Ab12Cd ", 100); len(hits) != 1 || hits[0].SKU != "NOSTOCK-S" {
		t.Fatalf("barcode Ab12Cd: %+v", hits)
	}
	if hits := search(t, f, f.tenantA, f.sellerA, "Ab12", 100); len(hits) != 0 {
		t.Fatalf("một phần barcode không khớp: %+v", hits)
	}
}

// Mã mẫu theo tiền tố: "p0420" ra 5 biến thể của P04200…P04209 đứng trước; trạng thái bán được đúng theo mẫu.
func testSearchByProductCode(t *testing.T, f *fixture) {
	hits := search(t, f, f.tenantA, f.sellerA, "p0420", 100)
	codes := hitCodes(hits)
	want := []string{"P04200", "P04201", "P04202", "P04203", "P04204", "P04205", "P04206", "P04207", "P04208", "P04209"}
	slices.Sort(codes)
	if !slices.Equal(codes, want) {
		t.Fatalf("muốn %v, nhận %v", want, codes)
	}
	for _, h := range hits {
		// status theo g % 4: 1 → active; 2 → draft; 3 → discontinued; 0 → active.
		n := int(h.ProductCode[5] - '0')
		if wantSellable := n%4 == 0 || n%4 == 1; h.Sellable != wantSellable {
			t.Fatalf("%s: sellable %v, muốn %v", h.SKU, h.Sellable, wantSellable)
		}
	}
}

// Tenant B tìm bằng người bán của A: 0 dòng (hàm lọc tenant theo ngữ cảnh, không theo người bán gửi vào). Tenant B
// tìm của mình: chỉ dòng của B.
func testSearchOtherTenant(t *testing.T, f *fixture) {
	if got := search(t, f, f.tenantB, f.sellerA, "dam lua", 100); len(got) != 0 {
		t.Fatalf("tenant B thấy dòng của A: %d", len(got))
	}
	hits := search(t, f, f.tenantB, f.sellerB, "DAM01-", 100)
	if len(hits) != 3 {
		t.Fatalf("tenant B có 3 SKU DAM01-, nhận %d", len(hits))
	}
	for _, h := range hits {
		if h.ProductName != "Đầm của tenant B" {
			t.Fatalf("dòng lạ ở tenant B: %+v", h)
		}
	}
	for _, h := range search(t, f, f.tenantB, f.sellerB, "dam lua", 100) {
		if !strings.HasPrefix(h.ProductCode, "B") {
			t.Fatalf("tenant B thấy dòng của A: %+v", h)
		}
	}
}

// Ngoài ngữ cảnh tenant: hai hàm tìm trả 0 dòng.
func testSearchOutsideTenant(t *testing.T, f *fixture) {
	for _, q := range []string{
		"SELECT count(*) FROM catalog.search_variants($1, $2, $2, 100)",
		"SELECT count(*) FROM catalog.search_products($1, $2, $2, 100)",
	} {
		var n int
		if err := f.app.Pool.QueryRow(t.Context(), q, f.sellerA, "dam lua").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%s ngoài ngữ cảnh: %d dòng, muốn 0", q, n)
		}
	}
}

// q rỗng hoặc quá dài, limit ngoài 1–100: ErrInvalidInput. Ký tự đại diện của LIKE không thành "khớp mọi thứ".
func testSearchInvalid(t *testing.T, f *fixture) {
	for _, c := range []struct {
		q     string
		limit int
	}{{"   ", 10}, {strings.Repeat("a", 101), 10}, {"dam", 0}, {"dam", 101}} {
		f.inTenant(t, f.tenantA, func(tx pgx.Tx) error {
			_, err := queries.SearchVariants(t.Context(), tx, f.sellerA, c.q, c.limit)
			if !errors.Is(err, queries.ErrInvalidInput) {
				t.Errorf("q %q limit %d: muốn ErrInvalidInput, nhận %v", c.q, c.limit, err)
			}
			return nil
		})
	}
	for _, q := range []string{"%", "_", "\\"} {
		if got := search(t, f, f.tenantA, f.sellerA, q, 100); len(got) != 0 {
			t.Fatalf("q %q: %d dòng, muốn 0", q, len(got))
		}
	}
}

// 250 mẫu, 50 cái cùng updated_at: duyệt bằng con trỏ (trang 37) ra đúng 250, không trùng, không sót, đúng thứ tự.
func testPagesAll(t *testing.T, f *fixture) {
	seen := map[uuid.UUID]bool{}
	var all []queries.ProductRow
	pf := queries.ProductFilter{SellerID: f.sellerC, Limit: 37}
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("quá nhiều trang")
		}
		rows, next := list(t, f, f.tenantC, pf)
		for _, r := range rows {
			if seen[r.ProductID] {
				t.Fatalf("mẫu %s hiện hai lần", r.Code)
			}
			seen[r.ProductID] = true
		}
		all = append(all, rows...)
		if next == nil {
			break
		}
		// Con trỏ đi qua chuỗi như ở API.
		c, err := queries.ParseCursor(next.String())
		if err != nil {
			t.Fatal(err)
		}
		pf.Cursor = &c
	}
	if len(all) != 250 {
		t.Fatalf("muốn 250 mẫu, nhận %d", len(all))
	}
	for i := 1; i < len(all); i++ {
		a, b := all[i-1], all[i]
		if a.UpdatedAt.Before(b.UpdatedAt) || (a.UpdatedAt.Equal(b.UpdatedAt) && strings.Compare(a.ProductID.String(), b.ProductID.String()) < 0) {
			t.Fatalf("sai thứ tự ở %d: %s rồi %s", i, a.Code, b.Code)
		}
	}
}

// Cột của một mẫu: số biến thể, giá nhỏ nhất/lớn nhất, tổng có thể bán ở kho, đường danh mục.
func testListColumns(t *testing.T, f *fixture) {
	rows, next := list(t, f, f.tenantA, queries.ProductFilter{SellerID: f.sellerA, Q: "P00003", WarehouseID: f.warehouseA, Limit: 10})
	if len(rows) != 1 || next != nil {
		t.Fatalf("muốn đúng 1 mẫu P00003, nhận %d (next %v)", len(rows), next)
	}
	r := rows[0]
	// g = 3: Chân váy jean? kind 3 = Chân váy, mat (3/5)%4 = 0 = lụa; status draft? (1+3%4) = 4 → discontinued.
	if r.Code != "P00003" || r.Name != "Chân váy lụa mẫu 3" || r.Status != "discontinued" {
		t.Fatalf("mẫu sai: %+v", r)
	}
	if r.VariantCount != 5 || r.MinPrice != 110000 || r.MaxPrice != 150000 {
		t.Fatalf("biến thể sai: %+v", r)
	}
	// 5 biến thể × (10 − 12) = −10: âm vẫn trả (thiếu so với phần giữ).
	if r.Available == nil || *r.Available != -10 {
		t.Fatalf("available sai: %v", r.Available)
	}
	// g % 3 = 0 → Váy > Váy dài.
	if r.CategoryID == nil || *r.CategoryID != f.catLong || !slices.Equal(r.CategoryPath, []string{"Váy", "Váy dài"}) {
		t.Fatalf("danh mục sai: %v %v", r.CategoryID, r.CategoryPath)
	}
	// Ảnh bìa (Task M1-44): ảnh vị trí 1 trong 3 ảnh của mẫu.
	var firstFile uuid.UUID
	f.inTenant(t, f.tenantA, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), "SELECT file_id FROM catalog.product_images_v WHERE product_id = $1 AND position = 1",
			r.ProductID).Scan(&firstFile)
	})
	if r.CoverImageID == nil || r.CoverFileID == nil || *r.CoverFileID != firstFile {
		t.Fatalf("ảnh bìa sai: %v %v (muốn tệp %s)", r.CoverImageID, r.CoverFileID, firstFile)
	}
	rows, _ = list(t, f, f.tenantA, queries.ProductFilter{SellerID: f.sellerA, Q: "P00004", Limit: 10})
	if len(rows) != 1 || rows[0].Available != nil || rows[0].CategoryID == nil || *rows[0].CategoryID != f.catTop ||
		!slices.Equal(rows[0].CategoryPath, []string{"Áo"}) {
		t.Fatalf("không kho thì không có available; danh mục Áo: %+v", rows)
	}
	// Mẫu chưa có dòng số dư ở kho: available 0 (không phải không có).
	rows, _ = list(t, f, f.tenantA, queries.ProductFilter{SellerID: f.sellerA, Q: "NOSTOCK", WarehouseID: f.warehouseA, Limit: 10})
	if len(rows) != 1 || rows[0].Available == nil || *rows[0].Available != 0 || rows[0].VariantCount != 1 ||
		rows[0].CoverImageID != nil {
		t.Fatalf("mẫu chưa nhập kho: %+v", rows)
	}
	rows, _ = list(t, f, f.tenantA, queries.ProductFilter{SellerID: f.sellerA, Q: "P00005", Limit: 10})
	if len(rows) != 1 || rows[0].CategoryID != nil || len(rows[0].CategoryPath) != 0 {
		t.Fatalf("mẫu không danh mục: %+v", rows)
	}
}

// Lọc trạng thái, danh mục (gồm con), nhãn, Q; đếm bằng duyệt hết trang 100.
func testListFilters(t *testing.T, f *fixture) {
	count := func(pf queries.ProductFilter) int {
		pf.SellerID, pf.Limit = f.sellerA, 100
		n := 0
		for {
			rows, next := list(t, f, f.tenantA, pf)
			n += len(rows)
			if next == nil {
				return n
			}
			pf.Cursor = next
		}
	}
	dress := f.catDress
	tag := f.tagNew
	for _, c := range []struct {
		name string
		pf   queries.ProductFilter
		want int
	}{
		// 10.000 mẫu P + DAM01, DAM011, NOSTOCK.
		{"tất cả", queries.ProductFilter{}, 10003},
		// draft: g % 4 = 1 → (1 + g%4) = 2? ARRAY[active, active, draft, discontinued][1 + g%4]: draft khi g%4 = 2.
		{"nháp", queries.ProductFilter{Status: []string{"draft"}}, 2500},
		{"nháp hoặc ngừng", queries.ProductFilter{Status: []string{"draft", "discontinued"}}, 5000},
		// Váy gồm Váy dài: g % 3 = 0 → 3333 mẫu.
		{"danh mục cha", queries.ProductFilter{CategoryID: &dress}, 3333},
		{"nhãn", queries.ProductFilter{TagID: &tag}, 1428},
		{"tìm đầm lụa", queries.ProductFilter{Q: "đầm lụa"}, 500},
		{"tìm đầm lụa nháp", queries.ProductFilter{Q: "đầm lụa", Status: []string{"draft"}}, 0},
		{"tìm theo SKU", queries.ProductFilter{Q: "DAM01-"}, 1},
	} {
		if got := count(c.pf); got != c.want {
			t.Errorf("%s: %d mẫu, muốn %d", c.name, got, c.want)
		}
	}
}

func testListInvalid(t *testing.T, f *fixture) {
	for _, pf := range []queries.ProductFilter{
		{SellerID: f.sellerA, Limit: 0},
		{SellerID: f.sellerA, Limit: 101},
		{SellerID: f.sellerA, Limit: 10, Status: []string{"deleted"}},
		{SellerID: f.sellerA, Limit: 10, Q: strings.Repeat("a", 101)},
	} {
		f.inTenant(t, f.tenantA, func(tx pgx.Tx) error {
			_, _, err := queries.ListProducts(t.Context(), tx, pf)
			if !errors.Is(err, queries.ErrInvalidInput) {
				t.Errorf("%+v: muốn ErrInvalidInput, nhận %v", pf, err)
			}
			return nil
		})
	}
}

func testCursorRoundTrip(t *testing.T) {
	for _, s := range []string{"", "abc", "!!!", "MjAyNi0xMS0wMQ"} {
		if _, err := queries.ParseCursor(s); !errors.Is(err, queries.ErrInvalidInput) {
			t.Errorf("ParseCursor(%q): muốn ErrInvalidInput, nhận %v", s, err)
		}
	}
}
