package promotion_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

var vn = time.FixedZone("ICT", 7*3600) //nolint:gochecknoglobals // múi giờ hằng của test

// bdNow: 24/11/2026 10:00 giờ Việt Nam.
func bdNow() time.Time { return time.Date(2026, 11, 24, 10, 0, 0, 0, vn) }

// giftSettings: bật, 50.000, đơn từ 300.000, phát trước 7 ngày, hạn 14 ngày, đăng ký trước 30 ngày.
func giftSettings() promotion.BirthdaySettings {
	return promotion.BirthdaySettings{
		Enabled: true, Amount: 50000, MinOrder: 300000, DaysBefore: 7, ValidDays: 14, MinLeadDays: 30,
	}
}

// bdCustomer đăng ký sinh nhật (day, month) cho SĐT phone, lùi lúc đăng ký về savedAgo trước bdNow, đặt delivered_count
// và legacy_delivered của khách.
type bdCustomer struct {
	phone        string
	day, month   int
	savedAgo     time.Duration
	delivered    int
	legacy, zalo bool
	mail         bool
	stopped      bool
	customerID   uuid.UUID
	expectGift   bool
}

func (e *env) seedBirthday(c *bdCustomer) {
	e.t.Helper()
	in := customer.BirthdayInput{
		Phone: c.phone, FullName: "Khách " + c.phone[len(c.phone)-3:], Day: c.day, Month: c.month,
		Zalo: c.zalo, Mail: c.mail,
	}
	if c.mail {
		in.Email = "k" + c.phone[len(c.phone)-3:] + "@example.com"
	}
	if err := e.app(func(tx pgx.Tx) error {
		p, _, err := e.customerWriter().RegisterBirthday(e.t.Context(), tx, in,
			customer.Actor{Type: customer.ActorService}, bdNow().Add(-c.savedAgo))
		c.customerID = p.CustomerID
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE customer.customers SET delivered_count = $2 WHERE id = $1`,
			c.customerID, c.delivered)
		if err == nil {
			_, err = tx.Exec(e.t.Context(), `UPDATE customer.birthday_profiles SET legacy_delivered = $2,
				stopped_at = CASE WHEN $3 THEN now() END WHERE customer_id = $1`, c.customerID, c.legacy, c.stopped)
		}
		return err //nolint:wrapcheck // test
	})
}

func (e *env) saveGiftSettings(s promotion.BirthdaySettings, version int) (promotion.BirthdaySettings, error) {
	var out promotion.BirthdaySettings
	err := e.app(func(tx pgx.Tx) error {
		var err error
		_, out, err = promotion.Writer{}.SaveBirthdaySettings(e.t.Context(), tx, s, version, e.actor(), bdNow())
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func (e *env) runGifts(trigger string, now time.Time) (promotion.BirthdayRun, error) {
	var r promotion.BirthdayRun
	err := e.app(func(tx pgx.Tx) error {
		var err error
		r, err = promotion.Writer{}.RunBirthdayGifts(e.t.Context(), tx, trigger, e.actor(), now)
		return err //nolint:wrapcheck // test
	})
	return r, err
}

func (e *env) customers(q promotion.BirthdayCustomerQuery) promotion.BirthdayCustomerPage {
	e.t.Helper()
	var p promotion.BirthdayCustomerPage
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		p, err = promotion.ListBirthdayCustomers(e.t.Context(), tx, q, bdNow())
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func stateOf(p promotion.BirthdayCustomerPage, id uuid.UUID) string {
	for _, c := range p.Items {
		if c.CustomerID == id {
			return c.State
		}
	}
	return ""
}

// Luật ngày như website: quà đến hạn từ (sinh nhật − days_before) tới sinh nhật; đăng ký sau (sinh nhật − min_lead) thì
// đợi năm sau; 29/2 năm không nhuận phát theo 28/2; cuối năm xét sinh nhật năm sau; hạn = ngày phát + valid_days − 1,
// không trước hôm nay.
func TestBirthdayDue_Rules(t *testing.T) {
	t.Parallel()
	s := giftSettings()
	d := func(y, m, dd int) time.Time { return time.Date(y, time.Month(m), dd, 0, 0, 0, 0, vn) }
	cases := []struct {
		name               string
		day, month         int
		saved, today       time.Time
		s                  func(promotion.BirthdaySettings) promotion.BirthdaySettings
		ok                 bool
		year               int
		validFrom, validTo time.Time
	}{
		{"ngày phát", 1, 12, d(2026, 1, 1), d(2026, 11, 24), nil, true, 2026, d(2026, 11, 24), d(2026, 12, 7)},
		{"trước ngày phát", 2, 12, d(2026, 1, 1), d(2026, 11, 24), nil, false, 0, time.Time{}, time.Time{}},
		{"đúng sinh nhật", 24, 11, d(2026, 1, 1), d(2026, 11, 24), nil, true, 2026, d(2026, 11, 24), d(2026, 11, 30)},
		{"qua sinh nhật", 23, 11, d(2026, 1, 1), d(2026, 11, 24), nil, false, 0, time.Time{}, time.Time{}},
		{"đăng ký muộn", 1, 12, d(2026, 11, 14), d(2026, 11, 24), nil, false, 0, time.Time{}, time.Time{}},
		{"đủ 30 ngày", 1, 12, d(2026, 11, 1), d(2026, 11, 24), nil, true, 2026, d(2026, 11, 24), d(2026, 12, 7)},
		{"29/2 năm thường", 29, 2, d(2026, 1, 1), d(2027, 2, 21), nil, true, 2027, d(2027, 2, 21), d(2027, 3, 6)},
		{"29/2 năm nhuận", 29, 2, d(2026, 1, 1), d(2028, 2, 21), nil, false, 0, time.Time{}, time.Time{}},
		{"qua năm", 2, 1, d(2026, 1, 1), d(2026, 12, 28), nil, true, 2027, d(2026, 12, 28), d(2027, 1, 8)},
		{
			"hạn ngắn hơn hôm nay", 30, 11, d(2026, 1, 1), d(2026, 11, 30),
			func(s promotion.BirthdaySettings) promotion.BirthdaySettings { s.ValidDays = 1; return s },
			true, 2026, d(2026, 11, 30), d(2026, 11, 30),
		},
	}
	for _, c := range cases {
		st := s
		if c.s != nil {
			st = c.s(st)
		}
		g, ok := promotion.DueGift(c.day, c.month, c.saved, c.today.Add(15*time.Hour), st)
		if ok != c.ok {
			t.Errorf("%s: đến hạn %v, muốn %v", c.name, ok, c.ok)
			continue
		}
		if ok && (g.Year != c.year || !g.ValidFrom.Equal(c.validFrom) || !g.ValidTo.Equal(c.validTo.AddDate(0, 0, 1))) {
			t.Errorf("%s: %+v, muốn năm %d từ %v tới hết %v", c.name, g, c.year, c.validFrom, c.validTo)
		}
	}
}

// [WB05] Lượt phát: chỉ khách đủ điều kiện (đăng ký đủ sớm, có đơn giao hay cờ đơn cũ, chưa dừng, sinh nhật trong cửa
// sổ) nhận đúng một mã gắn SĐT, mỗi mã một lượt, giảm 50.000 cho đơn từ 300.000, kênh web; chạy lại cùng ngày không phát
// mã thứ hai; tắt → không phát. Lượt ghi vào birthday_runs.
func TestRunBirthdayGifts_OnlyEligibleOnce(t *testing.T) {
	t.Parallel()
	e := newGiftEnv(t)
	day := 24 * time.Hour
	cs := []*bdCustomer{
		{phone: "0912000001", day: 28, month: 11, savedAgo: 60 * day, delivered: 1, zalo: true, expectGift: true},
		{phone: "0912000002", day: 28, month: 11, savedAgo: 10 * day, delivered: 1}, // min_lead 30
		{phone: "0912000003", day: 28, month: 11, savedAgo: 60 * day},               // chưa đơn giao
		{phone: "0912000004", day: 1, month: 12, savedAgo: 60 * day, legacy: true, expectGift: true},
		{phone: "0912000005", day: 28, month: 11, savedAgo: 60 * day, delivered: 2, stopped: true},
		{phone: "0912000006", day: 20, month: 3, savedAgo: 60 * day, delivered: 1},
	}
	for _, c := range cs {
		e.seedBirthday(c)
	}
	r, err := e.runGifts(promotion.BirthdayTriggerManual, bdNow())
	if err != nil || r.Given != 2 || r.Failed != 0 || r.Trigger != "manual" || r.StartedBy == nil || r.FinishedAt == nil {
		t.Fatalf("lượt phát: %+v %v", r, err)
	}
	for _, c := range cs {
		n := e.count(`SELECT count(*) FROM promotion.birthday_gifts g JOIN promotion.coupons k ON k.id = g.coupon_id
			JOIN promotion.coupon_batches b ON b.id = k.batch_id
			WHERE g.customer_id = $1 AND g.gift_year = 2026 AND g.run_id = $2 AND k.bound_phone = $3 AND k.max_uses = 1
			AND k.discount_kind = 'fixed' AND k.value = 50000 AND k.min_subtotal = 300000 AND k.channels = '{web}'
			AND k.valid_from = '2026-11-24 00:00+07' AND k.given_at IS NULL AND b.purpose = 'birthday'`,
			c.customerID, r.ID, "+84"+c.phone[1:])
		if want := map[bool]int{true: 1, false: 0}[c.expectGift]; n != want {
			t.Errorf("%s: %d quà, muốn %d", c.phone, n, want)
		}
	}
	// 28/11: phát từ 21/11 → hạn hết 4/12; 1/12: phát từ 24/11 → hạn hết 7/12 (hai lô khác hạn).
	if n := e.count(`SELECT count(*) FROM promotion.coupons WHERE valid_to = '2026-12-05 00:00+07'
		AND bound_phone = '+84912000001'`); n != 1 {
		t.Errorf("hạn tới hết 4/12 (21/11 + 14 − 1): %d", n)
	}
	if n := e.count(`SELECT count(*) FROM promotion.coupons WHERE valid_to = '2026-12-08 00:00+07'
		AND bound_phone = '+84912000004'`); n != 1 {
		t.Errorf("hạn tới hết 7/12: %d", n)
	}
	if n := e.count(`SELECT count(DISTINCT batch_id) FROM promotion.coupons`); n != 2 {
		t.Errorf("số lô: %d", n)
	}
	again, err := e.runGifts(promotion.BirthdayTriggerManual, bdNow().Add(time.Hour))
	if err != nil || again.Given != 0 {
		t.Fatalf("chạy lại: %+v %v", again, err)
	}
	if n := e.count(`SELECT count(*) FROM promotion.coupons`); n != 2 {
		t.Errorf("chạy lại phát thêm mã: %d", n)
	}
	last := e.lastRun()
	if last == nil || last.ID != again.ID {
		t.Errorf("lượt cuối: %+v", last)
	}
	off := giftSettings()
	off.Enabled = false
	if _, err := e.saveGiftSettings(off, 2); err != nil {
		t.Fatal(err)
	}
	late := &bdCustomer{phone: "0912000007", day: 30, month: 11, savedAgo: 60 * day, delivered: 1}
	e.seedBirthday(late)
	if r, err := e.runGifts(promotion.BirthdayTriggerManual, bdNow()); err != nil || r.Given != 0 {
		t.Errorf("tắt vẫn phát: %+v %v", r, err)
	}
}

func (e *env) lastRun() *promotion.BirthdayRun {
	e.t.Helper()
	var r *promotion.BirthdayRun
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		r, err = promotion.LastBirthdayRun(e.t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	return r
}

// Hai lượt cùng lúc: lượt đang giữ khóa chạy, lượt kia ErrRunInProgress ngay (không chờ).
func TestRunBirthdayGifts_OneAtATime(t *testing.T) {
	t.Parallel()
	e := newGiftEnv(t)
	var second error
	if err := e.whileRunning(func() error {
		_, second = e.runGifts(promotion.BirthdayTriggerSchedule, bdNow())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(second, promotion.ErrRunInProgress) {
		t.Fatalf("lượt thứ hai: %v", second)
	}
	if _, err := e.runGifts(promotion.BirthdayTriggerSchedule, bdNow()); err != nil {
		t.Fatalf("sau khi lượt đầu xong: %v", err)
	}
}

// Lịch mỗi giờ chỉ phát trong 9:00–21:00 giờ Việt Nam: 22:00 và 8:59 không ghi lượt; 9:00, 20:59 có. Kết quả việc chỉ có
// số (không SĐT); lượt đang chạy → việc xong, không lỗi.
func TestBirthdaySchedule_Window(t *testing.T) {
	t.Parallel()
	e := newGiftEnv(t)
	e.seedBirthday(&bdCustomer{phone: "0912000001", day: 28, month: 11, savedAgo: 60 * 24 * time.Hour, delivered: 1})
	day := time.Date(2026, 11, 24, 0, 0, 0, 0, vn)
	for _, c := range []struct {
		at  time.Duration
		run bool
	}{{22 * time.Hour, false}, {8*time.Hour + 59*time.Minute, false}, {9 * time.Hour, true}, {20*time.Hour + 59*time.Minute, true}} {
		before := e.count(`SELECT count(*) FROM promotion.birthday_runs`)
		now := day.Add(c.at)
		h := promotion.BirthdayRunHandler(func() time.Time { return now })
		if h.Kind().Name != "task.promotion.birthday_run" {
			t.Fatalf("Kind: %+v", h.Kind())
		}
		var out json.RawMessage
		if err := e.app(func(tx pgx.Tx) error {
			var err error
			out, err = h.Handle(t.Context(), queue.Job{TenantID: &e.tenant, Payload: json.RawMessage(`{}`)}, tx)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatalf("%v: %v", c.at, err)
		}
		if got := e.count(`SELECT count(*) FROM promotion.birthday_runs WHERE trigger = 'schedule'`) - before; (got == 1) != c.run {
			t.Errorf("%v: %d lượt, muốn chạy %v", c.at, got, c.run)
		}
		if strings.Contains(string(out), "912000001") {
			t.Errorf("kết quả việc chứa SĐT: %s", out)
		}
	}
	if n := e.count(`SELECT count(*) FROM promotion.birthday_gifts`); n != 1 {
		t.Errorf("số quà sau bốn kỳ: %d", n)
	}
	if err := e.whileRunning(func() error {
		now := day.Add(10 * time.Hour)
		return tenancy.WithTenant(t.Context(), e.staff.App, e.tenant, func(tx2 pgx.Tx) error {
			_, err := promotion.BirthdayRunHandler(func() time.Time { return now }).Handle(t.Context(),
				queue.Job{TenantID: &e.tenant, Payload: json.RawMessage(`{}`)}, tx2)
			return err //nolint:wrapcheck // test
		})
	}); err != nil {
		t.Fatalf("lịch gặp lượt đang chạy: %v", err)
	}
}

// Cài đặt: chưa lưu → mặc định tắt, version 1; mệnh giá 10.500, 5.000, đơn tối thiểu âm, hạn 0 → ErrInvalidGiftSettings;
// version cũ → ErrVersionConflict; lưu đúng → version 2, người sửa.
func TestBirthdaySettings_SaveValidate(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var got promotion.BirthdaySettings
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		got, err = promotion.GetBirthdaySettings(t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Amount != 50000 || got.DaysBefore != 7 || got.ValidDays != 14 || got.MinLeadDays != 30 ||
		got.MinOrder != 0 || got.Version != 1 || got.UpdatedBy != nil {
		t.Fatalf("mặc định: %+v", got)
	}
	for name, edit := range map[string]func(*promotion.BirthdaySettings){
		"10.500":       func(s *promotion.BirthdaySettings) { s.Amount = 10500 },
		"5.000":        func(s *promotion.BirthdaySettings) { s.Amount = 5000 },
		"1.001.000":    func(s *promotion.BirthdaySettings) { s.Amount = 1001000 },
		"đơn âm":       func(s *promotion.BirthdaySettings) { s.MinOrder = -1 },
		"hạn 0":        func(s *promotion.BirthdaySettings) { s.ValidDays = 0 },
		"trước 31":     func(s *promotion.BirthdaySettings) { s.DaysBefore = 31 },
		"đăng ký 121":  func(s *promotion.BirthdaySettings) { s.MinLeadDays = 121 },
		"đơn quá trần": func(s *promotion.BirthdaySettings) { s.MinOrder = 100000001 },
	} {
		s := giftSettings()
		edit(&s)
		if _, err := e.saveGiftSettings(s, 1); !errors.Is(err, promotion.ErrInvalidGiftSettings) {
			t.Errorf("%s: %v", name, err)
		}
	}
	saved, err := e.saveGiftSettings(giftSettings(), 1)
	if err != nil || saved.Version != 2 || !saved.Enabled || saved.MinOrder != 300000 || saved.UpdatedBy == nil ||
		*saved.UpdatedBy != e.user {
		t.Fatalf("lưu: %+v %v", saved, err)
	}
	if _, err := e.saveGiftSettings(giftSettings(), 1); !errors.Is(err, promotion.ErrVersionConflict) {
		t.Fatalf("version cũ: %v", err)
	}
}

// Outbox của website: chỉ quà chưa trao của khách đồng ý Zalo hay email, chưa dừng; website báo đã trao (MarkGiven)
// thì rời outbox. Cần nhắn: mọi quà chưa trao (kể cả khách không đồng ý kênh nào); "Đã nhắn" ghi lúc trao, rời danh
// sách, mã lạ bị bỏ qua. Dừng nhận tin sau khi có quà → rời cả hai.
func TestBirthdayOutboxTodoMessaged(t *testing.T) {
	t.Parallel()
	e := newGiftEnv(t)
	d60 := 60 * 24 * time.Hour
	zalo := &bdCustomer{phone: "0912000001", day: 28, month: 11, savedAgo: d60, delivered: 1, zalo: true}
	mail := &bdCustomer{phone: "0912000002", day: 28, month: 11, savedAgo: d60, delivered: 1, mail: true}
	none := &bdCustomer{phone: "0912000003", day: 28, month: 11, savedAgo: d60, delivered: 1}
	stop := &bdCustomer{phone: "0912000004", day: 28, month: 11, savedAgo: d60, delivered: 1, zalo: true}
	for _, c := range []*bdCustomer{zalo, mail, none, stop} {
		e.seedBirthday(c)
	}
	if r, err := e.runGifts(promotion.BirthdayTriggerManual, bdNow()); err != nil || r.Given != 4 {
		t.Fatalf("lượt phát: %+v %v", r, err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE customer.birthday_profiles SET stopped_at = now() WHERE customer_id = $1`,
			stop.customerID)
		return err //nolint:wrapcheck // test
	})
	pending := e.pendingGifts
	out := pending(true)
	if len(out) != 2 || !out[zalo.customerID].Zalo || out[zalo.customerID].Mail || !out[mail.customerID].Mail ||
		string(out[mail.customerID].Email) != "k002@example.com" || out[zalo.customerID].StopToken == "" ||
		out[zalo.customerID].Amount != 50000 || out[zalo.customerID].MinOrder != 300000 ||
		string(out[zalo.customerID].Phone) != "+84912000001" {
		t.Fatalf("outbox: %+v", out)
	}
	todo := pending(false)
	if len(todo) != 3 || todo[none.customerID].Code == "" {
		t.Fatalf("cần nhắn: %+v", todo)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := promotion.MarkGiven(t.Context(), tx, out[zalo.customerID].Code, bdNow())
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	var marked []string
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		marked, err = promotion.MarkBirthdayMessaged(t.Context(), tx,
			[]string{todo[none.customerID].Code, "KHONGCO-123", out[zalo.customerID].Code}, bdNow())
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(marked) != 1 || marked[0] != todo[none.customerID].Code {
		t.Fatalf("đã nhắn: %v", marked)
	}
	if out := pending(true); len(out) != 1 || out[mail.customerID].Code == "" {
		t.Errorf("outbox sau khi trao: %+v", out)
	}
	if todo := pending(false); len(todo) != 1 {
		t.Errorf("cần nhắn sau khi nhắn: %+v", todo)
	}
}

