package webcatalog_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/inventory/invtest"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries/webcatalog"
	"elimaz.com.vn/crm/internal/testkit"
)

const testBase = "https://img.elimaz.test"

func view() webcatalog.ImageView { return webcatalog.ImageView{BaseURL: testBase, Preset: 1} }

// env: tenant có người bán ELIMAZ (400 mẫu) và PARTNER1 (50 mẫu), kho MAIN; mỗi mẫu 3 biến thể S/M/L; mọi dòng có
// updated_at lùi 1 ngày (để luồng thay đổi chỉ thấy việc của test).
type env struct {
	t                *testing.T
	staff            *testkit.Staff
	tenant           uuid.UUID
	seller, partner  uuid.UUID
	warehouse        uuid.UUID
	productsOfSeller map[uuid.UUID][]uuid.UUID
	n                int // số đơn thử đã chèn (mã đơn)
}

const seedSQL = `
INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, description, article, status, updated_at)
SELECT $1::uuid, CASE WHEN g <= 400 THEN $2::uuid ELSE $3::uuid END, 'W' || lpad(g::text, 3, '0'), 'Mẫu ' || g, 'mau ' || g,
	'Mô tả ' || g, '[]', 'active', now() - interval '1 day'
FROM generate_series(1, 450) AS g;
INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color, size, price, updated_at)
SELECT p.tenant_id, p.seller_id, p.id, p.code || '-' || s, 'B' || p.code || s, 'Đen', s, 200000, now() - interval '1 day'
FROM catalog.products AS p CROSS JOIN unnest(ARRAY['S', 'M', 'L']) AS s
WHERE p.tenant_id = $1::uuid AND $2::uuid IS NOT NULL AND $3::uuid IS NOT NULL;`

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, staff: testkit.OpenStaff(t)}
	e.tenant = e.staff.Tenant(t, "A")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		e.seller, e.partner = catalogtest.Sellers(t, tx, e.tenant)
		var err error
		if e.warehouse, err = inventory.EnsureDefaultWarehouse(t.Context(), tx, e.tenant, e.seller); err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, q := range strings.Split(seedSQL, ";\n") {
			if _, err := tx.Exec(t.Context(), q, e.tenant, e.seller, e.partner); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	e.productsOfSeller = map[uuid.UUID][]uuid.UUID{}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), "SELECT seller_id, id FROM catalog.products ORDER BY id")
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rows.Close()
		for rows.Next() {
			var s, id uuid.UUID
			if err := rows.Scan(&s, &id); err != nil {
				return err //nolint:wrapcheck // test
			}
			e.productsOfSeller[s] = append(e.productsOfSeller[s], id)
		}
		return rows.Err()
	})
	return e
}

// read chạy fn bằng crm_app trong ngữ cảnh tenant.
func (e *env) read(fn func(tx pgx.Tx) error) {
	e.t.Helper()
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn, db.TxAPI); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) owner(fn func(tx pgx.Tx) error) { e.t.Helper(); e.staff.Exec(e.t, e.tenant, fn) }

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	e.owner(func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), sql, args...)
		return err //nolint:wrapcheck // test
	})
}

// variantIDs: mã biến thể của mẫu theo mã (đọc bằng chủ bảng).
func (e *env) variantIDs(product uuid.UUID) []uuid.UUID {
	e.t.Helper()
	var vs []uuid.UUID
	e.owner(func(tx pgx.Tx) error {
		rows, err := tx.Query(e.t.Context(), "SELECT id FROM catalog.variants WHERE product_id = $1 ORDER BY id", product)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		vs, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err //nolint:wrapcheck // test
	})
	return vs
}

// byID: ProductsByID với view mặc định, khóa thường (không xem trước).
func (e *env) byID(ids []uuid.UUID) []webcatalog.WebProduct {
	e.t.Helper()
	return e.byIDWith(ids, false)
}

// byIDWith: ProductsByID với view mặc định; preview như khóa có web.catalog.preview (Task 261).
func (e *env) byIDWith(ids []uuid.UUID, preview bool) []webcatalog.WebProduct {
	e.t.Helper()
	var ps []webcatalog.WebProduct
	e.read(func(tx pgx.Tx) error {
		var err error
		ps, err = webcatalog.ProductsByID(e.t.Context(), tx, ids, view(), preview)
		return err //nolint:wrapcheck // test
	})
	return ps
}

