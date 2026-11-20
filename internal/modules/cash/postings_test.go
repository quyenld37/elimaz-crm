package cash_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Ghi sổ COD theo tác động của bước vận đơn (kế hoạch M3, Task M3-29). V1: COD 630.000.
const v1COD = 630000

// fx dựng sự kiện shipment.status_changed của bước seq (Seq = shipments.version) với tác động effects và tiền hãng
// báo đã thu collected (nil: không báo).
func (e *codEnv) fx(id uuid.UUID, seq int64, st fulfillment.Status, collected *int64, effects ...string) events.StoredEvent {
	e.t.Helper()
	p, err := json.Marshal(fulfillment.StatusChanged{
		ShipmentID: id, Status: st, CODAmount: v1COD, Effects: effects, CODCollected: collected,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return events.StoredEvent{
		ID: uuid.New(), TenantID: e.tenant, AggregateType: "shipment", AggregateID: id.String(), Seq: seq,
		Topic: fulfillment.TopicShipmentStatusChanged, Payload: p,
	}
}

// bal đọc số dư năm ngăn của vận đơn id bằng cash.BalancesFor (crm_app).
func (e *codEnv) bal(id uuid.UUID) cash.Balances {
	e.t.Helper()
	var b cash.Balances
	if err := e.app(func(tx pgx.Tx) (err error) {
		b, err = cash.BalancesFor(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("BalancesFor: %v", err)
	}
	return b
}

// balIs: số dư {to_collect, carrier_holding, shop_received, carrier_fee, sales_clearing} của id đúng want.
func (e *codEnv) balIs(id uuid.UUID, want [5]int64) {
	e.t.Helper()
	b := e.bal(id)
	if got := [5]int64{b.ToCollect, b.CarrierHolding, b.ShopReceived, b.CarrierFee, b.SalesClearing}; got != want {
		e.t.Fatalf("số dư %v, muốn %v", got, want)
	}
}

func (e *codEnv) transfers(id uuid.UUID, kind string) int64 {
	e.t.Helper()
	return e.count("SELECT count(*) FROM cash.cod_transfers WHERE shipment_id = $1 AND kind = $2", id, kind)
}

func ptr(v int64) *int64 { return &v }

// v1 tạo vận đơn V1 (COD 630.000) và cho consumer ghi bước Seq 3 với trạng thái st, tác động effects.
func (e *codEnv) v1(st fulfillment.Status, effects ...string) uuid.UUID {
	e.t.Helper()
	s := e.shipment(v1COD)
	e.mustHandle(e.fx(s, 3, st, nil, effects...))
	return s
}

const (
	effPickedUp  = "picked_up"
	effDelivered = "delivered"
	effReturning = "returning"
	effReversed  = "delivery_reversed"
	effCancel    = "cancel_confirmed"
	effFailed    = "delivery_failed"
)

// TI01: lấy hàng → to_collect; giao xong → hãng đang giữ, chưa tính là tiền đã về.
func TestPostings_TI01PickupThenDelivered(t *testing.T) {
	t.Parallel()
	eachSeller(t, func(t *testing.T, e *codEnv) {
		s := e.shipment(v1COD)
		e.mustHandle(e.fx(s, 2, fulfillment.StatusAwaitingPickup, nil))
		e.balIs(s, [5]int64{})
		e.mustHandle(e.fx(s, 3, fulfillment.StatusShipping, nil, effPickedUp))
		e.balIs(s, [5]int64{v1COD, 0, 0, 0, -v1COD})
		e.mustHandle(e.fx(s, 4, fulfillment.StatusDelivered, nil, effDelivered, "close_callbacks"))
		e.balIs(s, [5]int64{0, v1COD, 0, 0, -v1COD})
		if got := e.count(`SELECT count(*) FROM cash.cod_transfers WHERE shipment_id = $1 AND actor_type <> 'carrier'`,
			s); got != 0 {
			t.Errorf("%d bút toán không do hãng", got)
		}
	})
}

// VC08 (tiền): sự kiện giao xong được giao lại hai lần (worker chết sau khi xử lý, việc chạy lại), kể cả sau một lần
// chết trước commit → vẫn một bút toán collected.
func TestPostings_VC08DeliveredReplayed(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.v1(fulfillment.StatusShipping, effPickedUp)
	d := e.fx(s, 4, fulfillment.StatusDelivered, nil, effDelivered)
	crash := errors.New("worker chết trước commit")
	if err := e.app(func(tx pgx.Tx) error {
		if err := (cash.Consumer{}).Handle(t.Context(), d, tx); err != nil {
			return err //nolint:wrapcheck // test
		}
		return crash
	}); !errors.Is(err, crash) {
		t.Fatalf("lần chết: %v", err)
	}
	for range 3 {
		e.mustHandle(d)
	}
	if n := e.transfers(s, cash.KindCollected); n != 1 {
		t.Fatalf("%d bút toán collected", n)
	}
	e.balIs(s, [5]int64{0, v1COD, 0, 0, -v1COD})
}

// Hoàn trước khi giao (Q-10): đang giao → hoàn → mọi ngăn 0 ngay lúc returning.
func TestPostings_ReturnBeforeDelivery(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.v1(fulfillment.StatusShipping, effPickedUp)
	e.mustHandle(e.fx(s, 4, fulfillment.StatusDeliveryFailed, nil, effFailed))
	e.balIs(s, [5]int64{v1COD, 0, 0, 0, -v1COD})
	r := e.fx(s, 5, fulfillment.StatusReturning, nil, effReturning, "close_callbacks")
	e.mustHandle(r)
	e.mustHandle(r)
	e.balIs(s, [5]int64{})
	if n := e.transfers(s, cash.KindReturned); n != 1 {
		t.Fatalf("%d bút toán returned", n)
	}
	// Lấy hàng và hoàn cùng một bước (hãng báo thẳng "đang hoàn" từ chờ lấy).
	s2 := e.v1(fulfillment.StatusReturning, effPickedUp, effReturning)
	e.balIs(s2, [5]int64{})
}

// Giao một phần (Task 26): thu 430.000 → hãng giữ 430.000, phần còn lại returned.
func TestPostings_PartialDelivery(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.v1(fulfillment.StatusShipping, effPickedUp)
	e.mustHandle(e.fx(s, 4, fulfillment.StatusDelivered, ptr(430000), effDelivered))
	e.balIs(s, [5]int64{0, 430000, 0, 0, -430000})
	if e.transfers(s, cash.KindCollected) != 1 || e.transfers(s, cash.KindReturned) != 1 {
		t.Fatal("muốn một collected và một returned")
	}
}

// Đảo "Thành công" khi chưa có tiền về (Task 27, Q-1): bút toán reversal trỏ collected, rồi returned → mọi ngăn 0.
// Giao lại sự kiện đảo không đảo thêm.
func TestPostings_ReversalBeforeRemit(t *testing.T) {
	t.Parallel()
	eachSeller(t, func(t *testing.T, e *codEnv) {
		s := e.v1(fulfillment.StatusDelivered, effPickedUp, effDelivered)
		rev := e.fx(s, 4, fulfillment.StatusReturning, nil, effReversed, effReturning)
		e.mustHandle(rev)
		e.mustHandle(rev)
		e.balIs(s, [5]int64{})
		if got := e.count(`SELECT count(*) FROM cash.cod_transfers r JOIN cash.cod_transfers c
			ON c.id = r.reverses_transfer_id WHERE r.shipment_id = $1 AND r.kind = 'reversal' AND c.kind = 'collected'
			AND r.reason <> ''`, s); got != 1 {
			t.Fatalf("%d bút toán reversal trỏ collected", got)
		}
		if n := e.count("SELECT count(*) FROM cash.reconcile_findings WHERE shipment_id = $1", s); n != 0 {
			t.Errorf("%d phát hiện", n)
		}
	})
}

// remit ghi bút toán remitted như bảng kê đã chốt: hãng giữ −630.000, về shop +598.000, phí +32.000.
func (e *codEnv) remit(s uuid.UUID) {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) error {
		_, err := cash.Post(e.t.Context(), tx, cash.Transfer{
			ShipmentID: s, Kind: cash.KindRemitted, IdempotencyKey: "stmt:" + s.String(),
			Actor: cash.Actor{Type: cash.ActorSystem}, Entries: []cash.Entry{
				{Account: cash.AccountCarrierHolding, Amount: -v1COD},
				{Account: cash.AccountShopReceived, Amount: 598000},
				{Account: cash.AccountCarrierFee, Amount: 32000},
			},
		})
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("ghi remitted: %v", err)
	}
}

type fakeAlerter struct{ key, severity, text string }

func (f *fakeAlerter) Notify(_ context.Context, key, severity, text string) error {
	f.key, f.severity, f.text = key, severity, text
	return nil
}

// Đảo khi đã có tiền về (shop_received 598.000): số dư không đổi; một phát hiện remitted_then_returned; một lệnh cảnh
// báo, văn bản chỉ có mã vận đơn nội bộ (không mã đơn, không SĐT). Giao lại sự kiện không mở thêm.
func TestPostings_ReversalAfterRemit(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.v1(fulfillment.StatusDelivered, effPickedUp, effDelivered)
	e.remit(s)
	before := [5]int64{0, 0, 598000, 32000, -v1COD}
	e.balIs(s, before)
	rev := e.fx(s, 4, fulfillment.StatusReturning, nil, effReversed, effReturning)
	e.mustHandle(rev)
	e.mustHandle(rev)
	e.balIs(s, before)
	if n := e.count(`SELECT count(*) FROM cash.reconcile_findings WHERE shipment_id = $1 AND kind = $2
		AND run_id IS NULL`, s, cash.FindingRemittedThenReturned); n != 1 {
		t.Fatalf("%d phát hiện remitted_then_returned", n)
	}
	if n := e.transfers(s, cash.KindReversal); n != 0 {
		t.Fatalf("%d bút toán đảo", n)
	}
	var payloads []json.RawMessage
	var shipCode, orderCode, phone string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), "SELECT payload FROM platform.jobs WHERE kind = $1", cash.RemittedReturnedAlertKind)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if payloads, err = pgx.CollectRows(rows, pgx.RowTo[json.RawMessage]); err != nil {
			return err //nolint:wrapcheck // test
		}
		return tx.QueryRow(t.Context(), `SELECT s.code, o.code, coalesce(o.ship_phone, '')
			FROM fulfillment.shipments s JOIN ord.orders o ON o.id = s.order_id WHERE s.id = $1`, s).
			Scan(&shipCode, &orderCode, &phone)
	})
	if len(payloads) != 1 {
		t.Fatalf("%d lệnh cảnh báo", len(payloads))
	}
	f := &fakeAlerter{}
	out, err := cash.NewRemittedReturnedAlertCommand(f).Execute(t.Context(), queue.Job{Payload: payloads[0]})
	if err != nil || out.Status != commands.StatusDone {
		t.Fatalf("gửi cảnh báo: %+v %v", out, err)
	}
	if f.key != "cod-remitted-returned:"+shipCode || f.severity != "normal" || !strings.Contains(f.text, shipCode) {
		t.Errorf("cảnh báo %q %q %q", f.key, f.severity, f.text)
	}
	for _, leak := range []string{orderCode, phone} {
		if leak != "" && (strings.Contains(f.text, leak) || strings.Contains(string(payloads[0]), leak)) {
			t.Errorf("cảnh báo lộ %q: %s / %s", leak, f.text, payloads[0])
		}
	}
	if regexp.MustCompile(`\d{9,}`).MatchString(f.text) {
		t.Errorf("cảnh báo có dãy số như SĐT: %s", f.text)
	}
	// Không có Alerter → rejected, không treo.
	if out, _ := cash.NewRemittedReturnedAlertCommand(nil).Execute(t.Context(), queue.Job{Payload: payloads[0]}); out.Status != commands.StatusRejected {
		t.Errorf("không Alerter: %+v", out)
	}
}

