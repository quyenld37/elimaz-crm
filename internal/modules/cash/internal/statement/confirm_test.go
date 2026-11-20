package statement_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/cash/internal/statement"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// settledFor đếm sự kiện cod.settled của vận đơn id.
func (e *flowEnv) settledFor(id uuid.UUID) int {
	e.t.Helper()
	return e.count("SELECT count(*) FROM platform.domain_events WHERE topic = $1 AND payload->>'shipmentId' = $2",
		cash.TopicCODSettled, id.String())
}

// TI02: FK0001 630.000 phí 32.000 → matched; chốt → V1 {0, 0, 598000, 32000, −630000}; một bút toán remitted mang dòng
// bảng kê, tác nhân người chốt; cod.settled cho V1; dòng có posted_transfer_id; bảng kê confirmed có người chốt.
func TestConfirm_TI02(t *testing.T) {
	t.Parallel()
	e := newFlowEnv(t)
	v1 := e.shipment(ship{tracking: "FK0001", cod: 630000, status: fulfillment.StatusDelivered})
	id := e.statement(uuid.Nil, nil, line{"FK0001", 630000, 32000, 0})
	c := e.mustConfirm(id)
	if c.Statement.Status != statement.StatusConfirmed || c.Statement.ConfirmedBy == nil || *c.Statement.ConfirmedBy != e.user ||
		len(c.Warnings) != 0 {
		t.Fatalf("chốt: %+v", c)
	}
	if got, want := e.bal(v1), [5]int64{0, 0, 598000, 32000, -630000}; got != want {
		t.Errorf("V1 %v, mong %v", got, want)
	}
	l := lineOf(t, e.get(id), "FK0001")
	if l.PostedTransferID == nil || l.MatchStatus != statement.MatchMatched {
		t.Fatalf("dòng sau chốt: %+v", l)
	}
	if n := e.count(`SELECT count(*) FROM cash.cod_transfers WHERE id = $1 AND kind = 'remitted'
		AND statement_line_id = $2 AND actor_type = 'staff' AND actor_id = $3`, *l.PostedTransferID, l.ID, e.user); n != 1 {
		t.Errorf("bút toán của dòng: %d, mong 1 remitted mang dòng và người chốt", n)
	}
	if n := e.settledFor(v1); n != 1 {
		t.Errorf("cod.settled của V1: %d, mong 1", n)
	}
	if !hasSignal(e.bus, cash.SignalStatementChanged, id) {
		t.Error("không báo hiệu bảng kê đổi")
	}
}

// TI05/AT16: FK0002 420.000 phí 30.000 (thiếu 30.000) → amount_mismatch −30000; chốt → V2 carrier_holding 30.000 còn
// mở, có trong "Lệch đối soát" (thiếu 30.000), chưa cod.settled; bảng kê sau FK0002 30.000 phí 0 → matched phần còn
// lại, V2 về 0, rời danh sách, cod.settled.
func TestConfirm_TI05Shortfall(t *testing.T) {
	t.Parallel()
	e := newFlowEnv(t)
	v2 := e.shipment(ship{tracking: "FK0002", cod: 450000, status: fulfillment.StatusDelivered})
	first := e.statement(uuid.Nil, nil, line{"FK0002", 420000, 30000, 0})
	if l := lineOf(t, e.get(first), "FK0002"); l.MatchStatus != statement.MatchAmountMismatch || diffOf(l) != "-30000" {
		t.Fatalf("khớp: %s chênh %s", l.MatchStatus, diffOf(l))
	}
	e.mustConfirm(first)
	if got, want := e.bal(v2), [5]int64{0, 30000, 390000, 30000, -450000}; got != want {
		t.Errorf("V2 sau bảng kê 1: %v, mong %v", got, want)
	}
	found := false
	for _, d := range e.discrepancies() {
		if d.ShipmentID != nil && *d.ShipmentID == v2 {
			found = d.Kind == cash.DiscrepancyStatementLine && d.MatchStatus == statement.MatchAmountMismatch &&
				d.Diff != nil && *d.Diff == -30000 && d.StatementID != nil && *d.StatementID == first && d.LineNo == 1 &&
				d.COD == 420000 && d.Expected != nil && *d.Expected == 450000 && d.TrackingCode == "FK0002"
		}
	}
	if !found {
		t.Errorf("lệch đối soát không có V2 thiếu 30.000: %+v", e.discrepancies())
	}
	if n := e.settledFor(v2); n != 0 {
		t.Errorf("cod.settled khi còn thiếu: %d", n)
	}
	second := e.statement(uuid.Nil, nil, line{"FK0002", 30000, 0, 0})
	if l := lineOf(t, e.get(second), "FK0002"); l.MatchStatus != statement.MatchMatched {
		t.Fatalf("bảng kê 2: %s, mong matched", l.MatchStatus)
	}
	e.mustConfirm(second)
	if got, want := e.bal(v2), [5]int64{0, 0, 420000, 30000, -450000}; got != want {
		t.Errorf("V2 sau bảng kê 2: %v, mong %v", got, want)
	}
	for _, d := range e.discrepancies() {
		if d.ShipmentID != nil && *d.ShipmentID == v2 {
			t.Errorf("V2 vẫn trong lệch đối soát: %+v", d)
		}
	}
	if n := e.settledFor(v2); n != 1 {
		t.Errorf("cod.settled của V2: %d, mong 1", n)
	}
}

