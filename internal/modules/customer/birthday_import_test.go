package customer_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

// importBirthdays nhập hồ sơ sinh nhật của website trong tenant A.
func (e *writeEnv) importBirthdays(items []customer.BirthdayImport) (customer.BirthdayImportReport, error) {
	e.t.Helper()
	var r customer.BirthdayImportReport
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		var err error
		r, err = e.w.ImportBirthdays(e.t.Context(), tx, items, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	return r, err
}

// legacyToken là mã dừng kiểu website (18 byte base64url = 24 ký tự).
func legacyToken(n int) string { return fmt.Sprintf("WebStopToken-%011d", n) }

func importItem(phone string, n int) customer.BirthdayImport {
	return customer.BirthdayImport{
		Phone: phone, FullName: "Khách " + phone[len(phone)-3:], Day: 12, Month: 3, Zalo: true,
		StopToken: legacyToken(n), SavedAt: time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC),
	}
}

// Nhập hồ sơ sinh nhật cũ của website (giữ mã dừng): SĐT mới → khách mới (tên rỗng → "Khách website"), hồ sơ nguồn
// import giữ mã dừng, lúc đăng ký, lúc dừng, cờ đơn cũ; link dừng cũ dừng được hồ sơ CRM. SĐT đã có hồ sơ (lần đầu
// thắng) → conflicts (vị trí mục), mã cũ không thay; website ghi đã dừng → hồ sơ CRM cũng dừng. Mã dừng của khách khác →
// conflicts. Nhập lại → skipped, không thêm dòng, không dừng lại. Audit chỉ tên trường.
func TestImportBirthdays_KeepsLegacyStopToken(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	had, _, err := e.registerBirthday(customer.BirthdayInput{Phone: "0912000103", FullName: "Đã có", Day: 1, Month: 1})
	if err != nil {
		t.Fatal(err)
	}
	stoppedAt := time.Date(2026, 9, 2, 1, 0, 0, 0, time.UTC)
	fresh := importItem("0912000101", 1)
	fresh.FullName, fresh.LegacyDelivered, fresh.Mail, fresh.Email = "", true, true, "a@example.com"
	stopped := importItem("+84 912 000 102", 2)
	stopped.StoppedAt = &stoppedAt
	clash := importItem("0912000103", 3)
	clash.StoppedAt = &stoppedAt
	taken := importItem("0912000104", 4)
	taken.StopToken = e.stopToken(had.CustomerID)
	items := []customer.BirthdayImport{fresh, stopped, clash, taken}

	r, err := e.importBirthdays(items)
	if err != nil {
		t.Fatal(err)
	}
	if r.Inserted != 2 || r.Skipped != 0 || r.Stopped != 2 || !slices.Equal(r.Conflicts, []int{2, 3}) {
		t.Fatalf("báo cáo: %+v", r)
	}
	if n := e.count(`SELECT count(*) FROM customer.birthday_profiles_v p WHERE p.stop_token = $1 AND p.source = 'import'
		AND p.full_name = 'Khách website' AND p.phone = '+84912000101' AND p.legacy_delivered AND p.consent_mail
		AND p.email = 'a@example.com' AND p.created_at = $2 AND p.stopped_at IS NULL AND p.day = 12 AND p.month = 3`,
		fresh.StopToken, fresh.SavedAt); n != 1 {
		t.Fatal("hồ sơ mới: mã dừng, nguồn, tên, lúc đăng ký, cờ đơn cũ")
	}
	if n := e.count(`SELECT count(*) FROM customer.birthday_profiles WHERE stop_token = $1 AND stopped_at = $2`,
		stopped.StopToken, stoppedAt); n != 1 {
		t.Fatal("hồ sơ đã dừng trên website")
	}
	if n := e.count(`SELECT count(*) FROM customer.birthday_profiles WHERE customer_id = $1 AND stop_token <> $2
		AND stopped_at = $3 AND day = 1`, had.CustomerID, clash.StopToken, stoppedAt); n != 1 {
		t.Fatal("hồ sơ có sẵn: giữ ngày, mã của CRM, nhận lúc dừng của website")
	}
	if n := e.count(`SELECT count(*) FROM customer.birthday_profiles`); n != 3 {
		t.Fatalf("số hồ sơ: %d", n)
	}

	var s customer.BirthdayStop
	e.inTx(func(tx pgx.Tx) error {
		s, err = e.w.StopBirthday(t.Context(), tx, fresh.StopToken, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	if !s.First {
		t.Fatalf("link dừng cũ: %+v", s)
	}

	again, err := e.importBirthdays(items)
	if err != nil {
		t.Fatal(err)
	}
	if again.Inserted != 0 || again.Skipped != 2 || again.Stopped != 0 || !slices.Equal(again.Conflicts, []int{2, 3}) {
		t.Fatalf("nhập lại: %+v", again)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 4 {
		t.Fatalf("số khách: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.birthday.import'`); n != 2 {
		t.Errorf("audit nhập: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE row_to_json(audit_logs)::text LIKE '%912000%'
		OR row_to_json(audit_logs)::text LIKE '%WebStopToken%' OR row_to_json(audit_logs)::text LIKE '%a@example%'`); n != 0 {
		t.Errorf("audit chứa SĐT, mã dừng hay email: %d", n)
	}
}

// Đầu vào sai → ErrInvalidInput (SĐT sai → ErrInvalidPhone), không ghi gì: 0 mục, 501 mục, SĐT trùng (viết khác), mã
// dừng trùng, mã dừng sai mẫu hay quá ngắn, thiếu lúc đăng ký, ngày không có, nhận email mà không email.
func TestImportBirthdays_Invalid(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	many := make([]customer.BirthdayImport, 501)
	for i := range many {
		many[i] = importItem(fmt.Sprintf("09130%05d", i), i)
	}
	with := func(f func(*customer.BirthdayImport)) []customer.BirthdayImport {
		it := importItem("0912000201", 1)
		f(&it)
		return []customer.BirthdayImport{importItem("0912000200", 0), it}
	}
	cases := map[string][]customer.BirthdayImport{
		"0 mục":         nil,
		"501 mục":       many,
		"SĐT trùng":     with(func(it *customer.BirthdayImport) { it.Phone = "+84912000200" }),
		"mã dừng trùng": with(func(it *customer.BirthdayImport) { it.StopToken = legacyToken(0) }),
		"mã dừng ngắn":  with(func(it *customer.BirthdayImport) { it.StopToken = "ngan" }),
		"mã dừng lạ":    with(func(it *customer.BirthdayImport) { it.StopToken = strings.Repeat("ă", 20) }),
		"thiếu lúc đk":  with(func(it *customer.BirthdayImport) { it.SavedAt = time.Time{} }),
		"30/2":          with(func(it *customer.BirthdayImport) { it.Day, it.Month = 30, 2 }),
		"mail":          with(func(it *customer.BirthdayImport) { it.Mail = true }),
	}
	for name, items := range cases {
		if _, err := e.importBirthdays(items); !errors.Is(err, customer.ErrInvalidInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.importBirthdays(with(func(it *customer.BirthdayImport) { it.Phone = "0012345678" })); !errors.Is(err,
		customer.ErrInvalidPhone) {
		t.Errorf("SĐT sai: %v", err)
	}
	if n := e.count(`SELECT count(*) FROM customer.birthday_profiles`); n != 0 {
		t.Fatalf("hồ sơ: %d", n)
	}
	if _, err := e.importBirthdays(many[:500]); err != nil {
		t.Fatalf("500 mục: %v", err)
	}
}