func (e *env) all() []uuid.UUID {
	return append(slices.Clone(e.productsOfSeller[e.seller]), e.productsOfSeller[e.partner]...)
}

// pages đọc mọi trang limit (khóa thường) và trả mã mẫu theo thứ tự cùng cỡ từng trang.
func (e *env) pages(limit int) ([]uuid.UUID, []int) {
	e.t.Helper()
	ids, sizes, _ := e.pagesWith(limit, false)
	return ids, sizes
}

// pagesWith như pages với cờ xem trước preview; trả thêm mã các mẫu có Preview = true.
func (e *env) pagesWith(limit int, preview bool) (ids []uuid.UUID, sizes []int, previewed []uuid.UUID) {
	e.t.Helper()
	after := uuid.Nil
	for range 10 {
		var ps []webcatalog.WebProduct
		var next uuid.UUID
		e.read(func(tx pgx.Tx) error {
			var err error
			ps, next, err = webcatalog.Products(e.t.Context(), tx, after, limit, view(), preview)
			return err //nolint:wrapcheck // test
		})
		sizes = append(sizes, len(ps))
		for _, p := range ps {
			ids = append(ids, p.ID)
			if p.Preview {
				previewed = append(previewed, p.ID)
			}
		}
		if next == uuid.Nil {
			return ids, sizes, previewed
		}
		after = next
	}
	e.t.Fatal("quá 10 trang")
	return nil, nil, nil
}