// TI06: dòng ngoài hệ thống → external, không bút toán, tổng số liệu COD không đổi, có trong "Lệch đối soát"; fee_only
// FK0005 0 phí 25.000 → carrier_fee +25000, shop_received −25000 (bút toán fee); tổng khai khác tổng thực nhận →
// cảnh báo, không chặn.
func TestConfirm_ExternalFeeOnlyWarning(t *testing.T) {
	t.Parallel()
	e := newFlowEnv(t)
	v5 := e.shipment(ship{tracking: "FK0005", cod: 0, status: fulfillment.StatusDelivered})
	sumBefore := e.count("SELECT coalesce(sum(abs(amount)), 0)::int FROM cash.cod_ledger_entries")
	declared := int64(-1)
	id := e.statement(uuid.Nil, &declared, line{"NHANH9999", 450000, 30000, 0}, line{"FK0005", 0, 25000, 0})
	c := e.mustConfirm(id)
	if len(c.Warnings) != 1 {
		t.Errorf("cảnh báo %v, mong 1 (tổng khai −1 ≠ thực nhận −25.000)", c.Warnings)
	}
	if got, want := e.bal(v5), [5]int64{0, 0, -25000, 25000, 0}; got != want {
		t.Errorf("V5 %v, mong %v", got, want)
	}
	d := e.get(id)
	ext := lineOf(t, d, "NHANH9999")
	if ext.MatchStatus != statement.MatchExternal || ext.PostedTransferID != nil {
		t.Errorf("dòng ngoài hệ thống: %+v", ext)
	}
	if fee := lineOf(t, d, "FK0005"); fee.MatchStatus != statement.MatchFeeOnly || fee.PostedTransferID == nil {
		t.Errorf("dòng phí: %+v", fee)
	}
	if n := e.count("SELECT count(*) FROM cash.cod_transfers WHERE kind = 'fee' AND shipment_id = $1", v5); n != 1 {
		t.Errorf("bút toán fee: %d, mong 1", n)
	}
	if got := e.count("SELECT coalesce(sum(abs(amount)), 0)::int FROM cash.cod_ledger_entries"); got != sumBefore+50000 {
		t.Errorf("sổ đổi %d, mong chỉ +50.000 của dòng phí (dòng ngoài hệ thống không ghi)", got-sumBefore)
	}
	ok := false
	for _, x := range e.discrepancies() {
		ok = ok || (x.Kind == cash.DiscrepancyStatementLine && x.MatchStatus == statement.MatchExternal &&
			x.TrackingCode == "NHANH9999" && x.ShipmentID == nil)
	}
	if !ok {
		t.Error("lệch đối soát không có dòng ngoài hệ thống")
	}
	if c2 := e.mustConfirm(e.statement(uuid.Nil, new(int64(0)), line{"NHANH9998", 1, 0, 0})); len(c2.Warnings) != 0 {
		t.Errorf("tổng khai 0 = thực nhận 0: cảnh báo %v", c2.Warnings)
	}
}

