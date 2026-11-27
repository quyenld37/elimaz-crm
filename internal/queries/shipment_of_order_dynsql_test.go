package queries_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries"
)

// TrackingCodeOfOrder (Task 162): đơn không có, đơn chưa có vận đơn hiện hành, vận đơn hiện hành chưa có mã → không
// có mã; vận đơn hiện hành có mã VTP123456 → mã đó (kể cả khi còn vận đơn cũ đã hủy mã OLD1 của cùng đơn: chỉ vận đơn
// hiện hành); tenant khác không thấy.
func TestTrackingCodeOfOrder(t *testing.T) {
	t.Parallel()
	f := newOneShop(t)
	other := f.s.Tenant(t, "B")
	o := f.order(t)
	read := func(tenant, id uuid.UUID) (string, bool) {
		t.Helper()
		var (
			code string
			ok   bool
		)
		if err := tenancy.WithTenant(t.Context(), f.s.App, tenant, func(tx pgx.Tx) error {
			var err error
			code, ok, err = queries.TrackingCodeOfOrder(t.Context(), tx, id)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
		return code, ok
	}
	if _, ok := read(f.a, uuid.New()); ok {
		t.Fatal("đơn không có: muốn không có mã")
	}
	if _, ok := read(f.a, o.ID); ok {
		t.Fatal("đơn chưa có vận đơn: muốn không có mã")
	}
	var old, cur uuid.UUID
	ship := func(dst *uuid.UUID) {
		if err := tenancy.WithTenant(t.Context(), f.s.App, f.a, func(tx pgx.Tx) error {
			sh, err := f.shop.Shipment(t.Context(), tx, f.shop.Seller, o.ID, f.user)
			*dst = sh.ID
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string, args ...any) {
		t.Helper()
		f.s.Exec(t, f.a, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), q, args...)
			return err //nolint:wrapcheck // test
		})
	}
	ship(&old)
	exec(`UPDATE ord.orders SET current_shipment_id = $2 WHERE id = $1`, o.ID, old)
	if _, ok := read(f.a, o.ID); ok {
		t.Fatal("vận đơn hiện hành chưa có mã (creating): muốn không có mã")
	}
	exec(`UPDATE fulfillment.shipments SET tracking_code = 'OLD1', status = 'cancelled', cancel_kind = 'manual' WHERE id = $1`, old)
	exec(`UPDATE ord.orders SET current_shipment_id = NULL WHERE id = $1`, o.ID)
	ship(&cur)
	exec(`UPDATE fulfillment.shipments SET tracking_code = 'VTP123456', status = 'shipping' WHERE id = $1`, cur)
	exec(`UPDATE ord.orders SET current_shipment_id = $2 WHERE id = $1`, o.ID, cur)
	if code, ok := read(f.a, o.ID); !ok || code != "VTP123456" {
		t.Fatalf("vận đơn hiện hành: %q %v", code, ok)
	}
	if _, ok := read(other, o.ID); ok {
		t.Fatal("tenant khác thấy mã vận đơn")
	}
}
