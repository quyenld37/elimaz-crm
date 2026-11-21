package order_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

func (e *orderEnv) detail(seller, id uuid.UUID) (order.OrderDetail, error) {
	var d order.OrderDetail
	err := e.app(func(tx pgx.Tx) error {
		var err error
		d, err = order.GetDetail(e.t.Context(), tx, seller, id)
		return err //nolint:wrapcheck // test
	})
	return d, err
}

// GetDetail: đơn (như Get), nhãn, phạm vi sửa, khách gốc (đơn của hồ sơ đã gộp; chỉ số khách tính ở Task M2-25), mã giảm giá, cảnh báo đơn
// nghi trùng chưa xử lý, phiếu trả; người bán khác, đơn không có → ErrNotFound.
func TestGetDetail(t *testing.T) {
	t.Parallel()
	e := newM2Env(t)
	f := newListFixture(e)
	v1, v2 := e.variant(250000, 10), e.variant(180000, 10)
	e.coupon(sale10("SALE10", 0))
	in := e.dh01(v1, v2, "SALE10")
	in.Customer = customer.OrderCustomerInput{CustomerID: &f.b.ID}
	in.SellerID = e.seller
	o := e.mustCreateM2(in).Order

	retID := e.seedReturnAndLinks(o.ID, f.o1.ID, f.o2.ID)

	d, err := e.detail(e.seller, o.ID)
	if err != nil {
		t.Fatalf("GetDetail: %v", err)
	}
	if d.ID != o.ID || d.Code != o.Code || d.Version != o.Version || len(d.Items) != 2 || d.GrandTotal != o.GrandTotal {
		t.Errorf("phần đơn: %+v", d.Order)
	}
	if d.Label != orderview.LabelConfirmed || d.CouponCode != "SALE10" {
		t.Errorf("nhãn %q, mã %q", d.Label, d.CouponCode)
	}
	if !d.EditScope.Lines || !d.EditScope.Address || !d.EditScope.CareBy {
		t.Errorf("phạm vi sửa của đơn confirmed chưa vận đơn: %+v", d.EditScope)
	}
	c := d.Customer
	if c.ID != f.b.ID || c.RootID != f.a.ID || c.FullName != "Hồng Nhung" || c.Code != f.a.Code {
		t.Errorf("khách: %+v", c)
	}
	if len(d.Warnings) != 1 || d.Warnings[0].Code != order.WarningPossibleDuplicate || d.Warnings[0].RefID != f.o1.ID {
		t.Errorf("cảnh báo đơn trùng: %+v", d.Warnings)
	}
	if len(d.Returns) != 1 || d.Returns[0].ID != retID || d.Returns[0].Code != "TH0000001" ||
		d.Returns[0].Kind != "customer_return" || d.Returns[0].Status != "requested" {
		t.Errorf("phiếu trả: %+v", d.Returns)
	}

	cancelled := f.o1
	if err := e.app(func(tx pgx.Tx) error {
		return order.ApplyTransition(t.Context(), tx, e.writer(), &cancelled, order.StatusCancelled, "", "", e.actor, e.now())
	}); err != nil {
		t.Fatal(err)
	}
	d, err = e.detail(e.seller, f.o1.ID)
	if err != nil || d.Label != orderview.LabelCancelled || d.EditScope != (order.EditScope{CareBy: true}) ||
		d.CouponCode != "" || len(d.Warnings) != 1 || d.Warnings[0].RefID != o.ID || len(d.Returns) != 0 {
		t.Errorf("đơn đã hủy: %+v %v", d, err)
	}

	if _, err := e.detail(e.seller, uuid.New()); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("đơn không có: %v, muốn ErrNotFound", err)
	}
	if _, err := e.detail(e.partner, o.ID); !errors.Is(err, order.ErrNotFound) {
		t.Errorf("người bán khác: %v, muốn ErrNotFound", err)
	}
}

// seedReturnAndLinks chèn (bằng chủ bảng) phiếu trả TH0000001 của đơn id, cặp nghi trùng (id, open) chưa xử lý và
// (id, resolved) đã xử lý; trả mã phiếu trả.
func (e *orderEnv) seedReturnAndLinks(id, open, resolved uuid.UUID) uuid.UUID {
	e.t.Helper()
	t := e.t
	var retID uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(t.Context(), `INSERT INTO ord.returns (tenant_id, code, order_id, kind)
			VALUES (current_setting('app.tenant_id')::uuid, 'TH0000001', $1, 'customer_return') RETURNING id`,
			id).Scan(&retID); err != nil {
			return err //nolint:wrapcheck // test
		}
		for _, other := range []uuid.UUID{open, resolved} {
			lo, hi := id, other
			if bytes.Compare(lo[:], hi[:]) > 0 {
				lo, hi = hi, lo
			}
			if _, err := tx.Exec(t.Context(), `INSERT INTO ord.order_duplicate_links (tenant_id, order_id, other_order_id,
				matched_by) VALUES (current_setting('app.tenant_id')::uuid, $1, $2, 'customer')`, lo, hi); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		_, err := tx.Exec(t.Context(), `UPDATE ord.order_duplicate_links SET resolved_at = now(), resolution = 'not_duplicate'
			WHERE other_order_id = $1 OR order_id = $1`, resolved)
		return err //nolint:wrapcheck // test
	})
	return retID
}
