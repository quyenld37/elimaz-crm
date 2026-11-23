package customer_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

// Tạo khách có SĐT: code KH…, danh tính +84…, name_search bỏ dấu, sự kiện customer.created Seq 1, một dòng audit có
// changed_fields chứa phone, full_name mà JSON của dòng không chứa SĐT hay tên; một báo hiệu customer.changed.
func TestCreateCustomerWithPhone(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	c := e.create(customer.CreateInput{FullName: "Nguyễn Thị Hồng Nhung", Phone: "0912345678"})
	if !khPattern.MatchString(c.Code) || c.RootID != c.ID || c.Version != 1 || c.Lifecycle != "lead" ||
		string(c.FullName) != "Nguyễn Thị Hồng Nhung" {
		t.Fatalf("khách mới: %+v", c)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE id = $1 AND name_search = 'nguyen thi hong nhung'
		AND created_by = $2`, c.ID, e.actor.ID); n != 1 {
		t.Errorf("name_search, created_by: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_identities WHERE customer_id = $1 AND kind = 'phone'
		AND scope = '' AND value = '+84912345678' AND value_reversed = '87654321948' AND source = 'staff'`, c.ID); n != 1 {
		t.Errorf("danh tính phone: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'customer.created' AND seq = 1
		AND aggregate_type = 'customer' AND aggregate_id = $1 AND payload->>'customerId' = $1`, c.ID.String()); n != 1 {
		t.Errorf("sự kiện customer.created: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.create' AND entity_id = $1
		AND changed_fields @> ARRAY['phone', 'full_name'] AND actor_type = 'staff' AND actor_id = $2
		AND request_id = 'req-1'`, c.ID.String(), e.actor.ID.String()); n != 1 {
		t.Errorf("audit customer.create: %d", n)
	}
	var row string
	e.inTx(func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT row_to_json(a)::text FROM identity.audit_logs a WHERE entity_id = $1`,
			c.ID.String()).Scan(&row)
	})
	for _, bad := range []string{"912345678", "Hồng Nhung", "nguyen thi"} {
		if strings.Contains(row, bad) {
			t.Errorf("audit chứa %q: %s", bad, row)
		}
	}
	sig := e.bus.Signals()
	if len(sig) != 1 || sig[0].Topic != "customer.changed" || sig[0].Requires != "customer.read" ||
		sig[0].Entity.ID != c.ID.String() || sig[0].TenantID != e.tenant {
		t.Errorf("báo hiệu: %+v", sig)
	}
}

// SĐT đã thuộc khách khác (viết khác dạng) → *IdentityTakenError có gốc chủ; không để lại hồ sơ, sự kiện, audit.
func TestCreateCustomerPhoneTaken(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	first := e.create(customer.CreateInput{FullName: "Chị Hồng", Phone: "0912345678"})
	before := e.count(`SELECT count(*) FROM customer.customers`)
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		_, err := e.w.CreateCustomer(t.Context(), tx, customer.CreateInput{FullName: "Chị Khác", Phone: "0912 345 678"}, e.actor, e.now)
		if err == nil {
			t.Fatal("muốn lỗi identity_taken")
		}
		// Transaction vẫn dùng được (savepoint đã lùi): bên gọi chạy tiếp.
		var n int
		if qerr := tx.QueryRow(t.Context(), "SELECT count(*) FROM customer.customers WHERE full_name = 'Chị Khác'").Scan(&n); qerr != nil || n != 0 {
			t.Errorf("sau khi lùi savepoint: %d %v", n, qerr)
		}
		return err //nolint:wrapcheck // test
	})
	var taken *customer.IdentityTakenError
	if !errors.As(err, &taken) || taken.OwnerRootID != first.RootID || !errors.Is(err, customer.ErrIdentityTaken) {
		t.Fatalf("lỗi: %v", err)
	}
	// Kiểm SĐT đầy đủ ở mọi dạng đã gặp (gõ vào, đã chuẩn hóa, đảo ngược để tìm đuôi), không kiểm mẩu ngắn như "912":
	// lỗi mang mã gốc chủ là UUID ngẫu nhiên, 32 chữ số hex có thể chứa "912" (~0,7% lần chạy đỏ oan).
	for _, bad := range []string{"912345678", "912 345 678", "87654321948"} {
		if strings.Contains(err.Error(), bad) {
			t.Errorf("lỗi mang SĐT (%q): %v", bad, err)
		}
	}
	if after := e.count(`SELECT count(*) FROM customer.customers`); after != before {
		t.Errorf("hồ sơ mồ côi: %d → %d", before, after)
	}
}

// Kiểm đầu vào: tên rỗng, quá dài, ký tự điều khiển; SĐT sai; nguồn lạ; địa chỉ sai tỉnh.
func TestCreateCustomerInvalid(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	for _, c := range []struct {
		name string
		in   customer.CreateInput
		want error
	}{
		{"tên rỗng", customer.CreateInput{FullName: " "}, customer.ErrInvalidInput},
		{"tên 121 ký tự", customer.CreateInput{FullName: strings.Repeat("ạ", 121)}, customer.ErrInvalidInput},
		{"tên có xuống dòng", customer.CreateInput{FullName: "Lan\nAnh"}, customer.ErrInvalidInput},
		{"SĐT sai", customer.CreateInput{FullName: "Lan", Phone: "12345"}, customer.ErrInvalidPhone},
		{"nguồn lạ", customer.CreateInput{FullName: "Lan", Source: "zalo"}, customer.ErrInvalidInput},
		{"xã không thuộc tỉnh", customer.CreateInput{FullName: "Lan", Address: &customer.AddressInput{
			Name: "Lan", Phone: "0901000002", Line: "1 Lê Lợi", ProvinceCode: "T01", WardCode: "T02001",
		}}, customer.ErrWardNotInProvince},
	} {
		err := e.try(e.tenant, func(tx pgx.Tx) error {
			_, err := e.w.CreateCustomer(t.Context(), tx, c.in, e.actor, e.now)
			return err //nolint:wrapcheck // test
		})
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, muốn %v", c.name, err, c.want)
		}
	}
	if n := e.count(`SELECT count(*) FROM customer.customers`); n != 0 {
		t.Errorf("còn %d khách sau các lần sai", n)
	}
}

// Tạo khách kèm ngày sinh và địa chỉ: địa chỉ mặc định, ngày sinh lưu đúng ngày; Get trả cả hai.
func TestCreateCustomerWithAddress(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	bd := time.Date(1990, 3, 8, 0, 0, 0, 0, time.UTC)
	c := e.create(customer.CreateInput{FullName: "Lan", Birthday: &bd, Address: &customer.AddressInput{
		Name: " Mẹ Lan ", Phone: "+84 901 000 002", Line: "12 Lê Lợi", ProvinceCode: "T01", WardCode: "T01002",
	}})
	p := e.get(c.ID)
	if p.Birthday == nil || !time.Time(*p.Birthday).Equal(bd) {
		t.Errorf("ngày sinh: %v", p.Birthday)
	}
	if len(p.Addresses) != 1 {
		t.Fatalf("địa chỉ: %+v", p.Addresses)
	}
	a := p.Addresses[0]
	if !a.IsDefault || a.Name != "Mẹ Lan" || a.Phone != "+84901000002" || a.Line != "12 Lê Lợi" ||
		a.ProvinceName != "Thành phố Thử Một" || a.WardName != "Phường Đa Kao" || a.Version != 1 {
		t.Errorf("địa chỉ: %+v", a)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE entity_id = $1
		AND changed_fields @> ARRAY['address', 'birthday', 'full_name'] AND NOT changed_fields @> ARRAY['phone']`, c.ID.String()); n != 1 {
		t.Errorf("audit tạo có địa chỉ: %d", n)
	}
}

// Update: version cũ → ErrVersionConflict; đổi tên đổi name_search, tăng version; xóa ngày sinh; khách không có hay
// của tenant khác → ErrNotFound.
func TestUpdateCustomer(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	bd := time.Date(1990, 3, 8, 0, 0, 0, 0, time.UTC)
	c := e.create(customer.CreateInput{FullName: "Lan", Birthday: &bd})
	name := "Trần Thị Lan Anh"
	var got customer.Customer
	e.inTx(func(tx pgx.Tx) error {
		var err error
		got, err = e.w.Update(t.Context(), tx, c.ID, 1, customer.UpdateInput{FullName: &name, ClearBirthday: true}, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	if got.Version != 2 || string(got.FullName) != name || got.Birthday != nil {
		t.Fatalf("sau sửa: %+v", got)
	}
	if n := e.count(`SELECT count(*) FROM customer.customers WHERE id = $1 AND name_search = 'tran thi lan anh'`, c.ID); n != 1 {
		t.Errorf("name_search sau đổi tên: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.update' AND entity_id = $1
		AND changed_fields @> ARRAY['full_name', 'birthday', 'version'] AND after::text NOT LIKE '%Lan%'`, c.ID.String()); n != 1 {
		t.Errorf("audit customer.update: %d", n)
	}
	for _, x := range []struct {
		name    string
		tenant  uuid.UUID
		id      uuid.UUID
		version int64
		want    error
	}{
		{"version cũ", e.tenant, c.ID, 1, customer.ErrVersionConflict},
		{"không có", e.tenant, uuid.New(), 1, customer.ErrNotFound},
		{"tenant khác", e.other, c.ID, 2, customer.ErrNotFound},
	} {
		err := e.try(x.tenant, func(tx pgx.Tx) error {
			_, err := e.w.Update(t.Context(), tx, x.id, x.version, customer.UpdateInput{FullName: &name}, e.actor, e.now)
			return err //nolint:wrapcheck // test
		})
		if !errors.Is(err, x.want) {
			t.Errorf("%s: %v", x.name, err)
		}
	}
}

// Hai lần sửa song song cùng version: một thành công, một ErrVersionConflict.
func TestUpdateCustomerConcurrent(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	c := e.create(customer.CreateInput{FullName: "Lan"})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			name := []string{"Lan Một", "Lan Hai"}[i]
			errs[i] = e.try(e.tenant, func(tx pgx.Tx) error {
				_, err := e.w.Update(t.Context(), tx, c.ID, 1, customer.UpdateInput{FullName: &name}, e.actor, e.now)
				return err //nolint:wrapcheck // test
			})
		})
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, customer.ErrVersionConflict):
			conflict++
		default:
			t.Errorf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("thành công %d, xung đột %d", ok, conflict)
	}
}
