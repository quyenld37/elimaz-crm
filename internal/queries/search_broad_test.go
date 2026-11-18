package queries_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/textnorm"
	"elimaz.com.vn/crm/internal/queries"
)

// refSearchSQL là cách khớp và thứ tự của catalog.search_variants viết thẳng trên view (chậm, đọc cả bảng: chỉ để
// so trong test): barcode đúng (hạng 0), SKU theo tiền tố (1), mã mẫu theo tiền tố (2), tên — các từ theo thứ tự (3);
// mỗi biến thể lấy hạng nhỏ nhất; bỏ mẫu đã xóa; sắp theo hạng rồi SKU; tối đa $4 dòng. $2 là q đã Fold, $3 chuỗi gốc.
const refSearchSQL = `
WITH hits AS (
	SELECT v.variant_id AS id, 0 AS rank FROM catalog.variants_v AS v WHERE v.seller_id = $1 AND v.barcode = $3
	UNION ALL
	SELECT v.variant_id, 1 FROM catalog.variants_v AS v
	WHERE v.seller_id = $1 AND v.sku ~>=~ upper($2) AND v.sku ~<~ (upper($2) || chr(1114111))
	UNION ALL
	SELECT v.variant_id, 2 FROM catalog.variants_v AS v JOIN catalog.products_v AS p ON p.product_id = v.product_id
	WHERE p.seller_id = $1 AND p.code ~>=~ upper($2) AND p.code ~<~ (upper($2) || chr(1114111))
	UNION ALL
	SELECT v.variant_id, 3 FROM catalog.variants_v AS v JOIN catalog.products_v AS p ON p.product_id = v.product_id
	WHERE p.seller_id = $1
		AND p.name_search LIKE '%' || replace(replace(replace(replace($2, '\', '\\'), '%', '\%'), '_', '\_'), ' ', '%') || '%'
)
SELECT h.id, h.rank, length(p.name_search)
FROM (SELECT hits.id, min(hits.rank) AS rank FROM hits GROUP BY hits.id) AS h
JOIN catalog.variants_v AS v ON v.variant_id = h.id
JOIN catalog.products_v AS p ON p.product_id = v.product_id
WHERE p.deleted_at IS NULL
ORDER BY h.rank, v.sku
LIMIT $4`

// refHit là một dòng của refSearchSQL.
type refHit struct {
	id      uuid.UUID
	rank    int
	nameLen int
}

func refSearch(t *testing.T, f *fixture, q string, limit int) []refHit {
	t.Helper()
	var out []refHit
	f.inTenant(t, f.tenantA, func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), refSearchSQL, f.sellerA, textnorm.Fold(q), q, limit)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (refHit, error) {
			var h refHit
			return h, r.Scan(&h.id, &h.rank, &h.nameLen)
		})
		return err //nolint:wrapcheck // test
	})
	return out
}

func hitIDs(hits []queries.VariantHit) []uuid.UUID {
	out := make([]uuid.UUID, len(hits))
	for i, h := range hits {
		out[i] = h.VariantID
	}
	return out
}

// Từ khóa hẹp, SKU, mã mẫu, barcode, số khớp không vượt giới hạn: đúng như cách khớp cũ (cùng dòng, cùng thứ tự).
// "p0" (khớp SKU của 50.000 biến thể): nhóm SKU vẫn lấy các SKU nhỏ nhất như cũ.
func testSearchSameAsBefore(t *testing.T, f *fixture) {
	for _, c := range []struct {
		q     string
		limit int
	}{
		{"dam lua mau 12", 100},
		{"dam lua mau 1200", 100},
		{"DAM01-", 100},
		{"p0420", 100},
		{"p0420", 7},
		{f.barcodeOfP00042S, 100},
		{"Ab12Cd", 100},
		{"DAM0", 100},
		{"dam thu tien", 100},
		{"p0", 100},
		{"lua dam", 100},
	} {
		got := hitIDs(search(t, f, f.tenantA, f.sellerA, c.q, c.limit))
		var want []uuid.UUID
		for _, h := range refSearch(t, f, c.q, c.limit) {
			want = append(want, h.id)
		}
		if !slices.Equal(got, want) {
			t.Errorf("q %q limit %d: %d dòng khác cách khớp cũ (%d dòng)", c.q, c.limit, len(got), len(want))
		}
	}
}

