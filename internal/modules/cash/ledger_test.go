package cash_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/cash"
)

// codV1 là COD của vận đơn V1 trong các ca của kế hoạch (Task M3-02).
const codV1 = 630000

// transfer dựng bút toán kind của vận đơn s với khóa key và các vế theo cặp (ngăn, số tiền).
func transfer(s uuid.UUID, kind, key string, entries ...cash.Entry) cash.Transfer {
	return cash.Transfer{
		ShipmentID: s, Kind: kind, IdempotencyKey: key, Actor: cash.Actor{Type: cash.ActorCarrier}, Entries: entries,
	}
}

func expectedTr(s uuid.UUID, key string, x int64) cash.Transfer {
	return transfer(s, cash.KindExpected, key,
		cash.Entry{Account: cash.AccountToCollect, Amount: x}, cash.Entry{Account: cash.AccountSalesClearing, Amount: -x})
}

func collectedTr(s uuid.UUID, key string, x int64) cash.Transfer {
	return transfer(s, cash.KindCollected, key,
		cash.Entry{Account: cash.AccountToCollect, Amount: -x}, cash.Entry{Account: cash.AccountCarrierHolding, Amount: x})
}

// postID gọi cash.PostWithID trong một transaction crm_app của tenant.
func (e *codEnv) postID(tr cash.Transfer) (id uuid.UUID, created bool, err error) {
	err = e.app(func(tx pgx.Tx) error {
		var err error
		id, created, err = cash.PostWithID(e.t.Context(), tx, tr)
		return err //nolint:wrapcheck // test
	})
	return id, created, err
}

func (e *codEnv) mustPost(tr cash.Transfer) uuid.UUID {
	e.t.Helper()
	id, _, err := e.postID(tr)
	if err != nil {
		e.t.Fatalf("PostWithID %s: %v", tr.IdempotencyKey, err)
	}
	return id
}

// reverse gọi cash.Reverse trong một transaction crm_app của tenant.
func (e *codEnv) reverse(id uuid.UUID, reason string) (uuid.UUID, error) {
	var rid uuid.UUID
	err := e.app(func(tx pgx.Tx) error {
		var err error
		rid, err = cash.Reverse(e.t.Context(), tx, id, reason, cash.Actor{Type: cash.ActorStaff, ID: e.user})
		return err //nolint:wrapcheck // test
	})
	return rid, err
}

