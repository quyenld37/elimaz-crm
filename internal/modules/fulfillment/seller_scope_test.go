package fulfillment_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// sellerObjects là mã một đối tượng mỗi loại (vận đơn và việc con của nó) của một người bán.
type sellerObjects map[fulfillment.ScopeKind]uuid.UUID

// objectsOf tạo đơn, vận đơn của người bán của e và các việc con của vận đơn (shiptest.ChildrenOf); trả mã theo loại.
func (e *shipEnv) objectsOf() sellerObjects {
	e.t.Helper()
	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	var c shiptest.Children
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		c = shiptest.ChildrenOf(e.t, tx, e.tenant, s.ID, o.ID, e.actor.UserID)
		return nil
	})
	return sellerObjects{
		fulfillment.ScopeShipment: s.ID, fulfillment.ScopeCallback: c.Callback, fulfillment.ScopeAnomaly: c.Anomaly,
		fulfillment.ScopeAddressChange: c.AddressChange, fulfillment.ScopeLabelBatch: c.LabelBatch,
	}
}

// TestCheckSeller (ADR-29, Task 527): mỗi loại đối tượng của API (vận đơn, việc gọi lại, việc cần xem, yêu cầu đổi
// địa chỉ, lô nhãn) — đúng người bán → nil; người bán khác, mã lạ → sentinel "không có" của loại đó (404, không lộ có).
func TestCheckSeller(t *testing.T) {
	t.Parallel()
	a := newShipEnv(t)
	b := *a
	b.seller, b.account = a.shop.Partner, a.shop.PartnerAccount
	objs := map[uuid.UUID]sellerObjects{a.shop.Seller: a.objectsOf(), a.shop.Partner: b.objectsOf()}
	hidden := map[fulfillment.ScopeKind]error{
		fulfillment.ScopeShipment: fulfillment.ErrNotFound, fulfillment.ScopeCallback: fulfillment.ErrCallbackNotFound,
		fulfillment.ScopeAnomaly: fulfillment.ErrAnomalyNotFound, fulfillment.ScopeAddressChange: fulfillment.ErrNotFound,
		fulfillment.ScopeLabelBatch: fulfillment.ErrNotFound,
	}
	other := map[uuid.UUID]uuid.UUID{a.shop.Seller: a.shop.Partner, a.shop.Partner: a.shop.Seller}
	for seller, byKind := range objs {
		for kind, id := range byKind {
			check := func(s, id uuid.UUID) error {
				var err error
				if e := a.app(func(tx pgx.Tx) error {
					err = fulfillment.CheckSeller(t.Context(), tx, s, kind, id)
					return nil
				}); e != nil {
					t.Fatal(e)
				}
				return err //nolint:wrapcheck // test so sentinel
			}
			if err := check(seller, id); err != nil {
				t.Errorf("%s của chính người bán: %v, muốn nil", kind, err)
			}
			if err := check(other[seller], id); !errors.Is(err, hidden[kind]) {
				t.Errorf("%s của người bán khác: %v, muốn %v", kind, err, hidden[kind])
			}
			if err := check(seller, uuid.New()); !errors.Is(err, hidden[kind]) {
				t.Errorf("%s mã lạ: %v, muốn %v", kind, err, hidden[kind])
			}
		}
	}
	if err := a.app(func(tx pgx.Tx) error {
		return fulfillment.CheckSeller(t.Context(), tx, a.shop.Seller, "khác", uuid.New())
	}); err == nil {
		t.Fatal("loại lạ: nil, muốn lỗi")
	}
}