// 450 mẫu, limit 200 → 3 trang 200, 200, 50; đủ 450 mã, không trùng, mã tăng dần; mỗi mẫu 3 biến thể.
func TestProducts_Pages(t *testing.T) {
	e := newEnv(t)
	ids, sizes := e.pages(200)
	if !slices.Equal(sizes, []int{200, 200, 50}) {
		t.Fatalf("cỡ trang: %v", sizes)
	}
	want := e.all()
	slices.SortFunc(want, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	if !slices.Equal(ids, want) {
		t.Fatalf("mã theo trang khác mã đã tạo (%d / %d)", len(ids), len(want))
	}
	e.read(func(tx pgx.Tx) error {
		ps, _, err := webcatalog.Products(t.Context(), tx, uuid.Nil, 5, view(), false)
		for _, p := range ps {
			if len(p.Variants) != 3 || p.ShortDescription == "" || string(p.Article) != "[]" {
				t.Errorf("mẫu %s: %d biến thể, mô tả %q, bài viết %s", p.Code, len(p.Variants), p.ShortDescription, p.Article)
			}
		}
		return err //nolint:wrapcheck // test
	})
	for _, bad := range []int{0, webcatalog.MaxPage + 1} {
		e.read(func(tx pgx.Tx) error {
			if _, _, err := webcatalog.Products(t.Context(), tx, uuid.Nil, bad, view(), false); !errors.Is(err, webcatalog.ErrBadLimit) {
				t.Errorf("limit %d: mong ErrBadLimit, có %v", bad, err)
			}
			return nil
		})
	}
}

// ids: 2 mã (kèm 1 mã lạ, 1 mã trùng) → đúng 2 mẫu; 101 mã → ErrBadLimit; 100 mã → 100 mẫu.
func TestProductsByID(t *testing.T) {
	e := newEnv(t)
	all := e.all()
	e.read(func(tx pgx.Tx) error {
		ps, err := webcatalog.ProductsByID(t.Context(), tx, []uuid.UUID{all[7], uuid.New(), all[3], all[7]}, view(), false)
		if err != nil || len(ps) != 2 {
			t.Fatalf("2 mã: %d mẫu, %v", len(ps), err)
		}
		if _, err := webcatalog.ProductsByID(t.Context(), tx, all[:webcatalog.MaxIDs+1], view(), false); !errors.Is(err, webcatalog.ErrBadLimit) {
			t.Errorf("101 mã: mong ErrBadLimit, có %v", err)
		}
		if ps, err := webcatalog.ProductsByID(t.Context(), tx, all[:webcatalog.MaxIDs], view(), false); err != nil || len(ps) != webcatalog.MaxIDs {
			t.Errorf("100 mã: %d mẫu, %v", len(ps), err)
		}
		return nil
	})
}

// createdAt: ngày tạo của mẫu (cột created_at), giữ nguyên khi sửa mẫu (updated_at đổi), theo UTC; cùng giá trị ở
// Products và ProductsByID; mẫu khác có createdAt khác 0.
func TestProducts_CreatedAt(t *testing.T) {
	e := newEnv(t)
	all := e.all()
	old := time.Date(2024, 3, 9, 1, 2, 3, 456000000, time.FixedZone("ICT", 7*3600))
	e.exec("UPDATE catalog.products SET created_at = $2 WHERE id = $1", all[5], old)
	e.exec("UPDATE catalog.products SET name = 'Đổi tên', updated_at = now() WHERE id = $1", all[5])
	var byID []webcatalog.WebProduct
	e.read(func(tx pgx.Tx) error {
		var err error
		byID, err = webcatalog.ProductsByID(t.Context(), tx, []uuid.UUID{all[5], all[6]}, view(), false)
		return err //nolint:wrapcheck // test
	})
	if len(byID) != 2 {
		t.Fatalf("mong 2 mẫu, có %d", len(byID))
	}
	checkCreatedAt(t, "ProductsByID", byID, all[5], old)
	checkCreatedAt(t, "Products", e.allProducts(), all[5], old)
}

// Task 516, 517: số đo đặt thẳng trên bảng (Dài 725 mm, Cao 3 mm) → MeasureMm của ProductsByID và Products đúng ba số (Rộng nil);
// mẫu khác không có số đo.
func TestProducts_Measure(t *testing.T) {
	e := newEnv(t)
	all := e.all()
	e.exec("UPDATE catalog.products SET length_mm = 725, height_mm = 3 WHERE id = $1", all[3])
	var byID []webcatalog.WebProduct
	e.read(func(tx pgx.Tx) error {
		var err error
		byID, err = webcatalog.ProductsByID(t.Context(), tx, []uuid.UUID{all[3], all[4]}, view(), false)
		return err //nolint:wrapcheck // test
	})
	for from, ps := range map[string][]webcatalog.WebProduct{"ProductsByID": byID, "Products": e.allProducts()} {
		seen := false
		for _, p := range ps {
			m := p.MeasureMm
			if p.ID != all[3] {
				if m != (webcatalog.MeasureMm{}) {
					t.Errorf("%s: mẫu %s có số đo %+v", from, p.Code, m)
				}
				continue
			}
			seen = true
			if m.Length == nil || *m.Length != 725 || m.Width != nil || m.Height == nil || *m.Height != 3 {
				t.Errorf("%s: số đo %+v, mong Dài 725, Cao 3", from, m)
			}
		}
		if !seen {
			t.Errorf("%s: không thấy mẫu có số đo", from)
		}
	}
}

// checkCreatedAt: mẫu id có createdAt = want (UTC), sau updatedAt; mọi mẫu khác có createdAt khác 0, theo UTC.
func checkCreatedAt(t *testing.T, from string, ps []webcatalog.WebProduct, id uuid.UUID, want time.Time) {
	t.Helper()
	found := false
	for _, p := range ps {
		switch {
		case p.ID == id:
			found = true
			if !p.CreatedAt.Equal(want) || p.CreatedAt.Location() != time.UTC || !p.UpdatedAt.After(p.CreatedAt) {
				t.Errorf("%s: createdAt %v (mong %v UTC), updatedAt %v", from, p.CreatedAt, want.UTC(), p.UpdatedAt)
			}
		case p.CreatedAt.IsZero() || p.CreatedAt.Location() != time.UTC:
			t.Errorf("%s: mẫu %s createdAt %v", from, p.Code, p.CreatedAt)
		}
	}
	if !found {
		t.Errorf("%s: không thấy mẫu đã đặt ngày tạo", from)
	}
}

// allProducts đọc mọi trang MaxPage của Products.
func (e *env) allProducts() []webcatalog.WebProduct {
	e.t.Helper()
	var out []webcatalog.WebProduct
	for after := uuid.Nil; ; {
		var (
			ps   []webcatalog.WebProduct
			next uuid.UUID
		)
		e.read(func(tx pgx.Tx) error {
			var err error
			ps, next, err = webcatalog.Products(e.t.Context(), tx, after, webcatalog.MaxPage, view(), false)
			return err //nolint:wrapcheck // test
		})
		out = append(out, ps...)
		if next == uuid.Nil {
			return out
		}
		after = next
	}
}

// ADR-29: người bán thứ hai suspended → 50 mẫu của họ không có trong Products, ProductsByID; active lại → có.
func TestProducts_SellerSuspended(t *testing.T) {
	e := newEnv(t)
	e.exec("UPDATE catalog.sellers SET status = 'suspended' WHERE id = $1", e.partner)
	ids, _ := e.pages(200)
	if len(ids) != 400 || slices.ContainsFunc(ids, func(id uuid.UUID) bool { return slices.Contains(e.productsOfSeller[e.partner], id) }) {
		t.Fatalf("người bán ngừng: mong 400 mẫu của ELIMAZ, có %d", len(ids))
	}
	e.read(func(tx pgx.Tx) error {
		ps, err := webcatalog.ProductsByID(t.Context(), tx, e.productsOfSeller[e.partner][:3], view(), false)
		if err != nil || len(ps) != 0 {
			t.Errorf("theo mã của người bán ngừng: %d mẫu, %v", len(ps), err)
		}
		return nil
	})
	e.exec("UPDATE catalog.sellers SET status = 'active' WHERE id = $1", e.partner)
	if ids, _ := e.pages(200); len(ids) != 450 {
		t.Fatalf("người bán hoạt động lại: mong 450, có %d", len(ids))
	}
}

// [WB09]: biến thể ngừng bán vẫn có trong kết quả với status discontinued, sellable false; mẫu nháp (chỉ khi xem
// trước, Task 261) có với status draft, preview true và mọi biến thể sellable false; còn hàng ở kho MAIN → available; cân nặng rỗng → nil, 450 → 450.
func TestProducts_VariantFields(t *testing.T) {
	e := newEnv(t)
	all := e.all()
	vs := e.variantIDs(all[0])
	e.owner(func(tx pgx.Tx) error {
		catalogtest.Discontinue(t, tx, e.seller, vs[1])
		invtest.SeedStock(t, tx, vs[0], e.warehouse, 3)
		_, err := tx.Exec(t.Context(), "UPDATE catalog.variants SET weight_grams = 450 WHERE id = $1", vs[2])
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err = tx.Exec(t.Context(), "UPDATE catalog.products SET status = 'draft' WHERE id = $1", all[1])
		return err //nolint:wrapcheck // test
	})
	ps := e.byIDWith(all[:2], true)
	got := ps[0].Variants
	if got[0].ID != vs[0] || !got[0].Available || !got[0].Sellable || got[0].Status != "active" || got[0].WeightGrams != nil {
		t.Errorf("biến thể còn hàng: %+v", got[0])
	}
	if got[1].Status != "discontinued" || got[1].Sellable || got[1].Available {
		t.Errorf("biến thể ngừng bán: %+v", got[1])
	}
	if got[2].WeightGrams == nil || *got[2].WeightGrams != 450 || got[2].Available {
		t.Errorf("biến thể có cân nặng, chưa nhập kho: %+v", got[2])
	}
	if ps[0].Preview || !ps[1].Preview || ps[1].Status != "draft" || slices.ContainsFunc(ps[1].Variants, func(v webcatalog.WebVariant) bool { return v.Sellable }) {
		t.Errorf("mẫu nháp: %s, %+v", ps[1].Status, ps[1].Variants)
	}
}

// dbNow là now() của PostgreSQL (một đồng hồ cho mọi so sánh mốc, Bẫy đã gặp).
func (e *env) dbNow() time.Time {
	e.t.Helper()
	var now time.Time
	e.read(func(tx pgx.Tx) error { return tx.QueryRow(e.t.Context(), "SELECT now()").Scan(&now) })
	return now
}

func (e *env) changes(since time.Time, limit int) ([]uuid.UUID, time.Time, bool) {
	e.t.Helper()
	ids, until, more, _ := e.changesAt(since, limit)
	return ids, until, more
}

// changesAt như changes, kèm serverTime.
func (e *env) changesAt(since time.Time, limit int) ([]uuid.UUID, time.Time, bool, time.Time) {
	e.t.Helper()
	var (
		ids          []uuid.UUID
		until, srvAt time.Time
		more         bool
	)
	e.read(func(tx pgx.Tx) error {
		var err error
		ids, until, more, srvAt, err = webcatalog.Changes(e.t.Context(), tx, since, limit)
		return err //nolint:wrapcheck // test
	})
	return ids, until, more, srvAt
}

// Đổi giá biến thể v1 lúc T → Changes(T − 30 s) có đúng mẫu của v1; nhập kho cho v2 → thêm mẫu của v2; sửa mẫu →
// thêm mẫu đó. Chưa đủ limit → until là now() của lần đọc, more false; đủ limit → until là mốc của mã cuối, more true.
func TestChanges(t *testing.T) {
	e := newEnv(t)
	all := e.all()
	t0 := e.dbNow()
	if ids, until, more := e.changes(t0.Add(-30*time.Second), 1000); len(ids) != 0 || more || until.Before(t0) {
		t.Fatalf("chưa đổi gì: %v, until %v (t0 %v), more %v", ids, until, t0, more)
	}
	v1, v2 := e.firstVariant(all[10]), e.firstVariant(all[20])
	e.updatePrice(v1)
	ids, _, _ := e.changes(t0.Add(-30*time.Second), 1000)
	if !slices.Equal(ids, []uuid.UUID{all[10]}) {
		t.Fatalf("sau đổi giá v1: mong đúng mẫu của v1, có %v", ids)
	}
	e.owner(func(tx pgx.Tx) error { invtest.SeedStock(t, tx, v2, e.warehouse, 5); return nil })
	e.exec("UPDATE catalog.products SET name = 'Đổi tên', updated_at = now() WHERE id = $1", all[30])
	ids, until, more := e.changes(t0.Add(-30*time.Second), 1000)
	if !slices.Equal(ids, []uuid.UUID{all[10], all[20], all[30]}) || more {
		t.Fatalf("sau nhập kho v2, sửa mẫu: %v (more %v)", ids, more)
	}
	ids, until2, more, srv2 := e.changesAt(t0.Add(-30*time.Second), 2)
	if len(ids) != 2 || !more || !until2.Before(until) {
		t.Fatalf("limit 2: %v, until %v, more %v", ids, until2, more)
	}
	// serverTime: now() của lần đọc, có cả khi more (until là mốc của mã cuối, sớm hơn).
	if srv2.Before(until) || srv2.Location() != time.UTC {
		t.Errorf("limit 2: serverTime %v phải ≥ until đầy đủ %v, theo UTC", srv2, until)
	}
	// Chưa đủ limit: serverTime = until = now() của lần đọc.
	if _, u, m, srv := e.changesAt(t0.Add(-30*time.Second), 1000); m || !srv.Equal(u) || srv.Before(t0) {
		t.Errorf("chưa đủ limit: serverTime %v, until %v, more %v", srv, u, m)
	}
	if ids, _, _ := e.changes(until.Add(time.Second), 1000); len(ids) != 0 {
		t.Errorf("sau mốc until: mong rỗng, có %v", ids)
	}
	for _, bad := range []int{0, webcatalog.MaxChanges + 1} {
		e.read(func(tx pgx.Tx) error {
			if _, _, _, _, err := webcatalog.Changes(t.Context(), tx, t0, bad); !errors.Is(err, webcatalog.ErrBadLimit) {
				t.Errorf("limit %d: mong ErrBadLimit, có %v", bad, err)
			}
			return nil
		})
	}
}

// updatePrice đổi giá biến thể id qua catalog.Writer (đường ghi thật: tăng updated_at của mẫu, phát price.changed).
func (e *env) updatePrice(id uuid.UUID) {
	e.t.Helper()
	uid := e.staff.User(e.t, e.tenant, "chu-"+id.String()[:8]+"@elimaz.test", "active")
	actor := identity.AuthUser{UserID: uid, TenantID: e.tenant}
	if err := tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, func(tx pgx.Tx) error {
		var version int64
		if err := tx.QueryRow(e.t.Context(), "SELECT version FROM catalog.variants WHERE id = $1", id).Scan(&version); err != nil {
			return err //nolint:wrapcheck // test
		}
		w := catalog.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
		_, err := w.UpdateVariant(e.t.Context(), tx, e.seller, id, version, catalog.VariantPatch{Price: ptr(int64(210000))}, actor)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) firstVariant(product uuid.UUID) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.owner(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), "SELECT id FROM catalog.variants WHERE product_id = $1 ORDER BY id LIMIT 1", product).Scan(&id)
	})
	return id
}