// balancesFor đọc cash.BalancesFor của vận đơn s.
func (e *codEnv) balancesFor(s uuid.UUID) cash.Balances {
	e.t.Helper()
	var b cash.Balances
	if err := e.app(func(tx pgx.Tx) (err error) {
		b, err = cash.BalancesFor(e.t.Context(), tx, s)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("BalancesFor: %v", err)
	}
	return b
}

// viewBalances đọc cash.shipment_cod_v của vận đơn s (không dòng → mọi ngăn 0).
func (e *codEnv) viewBalances(s uuid.UUID) cash.Balances {
	e.t.Helper()
	var b cash.Balances
	if err := e.app(func(tx pgx.Tx) error {
		err := tx.QueryRow(e.t.Context(), `SELECT to_collect, carrier_holding, shop_received, carrier_fee, sales_clearing
			FROM cash.shipment_cod_v WHERE shipment_id = $1`, s).
			Scan(&b.ToCollect, &b.CarrierHolding, &b.ShopReceived, &b.CarrierFee, &b.SalesClearing)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("đọc shipment_cod_v: %v", err)
	}
	return b
}

// TI10: database từ chối lúc commit bút toán lệch, thiếu dòng, hay có dòng khác vận đơn của bút toán, kể cả khi ghi
// thẳng bằng SQL không qua PostWithID.
func TestTI10_DatabaseRejectsBadTransfer(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	v1, v2 := e.shipment(codV1), e.shipment(codV1)
	tc, sc := cash.AccountToCollect, cash.AccountSalesClearing
	for _, c := range []struct {
		name  string
		lines []rawLine
		ok    bool
	}{
		{"lệch 1 đồng", []rawLine{{v1, tc, codV1}, {v1, sc, -(codV1 - 1)}}, false},
		{"một dòng", []rawLine{{v1, tc, codV1}}, false},
		{"không dòng nào", nil, false},
		{"cân nhưng một dòng khác vận đơn", []rawLine{{v1, tc, codV1}, {v2, sc, -codV1}}, false},
		{"cân nhưng mọi dòng khác vận đơn", []rawLine{{v2, tc, codV1}, {v2, sc, -codV1}}, false},
		{"cân, cùng vận đơn", []rawLine{{v1, tc, codV1}, {v1, sc, -codV1}}, true},
	} {
		err := e.rawLines(v1, c.lines...)
		if c.ok {
			if err != nil {
				t.Errorf("%s: mong được, có %v", c.name, err)
			}
			continue
		}
		if pgCode(err) != "23514" || !strings.Contains(err.Error(), "unbalanced") {
			t.Errorf("%s: mong lỗi 23514 chứa unbalanced, có %v", c.name, err)
		}
	}
	if n := e.count("SELECT count(*) FROM cash.cod_transfers"); n != 1 {
		t.Errorf("số bút toán = %d, mong 1", n)
	}
	if got := e.total(); got != 0 {
		t.Errorf("tổng sổ = %d, mong 0", got)
	}
}

// countingTx đếm câu lệnh gửi tới database qua tx (tracer thử: bọc pgx.Tx, chuyển tiếp).
type countingTx struct {
	pgx.Tx
	mu sync.Mutex
	n  int
}

func (c *countingTx) hit() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *countingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	c.hit()
	return c.Tx.Exec(ctx, sql, args...) //nolint:wrapcheck // test
}

//nolint:ireturn // hiện thực pgx.Tx
func (c *countingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.hit()
	return c.Tx.Query(ctx, sql, args...) //nolint:wrapcheck // test
}

//nolint:ireturn // hiện thực pgx.Tx
func (c *countingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	c.hit()
	return c.Tx.QueryRow(ctx, sql, args...)
}

//nolint:ireturn // hiện thực pgx.Tx
func (c *countingTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	c.hit()
	return c.Tx.SendBatch(ctx, b)
}

// PostWithID kiểm ở Go trước: bút toán lệch, thiếu dòng, lý do sai loại, đảo thiếu đích → lỗi, không câu lệnh nào tới
// database.
func TestPostWithIDRejectsBeforeDatabase(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	v1 := e.shipment(codV1)
	lineID := uuid.New()
	for _, c := range []struct {
		name string
		tr   cash.Transfer
		want error
	}{
		{"tổng khác 0", transfer(v1, cash.KindExpected, "k",
			cash.Entry{Account: cash.AccountToCollect, Amount: codV1},
			cash.Entry{Account: cash.AccountSalesClearing, Amount: -(codV1 - 1)}), cash.ErrUnbalanced},
		{
			"một dòng", transfer(v1, cash.KindExpected, "k", cash.Entry{Account: cash.AccountToCollect, Amount: codV1}),
			cash.ErrUnbalanced,
		},
		{"adjustment thiếu lý do", func() cash.Transfer {
			tr := expectedTr(v1, "k", codV1)
			tr.Kind = cash.KindAdjustment
			return tr
		}(), cash.ErrInvalidTransfer},
		{
			"expected có lý do", func() cash.Transfer { tr := expectedTr(v1, "k", codV1); tr.Reason = "x"; return tr }(),
			cash.ErrInvalidTransfer,
		},
		{"reversal thiếu bút toán bị đảo", func() cash.Transfer {
			tr := expectedTr(v1, "k", codV1)
			tr.Kind, tr.Reason = cash.KindReversal, "sai"
			return tr
		}(), cash.ErrInvalidTransfer},
		{"loại khác reversal mà có bút toán bị đảo", func() cash.Transfer {
			tr := expectedTr(v1, "k", codV1)
			tr.ReversesTransferID = &lineID
			return tr
		}(), cash.ErrInvalidTransfer},
	} {
		var got error
		ct := &countingTx{}
		if err := e.app(func(tx pgx.Tx) error {
			ct.Tx = tx
			_, _, got = cash.PostWithID(t.Context(), ct, c.tr)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(got, c.want) {
			t.Errorf("%s: mong %v, có %v", c.name, c.want, got)
		}
		if ct.n != 0 {
			t.Errorf("%s: %d câu lệnh tới database, mong 0", c.name, ct.n)
		}
	}
	// Bút toán đúng thì có câu lệnh (tracer đếm thật).
	ct := &countingTx{}
	if err := e.app(func(tx pgx.Tx) error {
		ct.Tx = tx
		_, _, err := cash.PostWithID(t.Context(), ct, expectedTr(v1, "ok", codV1))
		return err //nolint:wrapcheck // test
	}); err != nil || ct.n == 0 {
		t.Fatalf("bút toán đúng: err=%v, %d câu lệnh", err, ct.n)
	}
}

// PostWithID chống trùng: cùng khóa hai lần → created true rồi false, cùng id, đúng 2 dòng sổ; Post của M1 cùng khóa
// → false. Dòng sổ mang vận đơn của bút toán; occurred_at rỗng → giờ ghi, có giá trị → giữ.
func TestPostWithIDIdempotent(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	v1 := e.shipment(codV1)
	id1, c1, err1 := e.postID(expectedTr(v1, "expected:V1", codV1))
	id2, c2, err2 := e.postID(expectedTr(v1, "expected:V1", codV1))
	if err1 != nil || err2 != nil || !c1 || c2 || id1 != id2 || id1 == uuid.Nil {
		t.Fatalf("hai lần: (%s %v %v), (%s %v %v)", id1, c1, err1, id2, c2, err2)
	}
	var created bool
	if err := e.app(func(tx pgx.Tx) (err error) {
		created, err = cash.Post(t.Context(), tx, expectedTr(v1, "expected:V1", codV1))
		return err //nolint:wrapcheck // test
	}); err != nil || created {
		t.Fatalf("Post cùng khóa: created=%v err=%v", created, err)
	}
	if n := e.count("SELECT count(*) FROM cash.cod_ledger_entries WHERE transfer_id = $1 AND shipment_id = $2",
		id1, v1); n != 2 {
		t.Fatalf("dòng sổ của bút toán có vận đơn V1 = %d, mong 2", n)
	}
	if n := e.count("SELECT count(*) FROM cash.cod_ledger_entries"); n != 2 {
		t.Fatalf("số dòng sổ = %d, mong 2", n)
	}
	if n := e.count(`SELECT count(*) FROM cash.cod_transfers WHERE id = $1
		AND occurred_at BETWEEN now() - interval '1 minute' AND now()`, id1); n != 1 {
		t.Fatalf("occurred_at rỗng không thành giờ ghi")
	}
	at := time.Date(2026, 11, 3, 8, 30, 0, 0, time.UTC)
	tr := collectedTr(v1, "collected:V1", codV1)
	tr.OccurredAt = at
	id := e.mustPost(tr)
	if n := e.count("SELECT count(*) FROM cash.cod_transfers WHERE id = $1 AND occurred_at = $2", id, at); n != 1 {
		t.Fatalf("occurred_at không giữ giá trị truyền vào")
	}
}

// TI01: sau expected và collected, tiền nằm ở "hãng đang giữ", shop chưa nhận; BalancesFor và cash.shipment_cod_v
// cùng số. Reverse(collected) đưa về sau expected; đảo lần hai → ErrAlreadyReversed.
func TestTI01_BalancesAndReverse(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	v1 := e.shipment(codV1)
	if b := e.balancesFor(v1); b != (cash.Balances{}) {
		t.Fatalf("chưa bút toán: %+v", b)
	}
	e.mustPost(expectedTr(v1, "expected:V1", codV1))
	collected := e.mustPost(collectedTr(v1, "collected:V1", codV1))
	want := cash.Balances{CarrierHolding: codV1, SalesClearing: -codV1}
	if b := e.balancesFor(v1); b != want {
		t.Fatalf("BalancesFor = %+v, mong %+v", b, want)
	}
	if b := e.viewBalances(v1); b != want {
		t.Fatalf("shipment_cod_v = %+v, mong %+v", b, want)
	}
	rid, err := e.reverse(collected, "hãng báo nhầm đã giao")
	if err != nil || rid == uuid.Nil {
		t.Fatalf("Reverse: %s %v", rid, err)
	}
	want = cash.Balances{ToCollect: codV1, SalesClearing: -codV1}
	if b := e.balancesFor(v1); b != want {
		t.Fatalf("sau Reverse: %+v, mong %+v", b, want)
	}
	if b := e.viewBalances(v1); b != want {
		t.Fatalf("shipment_cod_v sau Reverse: %+v", b)
	}
	if n := e.count(`SELECT count(*) FROM cash.cod_transfers_v WHERE id = $1 AND kind = 'reversal'
		AND reverses_transfer_id = $2 AND reason = 'hãng báo nhầm đã giao' AND actor_type = 'staff' AND shipment_id = $3`,
		rid, collected, v1); n != 1 {
		t.Fatalf("bút toán đảo trong cod_transfers_v không đúng")
	}
	if n := e.count("SELECT count(*) FROM cash.cod_transfers WHERE id = $1 AND idempotency_key = $2",
		rid, "reversal:"+collected.String()); n != 1 {
		t.Fatalf("khóa chống trùng của bút toán đảo không theo reversal:<id>")
	}
	if _, err := e.reverse(collected, "lần hai"); !errors.Is(err, cash.ErrAlreadyReversed) {
		t.Fatalf("đảo lần hai: mong ErrAlreadyReversed, có %v", err)
	}
	if _, err := e.reverse(rid, "đảo bút toán đảo"); !errors.Is(err, cash.ErrInvalidTransfer) {
		t.Fatalf("đảo bút toán đảo: mong ErrInvalidTransfer, có %v", err)
	}
	if _, err := e.reverse(uuid.New(), "không có"); !errors.Is(err, cash.ErrTransferNotFound) {
		t.Fatalf("đảo bút toán không có: mong ErrTransferNotFound, có %v", err)
	}
	if _, err := e.reverse(collected, ""); !errors.Is(err, cash.ErrInvalidTransfer) {
		t.Fatalf("đảo thiếu lý do: mong ErrInvalidTransfer, có %v", err)
	}
	if got := e.total(); got != 0 {
		t.Fatalf("tổng sổ = %d", got)
	}
}

// Bút toán reversal ghi bằng PostWithID với khóa khác cho bút toán đã bị đảo → ErrAlreadyReversed (unique một phần).
func TestPostWithIDSecondReversalOtherKey(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	v1 := e.shipment(codV1)
	exp := e.mustPost(expectedTr(v1, "expected:V1", codV1))
	if _, err := e.reverse(exp, "nhầm"); err != nil {
		t.Fatal(err)
	}
	tr := transfer(v1, cash.KindReversal, "khóa-khác",
		cash.Entry{Account: cash.AccountToCollect, Amount: -codV1},
		cash.Entry{Account: cash.AccountSalesClearing, Amount: codV1})
	tr.Reason, tr.ReversesTransferID = "lần hai", &exp
	if _, _, err := e.postID(tr); !errors.Is(err, cash.ErrAlreadyReversed) {
		t.Fatalf("mong ErrAlreadyReversed, có %v", err)
	}
}

// Hai Reverse cùng lúc trên một bút toán: đúng một bút toán đảo, bên còn lại ErrAlreadyReversed.
func TestReverseConcurrent(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	v1 := e.shipment(codV1)
	exp := e.mustPost(expectedTr(v1, "expected:V1", codV1))
	const n = 4
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { _, errs[i] = e.reverse(exp, "đảo song song") })
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, cash.ErrAlreadyReversed):
			t.Errorf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d lần đảo thành công, mong 1", ok)
	}
	if b := e.balancesFor(v1); b != (cash.Balances{}) {
		t.Fatalf("số dư sau đảo: %+v", b)
	}
}

