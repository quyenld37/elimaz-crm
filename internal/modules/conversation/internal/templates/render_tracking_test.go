package templates_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/templates"
	"elimaz.com.vn/crm/internal/testkit"
)

// ship tạo vận đơn của đơn orderID (qua fulfillment) rồi gán mã vận đơn tracking và trạng thái status bằng chủ bảng
// (hãng giả không trả mã ngay); current → gắn làm vận đơn hiện hành của đơn. Trả mã vận đơn (id).
func (e *tplEnv) ship(orderID uuid.UUID, tracking, status string, current bool) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	testkit.InTenant(e.t, e.staff.App, e.tenant, func(tx pgx.Tx) error {
		sh, err := e.shop.Shipment(e.t.Context(), tx, e.shop.Seller, orderID, uuid.Nil)
		id = sh.ID
		return err //nolint:wrapcheck // test
	})
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		cancelKind := any(nil)
		if status == "cancelled" {
			cancelKind = "manual"
		}
		if _, err := tx.Exec(e.t.Context(), `UPDATE fulfillment.shipments
			SET tracking_code = $2, status = $3, cancel_kind = $4 WHERE id = $1`, id, tracking, status, cancelKind); err != nil {
			return err //nolint:wrapcheck // test
		}
		var cur any // fulfillment.Ship có thể đã gắn vận đơn hiện hành: đặt lại đúng theo current
		if current {
			cur = id
		}
		_, err := tx.Exec(e.t.Context(), `UPDATE ord.orders SET current_shipment_id = $2 WHERE id = $1`, orderID, cur)
		return err //nolint:wrapcheck // test
	})
	return id
}

// {ma_van_don} (Task 162, 01 mục 6): mã vận đơn của vận đơn hiện hành của đơn dùng cho {ma_don}.
func TestRender_TrackingCode(t *testing.T) {
	t.Parallel()
	e := newTplEnv(t)
	tp := e.mustCreate(e.owner, templates.Input{Shortcut: "vd", Title: "Vận đơn", Body: "Mã vận đơn của bạn là {ma_van_don}"})
	render := func(order *uuid.UUID) (templates.Rendered, error) {
		return e.render(tp.ID, templates.RenderInput{ConversationID: e.convLan, OrderID: order, Actor: &e.reader})
	}
	missing := func(label string, order *uuid.UUID) {
		t.Helper()
		if _, err := render(order); missingName(err) != "ma_van_don" {
			t.Fatalf("%s: muốn thiếu ma_van_don, được %v", label, err)
		}
	}

	missing("khách chưa có đơn", nil)

	// Đơn chưa có vận đơn → thiếu giá trị.
	o1, _ := e.order(e.lan)
	missing("đơn chưa có vận đơn", nil)

	// Đơn có vận đơn VTP nhưng chưa gắn làm hiện hành → thiếu (chỉ đọc vận đơn hiện hành của đơn).
	old := e.ship(o1, "VTP123456", "shipping", false)
	missing("vận đơn không hiện hành", nil)
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE ord.orders SET current_shipment_id = $2 WHERE id = $1`, o1, old)
		return err //nolint:wrapcheck // test
	})
	got, err := render(nil)
	if err != nil || got.Text != "Mã vận đơn của bạn là VTP123456" {
		t.Fatalf("VTP: %+v %v", got, err)
	}

	// Vận đơn cũ hủy, vận đơn mới SPX9 gắn làm hiện hành → SPX9 (mỗi lần render đọc mới, không giữ giá trị cũ).
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error { // một đơn chỉ một vận đơn chưa hủy: hủy vận đơn cũ trước
		if _, err := tx.Exec(t.Context(), `UPDATE fulfillment.shipments SET status = 'cancelled', cancel_kind = 'manual'
			WHERE id = $1`, old); err != nil {
			return err //nolint:wrapcheck // test
		}
		_, err := tx.Exec(t.Context(), `UPDATE ord.orders SET current_shipment_id = NULL WHERE id = $1`, o1)
		return err //nolint:wrapcheck // test
	})
	e.ship(o1, "SPX9", "awaiting_pickup", true)
	got, err = render(nil)
	if err != nil || got.Text != "Mã vận đơn của bạn là SPX9" {
		t.Fatalf("SPX9: %+v %v", got, err)
	}

	// Cùng đơn với {ma_don}: đơn mới nhất (o2) chưa có vận đơn → thiếu, dù đơn cũ o1 có SPX9; chỉ định o1 → SPX9.
	o2, _ := e.order(e.lan)
	missing("đơn mới nhất chưa có vận đơn", nil)
	got, err = render(&o1)
	if err != nil || got.Text != "Mã vận đơn của bạn là SPX9" {
		t.Fatalf("chỉ định o1: %+v %v", got, err)
	}
	both := e.mustCreate(e.owner, templates.Input{Shortcut: "cd", Title: "Cả hai", Body: "{ma_don}/{ma_van_don}"})
	e.ship(o2, "GHN77", "shipping", true)
	got, err = e.render(both.ID, templates.RenderInput{ConversationID: e.convLan, Actor: &e.reader})
	if err != nil || !strings.HasSuffix(got.Text, "/GHN77") {
		t.Fatalf("cả hai: %+v %v", got, err)
	}

	// Đơn của khách khác hay không có → ErrOrderNotOfCustomer (không lộ mã vận đơn của đơn người khác).
	maiOrder, _ := e.order(e.mai)
	e.ship(maiOrder, "MAI1", "shipping", true)
	for _, o := range []uuid.UUID{maiOrder, uuid.New()} {
		if _, err := render(&o); !errors.Is(err, templates.ErrOrderNotOfCustomer) {
			t.Fatalf("đơn %s: %v", o, err)
		}
	}
}

// Nút trả lời nhanh cũng điền {ma_van_don}; vận đơn hiện hành còn ở trạng thái creating (chưa có mã) → thiếu giá trị.
func TestRender_TrackingCodeQuickReply(t *testing.T) {
	t.Parallel()
	e := newTplEnv(t)
	o, _ := e.order(e.lan)
	var sid uuid.UUID
	testkit.InTenant(t, e.staff.App, e.tenant, func(tx pgx.Tx) error {
		sh, err := e.shop.Shipment(t.Context(), tx, e.shop.Seller, o, uuid.Nil)
		sid = sh.ID
		return err //nolint:wrapcheck // test
	})
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE ord.orders SET current_shipment_id = $2 WHERE id = $1`, o, sid)
		return err //nolint:wrapcheck // test
	})
	in := templates.Input{
		Shortcut: "vd", Title: "Vận đơn", Body: "Xin chào",
		QuickReplies: []channel.QuickReply{{Title: "Mã {ma_van_don}", Payload: "TRK"}},
	}
	tp := e.mustCreate(e.owner, in)
	if _, err := e.render(tp.ID, templates.RenderInput{ConversationID: e.convLan, Actor: &e.reader}); missingName(err) != "ma_van_don" {
		t.Fatalf("chưa có mã: %v", err)
	}
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE fulfillment.shipments SET tracking_code = 'QR5', status = 'shipping' WHERE id = $1`, sid)
		return err //nolint:wrapcheck // test
	})
	got, err := e.render(tp.ID, templates.RenderInput{ConversationID: e.convLan, Actor: &e.reader})
	if err != nil || len(got.QuickReplies) != 1 || got.QuickReplies[0] != (channel.QuickReply{Title: "Mã QR5", Payload: "TRK"}) {
		t.Fatalf("có mã: %+v %v", got, err)
	}
}