// Vận đơn COD 0 (đã trả trước): lấy hàng, giao xong, hoàn → không bút toán nào.
func TestPostings_ZeroCOD(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.shipment(0)
	e.mustHandle(e.fx(s, 3, fulfillment.StatusShipping, nil, effPickedUp))
	e.mustHandle(e.fx(s, 4, fulfillment.StatusDelivered, ptr(0), effDelivered))
	e.mustHandle(e.fx(s, 5, fulfillment.StatusReturning, nil, effReversed, effReturning))
	if n := e.count("SELECT count(*) FROM cash.cod_transfers WHERE shipment_id = $1", s); n != 0 {
		t.Fatalf("%d bút toán", n)
	}
}

// Hủy được xác nhận sau khi sổ đã ghi lấy hàng (không xảy ra theo máy trạng thái): phát hiện, không ghi tiền.
func TestPostings_CancelAfterPickup(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.v1(fulfillment.StatusShipping, effPickedUp)
	c := e.fx(s, 4, fulfillment.StatusCancelled, nil, effCancel)
	e.mustHandle(c)
	e.mustHandle(c)
	e.balIs(s, [5]int64{v1COD, 0, 0, 0, -v1COD})
	if n := e.count("SELECT count(*) FROM cash.reconcile_findings WHERE shipment_id = $1 AND kind = $2", s,
		cash.FindingUnexpectedCancelAfterPickup); n != 1 {
		t.Fatalf("%d phát hiện", n)
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE kind = $1", cash.RemittedReturnedAlertKind); n != 0 {
		t.Errorf("hủy bất thường không gửi cảnh báo tiền đã về, có %d", n)
	}
}

