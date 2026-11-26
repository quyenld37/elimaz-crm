package fulfillment_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Mã địa giới thử của hãng giả (Task 511, seed-web-fixture): thiếu → ghi mã hãng = mã CRM, LocationMap tra được;
// chạy lại → 0; mã hãng khác (đồng bộ thật đã ghi) → đổi về mã CRM; hãng khác không bị chạm; tenant khác không thấy.
func TestEnsureSeedLocations(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	locs := []fulfillment.SeedLocation{
		{Code: "202", Name: "Tỉnh/thành thử nghiệm B"},
		{Code: "20201", Name: "Phường thử nghiệm B", Ward: true},
		{Code: "101", Name: "Tỉnh/thành thử nghiệm A"},
		{Code: "10101", Name: "Phường thử nghiệm A", Ward: true},
	}
	ensure := func(key string) int {
		t.Helper()
		var n int
		if err := e.app(func(tx pgx.Tx) (err error) {
			n, err = fulfillment.EnsureSeedLocations(t.Context(), tx, key, locs)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	svc := fulfillment.New(fulfillment.Deps{DB: e.staff.App}).Service
	lookup := func(key, level, code string) (string, bool) {
		t.Helper()
		var m fulfillment.LocationMap
		if err := e.app(func(tx pgx.Tx) (err error) {
			m, err = svc.LocationMap(t.Context(), tx, key)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return m.Carrier(level, code)
	}
	if n := ensure("fake"); n != 4 {
		t.Fatalf("lần đầu ghi %d, mong 4", n)
	}
	if c, ok := lookup("fake", "ward", "10101"); !ok || c != "10101" {
		t.Fatalf("phường 10101 của hãng giả = %q %v", c, ok)
	}
	if c, ok := lookup("fake", "province", "202"); !ok || c != "202" {
		t.Fatalf("tỉnh 202 của hãng giả = %q %v", c, ok)
	}
	if _, ok := lookup("fake", "province", "10101"); ok {
		t.Fatal("mã phường không được thành mã tỉnh")
	}
	if n := ensure("fake"); n != 0 {
		t.Fatalf("chạy lại ghi %d, mong 0", n)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE fulfillment.carrier_locations SET carrier_code = 'X' WHERE carrier_key = 'fake' AND our_code = '101'")
		return err //nolint:wrapcheck // test
	})
	if n := ensure("fake"); n != 1 {
		t.Fatalf("mã hãng lệch: ghi %d, mong 1", n)
	}
	if c, _ := lookup("fake", "province", "101"); c != "101" {
		t.Fatalf("mã hãng sau khi sửa = %q", c)
	}
	if _, ok := lookup("vtp", "province", "101"); ok {
		t.Fatal("hãng khác không được có mã")
	}
	other := e.staff.Tenant(t, "B")
	if n := e.staff.CountIn(t, other, "SELECT count(*) FROM fulfillment.carrier_locations"); n != 0 {
		t.Fatalf("tenant khác thấy %d dòng", n)
	}
}
