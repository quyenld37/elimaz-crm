package webcatalog_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// explainSeedSQL: tenant A có 2 người bán, kho MAIN, 10.000 mẫu, 50.000 biến thể (5 size mỗi mẫu), số dư cho mọi biến
// thể, 2 ảnh mỗi mẫu chẵn, nhãn ở mẫu chia hết cho 7; mốc updated_at rải 10.000 phút về trước (chỉ vài mẫu đổi trong 30
// s cuối). Tenant B có 1.000 mẫu cùng kiểu. Mã cố định viết thẳng ($n thay bằng FixedUUID ở explainSeed).
const explainSeedSQL = `
INSERT INTO platform.tenants (id, name) VALUES ($1, 'A'), ($2, 'B');
INSERT INTO catalog.sellers (id, tenant_id, code, name, kind) VALUES
	($3, $1, 'ELIMAZ', 'ELIMAZ', 'own'), ($4, $1, 'PARTNER1', 'PARTNER1', 'partner'), ($5, $2, 'ELIMAZ', 'ELIMAZ', 'own');
INSERT INTO inventory.warehouses (id, tenant_id, operated_by_seller_id, code, name) VALUES
	($6, $1, $3, 'MAIN', 'Kho chính'), ($7, $2, $5, 'MAIN', 'Kho chính');
INSERT INTO catalog.tags (id, tenant_id, code, name) VALUES ($8, $1, 'hang-moi', 'Hàng mới');
INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, article, status, updated_at)
SELECT t.tenant_id, CASE WHEN t.tenant_id = $1 AND g % 10 = 0 THEN $4 ELSE t.seller_id END, 'P' || lpad(g::text, 5, '0'),
	'Mẫu ' || g, 'mau ' || g, '[]', 'active', now() - make_interval(mins => g)
FROM (VALUES ($1::uuid, $3::uuid, 10000), ($2::uuid, $5::uuid, 1000)) AS t (tenant_id, seller_id, n)
CROSS JOIN LATERAL generate_series(1, t.n) AS g;
INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color, size, price, updated_at)
SELECT p.tenant_id, p.seller_id, p.id, p.code || '-' || s.size, p.code || s.size || left(p.tenant_id::text, 4), 'Đen', s.size,
	100000 + s.i, p.updated_at
FROM catalog.products AS p
CROSS JOIN (VALUES (1, 'S'), (2, 'M'), (3, 'L'), (4, 'XL'), (5, 'XXL')) AS s (i, size);
INSERT INTO inventory.stock_balances (tenant_id, variant_id, warehouse_id, seller_id, on_hand, updated_at)
SELECT v.tenant_id, v.id, CASE WHEN v.tenant_id = $1 THEN $6::uuid ELSE $7::uuid END, v.seller_id, 10, v.updated_at
FROM catalog.variants AS v;
INSERT INTO platform.files (tenant_id, purpose, bucket, key, original_name, declared_type, size_bytes, status)
SELECT p.tenant_id, 'product_image', 'tmp', 'tmp/' || p.id || '/' || k, 'anh.jpg', 'image/jpeg', 1000, 'ready'
FROM catalog.products AS p CROSS JOIN generate_series(1, 2) AS k
WHERE p.tenant_id = $1 AND substr(p.code, 2)::int % 2 = 0;
INSERT INTO catalog.product_images (tenant_id, product_id, file_id, position)
SELECT f.tenant_id, split_part(f.key, '/', 2)::uuid, f.id, split_part(f.key, '/', 3)::int
FROM platform.files AS f WHERE f.purpose = 'product_image';
INSERT INTO catalog.attributes (id, tenant_id, code, name, position) VALUES ($9, $1, 'chat-lieu', 'Chất liệu', 1);
INSERT INTO catalog.attribute_values (tenant_id, attribute_id, value, position)
SELECT $1, $9, 'Giá trị ' || g, g FROM generate_series(1, 20) AS g;
INSERT INTO catalog.product_attribute_values (tenant_id, product_id, attribute_value_id)
SELECT p.tenant_id, p.id, av.id FROM catalog.products AS p
JOIN catalog.attribute_values AS av ON av.position = 1 + substr(p.code, 2)::int % 20
WHERE p.tenant_id = $1;
INSERT INTO catalog.product_tags (tenant_id, product_id, tag_id)
SELECT tenant_id, id, $8 FROM catalog.products WHERE tenant_id = $1 AND substr(code, 2)::int % 7 = 0;
ANALYZE;
`