// Bút toán do consumer M1 đã ghi (khóa evt:<sự kiện>:<loại>) không bị luật M3 ghi lặp; tiền hãng báo đã thu âm → lỗi
// vĩnh viễn, không ghi.
func TestPostings_M1KeysAndBadCollected(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.shipment(v1COD)
	for _, tr := range []cash.Transfer{
		{
			ShipmentID: s, Kind: cash.KindExpected, IdempotencyKey: "evt:" + uuid.NewString() + ":expected",
			Actor: cash.Actor{Type: cash.ActorCarrier}, Entries: []cash.Entry{
				{Account: cash.AccountToCollect, Amount: v1COD}, {Account: cash.AccountSalesClearing, Amount: -v1COD},
			},
		},
	} {
		if err := e.app(func(tx pgx.Tx) error {
			_, err := cash.Post(t.Context(), tx, tr)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
	}
	e.mustHandle(e.fx(s, 3, fulfillment.StatusShipping, nil, effPickedUp))
	e.mustHandle(e.fx(s, 4, fulfillment.StatusDelivered, nil, effPickedUp, effDelivered))
	if e.transfers(s, cash.KindExpected) != 1 || e.transfers(s, cash.KindCollected) != 1 {
		t.Fatal("ghi lặp bút toán M1")
	}
	s2 := e.shipment(v1COD)
	if err := e.handle(e.fx(s2, 3, fulfillment.StatusDelivered, ptr(-1), effPickedUp, effDelivered)); !errors.Is(err, queue.ErrPermanent) {
		t.Fatalf("codCollected âm: mong ErrPermanent, có %v", err)
	}
	e.balIs(s2, [5]int64{})
}

// getShipmentCod: số dư và bút toán theo thứ tự ghi, vế, lý do, bút toán bị đảo; vận đơn không có → ErrShipmentNotFound.
func TestPostings_ShipmentCODRead(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.v1(fulfillment.StatusDelivered, effPickedUp, effDelivered)
	e.mustHandle(e.fx(s, 4, fulfillment.StatusReturning, nil, effReversed, effReturning))
	svc := cash.New(cash.Deps{DB: e.staff.App}).Service
	var got cash.ShipmentCOD
	if err := e.app(func(tx pgx.Tx) (err error) {
		got, err = svc.ShipmentCOD(t.Context(), tx, s)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	kinds := make([]string, 0, len(got.Transfers))
	for _, tr := range got.Transfers {
		kinds = append(kinds, tr.Kind)
		if len(tr.Entries) < 2 {
			t.Errorf("%s: %d vế", tr.Kind, len(tr.Entries))
		}
	}
	if strings.Join(kinds, ",") != "expected,collected,reversal,returned" {
		t.Fatalf("bút toán %v", kinds)
	}
	rev := got.Transfers[2]
	if rev.ReversesTransferID == nil || *rev.ReversesTransferID != got.Transfers[1].ID || rev.Reason == "" ||
		rev.Actor.Type != cash.ActorCarrier || rev.OccurredAt.IsZero() {
		t.Errorf("bút toán đảo %+v", rev)
	}
	if got.Balances != (cash.Balances{}) {
		t.Errorf("số dư %+v", got.Balances)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := svc.ShipmentCOD(t.Context(), tx, uuid.New())
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, cash.ErrShipmentNotFound) {
		t.Fatalf("vận đơn không có: %v", err)
	}
}