// Hai bảng kê khác tệp cùng có FK0001: bảng kê thứ hai (khớp sau khi bảng kê đầu đã chốt) → duplicate, không ghi
// tiền, có trong "Lệch đối soát". Hai bảng kê cùng khớp trước khi chốt: bảng kê chốt sau xếp lại thành duplicate lúc
// ghi tiền (không trả hai lần).
func TestConfirm_Duplicate(t *testing.T) {
	t.Parallel()
	e := newFlowEnv(t)
	v1 := e.shipment(ship{tracking: "FK0001", cod: 630000, status: fulfillment.StatusDelivered})
	e.mustConfirm(e.statement(uuid.Nil, nil, line{"FK0001", 630000, 32000, 0}))
	second := e.statement(uuid.Nil, nil, line{"FK0001", 630000, 32000, 0}, line{"PAD1", 1, 0, 0})
	if l := lineOf(t, e.get(second), "FK0001"); l.MatchStatus != statement.MatchDuplicate {
		t.Fatalf("bảng kê 2: %s, mong duplicate", l.MatchStatus)
	}
	e.mustConfirm(second)
	if l := lineOf(t, e.get(second), "FK0001"); l.PostedTransferID != nil {
		t.Error("dòng trùng đã ghi tiền")
	}
	if got, want := e.bal(v1), [5]int64{0, 0, 598000, 32000, -630000}; got != want {
		t.Errorf("V1 %v, mong %v (không ghi hai lần)", got, want)
	}

	v2 := e.shipment(ship{tracking: "FK0002", cod: 450000, status: fulfillment.StatusDelivered})
	a := e.statement(uuid.Nil, nil, line{"FK0002", 450000, 30000, 0})
	b := e.statement(uuid.Nil, nil, line{"FK0002", 450000, 30000, 0}, line{"PAD2", 1, 0, 0})
	if l := lineOf(t, e.get(b), "FK0002"); l.MatchStatus != statement.MatchMatched {
		t.Fatalf("bảng kê b trước khi chốt: %s, mong matched (xem trước)", l.MatchStatus)
	}
	e.mustConfirm(a)
	e.mustConfirm(b)
	if l := lineOf(t, e.get(b), "FK0002"); l.MatchStatus != statement.MatchDuplicate || l.PostedTransferID != nil {
		t.Errorf("bảng kê b sau chốt: %s %v, mong duplicate không bút toán", l.MatchStatus, l.PostedTransferID)
	}
	if got, want := e.bal(v2), [5]int64{0, 0, 420000, 30000, -450000}; got != want {
		t.Errorf("V2 %v, mong %v", got, want)
	}
	dups := 0
	for _, d := range e.discrepancies() {
		if d.MatchStatus == statement.MatchDuplicate {
			dups++
		}
	}
	if dups != 2 {
		t.Errorf("%d dòng trùng trong lệch đối soát, mong 2", dups)
	}
}

// Bảng kê nhiều hơn một lô (PostBatchSize + 5 dòng, nhiều dòng cùng vận đơn): mọi dòng có vận đơn ghi đúng một lần,
// lô sau nối tiếp lô trước; sự kiện lô giao lại không ghi thêm.
func TestConfirm_ManyBatches(t *testing.T) {
	t.Parallel()
	e := newFlowEnv(t)
	n := statement.PostBatchSize + 5
	ids := make([]uuid.UUID, 4)
	for i := range ids {
		ids[i] = e.shipment(ship{tracking: fmt.Sprintf("MB%d", i), cod: int64(n) * 1000, status: fulfillment.StatusDelivered})
	}
	ls := make([]line, n)
	for i := range ls {
		ls[i] = line{fmt.Sprintf("MB%d", i%4), 1000, 100, 0}
	}
	id := e.statement(uuid.Nil, nil, ls...)
	e.mustConfirm(id)
	if got := e.count("SELECT count(*) FROM cash.cod_statement_lines WHERE statement_id = $1 AND posted_transfer_id IS NOT NULL", id); got != n {
		t.Fatalf("%d dòng đã ghi, mong %d", got, n)
	}
	for i, s := range ids {
		per := int64((n - i + 3) / 4)
		want := [5]int64{0, int64(n)*1000 - per*1000, per * 900, per * 100, -int64(n) * 1000}
		if got := e.bal(s); got != want {
			t.Errorf("MB%d %v, mong %v", i, got, want)
		}
	}
	before := e.count("SELECT count(*) FROM cash.cod_transfers")
	// Giao lại sự kiện chốt: dòng đã có bút toán bị bỏ qua (cả chuỗi lô chạy lại).
	e.redeliver(id, cash.TopicStatementConfirmed)
	e.drain()
	if got := e.count("SELECT count(*) FROM cash.cod_transfers"); got != before {
		t.Errorf("giao lại sự kiện chốt ghi thêm %d bút toán", got-before)
	}
}

