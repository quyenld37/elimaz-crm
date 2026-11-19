package queries_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// fixture: tenant A có 10.000 mẫu, 50.000 biến thể (5 size mỗi mẫu), 1 kho có số dư cho mọi biến thể, 3 danh mục
// (Váy > Váy dài, Áo), 1 nhãn; tenant B có 1.000 mẫu, 5.000 biến thể cùng kiểu tên và cùng mã đặc biệt DAM01.
// Tên: <loại> <chất liệu> mẫu <g>; loại theo g % 5 (Đầm, Áo, Quần, Chân váy, Set), chất liệu theo (g / 5) % 4 (lụa,
// thun, jean, nhung) nên "dam lua" khớp đúng các mẫu g % 20 = 0. Mẫu DAM01 có SKU DAM01-S, DAM01-M, DAM01-L; mẫu DAM011
// có DAM011-S (không khớp tiền tố "DAM01-").
type fixture struct {
	app               *db.DB
	tenantA, tenantB  uuid.UUID
	sellerA, sellerB  uuid.UUID
	warehouseA        uuid.UUID
	catDress, catLong uuid.UUID // Váy, Váy > Váy dài
	catTop            uuid.UUID // Áo
	tagNew            uuid.UUID
	tenantC, sellerC  uuid.UUID // 250 mẫu, 50 cái cùng updated_at (thử trang)
	barcodeOfP00042S  string    // barcode của biến thể size S của mẫu P00042
}

const seedSQL = `
INSERT INTO platform.tenants (id, name) VALUES ($1, 'A'), ($2, 'B'), ($3, 'C');
INSERT INTO catalog.sellers (id, tenant_id, code, name, kind) VALUES
	($4, $1, 'ELIMAZ', 'ELIMAZ', 'own'), ($5, $2, 'ELIMAZ', 'ELIMAZ', 'own'), ($6, $3, 'ELIMAZ', 'ELIMAZ', 'own');
INSERT INTO inventory.warehouses (id, tenant_id, operated_by_seller_id, code, name) VALUES ($7, $1, $4, 'KHO1', 'Kho chính');
INSERT INTO catalog.categories (id, tenant_id, parent_id, name, slug, depth) VALUES
	($8, $1, NULL, 'Váy', 'vay', 1), ($9, $1, $8, 'Váy dài', 'vay-dai', 2), ($10, $1, NULL, 'Áo', 'ao', 1);
INSERT INTO catalog.tags (id, tenant_id, code, name) VALUES ($11, $1, 'hang-moi', 'Hàng mới');

CREATE TEMP TABLE kinds (i int, name text, fold text);
INSERT INTO kinds VALUES (0, 'Đầm', 'dam'), (1, 'Áo', 'ao'), (2, 'Quần', 'quan'), (3, 'Chân váy', 'chan vay'), (4, 'Set', 'set');
CREATE TEMP TABLE mats (i int, name text, fold text);
INSERT INTO mats VALUES (0, 'lụa', 'lua'), (1, 'thun', 'thun'), (2, 'jean', 'jean'), (3, 'nhung', 'nhung');

INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, status, category_id, updated_at)
SELECT t.tenant_id, t.seller_id, t.pfx || lpad(g::text, 5, '0'),
	k.name || ' ' || m.name || ' mẫu ' || g, k.fold || ' ' || m.fold || ' mau ' || g,
	(ARRAY['active', 'active', 'draft', 'discontinued'])[1 + g % 4],
	CASE WHEN t.tenant_id = $1 THEN (ARRAY[$9, $10, NULL]::uuid[])[1 + g % 3] END,
	now() - make_interval(secs => g)
FROM (VALUES ($1::uuid, $4::uuid, 'P', 10000), ($2::uuid, $5::uuid, 'B', 1000)) AS t (tenant_id, seller_id, pfx, n)
CROSS JOIN LATERAL generate_series(1, t.n) AS g
JOIN kinds AS k ON k.i = g % 5
JOIN mats AS m ON m.i = (g / 5) % 4;

INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color, size, price)
SELECT p.tenant_id, p.seller_id, p.id, p.code || '-' || s.size,
	CASE WHEN p.tenant_id = $1 THEN '89' ELSE '88' END || lpad((substr(p.code, 2)::int * 10 + s.i)::text, 11, '0'),
	'Đen', s.size, 100000 + s.i * 10000
FROM catalog.products AS p
CROSS JOIN (VALUES (1, 'S'), (2, 'M'), (3, 'L'), (4, 'XL'), (5, 'XXL')) AS s (i, size);

INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, status) VALUES
	($1, $4, 'DAM01', 'Đầm thử tiền tố', 'dam thu tien to', 'active'),
	($1, $4, 'DAM011', 'Đầm thử tiền tố dài', 'dam thu tien to dai', 'active'),
	($2, $5, 'DAM01', 'Đầm của tenant B', 'dam cua tenant b', 'active');
INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color, size, price)
SELECT p.tenant_id, p.seller_id, p.id, p.code || '-' || s, 'X' || p.code || s || CASE WHEN p.tenant_id = $1 THEN 'A' ELSE 'B' END,
	'Đỏ', s, 200000
FROM catalog.products AS p, unnest(ARRAY['S', 'M', 'L']) AS s
WHERE p.code IN ('DAM01', 'DAM011') AND (p.code = 'DAM01' OR s = 'S');

INSERT INTO inventory.stock_balances (tenant_id, variant_id, warehouse_id, seller_id, on_hand, held)
SELECT v.tenant_id, v.id, $7, v.seller_id, 10, CASE WHEN p.code = 'P00003' THEN 12 ELSE 1 END
FROM catalog.variants AS v JOIN catalog.products AS p ON p.id = v.product_id
WHERE v.tenant_id = $1;

-- Mẫu NOSTOCK: một biến thể barcode chữ hoa lẫn thường, chưa có dòng số dư ở kho nào (ghi sau khối số dư).
INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, status) VALUES
	($1, $4, 'NOSTOCK', 'Mẫu chưa nhập kho', 'mau chua nhap kho', 'active');
INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color, size, price)
SELECT tenant_id, seller_id, id, 'NOSTOCK-S', 'Ab12Cd', 'Đỏ', 'S', 90000 FROM catalog.products WHERE code = 'NOSTOCK';

INSERT INTO catalog.product_tags (tenant_id, product_id, tag_id)
SELECT tenant_id, id, $11 FROM catalog.products WHERE tenant_id = $1 AND code LIKE 'P%' AND substr(code, 2)::int % 7 = 0;

-- Ảnh (Task M1-44): mỗi mẫu P… của tenant A có 3 ảnh (ảnh thứ 3 đã xóa mềm ở mẫu chẵn), tệp ready; ảnh bìa của
-- danh sách đọc catalog.product_images_v theo index (tenant_id, product_id, position, id).
INSERT INTO platform.files (tenant_id, purpose, bucket, key, original_name, declared_type, size_bytes, status)
SELECT p.tenant_id, 'product_image', 'tmp', 'tmp/' || p.id || '/' || k, 'anh.jpg', 'image/jpeg', 1000, 'ready'
FROM catalog.products AS p CROSS JOIN generate_series(1, 3) AS k
WHERE p.tenant_id = $1 AND p.code LIKE 'P%';
INSERT INTO catalog.product_images (tenant_id, product_id, file_id, position, deleted_at)
SELECT f.tenant_id, split_part(f.key, '/', 2)::uuid, f.id, split_part(f.key, '/', 3)::int,
	CASE WHEN split_part(f.key, '/', 3) = '3' AND p.code ~ '[02468]$' THEN now() END
FROM platform.files AS f JOIN catalog.products AS p ON p.id = split_part(f.key, '/', 2)::uuid
WHERE f.purpose = 'product_image';

-- Tenant C: 250 mẫu, 50 cái (g ≤ 50) cùng updated_at.
INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, status, updated_at)
SELECT $3, $6, 'C' || g, 'Mẫu ' || g, 'mau ' || g, 'active',
	CASE WHEN g <= 50 THEN timestamptz '2026-11-01 00:00:00+00' ELSE timestamptz '2026-11-01 00:00:00+00' + make_interval(secs => g) END
FROM generate_series(1, 250) AS g;

ANALYZE;
`

