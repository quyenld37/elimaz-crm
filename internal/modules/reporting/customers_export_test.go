package reporting_test

import (
	"bytes"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/reporting"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/xlsx/xlsxtest"
	"elimaz.com.vn/crm/internal/testkit"
)

// Xuất danh sách khách (PQ06, Task 238 = M6-15) trên sáu khách: C1 thường, SĐT +84912345678 xác minh; C2 đã gộp vào C1
// (root_id = C1), SĐT +84987654321 xác minh sau của C1; C3 tên là công thức; C4 lead chưa mua; C5 thuộc tenant B; C6
// tên dài tối đa (120 ký tự: CHECK của customer.customers, kế hoạch ghi 300). Khách mặc định "Khách thử" của kịch bản
// chín đơn là lead.

// customerHeader là tiêu đề của tệp danh sách khách (không có cột địa chỉ).
func customerHeader() []string {
	return []string{
		"Mã khách", "Tên", "SĐT", "Số đơn", "Thành công", "Hoàn", "Tỷ lệ hoàn", "Tổng chi", "Mua cuối", "Nhãn", "Ngày tạo",
	}
}

// sixCustomers là dữ liệu sáu khách cùng các chuỗi cá nhân (tên, SĐT ở mọi dạng) để quét trong audit, log, params.
type sixCustomers struct {
	c1, c2, c3, c4, c5, c6 uuid.UUID
	codes                  map[uuid.UUID]string
	tenantB                uuid.UUID
	secrets                []string
	evil, long             string
}

// custSpec là một hồ sơ khách dựng bằng chủ bảng (test được ghi bảng khách; mã chạy thì không).
type custSpec struct {
	id, root                    uuid.UUID
	name, lifecycle             string
	orders, delivered, returned int
	spent                       int64
	lastOrder                   *time.Time
	created                     time.Time
	phone                       string
	verified                    *time.Time
}

// addCustomer chèn hồ sơ c (root rỗng: tự là gốc) vào tenant, kèm danh tính phone nếu có; trả mã KH… đã sinh.
func addCustomer(t *testing.T, st *testkit.Staff, tenant uuid.UUID, c custSpec) string {
	t.Helper()
	if c.root == uuid.Nil {
		c.root = c.id
	}
	var code string
	st.Exec(t, tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), `INSERT INTO customer.customers (id, tenant_id, root_id, full_name, name_search,
				lifecycle, order_count, delivered_count, returned_count, total_spent, last_order_at, created_at)
			VALUES ($1, $2, $3, $4, lower($4), $5, $6, $7, $8, $9, $10, $11) RETURNING code`,
			c.id, tenant, c.root, c.name, c.lifecycle, c.orders, c.delivered, c.returned, c.spent, c.lastOrder, c.created).
			Scan(&code); err != nil {
			return err //nolint:wrapcheck // test
		}
		if c.phone == "" {
			return nil
		}
		_, err := tx.Exec(t.Context(), `INSERT INTO customer.customer_identities (tenant_id, customer_id, kind, value,
				verified_at, source) VALUES ($1, $2, 'phone', $3, $4, 'staff')`, tenant, c.id, c.phone, c.verified)
		return err //nolint:wrapcheck // test
	})
	return code
}

// addTag tạo nhãn name và gắn cho các hồ sơ ids; trả mã nhãn.
func addTag(t *testing.T, st *testkit.Staff, tenant uuid.UUID, name string, ids ...uuid.UUID) uuid.UUID {
	t.Helper()
	var tag uuid.UUID
	st.Exec(t, tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), `INSERT INTO customer.customer_tags (tenant_id, name, color) VALUES ($1, $2, 'blue')
			RETURNING id`, tenant, name).Scan(&tag); err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, id := range ids {
			if _, err := tx.Exec(t.Context(), `INSERT INTO customer.customer_tag_links (tenant_id, customer_id, tag_id, source)
				VALUES ($1, $2, $3, 'staff')`, tenant, id, tag); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	return tag
}

