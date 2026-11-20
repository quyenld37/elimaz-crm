package inventory_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/testkit"
)

// SetWarehousePickup (Task M3-09): ghi địa chỉ lấy hàng khi version khớp → warehouses_v có địa chỉ mới, version tăng;
// version cũ → ErrVersionConflict, không đổi gì; kho lạ hay của tenant khác → ErrNotFound; tên, dòng địa chỉ rỗng →
// ErrInvalidInput.
func TestSetWarehousePickup(t *testing.T) {
	staff := testkit.OpenStaff(t)
	a := newInvEnvOn(t, staff, "A")
	b := newInvEnvOn(t, staff, "B")
	addr := inventory.PickupAddress{Name: "Kho MAIN", Line: "1 Hàng Bài", ProvinceCode: "01", WardCode: "00070"}
	phone := logging.Phone("+84901234567")
	set := func(e *invEnv, id uuid.UUID, version int64, ad inventory.PickupAddress) (v int64, err error) {
		err = e.app(func(tx pgx.Tx) error {
			v, err = inventory.SetWarehousePickup(t.Context(), tx, id, version, ad, phone)
			return err //nolint:wrapcheck // test
		})
		return v, err //nolint:wrapcheck // test
	}
	get := func() inventory.WarehousePickup {
		var p inventory.WarehousePickup
		if err := a.app(func(tx pgx.Tx) (err error) {
			p, err = inventory.GetWarehousePickup(t.Context(), tx, a.warehouse)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return p
	}
	before := get()
	if before.Version != 1 || before.Address != (inventory.PickupAddress{}) || before.Phone != "" {
		t.Fatalf("kho mới: %+v", before)
	}
	v, err := set(a, a.warehouse, 1, addr)
	if err != nil || v != 2 {
		t.Fatalf("ghi lần đầu: v=%d err=%v", v, err)
	}
	var raw []byte
	var gotPhone string
	staff.Exec(t, a.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT pickup_address, pickup_phone FROM inventory.warehouses_v WHERE warehouse_id = $1`,
			a.warehouse).Scan(&raw, &gotPhone)
	})
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil || m["name"] != "Kho MAIN" || m["line"] != "1 Hàng Bài" ||
		m["province_code"] != "01" || m["ward_code"] != "00070" || len(m) != 4 || gotPhone != string(phone) {
		t.Fatalf("warehouses_v: %s %q (%v)", raw, gotPhone, err)
	}
	if p := get(); p.Address != addr || p.Phone != phone || p.Version != 2 {
		t.Fatalf("GetWarehousePickup: %+v", p)
	}
	other := addr
	other.Line = "2 Tràng Tiền"
	if _, err := set(a, a.warehouse, 1, other); !errors.Is(err, inventory.ErrVersionConflict) {
		t.Fatalf("version cũ: %v, muốn ErrVersionConflict", err)
	}
	if p := get(); p.Address != addr || p.Version != 2 {
		t.Fatalf("version cũ không được đổi: %+v", p)
	}
	if _, err := set(a, uuid.New(), 1, addr); !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("kho lạ: %v, muốn ErrNotFound", err)
	}
	if _, err := set(b, a.warehouse, 2, addr); !errors.Is(err, inventory.ErrNotFound) {
		t.Fatalf("kho của tenant khác: %v, muốn ErrNotFound", err)
	}
	for _, bad := range []inventory.PickupAddress{
		{Line: "x", ProvinceCode: "01", WardCode: "1"},
		{Name: "x", ProvinceCode: "01", WardCode: "1"},
		{Name: "x", Line: "x", WardCode: "1"},
		{Name: "x", Line: "x", ProvinceCode: "01"},
	} {
		if _, err := set(a, a.warehouse, 2, bad); !errors.Is(err, inventory.ErrInvalidInput) {
			t.Errorf("địa chỉ thiếu trường %+v: %v, muốn ErrInvalidInput", bad, err)
		}
	}
	if v, err := set(a, a.warehouse, 2, other); err != nil || v != 3 {
		t.Fatalf("ghi lần hai: v=%d err=%v", v, err)
	}
}