// recordingTx ghi lại câu SQL của mọi lần Query qua tx (đếm lời gọi files.PublicVariants: câu ListPublicVariants).
type recordingTx struct {
	pgx.Tx
	sqls *[]string
}

//nolint:ireturn // hiện thực pgx.Tx
func (r recordingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	*r.sqls = append(*r.sqls, sql)
	return r.Tx.Query(ctx, sql, args...) //nolint:wrapcheck // test
}

var v1URL = regexp.MustCompile(`^` + regexp.QuoteMeta(testBase) + `/img/[0-9a-f]{64}/v1/`)

// A2c: mẫu có 2 ảnh ready và 1 ảnh đang xử lý → đúng 2 ảnh theo vị trí, mỗi ảnh đủ biến thể của bộ v1 (chiều rộng,
// định dạng, đường dẫn công khai); cả trang 200 mẫu gọi files.PublicVariants đúng một lần; trang không có ảnh không gọi.
func TestProducts_Images(t *testing.T) {
	e := newEnv(t)
	all := e.all()
	if n := e.publicVariantCalls(); n != 0 {
		t.Fatalf("trang không có ảnh: mong 0 lần PublicVariants, có %d", n)
	}
	var ready1, ready2, processing, other uuid.UUID
	e.owner(func(tx pgx.Tx) error {
		ready1, ready2 = filestest.Image(t, tx, e.tenant, "product_image"), filestest.Image(t, tx, e.tenant, "product_image")
		processing = filestest.Uploaded(t, tx, e.tenant, "product_image")
		other = filestest.Image(t, tx, e.tenant, "product_image")
		for _, im := range []struct {
			product, file uuid.UUID
			pos           int
			color         *string
		}{{all[0], ready2, 3, nil}, {all[0], processing, 2, nil}, {all[0], ready1, 1, ptr("Đen")}, {all[5], other, 1, nil}} {
			if _, err := tx.Exec(t.Context(), `INSERT INTO catalog.product_images (tenant_id, product_id, file_id, position, color)
				VALUES ($1, $2, $3, $4, $5)`, e.tenant, im.product, im.file, im.pos, im.color); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	if n := e.publicVariantCalls(); n != 1 {
		t.Fatalf("trang 200 mẫu có ảnh: mong 1 lần PublicVariants, có %d", n)
	}
	var p webcatalog.WebProduct
	e.read(func(tx pgx.Tx) error {
		ps, err := webcatalog.ProductsByID(t.Context(), tx, all[:1], view(), false)
		p = ps[0]
		return err //nolint:wrapcheck // test
	})
	if len(p.Images) != 2 || p.Images[0].Position != 1 || p.Images[1].Position != 3 {
		t.Fatalf("mong 2 ảnh ở vị trí 1, 3: %+v", p.Images)
	}
	if p.Images[0].Color == nil || *p.Images[0].Color != "Đen" || p.Images[1].Color != nil {
		t.Errorf("màu của ảnh: %+v", p.Images)
	}
	b, _ := json.Marshal(p.Images)
	for _, im := range p.Images {
		if len(im.Variants) != 12 {
			t.Errorf("mong 12 biến thể của bộ v1, có %d: %s", len(im.Variants), b)
		}
		for _, v := range im.Variants {
			if v.Width <= 0 || v.Format == "" || !v1URL.MatchString(v.URL) {
				t.Errorf("biến thể ảnh sai: %+v", v)
			}
		}
	}
}

func (e *env) publicVariantCalls() int {
	e.t.Helper()
	var sqls []string
	e.read(func(tx pgx.Tx) error {
		_, _, err := webcatalog.Products(e.t.Context(), recordingTx{Tx: tx, sqls: &sqls}, uuid.Nil, 200, view(), false)
		return err //nolint:wrapcheck // test
	})
	n := 0
	for _, s := range sqls {
		if strings.Contains(s, "ListPublicVariants") {
			n++
		}
	}
	return n
}

// Phân loại: chỉ danh mục active theo cấp, vị trí; thuộc tính theo vị trí kèm giá trị theo vị trí; nhãn theo tên. Mẫu
// mang nhãn (mã), thuộc tính (tên, giá trị, vị trí thuộc tính) và mã danh mục.
// seedClassification dựng danh mục (Váy > Váy dài, Áo, Cũ lưu trữ), thuộc tính Dáng, Chất liệu kèm giá trị, nhãn
// Hàng mới, Giảm giá; gắn Váy dài, hai nhãn, Lụa, Xòe cho product. Trả mã theo tên ngắn.
func (e *env) seedClassification(product uuid.UUID) map[string]uuid.UUID {
	e.t.Helper()
	ids := map[string]uuid.UUID{}
	e.owner(func(tx pgx.Tx) error {
		for _, q := range []struct{ name, sql string }{
			{"vay", `INSERT INTO catalog.categories (tenant_id, name, slug, depth, position) VALUES ($1, 'Váy', 'vay', 1, 2) RETURNING id`},
			{"ao", `INSERT INTO catalog.categories (tenant_id, name, slug, depth, position) VALUES ($1, 'Áo', 'ao', 1, 1) RETURNING id`},
			{"cu", `INSERT INTO catalog.categories (tenant_id, name, slug, depth, status) VALUES ($1, 'Cũ', 'cu', 1, 'archived') RETURNING id`},
			{"mau", `INSERT INTO catalog.attributes (tenant_id, code, name, position) VALUES ($1, 'chat-lieu', 'Chất liệu', 2) RETURNING id`},
			{"dang", `INSERT INTO catalog.attributes (tenant_id, code, name, position) VALUES ($1, 'dang', 'Dáng', 1) RETURNING id`},
			{"moi", `INSERT INTO catalog.tags (tenant_id, code, name) VALUES ($1, 'hang-moi', 'Hàng mới') RETURNING id`},
			{"sale", `INSERT INTO catalog.tags (tenant_id, code, name) VALUES ($1, 'sale', 'Giảm giá') RETURNING id`},
		} {
			var id uuid.UUID
			if err := tx.QueryRow(e.t.Context(), q.sql, e.tenant).Scan(&id); err != nil {
				return err //nolint:wrapcheck // test
			}
			ids[q.name] = id
		}
		for _, q := range []struct {
			name, sql string
			args      []any
		}{
			{"vaydai", `INSERT INTO catalog.categories (tenant_id, parent_id, name, slug, depth) VALUES ($1, $2, 'Váy dài', 'vay-dai', 2) RETURNING id`, []any{ids["vay"]}},
			{"lua", `INSERT INTO catalog.attribute_values (tenant_id, attribute_id, value, position) VALUES ($1, $2, 'Lụa', 2) RETURNING id`, []any{ids["mau"]}},
			{"thun", `INSERT INTO catalog.attribute_values (tenant_id, attribute_id, value, position) VALUES ($1, $2, 'Thun', 1) RETURNING id`, []any{ids["mau"]}},
			{"xoe", `INSERT INTO catalog.attribute_values (tenant_id, attribute_id, value, position) VALUES ($1, $2, 'Xòe', 1) RETURNING id`, []any{ids["dang"]}},
		} {
			var id uuid.UUID
			if err := tx.QueryRow(e.t.Context(), q.sql, append([]any{e.tenant}, q.args...)...).Scan(&id); err != nil {
				return err //nolint:wrapcheck // test
			}
			ids[q.name] = id
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`UPDATE catalog.products SET category_id = $2 WHERE id = $1`, []any{product, ids["vaydai"]}},
			{`INSERT INTO catalog.product_tags (tenant_id, product_id, tag_id) VALUES ($3, $1, $2)`, []any{product, ids["sale"], e.tenant}},
			{`INSERT INTO catalog.product_tags (tenant_id, product_id, tag_id) VALUES ($3, $1, $2)`, []any{product, ids["moi"], e.tenant}},
			{`INSERT INTO catalog.product_attribute_values (tenant_id, product_id, attribute_value_id) VALUES ($3, $1, $2)`, []any{product, ids["lua"], e.tenant}},
			{`INSERT INTO catalog.product_attribute_values (tenant_id, product_id, attribute_value_id) VALUES ($3, $1, $2)`, []any{product, ids["xoe"], e.tenant}},
		} {
			if _, err := tx.Exec(e.t.Context(), q.sql, q.args...); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	return ids
}

func TestTaxonomyAndClassification(t *testing.T) {
	e := newEnv(t)
	all := e.all()
	ids := e.seedClassification(all[0])
	var tx0 webcatalog.WebTaxonomy
	var p webcatalog.WebProduct
	e.read(func(tx pgx.Tx) error {
		var err error
		if tx0, err = webcatalog.Taxonomy(t.Context(), tx); err != nil {
			return err //nolint:wrapcheck // test
		}
		ps, err := webcatalog.ProductsByID(t.Context(), tx, all[:1], view(), false)
		p = ps[0]
		return err //nolint:wrapcheck // test
	})
	var cats []string
	for _, c := range tx0.Categories {
		cats = append(cats, c.Slug)
	}
	if !slices.Equal(cats, []string{"ao", "vay", "vay-dai"}) || tx0.Categories[2].ParentID == nil || *tx0.Categories[2].ParentID != ids["vay"] {
		t.Errorf("danh mục: %v %+v", cats, tx0.Categories)
	}
	if len(tx0.Attributes) != 2 || tx0.Attributes[0].Code != "dang" || len(tx0.Attributes[1].Values) != 2 ||
		tx0.Attributes[1].Values[0].Value != "Thun" {
		t.Errorf("thuộc tính: %+v", tx0.Attributes)
	}
	if len(tx0.Tags) != 2 || tx0.Tags[0].Code != "sale" || tx0.Tags[1].Name != "Hàng mới" {
		t.Errorf("nhãn (theo tên): %+v", tx0.Tags)
	}
	if !slices.Equal(p.Tags, []string{"hang-moi", "sale"}) || !slices.Equal(p.CategoryIDs, []uuid.UUID{ids["vaydai"]}) {
		t.Errorf("nhãn, danh mục của mẫu: %v %v", p.Tags, p.CategoryIDs)
	}
	if !slices.Equal(p.Attributes, []webcatalog.WebAttr{{Name: "Dáng", Value: "Xòe", Position: 1}, {Name: "Chất liệu", Value: "Lụa", Position: 2}}) {
		t.Errorf("thuộc tính của mẫu: %+v", p.Attributes)
	}
}

func ptr[T any](v T) *T { return &v }

// Kho mặc định của website trùng kho mặc định của inventory.
func TestDefaultWarehouseCode(t *testing.T) {
	if webcatalog.DefaultWarehouseCode != inventory.DefaultWarehouseCode {
		t.Fatalf("%q ≠ %q", webcatalog.DefaultWarehouseCode, inventory.DefaultWarehouseCode)
	}
}

// storeSQL trả câu SQL sqlc đã sinh cho truy vấn name (đúng câu ứng dụng chạy) từ mã sinh của store.
func storeSQL(t *testing.T, file, name string) string {
	t.Helper()
	src, err := os.ReadFile("internal/store/" + file)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)const " + name + " = `(.*?)`").FindSubmatch(src)
	if m == nil {
		t.Fatalf("không thấy câu %s", name)
	}
	return string(m[1])
}

// TestProducts_VariantOrder (Task 513): biến thể của mẫu trên API web theo vị trí nhân viên đặt, hòa vị trí thì theo mã;
// mẫu khác trong cùng lần đọc giữ thứ tự riêng của nó.
func TestProducts_VariantOrder(t *testing.T) {
	e := newEnv(t)
	all := e.all()
	vs := e.variantIDs(all[0])
	e.owner(func(tx pgx.Tx) error {
		// vs[2] đứng đầu (1), vs[0] và vs[1] cùng vị trí 2 (hòa → theo mã).
		_, err := tx.Exec(t.Context(), `UPDATE catalog.variants SET position = CASE id WHEN $1 THEN 1 ELSE 2 END
			WHERE product_id = $2`, vs[2], all[0])
		return err //nolint:wrapcheck // test
	})
	ps := e.byID(all[:2])
	ids := func(p webcatalog.WebProduct) []uuid.UUID {
		out := make([]uuid.UUID, len(p.Variants))
		for i := range p.Variants {
			out[i] = p.Variants[i].ID
		}
		return out
	}
	if got, want := ids(ps[0]), []uuid.UUID{vs[2], vs[0], vs[1]}; !slices.Equal(got, want) {
		t.Errorf("thứ tự biến thể trên web: %v, mong %v", got, want)
	}
	if got := ids(ps[1]); len(got) != 3 || !slices.IsSortedFunc(got, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) }) {
		t.Errorf("mẫu thứ hai (vị trí theo thứ tự chèn, mã uuidv7 tăng dần) phải theo mã: %v", got)
	}
}