// "dam" khớp tên của 2.002 mẫu (hơn 10.000 biến thể): đủ 100 dòng, mọi dòng đều khớp, đúng thứ tự hạng rồi SKU, nhóm
// tên lấy các mẫu liên quan nhất (tên không dấu ngắn nhất, các từ đã nằm trong tên); gõ có dấu, hoa thường cũng vậy.
func testSearchBroadName(t *testing.T, f *fixture) {
	var n int
	f.inTenant(t, f.tenantA, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM catalog.products_v
			WHERE seller_id = $1 AND name_search LIKE '%dam%' AND deleted_at IS NULL`, f.sellerA).Scan(&n)
	})
	if n < 2000 {
		t.Fatalf("fixture: \"dam\" phải khớp ≥ 2.000 mẫu, có %d", n)
	}
	hits := search(t, f, f.tenantA, f.sellerA, "dam", 100)
	if len(hits) != 100 {
		t.Fatalf("muốn 100 dòng, nhận %d", len(hits))
	}
	requireMostRelevant(t, refSearch(t, f, "dam", 1_000_000), hits)
	for _, q := range []string{"ĐẦM", " Đầm "} {
		if got := hitIDs(search(t, f, f.tenantA, f.sellerA, q, 100)); !slices.Equal(got, hitIDs(hits)) {
			t.Fatalf("q %q khác \"dam\"", q)
		}
	}
	// Thời gian (đo tại máy chạy test, lấy lần nhanh nhất trong 5 để bớt nhiễu khi máy tải nặng): "dam" (2.002 mẫu),
	// "mau" (mọi mẫu), "p0" (tiền tố của mọi mã mẫu và SKU).
	for _, q := range []string{"dam", "mau", "p0"} {
		best := time.Hour
		for range 5 {
			start := time.Now()
			if got := search(t, f, f.tenantA, f.sellerA, q, 100); len(got) != 100 {
				t.Fatalf("q %q: muốn 100 dòng, nhận %d", q, len(got))
			}
			best = min(best, time.Since(start))
		}
		t.Logf("tìm %q: nhanh nhất %v", q, best)
		if best >= 100*time.Millisecond {
			t.Fatalf("tìm %q mất %v, muốn < 100 ms", q, best)
		}
	}
}

// requireMostRelevant: mọi dòng của hits nằm trong all (mọi dòng khớp theo cách cũ, đã sắp hạng rồi SKU), giữ đúng thứ
// tự đó; không mẫu khớp tên nào bị bỏ mà có tên không dấu ngắn hơn mẫu dài nhất đã lấy.
func requireMostRelevant(t *testing.T, all []refHit, hits []queries.VariantHit) {
	t.Helper()
	pos := map[uuid.UUID]int{}
	for i, h := range all {
		pos[h.id] = i
	}
	in := map[uuid.UUID]bool{}
	last, maxIn := -1, 0
	for _, h := range hits {
		i, ok := pos[h.VariantID]
		if !ok {
			t.Fatalf("dòng không khớp: %+v", h)
		}
		if i <= last {
			t.Fatalf("sai thứ tự hạng, SKU ở %s", h.SKU)
		}
		last = i
		in[h.VariantID] = true
		if all[i].rank == 3 {
			maxIn = max(maxIn, all[i].nameLen)
		}
	}
	for _, h := range all {
		if h.rank == 3 && !in[h.id] && h.nameLen < maxIn {
			t.Fatalf("bỏ mẫu tên ngắn hơn (%d ký tự) mà lấy mẫu tên %d ký tự", h.nameLen, maxIn)
		}
	}
}