// Hai bảng kê cùng các vận đơn (thứ tự dòng ngược nhau) chốt và ghi tiền song song: không deadlock, mỗi vận đơn được
// trả đúng một lần (bảng kê sau xếp duplicate).
func TestConfirm_ParallelStatementsNoDeadlock(t *testing.T) {
	t.Parallel()
	e := newFlowEnv(t)
	var ls, rev []line
	ships := make([]uuid.UUID, 6)
	for i := range ships {
		tr := fmt.Sprintf("PD%d", i)
		ships[i] = e.shipment(ship{tracking: tr, cod: 100000, status: fulfillment.StatusDelivered})
		ls = append(ls, line{tr, 100000, 10000, 0})
	}
	for i := len(ls) - 1; i >= 0; i-- {
		rev = append(rev, ls[i])
	}
	rev = append(rev, line{"PAD", 1, 0, 0})
	a, b := e.statement(uuid.Nil, nil, ls...), e.statement(uuid.Nil, nil, rev...)
	for _, id := range []uuid.UUID{a, b} {
		if _, err := e.confirmOnly(id); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() { e.drainQuiet() })
	}
	wg.Wait()
	e.drain()
	for i, s := range ships {
		if got, want := e.bal(s), [5]int64{0, 0, 90000, 10000, -100000}; got != want {
			t.Errorf("PD%d %v, mong %v", i, got, want)
		}
	}
	if n := e.count(`SELECT count(*) FROM cash.cod_statement_lines WHERE statement_id IN ($1, $2)
		AND match_status = 'duplicate'`, a, b); n != len(ships) {
		t.Errorf("%d dòng duplicate, mong %d", n, len(ships))
	}
}

// redeliver giao lại sự kiện topic của bảng kê id thẳng cho consumer cash_statements (như hàng đợi giao lại).
func (e *flowEnv) redeliver(id uuid.UUID, topic string) {
	e.t.Helper()
	var c events.Consumer
	for _, x := range e.mod.Consumers() {
		if x.Name() == cash.StatementsConsumerName {
			c = x
		}
	}
	if c == nil {
		e.t.Fatal("không có consumer cash_statements")
	}
	p := []byte(`{"statementId":"` + id.String() + `"}`)
	e.mustApp(func(tx pgx.Tx) error {
		return c.Handle(e.t.Context(), events.StoredEvent{
			ID: uuid.New(), TenantID: e.tenant, AggregateType: "cod_statement", AggregateID: id.String(), Seq: 1,
			Topic: topic, Payload: p,
		}, tx)
	})
}

// confirmOnly chốt bảng kê id (version hiện tại) mà không chạy việc.
func (e *flowEnv) confirmOnly(id uuid.UUID) (cash.ConfirmedStatement, error) {
	e.t.Helper()
	v := e.get(id).Statement.Version
	var out cash.ConfirmedStatement
	err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = e.mod.Service.ConfirmStatement(e.t.Context(), tx, e.tenant, id, v, e.user)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

// drainQuiet chạy một worker riêng (mã giữ chỗ riêng) tới khi hết việc đến hạn; gọi được từ goroutine (lỗi bỏ qua:
// drain của goroutine test kiểm phần còn lại).
func (e *flowEnv) drainQuiet() {
	w := queue.NewWorker(e.staff.App, e.reg.JobHandlers(), queue.WorkerOptions{WorkerID: "p-" + uuid.NewString()})
	for range 200 {
		n, err := w.RunOnce(e.t.Context(), 5)
		if err != nil || n == 0 {
			return
		}
	}
}
