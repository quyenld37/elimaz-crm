package cash_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// rawTransfer chèn (crm_app, SQL thẳng, không qua Post) một bút toán của vận đơn id với các dòng amounts theo thứ tự
// ngăn accounts; trả lỗi của cả transaction (kể cả lỗi lúc commit của trigger hoãn).
func (e *codEnv) rawTransfer(id uuid.UUID, amounts ...int64) error {
	accounts := []string{cash.AccountToCollect, cash.AccountSalesClearing, cash.AccountCarrierHolding}
	return e.app(func(tx pgx.Tx) error {
		var tid uuid.UUID
		if err := tx.QueryRow(e.t.Context(), `INSERT INTO cash.cod_transfers
			(tenant_id, shipment_id, kind, idempotency_key, actor_type)
			VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, $1, 'adjustment', $2, 'staff')
			RETURNING id`, id, uuid.NewString()).Scan(&tid); err != nil {
			return err //nolint:wrapcheck // test
		}
		for i, a := range amounts {
			if _, err := tx.Exec(e.t.Context(), `INSERT INTO cash.cod_ledger_entries (tenant_id, transfer_id, account, amount)
				VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, $1, $2, $3)`,
				tid, accounts[i], a); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
}

// TI10 bản đầu: database từ chối bút toán lệch hai vế lúc commit, kể cả khi phần mềm ghi thẳng bằng SQL.
func TestDatabaseRejectsUnbalancedTransfer(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.shipment(530000)
	for _, tc := range []struct {
		name    string
		amounts []int64
		ok      bool
	}{
		{"không dòng nào", nil, false},
		{"một dòng", []int64{530000}, false},
		{"hai dòng lệch 1 đồng", []int64{530000, -529999}, false},
		{"ba dòng lệch", []int64{530000, -500000, -20000}, false},
		{"hai dòng cân", []int64{530000, -530000}, true},
		{"ba dòng cân", []int64{530000, -500000, -30000}, true},
	} {
		err := e.rawTransfer(s, tc.amounts...)
		if tc.ok {
			if err != nil {
				t.Errorf("%s: mong được, có lỗi %v", tc.name, err)
			}
			continue
		}
		if pgCode(err) != "23514" || !strings.Contains(err.Error(), "cod transfer unbalanced") {
			t.Errorf("%s: mong lỗi 23514 cod transfer unbalanced, có %v", tc.name, err)
		}
	}
	// Chỉ hai bút toán cân còn lại: 4 dòng, tổng 0.
	if n := e.count("SELECT count(*) FROM cash.cod_transfers"); n != 2 {
		t.Errorf("số bút toán = %d, mong 2", n)
	}
	if got := e.total(); got != 0 {
		t.Errorf("tổng sổ = %d, mong 0", got)
	}
}

// withBalancedTransfer dựng môi trường có một vận đơn COD 530000 và một bút toán cân (ghi thẳng bằng SQL).
func withBalancedTransfer(t *testing.T) *codEnv {
	t.Helper()
	e := newCODEnv(t)
	if err := e.rawTransfer(e.shipment(530000), 530000, -530000); err != nil {
		t.Fatal(err)
	}
	return e
}

// Một dòng thêm vào bút toán cũ (transaction sau) mà làm lệch cũng bị chặn lúc commit.
func TestDatabaseRejectsLaterUnbalancedLine(t *testing.T) {
	t.Parallel()
	e := withBalancedTransfer(t)
	err := e.execApp(`INSERT INTO cash.cod_ledger_entries (tenant_id, transfer_id, account, amount)
		SELECT tenant_id, id, 'carrier_fee', 1000 FROM cash.cod_transfers`)
	if pgCode(err) != "23514" {
		t.Fatalf("mong lỗi 23514, có %v", err)
	}
}

// Sổ chỉ ghi thêm: crm_app không sửa, không xóa được dòng sổ hay bút toán.
func TestLedgerIsAppendOnly(t *testing.T) {
	t.Parallel()
	e := withBalancedTransfer(t)
	for _, q := range []string{
		"UPDATE cash.cod_ledger_entries SET amount = amount * 2",
		"DELETE FROM cash.cod_ledger_entries",
		"UPDATE cash.cod_transfers SET reason = 'x'",
		"DELETE FROM cash.cod_transfers",
	} {
		if err := e.execApp(q); pgCode(err) != "42501" {
			t.Errorf("%s: mong lỗi quyền 42501, có %v", q, err)
		}
	}
	// Chủ bảng cũng bị trigger chặn.
	err := e.staff.TryExec(t.Context(), e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), "UPDATE cash.cod_ledger_entries SET amount = amount")
		return err //nolint:wrapcheck // test
	})
	if pgCode(err) != "42501" {
		t.Errorf("chủ bảng UPDATE: mong 42501, có %v", err)
	}
}