// custDay là ngày tạo của sáu khách (giờ VN).
func custDay() time.Time { return time.Date(2026, 11, 10, 9, 0, 0, 0, reporting.VN) }

func seedSixCustomers(t *testing.T, e *exportEnv) sixCustomers {
	t.Helper()
	s := sixCustomers{
		c1: uuid.New(), c2: uuid.New(), c3: uuid.New(), c4: uuid.New(), c5: uuid.New(), c6: uuid.New(),
		codes: map[uuid.UUID]string{}, evil: `=HYPERLINK("http://evil","bấm")`, long: strings.Repeat("Ê", 120),
	}
	last := custDay().Add(72 * time.Hour)
	v1, v2 := custDay().Add(time.Hour), custDay().Add(2*time.Hour)
	st := e.st
	s.codes[s.c1] = addCustomer(t, st, e.tenant, custSpec{
		id: s.c1, name: "Trần Thị Lan", lifecycle: "customer", orders: 3,
		delivered: 2, returned: 1, spent: 500_000, lastOrder: &last, created: custDay(), phone: "+84912345678", verified: &v1,
	})
	s.codes[s.c2] = addCustomer(t, st, e.tenant, custSpec{
		id: s.c2, root: s.c1, name: "Lan Trần", lifecycle: "customer",
		orders: 1, delivered: 1, created: custDay().Add(time.Minute), phone: "+84987654321", verified: &v2,
	})
	s.codes[s.c3] = addCustomer(t, st, e.tenant, custSpec{
		id: s.c3, name: s.evil, lifecycle: "customer", orders: 1,
		delivered: 1, spent: 200_000, lastOrder: &last, created: custDay().Add(2 * time.Minute),
	})
	s.codes[s.c4] = addCustomer(t, st, e.tenant, custSpec{
		id: s.c4, name: "Phạm Văn Lead", lifecycle: "lead",
		created: custDay().Add(3 * time.Minute), phone: "+84900000004",
	})
	s.codes[s.c6] = addCustomer(t, st, e.tenant, custSpec{
		id: s.c6, name: s.long, lifecycle: "customer", orders: 2,
		created: custDay().Add(4 * time.Minute), phone: "+84900000006",
	})
	s.tenantB = st.Tenant(t, "b")
	s.codes[s.c5] = addCustomer(t, st, s.tenantB, custSpec{
		id: s.c5, name: "Khách Tenant B", lifecycle: "customer",
		orders: 1, created: custDay(), phone: "+84900000005",
	})
	addTag(t, st, e.tenant, "VIP", s.c1, s.c2)
	addTag(t, st, e.tenant, "Sỉ", s.c2)
	s.secrets = []string{
		"Trần Thị Lan", "Lan Trần", "HYPERLINK", "Phạm Văn Lead", s.long, "Khách Tenant B",
		"+84912345678", "0912345678", "912345678", "+84987654321", "0987654321", "987654321",
		"900000004", "900000005", "900000006",
	}
	return s
}

// customersUser là người có customer.export toàn shop.
func (e *exportEnv) customersUser(t *testing.T, email string) identity.AuthUser {
	t.Helper()
	return e.user(t, email, []string{"customer.export"}, "all", "")
}

// customerExport tạo và chạy lần xuất khách với params; trả lần chạy và dòng tệp.
func (e *exportEnv) customerExport(t *testing.T, u identity.AuthUser, h *reporting.RunHandler, params string) (reporting.Run, exportRow) {
	t.Helper()
	run := e.mustCreate(t, u, xlsxInput(reporting.FamilyCustomers, reporting.KeyCustomers, params))
	e.exec(t, h, run.ID)
	return run, e.exportRow(t, run.ID)
}

// colRe lấy chữ cột của tham chiếu ô (A1 → A).
var colRe = regexp.MustCompile(`^[A-Z]+`)

