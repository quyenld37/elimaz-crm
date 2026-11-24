package customer_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

// registerBirthday đăng ký sinh nhật trong tenant A.
func (e *writeEnv) registerBirthday(in customer.BirthdayInput) (customer.BirthdayProfile, bool, error) {
	e.t.Helper()
	var (
		p       customer.BirthdayProfile
		created bool
	)
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		var err error
		p, created, err = e.w.RegisterBirthday(e.t.Context(), tx, in, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	return p, created, err
}

// stopToken đọc mã dừng của hồ sơ sinh nhật của khách id.
func (e *writeEnv) stopToken(id uuid.UUID) string {
	e.t.Helper()
	var tok string
	e.inTx(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT stop_token FROM customer.birthday_profiles WHERE customer_id = $1`,
			id).Scan(&tok)
	})
	return tok
}

// SĐT chưa có chủ: tạo khách lead có danh tính SĐT (nguồn web), hồ sơ sinh nhật nguồn web; lần đăng ký sau (ngày khác,
// SĐT viết khác) không đổi gì (lần đầu thắng). Audit chỉ có tên trường, không SĐT, ngày, email.
func TestRegisterBirthday_NewCustomerFirstWins(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	p, created, err := e.registerBirthday(customer.BirthdayInput{
		Phone: "0912345678", FullName: "Trần Thị Mai", Day: 29, Month: 2, Zalo: true, Mail: true,
		Email: "mai.tran@example.com",
	})
	if err != nil || !created {
		t.Fatalf("đăng ký: %v %v", created, err)
	}
	if p.Phone != "+84912345678" || p.Day != 29 || p.Month != 2 || !p.Zalo || !p.Mail || p.StoppedAt != nil ||
		!p.CreatedAt.Equal(e.now) {
		t.Fatalf("hồ sơ: %+v", p)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers c JOIN customer.customer_identities i
		ON i.customer_id = c.id AND i.kind = 'phone' AND i.value = '+84912345678' AND i.source = 'web'
		WHERE c.id = $1 AND c.lifecycle = 'lead' AND c.full_name = 'Trần Thị Mai'`, p.CustomerID); n != 1 {
		t.Errorf("khách mới có danh tính SĐT nguồn web: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.birthday_profiles WHERE customer_id = $1 AND source = 'web'
		AND email = 'mai.tran@example.com' AND length(stop_token) >= 32 AND NOT legacy_delivered`, p.CustomerID); n != 1 {
		t.Errorf("hồ sơ sinh nhật: %d", n)
	}
	again, created, err := e.registerBirthday(customer.BirthdayInput{
		Phone: "+84 912 345 678", FullName: "Mai", Day: 3, Month: 4, Zalo: false, Mail: false,
	})
	if err != nil || created || again.CustomerID != p.CustomerID || again.Day != 29 || again.Month != 2 || !again.Zalo {
		t.Fatalf("đăng ký lại: %v %+v %v", created, again, err)
	}
	e.expectOneCustomer()
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.birthday.register'
		AND entity_id = $1 AND actor_type = 'staff'`, p.CustomerID.String()); n != 1 {
		t.Errorf("audit đăng ký (một lần): %d", n)
	}
	var rows string
	e.inTx(func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT string_agg(row_to_json(a)::text, ' ') FROM identity.audit_logs a`).Scan(&rows)
	})
	for _, bad := range []string{"912345678", "mai.tran", "Trần Thị Mai", e.stopToken(p.CustomerID)} {
		if strings.Contains(rows, bad) {
			t.Errorf("audit chứa %q", bad)
		}
	}
}

// SĐT đã là danh tính của một khách: hồ sơ sinh nhật gắn vào khách đó, không tạo khách mới.
func TestRegisterBirthday_ExistingCustomerByPhone(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	c := e.create(customer.CreateInput{FullName: "Chị Lan", Phone: "0987654321"})
	p, created, err := e.registerBirthday(customer.BirthdayInput{
		Phone: "0987654321", FullName: "Lan Nguyễn", Day: 15, Month: 8,
	})
	if err != nil || !created || p.CustomerID != c.ID {
		t.Fatalf("đăng ký: %v %+v %v", created, p, err)
	}
	e.expectOneCustomer()
	if n := e.count(`SELECT count(*) FROM customer.birthday_profiles_v WHERE customer_id = $1 AND root_id = $1
		AND full_name = 'Chị Lan' AND delivered_count = 0`, c.ID); n != 1 {
		t.Errorf("view hồ sơ: %d", n)
	}
}

// Đầu vào sai → lỗi, không ghi gì: ngày không có trong tháng (30/2, 31/4), nhận email mà không có email, email sai dạng,
// SĐT sai, tên rỗng.
func TestRegisterBirthday_Invalid(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	ok := customer.BirthdayInput{Phone: "0912345678", FullName: "Mai", Day: 1, Month: 1}
	cases := map[string]struct {
		edit func(*customer.BirthdayInput)
		want error
	}{
		"30/2":           {func(in *customer.BirthdayInput) { in.Day, in.Month = 30, 2 }, customer.ErrInvalidInput},
		"31/4":           {func(in *customer.BirthdayInput) { in.Day, in.Month = 31, 4 }, customer.ErrInvalidInput},
		"ngày 0":         {func(in *customer.BirthdayInput) { in.Day = 0 }, customer.ErrInvalidInput},
		"tháng 13":       {func(in *customer.BirthdayInput) { in.Month = 13 }, customer.ErrInvalidInput},
		"mail không có":  {func(in *customer.BirthdayInput) { in.Mail = true }, customer.ErrInvalidInput},
		"email sai dạng": {func(in *customer.BirthdayInput) { in.Email = "khong-phai-email" }, customer.ErrInvalidInput},
		"SĐT sai":        {func(in *customer.BirthdayInput) { in.Phone = "12345" }, customer.ErrInvalidPhone},
		"tên rỗng":       {func(in *customer.BirthdayInput) { in.FullName = "  " }, customer.ErrInvalidInput},
	}
	for name, c := range cases {
		in := ok
		c.edit(&in)
		if _, _, err := e.registerBirthday(in); !errors.Is(err, c.want) {
			t.Errorf("%s: lỗi %v, muốn %v", name, err, c.want)
		}
	}
	if n := e.count(`SELECT count(*) FROM customer.birthday_profiles`); n != 0 {
		t.Errorf("hồ sơ ghi dù sai: %d", n)
	}
	if !customer.ValidBirthday(29, 2) || customer.ValidBirthday(30, 2) || !customer.ValidBirthday(31, 12) {
		t.Error("ValidBirthday")
	}
}

// Dừng nhận tin theo mã dừng: lần đầu ghi lúc dừng và audit; gọi lại giữ lúc đầu tiên (first false); mã lạ hay của
// tenant khác → ErrNotFound.
func TestStopBirthday(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	p, _, err := e.registerBirthday(customer.BirthdayInput{Phone: "0912345678", FullName: "Mai", Day: 5, Month: 6})
	if err != nil {
		t.Fatal(err)
	}
	tok := e.stopToken(p.CustomerID)
	stop := func(tenant uuid.UUID, token string, at time.Time) (customer.BirthdayStop, error) {
		var s customer.BirthdayStop
		err := e.try(tenant, func(tx pgx.Tx) error {
			var err error
			s, err = e.w.StopBirthday(t.Context(), tx, token, e.actor, at)
			return err //nolint:wrapcheck // test
		})
		return s, err
	}
	s, err := stop(e.tenant, tok, e.now)
	if err != nil || !s.First || s.CustomerID != p.CustomerID || !s.StoppedAt.Equal(e.now) {
		t.Fatalf("dừng: %+v %v", s, err)
	}
	s, err = stop(e.tenant, tok, e.now.Add(time.Hour))
	if err != nil || s.First || !s.StoppedAt.Equal(e.now) {
		t.Fatalf("dừng lại: %+v %v", s, err)
	}
	for name, c := range map[string]struct {
		tenant uuid.UUID
		token  string
	}{"mã lạ": {e.tenant, strings.Repeat("x", 43)}, "tenant khác": {e.other, tok}, "rỗng": {e.tenant, ""}} {
		if _, err := stop(c.tenant, c.token, e.now); !errors.Is(err, customer.ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.birthday.stop'
		AND entity_id = $1`, p.CustomerID.String()); n != 1 {
		t.Errorf("audit dừng: %d", n)
	}
}

// expectOneCustomer: tenant A có đúng một khách.
func (e *writeEnv) expectOneCustomer() {
	e.t.Helper()
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 1 {
		e.t.Errorf("số khách: %d", n)
	}
}