// seedNow là mốc updated_at mới nhất của mẫu trong tenant (≈ now() lúc dựng bộ dữ liệu).
func seedNow(t *testing.T, app *db.DB, tenant uuid.UUID) time.Time {
	t.Helper()
	var ts time.Time
	if err := tenancy.WithTenant(t.Context(), app, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT max(updated_at) FROM catalog.products_v`).Scan(&ts)
	}); err != nil {
		t.Fatal(err)
	}
	return ts
}

func explainSeed() (testkit.Seed, []uuid.UUID) {
	names := []string{"tenantA", "tenantB", "sellerA", "partnerA", "sellerB", "whA", "whB", "tag", "attr"}
	ids := make([]uuid.UUID, len(names))
	sql := explainSeedSQL
	for i := len(names); i >= 1; i-- {
		ids[i-1] = testkit.FixedUUID("webcatalog-explain/" + names[i-1])
		sql = strings.ReplaceAll(sql, "$"+strconv.Itoa(i), "'"+ids[i-1].String()+"'::uuid")
	}
	return testkit.Seed{Name: "webcatalog-explain", SQL: slices.Collect(strings.SplitSeq(sql, ";\n"))}, ids
}

// Kế hoạch của các câu đọc danh mục cho website, bằng crm_app trong ngữ cảnh tenant trên 10.000 mẫu, 50.000 biến thể:
// không Seq Scan (Task 190, M5-07).
func TestPlansHaveNoSeqScan(t *testing.T) {
	seed, ids := explainSeed()
	adminURL, _, appURL := testkit.StartPostgresSeeded(t, seed)
	testkit.EnableAutoExplain(t, adminURL)
	app, err := db.Open(t.Context(), appURL, db.PoolOptions{MaxConns: 4})
	t.Cleanup(func() {
		if app != nil {
			app.Pool.Close()
		}
	})
	if err != nil {
		t.Fatalf("mở pool crm_app: %v", err)
	}
	tenant := ids[0]
	var page []uuid.UUID
	if err := tenancy.WithTenant(t.Context(), app, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), `SELECT product_id FROM catalog.products_v ORDER BY product_id LIMIT 200`)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		page, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(page) != 200 {
		t.Fatalf("mong 200 mẫu, có %d", len(page))
	}
	// Mốc của luồng thay đổi theo mốc lúc dựng mẫu (now() của bộ dữ liệu bị đóng băng): website hỏi lại mỗi 15 s với
	// since = until − 30 s, cửa sổ thật chỉ vài chục giây; ở đây 3 phút (3 mẫu, 15 biến thể, 15 số dư) và 30 phút.
	// Cửa sổ vài giờ (website ngừng lâu) cho phép kế hoạch nối băm trên bảng mẫu: đọc một lần, không phải đường thường.
	newest := seedNow(t, app, tenant)
	sellers := []uuid.UUID{ids[2], ids[3]}
	cases := []struct {
		name, file, query string
		args              []any
	}{
		{"trang mẫu đầu", "products.sql.go", "productPage", []any{uuid.Nil, sellers, 200}},
		{"trang mẫu sau", "products.sql.go", "productPage", []any{page[199], sellers, 200}},
		{"mẫu theo mã", "products.sql.go", "productsByIDs", []any{page[:100], sellers}},
		{"biến thể của trang", "products.sql.go", "variantsOf", []any{ids[5], page}},
		{"ảnh của trang", "products.sql.go", "imagesOf", []any{page}},
		{"nhãn của trang", "products.sql.go", "tagIDsOf", []any{page}},
		{"thuộc tính của trang", "products.sql.go", "attributeValueIDsOf", []any{page}},
		{"thay đổi 3 phút", "changes.sql.go", "changes", []any{newest.Add(-3 * time.Minute), 1000}},
		{"thay đổi 30 phút", "changes.sql.go", "changes", []any{newest.Add(-30 * time.Minute), 1000}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nodes, err := testkit.ExplainAsApp(t.Context(), app, tenant, storeSQL(t, c.file, c.query), c.args...)
			if err != nil {
				t.Fatal(err)
			}
			testkit.RequireNoSeqScan(t, nodes)
		})
	}
}