// byTitle đổi các dòng dữ liệu (từ dòng 3: dòng 1 tiêu đề, dòng 2 phạm vi) sang map tiêu đề → ô, theo mã khách.
func byTitle(t *testing.T, rows []xlsxtest.Row) map[string]map[string]xlsxtest.Cell {
	t.Helper()
	if len(rows) < 2 {
		t.Fatalf("tệp có %d dòng", len(rows))
	}
	titles := map[string]string{}
	for _, c := range rows[0].Cells {
		titles[colRe.FindString(c.Ref)] = c.Value
	}
	out := map[string]map[string]xlsxtest.Cell{}
	for _, r := range rows[2:] {
		m := map[string]xlsxtest.Cell{}
		for _, c := range r.Cells {
			m[titles[colRe.FindString(c.Ref)]] = c
		}
		out[m["Mã khách"].Value] = m
	}
	return out
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Tệp lifecycle = customer: dòng C1, C3, C6 (C2 đã gộp, C4 lead, C5 tenant khác); số dòng bằng câu đếm độc lập bằng chủ
// bảng; SĐT của C1 = số xác minh mới nhất trong nhóm gộp, dạng 0…, ô chữ; tên C3 có nháy đứng đầu, không <f>; số liệu,
// tỷ lệ hoàn, nhãn gộp không trùng; không cột địa chỉ; khóa tiền tố customers/, hạn = lúc xong + 24 giờ; tên tệp.
func TestCustomersExport_File(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	s := seedSixCustomers(t, e)
	u := e.customersUser(t, "chu@elimaz.test")
	_, r := e.customerExport(t, u, e.xhandler(config.ReportConfig{}, nil, nil, nil), `{"lifecycle":"customer"}`)
	after := time.Now()
	if r.status != "ready" || r.rowCount == nil || *r.rowCount != 3 {
		t.Fatalf("lần chạy %+v", r)
	}
	independent := e.st.CountIn(t, e.tenant, `SELECT count(*) FROM customer.customers WHERE id = root_id AND lifecycle = 'customer'`)
	if *r.rowCount != independent {
		t.Errorf("số dòng %d, đếm độc lập %d", *r.rowCount, independent)
	}
	if !regexp.MustCompile(`^customers/`+e.tenant.String()+`/[0-9a-f-]{36}/[0-9a-f]{64}\.xlsx$`).MatchString(r.key) ||
		r.purpose != "customer_export" {
		t.Errorf("khóa %q, mục đích %q", r.key, r.purpose)
	}
	if d := r.expiresAt.Sub(after); d > 24*time.Hour || d < 24*time.Hour-time.Minute {
		t.Errorf("hạn %v sau lúc xong %v", r.expiresAt, after)
	}
	if r.fileExpires == nil || !r.fileExpires.Equal(r.expiresAt) {
		t.Errorf("hạn tệp %v, hạn lần chạy %v", r.fileExpires, r.expiresAt)
	}
	if r.fileName != "danh-sach-khach_"+reporting.Today(time.Now())+".xlsx" {
		t.Errorf("tên tệp %q", r.fileName)
	}
	b, _ := e.book(t, r.key)
	if parts := b.FormulaParts(); len(parts) != 0 {
		t.Errorf("có công thức: %v", parts)
	}
	rows := sheetRows(t, b)
	if !slices.Equal(values(rows[0]), customerHeader()) {
		t.Errorf("tiêu đề = %q", values(rows[0]))
	}
	got := byTitle(t, rows)
	if want := []string{s.codes[s.c1], s.codes[s.c3], s.codes[s.c6]}; !slices.Equal(keys(got), sorted(want)) {
		t.Fatalf("mã khách = %v, mong %v", keys(got), want)
	}
	checkSixCells(t, s, got)
}

// checkSixCells: SĐT của C1 là số xác minh mới nhất của nhóm gộp (dạng 0…, ô chữ), tên C3 có nháy đứng đầu, tên C6 đủ,
// số liệu, tỷ lệ hoàn, nhãn gộp, ô số và ngày.
func checkSixCells(t *testing.T, s sixCustomers, got map[string]map[string]xlsxtest.Cell) {
	t.Helper()
	c1, c3, c6 := got[s.codes[s.c1]], got[s.codes[s.c3]], got[s.codes[s.c6]]
	if p := c1["SĐT"]; p.Value != "0987654321" || p.Type == "" {
		t.Errorf("SĐT C1 = %+v (mong ô chữ 0987654321)", p)
	}
	if n := c3["Tên"].Value; n != "'"+s.evil {
		t.Errorf("tên C3 = %q", n)
	}
	if n := c6["Tên"].Value; n != s.long {
		t.Errorf("tên C6 dài %d", len([]rune(n)))
	}
	checkCells(t, "C1", c1, map[string]string{
		"Tên": "Trần Thị Lan", "Số đơn": "3", "Thành công": "2", "Hoàn": "1", "Tỷ lệ hoàn": "33,33%",
		"Tổng chi": "500000", "Nhãn": "Sỉ, VIP",
	})
	checkCells(t, "C3", c3, map[string]string{"Tỷ lệ hoàn": "0,00%", "SĐT": "", "Nhãn": ""})
	checkCells(t, "C6", c6, map[string]string{"Tỷ lệ hoàn": "—", "Mua cuối": "", "SĐT": "0900000006"})
	for _, title := range []string{"Số đơn", "Tổng chi", "Mua cuối", "Ngày tạo"} {
		if c := c1[title]; c.Type != "" && c.Type != "n" {
			t.Errorf("C1 %s kiểu %q, mong số", title, c.Type)
		}
	}
	// Ngày tạo của C1: 10/11/2026 09:00 giờ VN = 46336.375 (số ngày Excel).
	if v := c1["Ngày tạo"].Value; v != "46336.375" {
		t.Errorf("ngày tạo C1 = %q", v)
	}
}

func sorted(xs []string) []string { out := slices.Clone(xs); slices.Sort(out); return out }

// checkCells: các ô tiêu đề → giá trị của want (giá trị rỗng: ô không có hoặc rỗng).
func checkCells(t *testing.T, who string, row map[string]xlsxtest.Cell, want map[string]string) {
	t.Helper()
	for title, w := range want {
		if got := row[title].Value; got != w {
			t.Errorf("%s %s = %q, mong %q", who, title, got, w)
		}
	}
}

// Bộ lọc: ngày tạo, ngày mua cuối (giờ VN, cả hai đầu), vòng đời, nhãn (có ít nhất một nhãn trong danh sách, xét cả hồ
// sơ đã gộp), số đơn tối thiểu; tham số chữ tự do, trường lạ, giá trị sai → ErrBadParams (hay lỗi ngày); dạng json →
// ErrBadParams.
func TestCustomersExport_Filters(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	s := seedSixCustomers(t, e)
	sale := addTag(t, e.st, e.tenant, "Sale", s.c3)
	var vip uuid.UUID
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT id FROM customer.customer_tags WHERE name = 'VIP'`).Scan(&vip)
	})
	var si uuid.UUID
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT id FROM customer.customer_tags WHERE name = 'Sỉ'`).Scan(&si)
	})
	u := e.customersUser(t, "chu@elimaz.test")
	h := e.xhandler(config.ReportConfig{}, nil, nil, nil)
	day := custDay().Format("2006-01-02")
	lastDay := custDay().Add(72 * time.Hour).Format("2006-01-02")
	for _, c := range []struct {
		params string
		want   []uuid.UUID
	}{
		{`{"minOrders":2}`, []uuid.UUID{s.c1, s.c6}},
		{`{"tagIds":["` + si.String() + `"]}`, []uuid.UUID{s.c1}}, // nhãn của hồ sơ đã gộp C2
		{`{"tagIds":["` + vip.String() + `","` + sale.String() + `"]}`, []uuid.UUID{s.c1, s.c3}},
		{`{"lastOrderFrom":"` + lastDay + `","lastOrderTo":"` + lastDay + `"}`, []uuid.UUID{s.c1, s.c3}},
		{`{"createdFrom":"` + day + `","createdTo":"` + day + `","lifecycle":"lead"}`, []uuid.UUID{s.c4}},
		{`{"createdFrom":"2026-11-11","createdTo":"2026-11-12"}`, nil},
		{`{"createdTo":"2026-11-09"}`, nil},
	} {
		_, r := e.customerExport(t, u, h, c.params)
		if r.status != "ready" {
			t.Fatalf("%s: %+v", c.params, r)
		}
		got := keys(byTitle(t, sheetRows(t, mustBook(t, e, r.key))))
		var want []string
		for _, id := range c.want {
			want = append(want, s.codes[id])
		}
		slices.Sort(want)
		if !slices.Equal(got, want) && (len(got) != 0 || len(want) != 0) {
			t.Errorf("%s → %v, mong %v", c.params, got, want)
		}
	}
	bad := func(name, params string, want error) refusedRun {
		return refusedRun{name, u, xlsxInput(reporting.FamilyCustomers, reporting.KeyCustomers, params), want}
	}
	e.expectRefused(t, []refusedRun{
		bad("chữ tự do", `{"q":"Nguyễn"}`, reporting.ErrBadParams),
		bad("vòng đời lạ", `{"lifecycle":"vip"}`, reporting.ErrBadParams),
		bad("số đơn âm", `{"minOrders":-1}`, reporting.ErrBadParams),
		bad("nhãn không phải uuid", `{"tagIds":["Nguyễn"]}`, reporting.ErrBadParams),
		bad("ngày sai", `{"createdFrom":"10/11/2026"}`, reporting.ErrBadDate),
		bad("từ sau tới", `{"lastOrderFrom":"2026-11-12","lastOrderTo":"2026-11-11"}`, reporting.ErrFromAfterTo),
		bad("quá nhiều nhãn", `{"tagIds":[`+strings.TrimSuffix(strings.Repeat(`"`+vip.String()+`",`, 51), ",")+`]}`, reporting.ErrBadParams),
		{"dạng json", u, reporting.CreateRunInput{
			Family: reporting.FamilyCustomers, Report: reporting.KeyCustomers, Format: reporting.FormatJSON, Params: []byte(`{}`),
		}, reporting.ErrBadParams},
	})
}