// Loại fee (M3): phí hãng trừ từ tiền hãng giữ; adjustment có lý do được; database chặn (CHECK, ghi thẳng bằng SQL)
// adjustment, reversal thiếu lý do, expected có lý do, reversal thiếu đích, loại khác reversal có đích.
func TestFeeAndAdjustmentReason(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	v1 := e.shipment(codV1)
	exp := e.mustPost(expectedTr(v1, "expected:V1", codV1))
	e.mustPost(collectedTr(v1, "collected:V1", codV1))
	e.mustPost(transfer(v1, cash.KindFee, "fee:V1",
		cash.Entry{Account: cash.AccountCarrierHolding, Amount: -30000},
		cash.Entry{Account: cash.AccountCarrierFee, Amount: 30000}))
	adj := transfer(v1, cash.KindAdjustment, "adj:V1",
		cash.Entry{Account: cash.AccountCarrierHolding, Amount: -600000},
		cash.Entry{Account: cash.AccountShopReceived, Amount: 600000})
	adj.Reason = "đối chiếu tay"
	e.mustPost(adj)
	want := cash.Balances{ShopReceived: 600000, CarrierFee: 30000, SalesClearing: -codV1}
	if b := e.balancesFor(v1); b != want {
		t.Fatalf("số dư %+v, mong %+v", b, want)
	}
	// Mỗi ca chỉ vi phạm đúng một CHECK (lỗi ngay lúc INSERT, có tên ràng buộc — khác lỗi "unbalanced" lúc commit).
	target := "'" + exp.String() + "'"
	for _, c := range []struct{ kind, reason, reverses, constraint string }{
		{"adjustment", "", "NULL", "cod_transfers_reason_kind"},
		{"reversal", "", target, "cod_transfers_reason_kind"},
		{"expected", "x", "NULL", "cod_transfers_reason_kind"},
		{"reversal", "x", "NULL", "cod_transfers_reversal_target"},
		{"adjustment", "x", target, "cod_transfers_reversal_target"},
	} {
		err := e.execApp(`INSERT INTO cash.cod_transfers
			(tenant_id, shipment_id, kind, idempotency_key, reason, actor_type, reverses_transfer_id)
			VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, '` + v1.String() + `', '` + c.kind +
			`', '` + uuid.NewString() + `', '` + c.reason + `', 'staff', ` + c.reverses + `)`)
		if pg, ok := errors.AsType[*pgconn.PgError](err); !ok || pg.Code != "23514" || pg.ConstraintName != c.constraint {
			t.Errorf("%s lý do %q đích %s: mong 23514 %s, có %v", c.kind, c.reason, c.reverses, c.constraint, err)
		}
	}
}

// Sổ và bảng chỉ ghi thêm mới của M3: crm_app không có quyền sửa; chủ bảng bị trigger chặn (append-only).
func TestM3AppendOnly(t *testing.T) {
	t.Parallel()
	e := newCODEnv(t)
	v1 := e.shipment(codV1)
	e.mustPost(expectedTr(v1, "expected:V1", codV1))
	if err := e.execApp("UPDATE cash.cod_ledger_entries SET amount = amount"); pgCode(err) != "42501" {
		t.Errorf("crm_app UPDATE dòng sổ: mong 42501, có %v", err)
	}
	for _, q := range []string{
		"UPDATE cash.cod_ledger_entries SET amount = amount",
		"UPDATE cash.payments SET reference = reference",
		"DELETE FROM cash.reconcile_findings",
	} {
		err := e.staff.TryExec(t.Context(), e.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(t.Context(), q)
			return err //nolint:wrapcheck // test
		})
		if pgCode(err) != "42501" || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("chủ bảng %s: mong 42501 append-only, có %v", q, err)
		}
	}
}