// fixtureSeed là seedSQL với $n thay bằng hằng uuid (từ số lớn xuống để $1 không ăn vào $10, $11), tách từng câu:
// mỗi câu tự commit (gửi cả khối trong một transaction ngầm chậm hơn khoảng 6 lần).
func fixtureSeed(ids []uuid.UUID) testkit.Seed {
	sql := seedSQL
	for i := len(ids); i >= 1; i-- {
		sql = strings.ReplaceAll(sql, "$"+strconv.Itoa(i), "'"+ids[i-1].String()+"'::uuid")
	}
	return testkit.Seed{Name: "queries-fixture", SQL: slices.Collect(strings.SplitSeq(sql, ";\n"))}
}

// newFixture lấy database thử có sẵn dữ liệu (dựng một lần làm database mẫu bằng người quản trị, bỏ qua RLS, kèm
// ANALYZE), bật auto_explain rồi mở pool crm_app.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	// 11 mã cố định viết thẳng vào seedSQL ($1 … $11 theo thứ tự), qua testkit.FixedUUID (trước Task 423 là uuidv7
	// ngẫu nhiên mỗi lần chạy: mã cố định để bộ dữ liệu dựng một lần làm database mẫu).
	names := []string{
		"tenantA", "tenantB", "tenantC", "sellerA", "sellerB", "sellerC", "warehouseA", "catDress", "catLong", "catTop", "tagNew",
	}
	ids := make([]uuid.UUID, len(names))
	for i, n := range names {
		ids[i] = testkit.FixedUUID("queries-fixture/" + n)
	}
	f.tenantA, f.tenantB, f.tenantC = ids[0], ids[1], ids[2]
	f.sellerA, f.sellerB, f.sellerC = ids[3], ids[4], ids[5]
	f.warehouseA = ids[6]
	f.catDress, f.catLong, f.catTop = ids[7], ids[8], ids[9]
	f.tagNew = ids[10]
	f.barcodeOfP00042S = "8900000000421"

	adminURL, _, appURL := testkit.StartPostgresSeeded(t, fixtureSeed(ids))
	testkit.EnableAutoExplain(t, adminURL)
	app, err := db.Open(t.Context(), appURL, db.PoolOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Pool.Close)
	f.app = app
	return f
}

// inTenant chạy fn bằng crm_app trong ngữ cảnh tenant.
func (f *fixture) inTenant(t *testing.T, tenant uuid.UUID, fn func(tx pgx.Tx) error) {
	t.Helper()
	if err := tenancy.WithTenant(t.Context(), f.app, tenant, fn, db.TxAPI); err != nil {
		t.Fatal(err)
	}
}