// Quyền: customer.export phạm vi page → ErrForbidden (quyền toàn shop); không quyền → ErrForbidden.
func TestCustomersExport_Permission(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	page := e.user(t, "page@elimaz.test", []string{"customer.export"}, "page", uuid.NewString())
	none := e.user(t, "xem@elimaz.test", []string{"report.sales.read", "report.export"}, "all", "")
	in := xlsxInput(reporting.FamilyCustomers, reporting.KeyCustomers, `{}`)
	e.expectRefused(t, []refusedRun{
		{"phạm vi page", page, in, reporting.ErrForbidden},
		{"không customer.export", none, in, reporting.ErrForbidden},
	})
	e.mustCreate(t, e.customersUser(t, "chu@elimaz.test"), in)
}

// Ba dòng audit (export_requested, export_ready, export_downloaded), cột params của report_runs và log của cả lượt (tạo
// tới tải) không chứa tên hay SĐT nào của sáu khách; audit chỉ có bộ lọc, số dòng, mã lần chạy.
func TestCustomersExport_NoPersonalData(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	s := seedSixCustomers(t, e)
	u := e.customersUser(t, "chu@elimaz.test")
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&lockedWriter{w: &buf}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	run, r := e.customerExport(t, u, e.xhandler(config.ReportConfig{}, nil, nil, log), `{"lifecycle":"customer","minOrders":1}`)
	if r.status != "ready" {
		t.Fatalf("lần chạy %+v", r)
	}
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, _, err := reporting.FileLink(t.Context(), tx, u, reporting.FamilyCustomers, run.ID, e.mem, 5*time.Minute, time.Now())
		return err //nolint:wrapcheck // test
	})
	var audits []string
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), `SELECT action || ' ' || coalesce(after::text, '') || ' ' || coalesce(before::text, '')
			FROM identity.audit_logs WHERE entity_id = $1 ORDER BY id`, run.ID.String())
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		audits, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err //nolint:wrapcheck // test
	})
	actions := make([]string, 0, len(audits))
	for _, a := range audits {
		actions = append(actions, strings.Fields(a)[0])
	}
	if !slices.Equal(actions, []string{"customer.export_requested", "customer.export_ready", "customer.export_downloaded"}) {
		t.Errorf("audit = %v", actions)
	}
	var params string
	e.st.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT params::text FROM reporting.report_runs WHERE id = $1`, run.ID).Scan(&params)
	})
	text := strings.Join(audits, "\n") + "\n" + params + "\n" + buf.String()
	if !strings.Contains(text, `"minOrders": 1`) && !strings.Contains(text, `"minOrders":1`) {
		t.Errorf("audit thiếu bộ lọc: %s", strings.Join(audits, "\n"))
	}
	if !strings.Contains(buf.String(), `"run_id"`) {
		t.Errorf("log thiếu run_id: %s", buf.String())
	}
	for _, secret := range s.secrets {
		if strings.Contains(text, secret) {
			t.Errorf("audit/params/log chứa %q", secret)
		}
	}
}

// Migration 0605 (G10, R3): cột của customers_v sau = cột trước + return_rate_bp ở cuối, cùng tên, thứ tự, kiểu;
// do_not_contact, name_source của M4 vẫn còn; công thức = returned × 10000 / (delivered + returned), NULL khi mẫu số 0;
// tenant B đọc view → 0 dòng của A.
func TestMigration0605CustomersView(t *testing.T) {
	t.Parallel()
	_, ownerURL, _ := testkit.CreateDatabase(t)
	testkit.MigrateBefore(t, ownerURL, "0605")
	owner := testkit.OpenDB(t, ownerURL)
	before := testkit.Columns(t, owner, "customer", "customers_v")
	if _, err := db.Migrate(t.Context(), ownerURL); err != nil {
		t.Fatalf("migrate 0605: %v", err)
	}
	after := testkit.Columns(t, owner, "customer", "customers_v")
	if len(after) != len(before)+1 || !slices.Equal(after[:len(before)], before) || after[len(before)].Name != "return_rate_bp" {
		t.Fatalf("cột trước %v, sau %v", before, after)
	}
	names := make([]string, 0, len(after))
	for _, c := range after {
		names = append(names, c.Name)
	}
	if !slices.Contains(names, "do_not_contact") || !slices.Contains(names, "name_source") {
		t.Errorf("mất cột M4: %v", names)
	}
	var a, b uuid.UUID
	for name, dst := range map[string]*uuid.UUID{"A": &a, "B": &b} {
		if err := owner.Pool.QueryRow(t.Context(), "INSERT INTO platform.tenants (name) VALUES ($1) RETURNING id", name).
			Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	rates := map[[2]int]*int32{}
	for _, dr := range [][2]int{{2, 1}, {0, 0}, {0, 3}, {2, 0}, {1, 2}} {
		var got *int32
		testkit.InTenant(t, owner, a, func(tx pgx.Tx) error {
			id := uuid.New()
			if _, err := tx.Exec(t.Context(), `INSERT INTO customer.customers (id, tenant_id, root_id, full_name, lifecycle,
					delivered_count, returned_count) VALUES ($1, $2, $1, 'K', 'customer', $3, $4)`, id, a, dr[0], dr[1]); err != nil {
				return err //nolint:wrapcheck // test
			}
			return tx.QueryRow(t.Context(), `SELECT return_rate_bp FROM customer.customers_v WHERE id = $1`, id).Scan(&got)
		})
		rates[dr] = got
	}
	for dr, want := range map[[2]int]*int32{{2, 1}: new(int32(3333)), {0, 0}: nil, {0, 3}: new(int32(10000)), {2, 0}: new(int32(0)), {1, 2}: new(int32(6666))} {
		if got := rates[dr]; (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("giao %d hoàn %d → %v, mong %v", dr[0], dr[1], got, want)
		}
	}
	var n int
	testkit.InTenant(t, owner, b, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT count(*) FROM customer.customers_v`).Scan(&n)
	})
	if n != 0 {
		t.Errorf("tenant B thấy %d dòng của A", n)
	}
}
