package customer_test

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

// Dữ liệu giả: SĐT 0912 5xx xxx, tên "Khách giả …"; không có dữ liệu khách thật.

func (e *writeEnv) legacy() customer.LegacyImporter {
	return customer.LegacyImporter{DB: e.staff.App, Writer: e.w}
}

func (e *writeEnv) runLegacy(rows []customer.LegacyCustomer) customer.LegacyResult {
	e.t.Helper()
	res, err := e.legacy().Run(e.t.Context(), e.tenant, e.actor, rows, false)
	if err != nil {
		e.t.Fatalf("Run: %v", err)
	}
	return res
}

func legacyRow(n int, phones ...string) customer.LegacyCustomer {
	return customer.LegacyCustomer{Row: n, LegacyID: int64(1000 + n), Name: fmt.Sprintf("Khách giả %d", n), Phones: phones}
}

func bd(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

// Một khách đủ trường: hai SĐT (SĐT đầu là chính), ngày sinh, nhãn (nhóm + nhãn, trùng bỏ), ghi chú. Hồ sơ lead, hai
// danh tính phone nguồn import, hai nhãn nguồn staff, một ghi chú, một dòng đối chiếu mã cũ, một sự kiện
// customer.created; một audit customer.legacy_import chỉ có số đếm (không tên, SĐT); không audit từng khách.
func TestLegacyImport_CreatesFullCustomerAndMap(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	in := legacyRow(1, "0912 500 001", "+84912500002", "0912500001")
	in.Birthday, in.Tags, in.Note = bd(1990, 2, 1), []string{"VIP", "Nhóm cũ", "VIP"}, "Khách quen của cửa hàng"
	res := e.runLegacy([]customer.LegacyCustomer{in})
	if res.Total != 1 || res.OK != 1 || res.Skipped+res.Review+res.Failed != 0 || len(res.Errors) != 0 {
		t.Fatalf("kết quả: %+v", res)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers c
		JOIN customer.legacy_customers l ON l.customer_id = c.id AND l.legacy_id = 1001
		WHERE c.lifecycle = 'lead' AND c.full_name = 'Khách giả 1' AND c.birthday = DATE '1990-02-01'`); n != 1 {
		t.Errorf("khách và mã cũ: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_identities WHERE kind = 'phone' AND source = 'import'
		AND value IN ('+84912500001', '+84912500002')`); n != 2 {
		t.Errorf("hai danh tính phone: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_tag_links l JOIN customer.customer_tags g ON g.id = l.tag_id
		WHERE l.source = 'staff' AND g.name IN ('VIP', 'Nhóm cũ')`); n != 2 {
		t.Errorf("nhãn: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_notes WHERE body = 'Khách quen của cửa hàng'`); n != 1 {
		t.Errorf("ghi chú: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'customer.created'`); n != 1 {
		t.Errorf("sự kiện customer.created: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.create'`); n != 0 {
		t.Errorf("không audit từng khách: %d", n)
	}
	var raw string
	e.inTx(func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT row_to_json(a)::text FROM identity.audit_logs a
			WHERE action = 'customer.legacy_import'`).Scan(&raw)
	})
	for _, bad := range []string{"Khách giả", "0912", "912500", "VIP", "quen"} {
		if strings.Contains(raw, bad) {
			t.Errorf("audit chứa %q: %s", bad, raw)
		}
	}
	if !strings.Contains(raw, `"ok": 1`) && !strings.Contains(raw, `"ok":1`) {
		t.Errorf("audit thiếu số đếm: %s", raw)
	}
}

// Chạy lại cùng bản chụp: mọi khách đã có mã cũ → skipped; không thêm khách, danh tính, nhãn, ghi chú, sự kiện. Sau khi
// nhân viên đổi SĐT của khách đã chuyển, chạy lại vẫn không tạo khách thứ hai (mã cũ là khóa).
func TestLegacyImport_RerunNoDuplicates(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	rows := []customer.LegacyCustomer{legacyRow(1, "0912500011"), legacyRow(2, "0912500012", "0912500013")}
	rows[1].Tags, rows[1].Note = []string{"VIP"}, "ghi chú"
	if res := e.runLegacy(rows); res.OK != 2 {
		t.Fatalf("lần đầu: %+v", res)
	}
	before := e.count(`SELECT (SELECT count(*) FROM customer.customers) + (SELECT count(*) FROM customer.customer_identities)
		+ (SELECT count(*) FROM customer.customer_tag_links) + (SELECT count(*) FROM customer.customer_notes)
		+ (SELECT count(*) FROM platform.domain_events)`)
	res := e.runLegacy(rows)
	if res.Total != 2 || res.OK != 0 || res.Skipped != 2 || res.Review != 0 || res.Failed != 0 {
		t.Fatalf("chạy lại: %+v", res)
	}
	after := e.count(`SELECT (SELECT count(*) FROM customer.customers) + (SELECT count(*) FROM customer.customer_identities)
		+ (SELECT count(*) FROM customer.customer_tag_links) + (SELECT count(*) FROM customer.customer_notes)
		+ (SELECT count(*) FROM platform.domain_events)`)
	if after != before {
		t.Errorf("chạy lại làm đổi dữ liệu: %d → %d", before, after)
	}
	e.inTx(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `DELETE FROM customer.customer_identities WHERE value = '+84912500011'`)
		return err //nolint:wrapcheck // test
	})
	if res := e.runLegacy(rows[:1]); res.Skipped != 1 || res.OK != 0 {
		t.Errorf("khách đã chuyển đổi SĐT: %+v", res)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 2 {
		t.Errorf("số khách: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.legacy_import'`); n != 3 {
		t.Errorf("mỗi lần chạy một audit: %d", n)
	}
}

// SĐT đã là của khách CRM: skipped, không ghi đè tên, không thêm mã cũ, không vào danh sách cần xem.
func TestLegacyImport_ExistingPhoneIsSkippedNotOverwritten(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	had := e.create(customer.CreateInput{FullName: "Tên trong CRM", Phone: "0912500021"})
	in := legacyRow(1, "+84 912 500 021")
	in.Birthday = bd(1991, 3, 3)
	res := e.runLegacy([]customer.LegacyCustomer{in})
	if res.Total != 1 || res.Skipped != 1 || res.OK+res.Review+res.Failed != 0 {
		t.Fatalf("kết quả: %+v", res)
	}
	if p := e.get(had.ID); p.FullName != "Tên trong CRM" || p.Birthday != nil {
		t.Errorf("hồ sơ có sẵn bị đổi: %+v", p)
	}
	if n := e.count(`SELECT (SELECT count(*) FROM customer.legacy_customers) + (SELECT count(*) FROM customer.legacy_review)`); n != 0 {
		t.Errorf("không ghi mã cũ hay mục cần xem: %d", n)
	}
}

// Không tự gộp: SĐT thuộc hai khách CRM khác nhau → không tạo khách, mục phones_split kèm gốc của cả hai; SĐT chủ có
// sẵn mà còn SĐT khác chưa có → mục phone_not_added, SĐT thêm không được gắn; hai khách cũ cùng SĐT trong một lần →
// khách đầu tạo, khách sau vào duplicate_in_source kèm khách vừa tạo. Không dòng nào ở customer_merges, mọi khách vẫn
// là gốc của mình; chạy lại giữ nguyên các mục (không nhân đôi).
func TestLegacyImport_UncertainGoesToReviewNeverMerges(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	a := e.create(customer.CreateInput{FullName: "A", Phone: "0912500031"})
	b := e.create(customer.CreateInput{FullName: "B", Phone: "0912500032"})
	c := e.create(customer.CreateInput{FullName: "C", Phone: "0912500033"})
	rows := []customer.LegacyCustomer{
		legacyRow(1, "0912500031", "0912500032"),
		legacyRow(2, "0912500033", "0912500034"),
		legacyRow(3, "0912500035"),
		legacyRow(4, "0912500035", "0912500036"),
	}
	res := e.runLegacy(rows)
	if res.Total != 4 || res.OK != 1 || res.Review != 3 || res.Skipped != 0 || res.Failed != 0 {
		t.Fatalf("kết quả: %+v", res)
	}
	type rev struct {
		legacy  int64
		reason  string
		related []uuid.UUID
	}
	var got []rev
	e.inTx(func(tx pgx.Tx) error {
		rs, err := tx.Query(t.Context(), `SELECT legacy_id, reason, related_customer_ids FROM customer.legacy_review
			ORDER BY legacy_id, reason`)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		defer rs.Close()
		for rs.Next() {
			var r rev
			if err := rs.Scan(&r.legacy, &r.reason, &r.related); err != nil {
				return err //nolint:wrapcheck // test
			}
			slices.SortFunc(r.related, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
			got = append(got, r)
		}
		return rs.Err() //nolint:wrapcheck // test
	})
	ab := []uuid.UUID{a.ID, b.ID}
	slices.SortFunc(ab, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
	var first uuid.UUID
	e.inTx(func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT customer_id FROM customer.legacy_customers WHERE legacy_id = 1003`).Scan(&first)
	})
	want := []rev{
		{1001, "phones_split", ab},
		{1002, "phone_not_added", []uuid.UUID{c.ID}},
		{1004, "duplicate_in_source", []uuid.UUID{first}},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("mục cần xem:\n got  %v\n want %v", got, want)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_identities WHERE value IN ('+84912500034', '+84912500036')`); n != 0 {
		t.Errorf("SĐT chưa chắc chủ bị gắn: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_merges`); n != 0 {
		t.Errorf("không được tự gộp: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE id <> root_id`); n != 0 {
		t.Errorf("có hồ sơ không phải gốc: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 4 {
		t.Errorf("số khách (3 có sẵn + 1 mới): %d", n)
	}
	again := e.runLegacy(rows)
	if again.OK != 0 || again.Review != 3 || again.Skipped != 1 {
		t.Errorf("chạy lại: %+v", again)
	}
	if n := e.count(`SELECT count(*) FROM customer.legacy_review`); n != 3 {
		t.Errorf("mục cần xem nhân đôi: %d", n)
	}
}

// Lỗi theo {row, column, code}, không giá trị: không SĐT → required ở mobile; SĐT sai → invalid_phone; mã cũ ≤ 0 →
// required ở id; mọi SĐT sai dù có tên → lỗi, không tạo khách. Dòng tốt đứng giữa vẫn nhập.
func TestLegacyImport_RowErrorsCarryNoValues(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	noID := legacyRow(4, "0912500045")
	noID.LegacyID = 0
	rows := []customer.LegacyCustomer{
		legacyRow(1),
		legacyRow(2, "77777777", "abc"),
		legacyRow(3, "0912500043"),
		noID,
	}
	res := e.runLegacy(rows)
	if res.Total != 4 || res.OK != 1 || res.Failed != 3 {
		t.Fatalf("kết quả: %+v", res)
	}
	want := []customer.ImportError{
		{Row: 1, Column: "mobile", Code: "required"},
		{Row: 2, Column: "mobile", Code: "invalid_phone"},
		{Row: 4, Column: "id", Code: "required"},
	}
	if fmt.Sprint(res.Errors) != fmt.Sprint(want) {
		t.Errorf("lỗi:\n got  %+v\n want %+v", res.Errors, want)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 1 {
		t.Errorf("số khách: %d", n)
	}
}

// Trường phụ sai không làm mất khách: ngày sinh ở tương lai hay trước 1900 bỏ; nhãn quá dài hay có ký tự điều khiển bỏ;
// quá 20 nhãn chỉ giữ 20 đầu; ghi chú có ký tự điều khiển bỏ, quá 4000 ký tự cắt; tên rỗng → "Khách cũ".
func TestLegacyImport_BadOptionalFieldsAreDropped(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	many := make([]string, 0, 25)
	for i := range 25 {
		many = append(many, fmt.Sprintf("N%02d", i))
	}
	r1 := legacyRow(1, "0912500051")
	r1.Birthday = bd(2999, 1, 1)
	r1.Tags = []string{strings.Repeat("x", 51), "ok\x07", "Tốt"}
	r1.Note = "xấu\x00"
	r2 := legacyRow(2, "0912500052")
	r2.Name, r2.Birthday, r2.Tags, r2.Note = "  ", bd(1850, 1, 1), many, strings.Repeat("ê", 4500)
	res := e.runLegacy([]customer.LegacyCustomer{r1, r2})
	if res.OK != 2 || res.Failed != 0 {
		t.Fatalf("kết quả: %+v", res)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE birthday IS NOT NULL`); n != 0 {
		t.Errorf("ngày sinh sai phải bỏ: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE full_name = 'Khách cũ'`); n != 1 {
		t.Errorf("tên rỗng: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_tags WHERE name = 'Tốt'`); n != 1 {
		t.Errorf("nhãn tốt: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_tag_links`); n != 21 {
		t.Errorf("liên kết nhãn (1 + 20): %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_notes WHERE char_length(body) = 4000`); n != 1 {
		t.Errorf("ghi chú cắt 4000: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_notes`); n != 1 {
		t.Errorf("ghi chú xấu phải bỏ: %d", n)
	}
}

// Chạy khô: cùng đường xử lý (kể cả phát hiện trùng trong cùng lần), kết quả như thật nhưng không ghi gì.
func TestLegacyImport_DryRunWritesNothing(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	rows := []customer.LegacyCustomer{legacyRow(1, "0912500061"), legacyRow(2, "0912500061")}
	res, err := e.legacy().Run(t.Context(), e.tenant, e.actor, rows, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK != 1 || res.Review != 1 {
		t.Errorf("kết quả chạy khô: %+v", res)
	}
	if n := e.count(`SELECT (SELECT count(*) FROM customer.customers) + (SELECT count(*) FROM customer.legacy_customers)
		+ (SELECT count(*) FROM customer.legacy_review) + (SELECT count(*) FROM identity.audit_logs
		WHERE action = 'customer.legacy_import')`); n != 0 {
		t.Errorf("chạy khô đã ghi: %d", n)
	}
}

// Hơn một lô (500): 1.101 khách qua ba lô. Lượt chạy dở (500 khách đầu) rồi chạy cả bản chụp → 601 mới, 500 skipped, tổng
// 1.101, không trùng.
func TestLegacyImport_BatchesResumeAfterPartialRun(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	rows := make([]customer.LegacyCustomer, 0, 1101)
	for i := 1; i <= 1101; i++ {
		rows = append(rows, legacyRow(i, fmt.Sprintf("0913%06d", i)))
	}
	if res := e.runLegacy(rows[:customer.ImportBatchSize]); res.OK != customer.ImportBatchSize {
		t.Fatalf("lượt dở: %+v", res)
	}
	res := e.runLegacy(rows)
	if res.Total != 1101 || res.OK != 601 || res.Skipped != 500 || res.Failed != 0 {
		t.Fatalf("lượt đủ: %+v", res)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 1101 {
		t.Errorf("số khách: %d", n)
	}
	if n := e.count(`SELECT count(DISTINCT legacy_id) FROM customer.legacy_customers`); n != 1101 {
		t.Errorf("mã cũ: %d", n)
	}
}

// Hai lượt chạy cùng bản chụp một lúc: mỗi khách đúng một hồ sơ, không lỗi, mỗi dòng ra đúng một kết quả ở mỗi lượt.
func TestLegacyImport_ConcurrentRunsCreateEachOnce(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	rows := make([]customer.LegacyCustomer, 0, 60)
	for i := 1; i <= 60; i++ {
		rows = append(rows, legacyRow(i, fmt.Sprintf("0914%06d", i)))
	}
	var wg sync.WaitGroup
	results := make([]customer.LegacyResult, 2)
	errs := make([]error, 2)
	for k := range results {
		wg.Go(func() { results[k], errs[k] = e.legacy().Run(t.Context(), e.tenant, e.actor, rows, false) })
	}
	wg.Wait()
	for k, res := range results {
		if errs[k] != nil {
			t.Fatalf("lượt %d: %v", k, errs[k])
		}
		if res.Total != 60 || res.OK+res.Skipped != 60 || res.Review != 0 || res.Failed != 0 {
			t.Errorf("lượt %d: %+v", k, res)
		}
	}
	if results[0].OK+results[1].OK != 60 {
		t.Errorf("tổng khách tạo: %d + %d", results[0].OK, results[1].OK)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 60 {
		t.Errorf("số khách: %d", n)
	}
}

// Tenant khác có cùng mã cũ và cùng SĐT vẫn nhập riêng, không thấy khách của tenant này.
func TestLegacyImport_TenantsAreIsolated(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	rows := []customer.LegacyCustomer{legacyRow(1, "0912500071")}
	e.runLegacy(rows)
	res, err := e.legacy().Run(t.Context(), e.other, e.actor, rows, false)
	if err != nil || res.OK != 1 {
		t.Fatalf("tenant khác: %+v, %v", res, err)
	}
	if n := e.count(`SELECT count(*) FROM customer.legacy_customers`); n != 1 {
		t.Errorf("tenant A thấy %d mã cũ", n)
	}
}

// Writer chưa nối (thiếu Events/Queue) là lỗi, không nhập gì; danh sách rỗng không lỗi, không audit.
func TestLegacyImport_NotWiredAndEmpty(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	bad := customer.LegacyImporter{DB: e.staff.App}
	if _, err := bad.Run(t.Context(), e.tenant, e.actor, []customer.LegacyCustomer{legacyRow(1, "0912500081")}, false); err == nil {
		t.Error("Writer chưa nối mà không lỗi")
	}
	res := e.runLegacy(nil)
	if res.Total != 0 {
		t.Errorf("rỗng: %+v", res)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 0 {
		t.Errorf("số khách: %d", n)
	}
}
