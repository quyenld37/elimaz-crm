package customer_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// searchFixture: ba khách của kế hoạch trong tenant A (A, B, C) và một khách của tenant B có cùng đuôi SĐT với A.
type searchFixture struct {
	e       *writeEnv
	a, b, c customer.Customer
}

func newSearchFixture(t *testing.T) *searchFixture {
	t.Helper()
	e := newWriteEnv(t)
	f := &searchFixture{e: e}
	f.a = e.create(customer.CreateInput{FullName: "Nguyễn Thị Hồng Nhung", Phone: "0912345678"})
	f.b = e.create(customer.CreateInput{FullName: "Trần Văn Nam", Phone: "0987655678"})
	f.c = e.create(customer.CreateInput{FullName: "Lê Hồng", Phone: "0903000111"})
	// Tenant B: tên và đuôi số trùng với A — không bao giờ hiện khi tìm trong A.
	if err := e.try(e.other, func(tx pgx.Tx) error {
		_, err := e.w.CreateCustomer(t.Context(), tx, customer.CreateInput{FullName: "Hồng Nhung Khác", Phone: "0911115678"},
			e.actor, e.now)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *searchFixture) search(tenant uuid.UUID, q string) ([]customer.SearchHit, error) {
	var hits []customer.SearchHit
	err := f.e.try(tenant, func(tx pgx.Tx) error {
		var err error
		hits, err = customer.Search(f.e.t.Context(), tx, q, 0)
		return err //nolint:wrapcheck // test
	})
	return hits, err
}

func rootsOf(hits []customer.SearchHit) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.RootID)
	}
	return out
}

