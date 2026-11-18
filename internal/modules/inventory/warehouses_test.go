package inventory_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/testkit"
)

// ListWarehouses: kho của tenant, MAIN đứng đầu rồi theo mã; kho đã đóng vẫn có (kèm trạng thái); tenant khác không thấy.
func TestListWarehouses(t *testing.T) {
	staff := testkit.OpenStaff(t)
	a := newInvEnvOn(t, staff, "A")
	newInvEnvOn(t, staff, "B")
	staff.Exec(t, a.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name, status)
			VALUES ($1, $2, 'ZZ', 'Kho Z', 'active'), ($1, $2, 'AA', 'Kho A', 'closed')`, a.tenant, a.seller)
		return err //nolint:wrapcheck // test
	})
	var ws []inventory.Warehouse
	if err := a.app(func(tx pgx.Tx) error {
		var err error
		ws, err = inventory.ListWarehouses(t.Context(), tx)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, w := range ws {
		got = append(got, w.Code+"/"+w.Name+"/"+w.Status)
	}
	want := []string{"MAIN/Kho chính/active", "AA/Kho A/closed", "ZZ/Kho Z/active"}
	if len(got) != len(want) {
		t.Fatalf("muốn %v, nhận %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("muốn %v, nhận %v", want, got)
		}
	}
	if ws[0].ID != a.warehouse {
		t.Fatalf("mã kho MAIN sai: %v", ws[0].ID)
	}
}