func TestPostValidatesAndIsIdempotent(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.shipment(530000)
	carrier := cash.Actor{Type: cash.ActorCarrier}
	expected := func(key string, amounts ...int64) cash.Transfer {
		tr := cash.Transfer{ShipmentID: s, Kind: cash.KindExpected, IdempotencyKey: key, Actor: carrier}
		accounts := []string{cash.AccountToCollect, cash.AccountSalesClearing, cash.AccountCarrierFee}
		for i, a := range amounts {
			tr.Entries = append(tr.Entries, cash.Entry{Account: accounts[i], Amount: a})
		}
		return tr
	}
	post := func(tr cash.Transfer) (bool, error) {
		var created bool
		err := e.app(func(tx pgx.Tx) error {
			var err error
			created, err = cash.Post(t.Context(), tx, tr)
			return err //nolint:wrapcheck // test
		})
		return created, err
	}
	for _, tc := range []struct {
		name string
		tr   cash.Transfer
		want error
	}{
		{"một dòng", expected("k1", 530000), cash.ErrUnbalanced},
		{"lệch", expected("k1", 530000, -529999), cash.ErrUnbalanced},
		{"dòng 0", expected("k1", 530000, -530000, 0), cash.ErrInvalidTransfer},
		{"vượt 2^53", expected("k1", 1<<53, -(1 << 53)), cash.ErrInvalidTransfer},
		{"ngăn lạ", cash.Transfer{
			ShipmentID: s, Kind: cash.KindExpected, IdempotencyKey: "k1", Actor: carrier,
			Entries: []cash.Entry{{Account: "x", Amount: 1}, {Account: cash.AccountToCollect, Amount: -1}},
		}, cash.ErrInvalidTransfer},
		{"trùng ngăn", cash.Transfer{
			ShipmentID: s, Kind: cash.KindExpected, IdempotencyKey: "k1", Actor: carrier,
			Entries: []cash.Entry{{Account: cash.AccountToCollect, Amount: 1}, {Account: cash.AccountToCollect, Amount: -1}},
		}, cash.ErrInvalidTransfer},
		{"loại lạ", func() cash.Transfer { tr := expected("k1", 1, -1); tr.Kind = "gift"; return tr }(), cash.ErrInvalidTransfer},
		{"thiếu khóa", expected("", 1, -1), cash.ErrInvalidTransfer},
		{"thiếu vận đơn", func() cash.Transfer { tr := expected("k1", 1, -1); tr.ShipmentID = uuid.Nil; return tr }(), cash.ErrInvalidTransfer},
		{"tác nhân lạ", func() cash.Transfer { tr := expected("k1", 1, -1); tr.Actor.Type = "robot"; return tr }(), cash.ErrInvalidTransfer},
	} {
		if _, err := post(tc.tr); !errors.Is(err, tc.want) {
			t.Errorf("%s: mong %v, có %v", tc.name, tc.want, err)
		}
	}
	if n := e.count("SELECT count(*) FROM cash.cod_transfers"); n != 0 {
		t.Fatalf("bút toán bị từ chối vẫn ghi %d dòng", n)
	}
	if created, err := post(expected("k1", 530000, -530000)); err != nil || !created {
		t.Fatalf("lần đầu: created=%v err=%v", created, err)
	}
	if created, err := post(expected("k1", 530000, -530000)); err != nil || created {
		t.Fatalf("lần hai cùng khóa: created=%v err=%v, mong false, nil", created, err)
	}
	// Cùng khóa nhưng nội dung khác (vận đơn hay loại): lỗi, không nuốt.
	other := expected("k1", 530000, -530000)
	other.Kind = cash.KindCollected
	if _, err := post(other); !errors.Is(err, cash.ErrIdempotencyKeyReused) {
		t.Fatalf("khóa dùng lại cho loại khác: mong ErrIdempotencyKeyReused, có %v", err)
	}
	if n := e.count("SELECT count(*) FROM cash.cod_ledger_entries"); n != 2 {
		t.Fatalf("số dòng sổ = %d, mong 2", n)
	}
}

// Chạy với từng người bán (KH14): sổ COD theo vận đơn, không phụ thuộc người bán của đơn.
func TestConsumerShippingThenDelivered(t *testing.T) {
	t.Parallel()
	eachSeller(t, testConsumerShippingThenDelivered)
}

func testConsumerShippingThenDelivered(t *testing.T, e *codEnv) {
	s := e.shipment(530000)
	e.mustHandle(e.event(uuid.New(), s, fulfillment.StatusAwaitingPickup))
	if b := e.balances(s); len(b) != 0 {
		t.Fatalf("awaiting_pickup: mong chưa có bút toán, có %v", b)
	}
	e.mustHandle(e.event(uuid.New(), s, fulfillment.StatusShipping))
	if b := e.balances(s); b[cash.AccountToCollect] != 530000 || b[cash.AccountSalesClearing] != -530000 || len(b) != 2 {
		t.Fatalf("shipping: số dư %v", b)
	}
	e.mustHandle(e.event(uuid.New(), s, fulfillment.StatusDelivered))
	b := e.balances(s)
	if b[cash.AccountToCollect] != 0 || b[cash.AccountCarrierHolding] != 530000 || b[cash.AccountSalesClearing] != -530000 {
		t.Fatalf("delivered: số dư %v", b)
	}
	if got := e.total(); got != 0 {
		t.Fatalf("tổng sổ = %d", got)
	}
	var kinds []string
	if err := e.app(func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), `SELECT kind || ':' || actor_type FROM cash.cod_transfers WHERE shipment_id = $1
			ORDER BY created_at, kind DESC`, s)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		kinds, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 2 || kinds[0] != "expected:carrier" || kinds[1] != "collected:carrier" {
		t.Fatalf("bút toán = %v", kinds)
	}
}