// Phân loại chuỗi tìm: mã, SĐT đủ, đuôi số, tên không dấu (gõ thiếu, hoa thường, có dấu); ký tự đại diện đã thoát.
func TestSearchClassifies(t *testing.T) {
	f := newSearchFixture(t)
	a, b, c := f.a.ID, f.b.ID, f.c.ID
	cases := []struct {
		q       string
		want    []uuid.UUID
		matched string
	}{
		{"5678", []uuid.UUID{a, b}, customer.MatchedByPhoneTail},
		{"345678", []uuid.UUID{a}, customer.MatchedByPhoneTail},
		{"55678", []uuid.UUID{b}, customer.MatchedByPhoneTail},
		{"0111", []uuid.UUID{c}, customer.MatchedByPhoneTail},
		{"5-6.7 8", []uuid.UUID{a, b}, customer.MatchedByPhoneTail},
		{"0912 345 678", []uuid.UUID{a}, customer.MatchedByPhone},
		{"+84912345678", []uuid.UUID{a}, customer.MatchedByPhone},
		{"0912.345.678", []uuid.UUID{a}, customer.MatchedByPhone},
		{"nhung", []uuid.UUID{a}, customer.MatchedByName},
		{"NHUNG", []uuid.UUID{a}, customer.MatchedByName},
		{"Nhưng", []uuid.UUID{a}, customer.MatchedByName},
		{"hong nhun", []uuid.UUID{a}, customer.MatchedByName},
		{"nhung hong", []uuid.UUID{a}, customer.MatchedByName}, // các từ không theo thứ tự
		{"hong", []uuid.UUID{a, c}, customer.MatchedByName},
		{f.b.Code, []uuid.UUID{b}, customer.MatchedByCode},
		{strings.ToLower(f.b.Code), []uuid.UUID{b}, customer.MatchedByCode},
		{"KH999999", nil, customer.MatchedByCode},
		{"50%_", nil, customer.MatchedByName},
		{"%%", nil, customer.MatchedByName},
		{"__", nil, customer.MatchedByName},
		{"n_m", nil, customer.MatchedByName},
		{`a\`, nil, customer.MatchedByName},
		{"0999999999", nil, customer.MatchedByPhone},
	}
	for _, tc := range cases {
		t.Run(tc.q, func(t *testing.T) {
			hits, err := f.search(f.e.tenant, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			got := rootsOf(hits)
			if !sameSet(got, tc.want) {
				t.Fatalf("q %q: nhận %v, muốn %v", tc.q, got, tc.want)
			}
			for _, h := range hits {
				if h.MatchedBy != tc.matched {
					t.Errorf("q %q: matchedBy %q, muốn %q", tc.q, h.MatchedBy, tc.matched)
				}
			}
		})
	}
}

func sameSet(got, want []uuid.UUID) bool {
	if len(got) != len(want) {
		return false
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			return false
		}
	}
	return true
}

// Dòng kết quả mang mã, tên, SĐT và chỉ số của gốc.
func TestSearchHitFields(t *testing.T) {
	f := newSearchFixture(t)
	hits, err := f.search(f.e.tenant, "345678")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits: %+v", hits)
	}
	h := hits[0]
	if h.RootID != f.a.ID || h.Code != f.a.Code || string(h.FullName) != "Nguyễn Thị Hồng Nhung" ||
		!slices.Equal(h.Phones, []logging.Phone{"+84912345678"}) || h.Stats.OrderCount != 0 {
		t.Fatalf("dòng: %+v", h)
	}
}

// Chuỗi quá ngắn: dưới 4 chữ số, dưới 2 ký tự sau chuẩn hóa, rỗng → ErrQueryTooShort; lỗi không mang chuỗi tìm.
func TestSearchTooShort(t *testing.T) {
	f := newSearchFixture(t)
	for _, q := range []string{"12", "123", "+1", "a", "  ", "", "Đ ", "1.2", "%"} {
		_, err := f.search(f.e.tenant, q)
		if !errors.Is(err, customer.ErrQueryTooShort) {
			t.Errorf("q %q: lỗi %v, muốn ErrQueryTooShort", q, err)
		}
	}
	if _, err := f.search(f.e.tenant, "ab"); err != nil {
		t.Errorf("2 ký tự phải tìm được: %v", err)
	}
}

// Gộp: B đổi gốc về A → "5678" trả một dòng gốc A; tìm theo mã của B cũng trả gốc A; SĐT của cả nhóm hiện ở dòng gốc.
func TestSearchGroupsByRoot(t *testing.T) {
	f := newSearchFixture(t)
	f.e.inTx(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE customer.customers SET root_id = $1 WHERE id = $2", f.a.ID, f.b.ID)
		return err //nolint:wrapcheck // test
	})
	hits, err := f.search(f.e.tenant, "5678")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].RootID != f.a.ID || hits[0].Code != f.a.Code {
		t.Fatalf("gộp: %+v", hits)
	}
	phones := slices.Clone(hits[0].Phones)
	slices.Sort(phones)
	if !slices.Equal(phones, []logging.Phone{"+84912345678", "+84987655678"}) {
		t.Fatalf("SĐT của nhóm: %v", phones)
	}
	byCode, err := f.search(f.e.tenant, f.b.Code)
	if err != nil || len(byCode) != 1 || byCode[0].RootID != f.a.ID {
		t.Fatalf("mã của hồ sơ con: %+v %v", byCode, err)
	}
	byName, err := f.search(f.e.tenant, "tran van")
	if err != nil || len(byName) != 1 || byName[0].RootID != f.a.ID {
		t.Fatalf("tên của hồ sơ con: %+v %v", byName, err)
	}
}

// Tenant: trong WithTenant(B) không ra khách của A; hàm tên gọi ngoài ngữ cảnh tenant → 0 dòng, không lỗi.
func TestSearchTenantIsolation(t *testing.T) {
	f := newSearchFixture(t)
	for _, q := range []string{"345678", "0912345678", "nhung", "hong", f.a.Code} {
		hits, err := f.search(f.e.other, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hits {
			if h.RootID == f.a.ID || h.RootID == f.b.ID || h.RootID == f.c.ID {
				t.Fatalf("q %q trong tenant B ra khách của A: %+v", q, h)
			}
		}
	}
	// Tenant B có đúng khách của mình theo đuôi 5678.
	if hits, err := f.search(f.e.other, "5678"); err != nil || len(hits) != 1 || string(hits[0].FullName) != "Hồng Nhung Khác" {
		t.Fatalf("tenant B tìm đuôi: %+v %v", hits, err)
	}
	// Ngoài ngữ cảnh (pool crm_app, không set_config): 0 dòng, không lỗi.
	rows, err := f.e.staff.App.Pool.Query(t.Context(), "SELECT id FROM customer.search_by_name($1, 20)", []string{"nhung"})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil || len(ids) != 0 {
		t.Fatalf("ngoài ngữ cảnh: %v %v", ids, err)
	}
	// Trong ngữ cảnh A, hàm trả đúng A.
	f.e.inTx(func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), "SELECT root_id FROM customer.search_by_name($1, 20)", []string{"nhung"})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil || !slices.Equal(got, []uuid.UUID{f.a.ID}) {
			t.Errorf("hàm trong tenant A: %v %v", got, err)
		}
		return nil
	})
}

// Hàm tên: chủ crm_search, SECURITY DEFINER, search_path cố định, PUBLIC không gọi được.
func TestSearchByNameFunctionDefinition(t *testing.T) {
	e := newWriteEnv(t)
	var owner, cfg string
	var secdef, publicExec, appExec bool
	err := e.staff.Owner.Pool.QueryRow(t.Context(), `
		SELECT pg_get_userbyid(p.proowner), p.prosecdef, array_to_string(p.proconfig, ','),
		       has_function_privilege('public', p.oid, 'EXECUTE') OR
		           EXISTS (SELECT 1 FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) a WHERE a.grantee = 0),
		       has_function_privilege('crm_app', p.oid, 'EXECUTE')
		FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = 'customer' AND p.proname = 'search_by_name'`).Scan(&owner, &secdef, &cfg, &publicExec, &appExec)
	if err != nil {
		t.Fatal(err)
	}
	if owner != "crm_search" || !secdef || cfg != "search_path=pg_catalog, pg_temp" || publicExec || !appExec {
		t.Fatalf("hàm: chủ %s, secdef %v, cấu hình %q, PUBLIC %v, crm_app %v", owner, secdef, cfg, publicExec, appExec)
	}
}

// Kết quả theo lần mua gần nhất của gốc (chưa mua xếp sau), rồi theo id; limit cắt số nhóm.
func TestSearchOrderAndLimit(t *testing.T) {
	f := newSearchFixture(t)
	recent := f.e.now.Add(-time.Hour)
	f.e.inTx(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE customer.customers SET last_order_at = $1 WHERE id = $2", recent, f.c.ID)
		return err //nolint:wrapcheck // test
	})
	hits, err := f.search(f.e.tenant, "hong")
	if err != nil || len(hits) != 2 || hits[0].RootID != f.c.ID || hits[1].RootID != f.a.ID {
		t.Fatalf("thứ tự: %+v %v", hits, err)
	}
	if hits[0].Stats.LastOrderAt == nil || !hits[0].Stats.LastOrderAt.Equal(recent) {
		t.Fatalf("chỉ số: %+v", hits[0].Stats)
	}
	var one []customer.SearchHit
	f.e.inTx(func(tx pgx.Tx) error {
		var err error
		one, err = customer.Search(t.Context(), tx, "hong", 1)
		return err //nolint:wrapcheck // test
	})
	if len(one) != 1 || one[0].RootID != f.c.ID {
		t.Fatalf("limit 1: %+v", one)
	}
}

// list gọi customer.List trong tenant A.
func (f *searchFixture) list(fl customer.ListFilter) ([]customer.SearchHit, *customer.ListCursor) {
	f.e.t.Helper()
	var hits []customer.SearchHit
	var next *customer.ListCursor
	f.e.inTx(func(tx pgx.Tx) error {
		var err error
		hits, next, err = customer.List(f.e.t.Context(), tx, fl)
		return err //nolint:wrapcheck // test
	})
	return hits, next
}

// Danh sách khi không tìm: khách gốc mới nhất trước, trang theo con trỏ (created_at, id); con trỏ hỏng →
// ErrInvalidInput; tenant B chỉ thấy khách của mình.
func TestListCustomers(t *testing.T) {
	f := newSearchFixture(t)
	all, next := f.list(customer.ListFilter{Limit: 10})
	if !slices.Equal(rootsOf(all), []uuid.UUID{f.c.ID, f.b.ID, f.a.ID}) || next != nil {
		t.Fatalf("trang đầu: %v %v", rootsOf(all), next)
	}
	p1, next := f.list(customer.ListFilter{Limit: 2})
	if !slices.Equal(rootsOf(p1), []uuid.UUID{f.c.ID, f.b.ID}) || next == nil {
		t.Fatalf("trang 1: %v %v", rootsOf(p1), next)
	}
	parsed, err := customer.ParseListCursor(next.String())
	if err != nil || parsed.ID != next.ID || !parsed.CreatedAt.Equal(next.CreatedAt) {
		t.Fatalf("con trỏ: %v %v", parsed, err)
	}
	p2, next := f.list(customer.ListFilter{Limit: 2, After: &parsed})
	if !slices.Equal(rootsOf(p2), []uuid.UUID{f.a.ID}) || next != nil {
		t.Fatalf("trang 2: %v %v", rootsOf(p2), next)
	}
	if p2[0].MatchedBy != "" || !slices.Equal(p2[0].Phones, []logging.Phone{"+84912345678"}) {
		t.Fatalf("dòng danh sách: %+v", p2[0])
	}
	for _, bad := range []string{"%%%", "YWJj", ""} {
		if _, err := customer.ParseListCursor(bad); !errors.Is(err, customer.ErrInvalidInput) {
			t.Errorf("con trỏ %q: %v", bad, err)
		}
	}
	err = f.e.try(f.e.other, func(tx pgx.Tx) error {
		hits, _, err := customer.List(t.Context(), tx, customer.ListFilter{Limit: 10})
		if err == nil && (len(hits) != 1 || string(hits[0].FullName) != "Hồng Nhung Khác") {
			t.Errorf("tenant B: %+v", hits)
		}
		return err //nolint:wrapcheck // test
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Gộp và nhãn: hồ sơ con không hiện ở danh sách; lọc nhãn lấy nhóm có nhãn ở bất kỳ hồ sơ nào trong nhóm (cả ở List
// và SearchTagged); nhãn không có → rỗng.
func TestListCustomersMergedAndTagged(t *testing.T) {
	f := newSearchFixture(t)
	var tag uuid.UUID
	f.e.inTx(func(tx pgx.Tx) error {
		if _, err := tx.Exec(t.Context(), "UPDATE customer.customers SET root_id = $1 WHERE id = $2", f.a.ID, f.b.ID); err != nil {
			return err //nolint:wrapcheck // test
		}
		if err := tx.QueryRow(t.Context(), `INSERT INTO customer.customer_tags (tenant_id, name, color)
			VALUES (current_setting('app.tenant_id')::uuid, 'VIP', 'red') RETURNING id`).Scan(&tag); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `INSERT INTO customer.customer_tag_links (tenant_id, customer_id, tag_id, source)
			VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'staff')`, f.b.ID, tag)
		return err //nolint:wrapcheck // test
	})
	if all, _ := f.list(customer.ListFilter{Limit: 10}); !slices.Equal(rootsOf(all), []uuid.UUID{f.c.ID, f.a.ID}) {
		t.Fatalf("sau gộp: %v", rootsOf(all))
	}
	// Nhãn gắn trên hồ sơ con B → nhóm A có nhãn.
	if tagged, _ := f.list(customer.ListFilter{Limit: 10, TagID: &tag}); !slices.Equal(rootsOf(tagged), []uuid.UUID{f.a.ID}) {
		t.Fatalf("lọc nhãn: %v", rootsOf(tagged))
	}
	// "hong" khớp A, C → chỉ nhóm A còn lại.
	f.e.inTx(func(tx pgx.Tx) error {
		got, err := customer.SearchTagged(t.Context(), tx, "hong", tag, 0)
		if err == nil && (len(got) != 1 || got[0].RootID != f.a.ID || got[0].MatchedBy != customer.MatchedByName) {
			t.Errorf("tìm có nhãn: %+v", got)
		}
		return err //nolint:wrapcheck // test
	})
	other := uuid.New()
	if none, _ := f.list(customer.ListFilter{Limit: 10, TagID: &other}); len(none) != 0 {
		t.Fatalf("nhãn không có: %v", rootsOf(none))
	}
}
