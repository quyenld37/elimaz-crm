package customer_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
)

func addrIn(ward string) customer.AddressInput {
	return customer.AddressInput{Name: "Chị Lan", Phone: "0901000002", Line: "12 Lê Lợi", ProvinceCode: "T01", WardCode: ward}
}

func (e *writeEnv) addAddress(id uuid.UUID, in customer.AddressInput, isDefault bool) (uuid.UUID, error) {
	var got uuid.UUID
	err := e.try(e.tenant, func(tx pgx.Tx) error {
		var err error
		got, err = e.w.AddAddress(e.t.Context(), tx, id, in, isDefault, e.actor, e.now)
		return err //nolint:wrapcheck // test
	})
	return got, err
}

func defaults(p customer.Profile) []uuid.UUID {
	var out []uuid.UUID
	for _, a := range p.Addresses {
		if a.IsDefault {
			out = append(out, a.ID)
		}
	}
	return out
}

// AddAddress: địa chỉ đầu tiên thành mặc định; thêm địa chỉ mặc định thứ hai → địa chỉ cũ hết mặc định; xã không
// thuộc tỉnh → ErrWardNotInProvince; mã lạ → ErrUnknownAdminUnit; SĐT sai → ErrInvalidPhone.
func TestAddAddress(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	c := e.create(customer.CreateInput{FullName: "Lan"})
	first, err := e.addAddress(c.ID, addrIn("T01001"), false)
	if err != nil {
		t.Fatal(err)
	}
	if d := defaults(e.get(c.ID)); len(d) != 1 || d[0] != first {
		t.Fatalf("địa chỉ đầu tiên phải là mặc định: %v", d)
	}
	second, err := e.addAddress(c.ID, addrIn("T01002"), true)
	if err != nil {
		t.Fatal(err)
	}
	if d := defaults(e.get(c.ID)); len(d) != 1 || d[0] != second {
		t.Fatalf("mặc định sau khi thêm địa chỉ mặc định thứ hai: %v", d)
	}
	if _, err := e.addAddress(c.ID, addrIn("T01003"), false); err != nil {
		t.Fatal(err)
	}
	if d := defaults(e.get(c.ID)); len(d) != 1 || d[0] != second {
		t.Fatalf("địa chỉ không mặc định không đổi mặc định: %v", d)
	}
	bad := addrIn("T01001")
	bad.Phone = "123"
	for _, x := range []struct {
		name string
		id   uuid.UUID
		in   customer.AddressInput
		want error
	}{
		{"xã không thuộc tỉnh", c.ID, addrIn("T02001"), customer.ErrWardNotInProvince},
		{"mã lạ", c.ID, addrIn("X999"), customer.ErrUnknownAdminUnit},
		{"SĐT sai", c.ID, bad, customer.ErrInvalidPhone},
		{"khách không có", uuid.New(), addrIn("T01001"), customer.ErrNotFound},
	} {
		if _, err := e.addAddress(x.id, x.in, false); !errors.Is(err, x.want) {
			t.Errorf("%s: %v", x.name, err)
		}
	}
	if n := e.count(`SELECT count(*) FROM identity.audit_logs WHERE action = 'customer.address.add' AND entity_id = $1
		AND after::text NOT LIKE '%Lê Lợi%' AND after::text NOT LIKE '%901000002%'`, c.ID.String()); n != 3 {
		t.Errorf("audit thêm địa chỉ: %d", n)
	}
}

// Hai lần thêm địa chỉ mặc định song song cho cùng khách: cả hai thành công (khóa dòng khách), đúng một mặc định.
func TestAddAddressConcurrentDefault(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	c := e.create(customer.CreateInput{FullName: "Lan"})
	errs := make([]error, 4)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() { _, errs[i] = e.addAddress(c.ID, addrIn("T01001"), true) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if d := defaults(e.get(c.ID)); len(d) != 1 {
		t.Fatalf("mặc định: %v", d)
	}
}

// UpdateAddress, DeleteAddress: version cũ → ErrVersionConflict; đặt mặc định đổi mặc định cũ; xóa mềm ẩn địa chỉ;
// địa chỉ của nhóm khác → ErrNotFound.
func TestUpdateDeleteAddress(t *testing.T) {
	t.Parallel()
	e := newWriteEnv(t)
	c := e.create(customer.CreateInput{FullName: "Lan"})
	other := e.create(customer.CreateInput{FullName: "Khác"})
	first, _ := e.addAddress(c.ID, addrIn("T01001"), false)
	second, _ := e.addAddress(c.ID, addrIn("T01002"), false)
	update := func(id, addr uuid.UUID, version int64, p customer.AddressPatch) error {
		return e.try(e.tenant, func(tx pgx.Tx) error {
			return e.w.UpdateAddress(t.Context(), tx, id, addr, version, p, e.actor, e.now)
		})
	}
	yes := true
	moved := addrIn("T02002")
	moved.ProvinceCode = "T02"
	if err := update(c.ID, second, 1, customer.AddressPatch{Address: &moved, IsDefault: &yes}); err != nil {
		t.Fatal(err)
	}
	p := e.get(c.ID)
	if d := defaults(p); len(d) != 1 || d[0] != second {
		t.Fatalf("mặc định sau sửa: %v", d)
	}
	for _, a := range p.Addresses {
		if a.ID == second && (a.ProvinceCode != "T02" || a.WardCode != "T02002" || a.Version != 2) {
			t.Errorf("địa chỉ sau sửa: %+v", a)
		}
	}
	if err := update(c.ID, second, 1, customer.AddressPatch{IsDefault: &yes}); !errors.Is(err, customer.ErrVersionConflict) {
		t.Errorf("version cũ: %v", err)
	}
	if err := update(other.ID, second, 2, customer.AddressPatch{IsDefault: &yes}); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("địa chỉ của khách khác: %v", err)
	}
	bad := addrIn("T01001") // tỉnh T01, xã T01001 nhưng đổi tỉnh sai
	bad.ProvinceCode = "T02"
	if err := update(c.ID, first, 2, customer.AddressPatch{Address: &bad}); !errors.Is(err, customer.ErrWardNotInProvince) {
		t.Errorf("sửa sai tỉnh: %v", err)
	}

	del := func(id, addr uuid.UUID) error {
		return e.try(e.tenant, func(tx pgx.Tx) error {
			return e.w.DeleteAddress(t.Context(), tx, id, addr, e.actor, e.now)
		})
	}
	if err := del(other.ID, second); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("xóa địa chỉ của khách khác: %v", err)
	}
	if err := del(c.ID, second); err != nil {
		t.Fatal(err)
	}
	if err := del(c.ID, second); !errors.Is(err, customer.ErrNotFound) {
		t.Errorf("xóa lần hai: %v", err)
	}
	p = e.get(c.ID)
	if len(p.Addresses) != 1 || p.Addresses[0].ID != first {
		t.Errorf("sau xóa: %+v", p.Addresses)
	}
	if n := e.count(`SELECT count(*) FROM customer.customer_addresses WHERE id = $1 AND deleted_at IS NOT NULL
		AND NOT is_default`, second); n != 1 {
		t.Errorf("xóa mềm: %d", n)
	}
}