// Tất cả khách: tám trạng thái như admin (manual, due, unconfirmed, given, waiting, used, expired, stopped), đếm theo
// trạng thái và tháng, lọc tháng, trạng thái, tìm tên bỏ dấu hay ≥ 3 chữ số SĐT, trang theo con trỏ.
func TestListBirthdayCustomers_States(t *testing.T) {
	t.Parallel()
	e := newGiftEnv(t)
	d60 := 60 * 24 * time.Hour
	given := &bdCustomer{phone: "0912000001", day: 28, month: 11, savedAgo: d60, delivered: 1}
	manual := &bdCustomer{phone: "0912000002", day: 28, month: 11, savedAgo: d60, delivered: 1}
	used := &bdCustomer{phone: "0912000003", day: 27, month: 11, savedAgo: d60, delivered: 1}
	for _, c := range []*bdCustomer{given, manual, used} {
		e.seedBirthday(c)
	}
	if r, err := e.runGifts(promotion.BirthdayTriggerManual, bdNow()); err != nil || r.Given != 3 {
		t.Fatalf("lượt phát: %+v %v", r, err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE promotion.coupons k SET given_at = now() FROM promotion.birthday_gifts g
			WHERE g.coupon_id = k.id AND g.customer_id = $1`, given.customerID)
		if err == nil {
			_, err = tx.Exec(t.Context(), `UPDATE promotion.coupons k SET used_count = 1 FROM promotion.birthday_gifts g
				WHERE g.coupon_id = k.id AND g.customer_id = $1`, used.customerID)
		}
		return err //nolint:wrapcheck // test
	})
	due := &bdCustomer{phone: "0912000004", day: 29, month: 11, savedAgo: d60, delivered: 1}
	unconfirmed := &bdCustomer{phone: "0912000005", day: 29, month: 11, savedAgo: d60}
	waiting := &bdCustomer{phone: "0912000006", day: 20, month: 3, savedAgo: d60}
	stopped := &bdCustomer{phone: "0912000007", day: 29, month: 11, savedAgo: d60, delivered: 1, stopped: true}
	expired := &bdCustomer{phone: "0912000008", day: 1, month: 11, savedAgo: 400 * 24 * time.Hour, delivered: 1}
	for _, c := range []*bdCustomer{due, unconfirmed, waiting, stopped, expired} {
		e.seedBirthday(c)
	}
	if _, err := e.runGifts(promotion.BirthdayTriggerManual, time.Date(2026, 10, 26, 10, 0, 0, 0, vn)); err != nil {
		t.Fatal(err)
	}
	// Quà 1/11 của "expired" có hạn tới 8/11 (phát 25/10 + 14 − 1); lượt 26/10 cũng phát cho "due" nếu tới hạn — không.
	all := e.customers(promotion.BirthdayCustomerQuery{Limit: 50})
	want := map[uuid.UUID]string{
		given.customerID: "given", manual.customerID: "manual", used.customerID: "used", due.customerID: "due",
		unconfirmed.customerID: "unconfirmed", waiting.customerID: "waiting", stopped.customerID: "stopped",
		expired.customerID: "expired",
	}
	for id, st := range want {
		if got := stateOf(all, id); got != st {
			t.Errorf("khách %v: %q, muốn %q", id, got, st)
		}
	}
	if len(all.Items) != 8 || all.Items[0].State != "manual" || all.Items[1].State != "due" ||
		all.Items[7].State != "stopped" || all.StateCounts["due"] != 1 || all.MonthCounts[10] != 7 ||
		all.MonthCounts[2] != 1 || all.Next != nil {
		t.Fatalf("thứ tự, đếm: %+v", all)
	}
	e.checkBirthdayFilters(waiting.customerID)
	first := e.customers(promotion.BirthdayCustomerQuery{Limit: 5})
	if len(first.Items) != 5 || first.Next == nil {
		t.Fatalf("trang 1: %d %v", len(first.Items), first.Next)
	}
	second := e.customers(promotion.BirthdayCustomerQuery{Limit: 5, Cursor: *first.Next})
	if len(second.Items) != 3 || second.Next != nil || second.Items[0].CustomerID == first.Items[4].CustomerID {
		t.Fatalf("trang 2: %+v", second)
	}
	if _, err := func() (promotion.BirthdayCustomerPage, error) {
		var p promotion.BirthdayCustomerPage
		err := e.app(func(tx pgx.Tx) error {
			var err error
			p, err = promotion.ListBirthdayCustomers(t.Context(), tx, promotion.BirthdayCustomerQuery{State: "lạ"}, bdNow())
			return err //nolint:wrapcheck // test
		})
		return p, err
	}(); !errors.Is(err, promotion.ErrInvalidInput) {
		t.Errorf("trạng thái lạ: %v", err)
	}
}

// pendingGifts đọc outbox (onlyConsent) hay Cần nhắn lúc bdNow, theo khách.
func (e *env) pendingGifts(onlyConsent bool) map[uuid.UUID]promotion.BirthdayGift {
	e.t.Helper()
	var gs []promotion.BirthdayGift
	if err := e.app(func(tx pgx.Tx) error {
		var err error
		if onlyConsent {
			gs, err = promotion.BirthdayOutbox(e.t.Context(), tx, bdNow(), 100)
		} else {
			gs, err = promotion.ListBirthdayTodo(e.t.Context(), tx, bdNow(), 100)
		}
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatal(err)
	}
	out := map[uuid.UUID]promotion.BirthdayGift{}
	for _, g := range gs {
		out[g.CustomerID] = g
	}
	return out
}

// checkBirthdayFilters: lọc tháng 3 chỉ có khách waiting; lọc given có một khách; tìm theo đuôi SĐT, theo tên bỏ dấu.
func (e *env) checkBirthdayFilters(waiting uuid.UUID) {
	t := e.t
	t.Helper()
	if p := e.customers(promotion.BirthdayCustomerQuery{Month: 3, Limit: 50}); len(p.Items) != 1 || p.Items[0].CustomerID != waiting {
		t.Errorf("lọc tháng 3: %+v", p.Items)
	}
	if p := e.customers(promotion.BirthdayCustomerQuery{State: "given", Limit: 50}); len(p.Items) != 1 {
		t.Errorf("lọc given: %+v", p.Items)
	}
	if p := e.customers(promotion.BirthdayCustomerQuery{Q: "000006", Limit: 50}); len(p.Items) != 1 || p.Items[0].CustomerID != waiting {
		t.Errorf("tìm SĐT: %+v", p.Items)
	}
	if p := e.customers(promotion.BirthdayCustomerQuery{Q: "khach 008", Limit: 50}); len(p.Items) != 1 {
		t.Errorf("tìm tên bỏ dấu: %+v", p.Items)
	}
}

// newGiftEnv là env có cài đặt quà giftSettings đã lưu.
func newGiftEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	if _, err := e.saveGiftSettings(giftSettings(), 1); err != nil {
		t.Fatal(err)
	}
	return e
}

// whileRunning chạy fn trong lúc một lượt phát (nhân viên) đang giữ khóa ở transaction chưa commit.
func (e *env) whileRunning(fn func() error) error {
	return e.app(func(tx pgx.Tx) error {
		if _, err := (promotion.Writer{}).RunBirthdayGifts(e.t.Context(), tx, promotion.BirthdayTriggerManual, e.actor(),
			bdNow()); err != nil {
			return err //nolint:wrapcheck // test
		}
		return fn()
	})
}