// Hãng bỏ qua tín hiệu lấy hàng: delivered tới thẳng → expected rồi collected trong cùng việc.
func TestConsumerDeliveredWithoutShipping(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.shipment(530000)
	e.mustHandle(e.event(uuid.New(), s, fulfillment.StatusDelivered))
	b := e.balances(s)
	if b[cash.AccountToCollect] != 0 || b[cash.AccountCarrierHolding] != 530000 || b[cash.AccountSalesClearing] != -530000 {
		t.Fatalf("số dư %v", b)
	}
	if n := e.count("SELECT count(*) FROM cash.cod_transfers"); n != 2 {
		t.Fatalf("số bút toán = %d, mong 2", n)
	}
}

func TestConsumerIsIdempotent(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.shipment(530000)
	ev := e.event(uuid.New(), s, fulfillment.StatusShipping)
	e.mustHandle(ev)
	e.mustHandle(ev)
	// Sự kiện khác cùng trạng thái của cùng vận đơn cũng không ghi thêm.
	e.mustHandle(e.event(uuid.New(), s, fulfillment.StatusShipping))
	if n := e.count("SELECT count(*) FROM cash.cod_transfers"); n != 1 {
		t.Fatalf("số bút toán = %d, mong 1", n)
	}
	if n := e.count("SELECT count(*) FROM cash.cod_transfers WHERE idempotency_key = $1",
		"evt:"+ev.ID.String()+":expected"); n != 1 {
		t.Fatalf("khóa chống trùng không theo evt:<id>:expected")
	}
	d := e.event(uuid.New(), s, fulfillment.StatusDelivered)
	e.mustHandle(d)
	e.mustHandle(d)
	if n := e.count("SELECT count(*) FROM cash.cod_transfers"); n != 2 {
		t.Fatalf("số bút toán = %d, mong 2", n)
	}
}

func TestConsumerZeroCOD(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.shipment(0)
	e.mustHandle(e.event(uuid.New(), s, fulfillment.StatusShipping))
	e.mustHandle(e.event(uuid.New(), s, fulfillment.StatusDelivered))
	if n := e.count("SELECT count(*) FROM cash.cod_transfers"); n != 0 {
		t.Fatalf("COD 0 vẫn ghi %d bút toán", n)
	}
}

func TestConsumerPermanentErrors(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	bad := e.event(uuid.New(), uuid.New(), fulfillment.StatusShipping)
	if err := e.handle(bad); !errors.Is(err, queue.ErrPermanent) {
		t.Errorf("vận đơn không có: mong ErrPermanent, có %v", err)
	}
	bad.Payload = []byte(`{"shipmentId":"x"}`)
	if err := e.handle(bad); !errors.Is(err, queue.ErrPermanent) {
		t.Errorf("payload hỏng: mong ErrPermanent, có %v", err)
	}
}

// Nối dây: sổ sự kiện có consumer cash; Emit shipment.status_changed rồi worker chạy việc event.cash.
func TestConsumerThroughRegistryAndWorker(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	s := e.shipment(530000)
	r := events.NewRegistry()
	if err := r.Register(cash.Consumer{}); err != nil {
		t.Fatal(err)
	}
	if got := (cash.Consumer{}).Name(); got != "cash" {
		t.Fatalf("Name = %q", got)
	}
	for i, st := range []fulfillment.Status{fulfillment.StatusShipping, fulfillment.StatusDelivered} {
		if err := e.app(func(tx pgx.Tx) error {
			_, _, err := r.Emit(t.Context(), tx, queue.NewQueue(), events.Event{
				TenantID: e.tenant, AggregateType: "shipment", AggregateID: s.String(), Seq: int64(100 + i),
				Topic:   fulfillment.TopicShipmentStatusChanged,
				Payload: fulfillment.StatusChanged{ShipmentID: s, Status: st, CODAmount: 530000},
			})
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Fatal(err)
		}
	}
	w := queue.NewWorker(e.staff.App, r.JobHandlers(), queue.WorkerOptions{WorkerID: "test/" + uuid.NewString()})
	for range 2 {
		if _, err := w.RunOnce(t.Context(), 10); err != nil {
			t.Fatal(err)
		}
	}
	b := e.balances(s)
	if b[cash.AccountToCollect] != 0 || b[cash.AccountCarrierHolding] != 530000 {
		t.Fatalf("số dư %v", b)
	}
}
