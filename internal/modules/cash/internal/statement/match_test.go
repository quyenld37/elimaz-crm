package statement_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/cash"
	"elimaz.com.vn/crm/internal/modules/cash/internal/statement"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/files/filestest"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// flowEnv: database thử, tenant A (shiptest: tài khoản hãng giả A của ELIMAZ, A2 của hãng fake_b cùng người bán), module
// cash nối sổ sự kiện có mọi consumer của cash, worker chạy việc sự kiện và lệnh đọc tệp, đồng hồ giả.
type flowEnv struct {
	t       testing.TB
	staff   *testkit.Staff
	tenant  uuid.UUID
	user    uuid.UUID
	shop    *shiptest.Shop
	variant uuid.UUID
	a2      uuid.UUID
	reg     *events.Registry
	mod     *cash.Module
	wk      *queue.Worker
	bus     *realtimetest.RecordingBus
	now     time.Time
}

func newFlowEnv(t testing.TB) *flowEnv {
	t.Helper()
	e := &flowEnv{
		t: t, staff: testkit.OpenStaff(t), bus: &realtimetest.RecordingBus{},
		now: time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC),
	}
	e.tenant = e.staff.Tenant(t, "A")
	e.user = e.staff.User(t, e.tenant, "ketoan@elimaz.test", "active")
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		shop := shiptest.NewShop(t, tx, e.tenant)
		e.shop, e.variant = shop, shop.Variant(t, tx, shop.Seller, 250000, 100000)
		var err error
		e.a2, err = fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
			SellerID: e.shop.Seller, WarehouseID: e.shop.Warehouse, CarrierKey: "fake_b", Name: "Hãng giả B",
			ExternalAccountRef: "fake-b-1",
		})
		return err //nolint:wrapcheck // test
	})
	e.wire(filestest.MemFileStore(t))
	return e
}

// wire dựng module cash (kho tệp fs, đồng hồ giả của e) nối sổ sự kiện có mọi consumer của cash, và worker chạy việc
// sự kiện cùng lệnh của module.
func (e *flowEnv) wire(fs files.FileStore) {
	e.t.Helper()
	e.reg = events.NewRegistry()
	e.mod = cash.New(cash.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Events: e.reg, Bus: e.bus, Files: fs,
		Now: func() time.Time { return e.now },
	})
	for _, c := range e.mod.Consumers() {
		if err := e.reg.Register(c); err != nil {
			e.t.Fatal(err)
		}
	}
	e.wk = queue.NewWorker(e.staff.App, e.reg.JobHandlers(), queue.WorkerOptions{
		WorkerID: "w-" + uuid.NewString(), Commands: commands.NewRunner(e.staff.App, e.mod.CommandHandlers()),
	})
}

func (e *flowEnv) app(fn func(tx pgx.Tx) error) error {
	return tenancy.WithTenant(e.t.Context(), e.staff.App, e.tenant, fn) //nolint:wrapcheck // test
}

func (e *flowEnv) mustApp(fn func(tx pgx.Tx) error) {
	e.t.Helper()
	if err := e.app(fn); err != nil {
		e.t.Fatal(err)
	}
}

// ship mô tả một vận đơn thử: tài khoản (uuid.Nil: A), mã vận đơn, COD, trạng thái (delivered: sổ ghi expected,
// collected; shipping: chỉ expected), giao một phần, lúc giao xong.
type ship struct {
	account     uuid.UUID
	tracking    string
	cod         int64
	status      fulfillment.Status
	partial     bool
	deliveredAt time.Time
}

// shipment tạo đơn, vận đơn thật (crm_app), đặt (chủ bảng) mã vận đơn, tài khoản, COD, trạng thái theo s rồi ghi sổ
// COD theo trạng thái bằng consumer cash.
func (e *flowEnv) shipment(s ship) uuid.UUID {
	e.t.Helper()
	if s.account == uuid.Nil {
		s.account = e.shop.Account
	}
	carrier := shiptest.CarrierKey
	if s.account == e.a2 {
		carrier = "fake_b"
	}
	if s.deliveredAt.IsZero() {
		s.deliveredAt = e.now.Add(-24 * time.Hour)
	}
	var sid uuid.UUID
	e.mustApp(func(tx pgx.Tx) error {
		o, err := e.shop.Order(e.t.Context(), tx, e.shop.Seller, e.user, 0, order.ItemInput{VariantID: e.variant, Qty: 1})
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		sh, err := e.shop.Shipment(e.t.Context(), tx, e.shop.Seller, o.ID, e.user)
		sid = sh.ID
		return err //nolint:wrapcheck // test
	})
	var deliveredAt any = s.deliveredAt
	if s.status != fulfillment.StatusDelivered {
		deliveredAt = nil
	}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.t.Context(), `UPDATE fulfillment.shipments SET tracking_code = $2, carrier_account_id = $3,
			carrier_key = $4, cod_amount = $5, status = $6, partial = $7, delivered_at = $8 WHERE id = $1`,
			sid, s.tracking, s.account, carrier, s.cod, string(s.status), s.partial, deliveredAt)
		return err //nolint:wrapcheck // test
	})
	p, err := json.Marshal(fulfillment.StatusChanged{ShipmentID: sid, Status: s.status, CODAmount: s.cod})
	if err != nil {
		e.t.Fatal(err)
	}
	e.mustApp(func(tx pgx.Tx) error {
		return cash.Consumer{}.Handle(e.t.Context(), events.StoredEvent{
			ID: uuid.New(), TenantID: e.tenant, AggregateType: "shipment", AggregateID: sid.String(), Seq: 2,
			Topic: fulfillment.TopicShipmentStatusChanged, Payload: p,
		}, tx)
	})
	return sid
}

// line là một dòng bảng kê: mã vận đơn, COD, phí, phí khác.
type line struct {
	tracking       string
	cod, fee, othr int64
}

// statement ghi (chủ bảng) bảng kê parsed của tài khoản account (uuid.Nil: A) với các dòng ls, tổng khai declared
// (nil: không có), phát cod_statement.parsed như lệnh đọc tệp rồi chạy hết việc; trả mã bảng kê.
func (e *flowEnv) statement(account uuid.UUID, declared *int64, ls ...line) uuid.UUID {
	e.t.Helper()
	if account == uuid.Nil {
		account = e.shop.Account
	}
	id := uuid.Must(uuid.NewV7())
	var version int64
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(e.t.Context(), `INSERT INTO cash.cod_statements (id, tenant_id, carrier_key,
			carrier_account_id, file_id, file_sha256, format, status, imported_by, line_count, declared_total)
			SELECT $1::uuid, $2, carrier_key, id, gen_random_uuid(), encode(sha256(convert_to($1::uuid::text, 'UTF8')), 'hex'), 'elimaz_v1',
			       'parsed', $4, $5, $6
			FROM fulfillment.carrier_accounts WHERE id = $3
			RETURNING version`, id, e.tenant, account, e.user, len(ls), declared).Scan(&version); err != nil {
			return err //nolint:wrapcheck // test
		}
		for i, l := range ls {
			if _, err := tx.Exec(e.t.Context(), `INSERT INTO cash.cod_statement_lines (tenant_id, statement_id, line_no,
				tracking_code, cod_amount, fee_amount, other_fee, net_amount) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				e.tenant, id, i+1, l.tracking, l.cod, l.fee, l.othr, l.cod-l.fee-l.othr); err != nil {
				return err //nolint:wrapcheck // test
			}
		}
		return nil
	})
	e.mustApp(func(tx pgx.Tx) error {
		_, _, err := e.reg.Emit(e.t.Context(), tx, queue.NewQueue(), events.Event{
			TenantID: e.tenant, AggregateType: "cod_statement", AggregateID: id.String(), Seq: version,
			Topic: cash.TopicStatementParsed, Payload: map[string]any{"statementId": id},
		})
		return err //nolint:wrapcheck // test
	})
	e.drain()
	return id
}

func (e *flowEnv) drain() { e.t.Helper(); e.staff.DrainJobs(e.t, e.wk) }

// orderOf đọc (chủ bảng) mã đơn của vận đơn sid.
func (e *flowEnv) orderOf(sid uuid.UUID) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT order_id FROM fulfillment.shipments WHERE id = $1`, sid).Scan(&id)
	})
	return id
}

// get đọc bảng kê id cùng mọi dòng (≤ 1.000).
func (e *flowEnv) get(id uuid.UUID) cash.StatementDetail {
	e.t.Helper()
	var d cash.StatementDetail
	e.mustApp(func(tx pgx.Tx) error {
		var err error
		d, err = e.mod.Service.GetStatement(e.t.Context(), tx, id, "", 0, 1000)
		return err //nolint:wrapcheck // test
	})
	return d
}

// confirm chốt bảng kê id với version (lấy từ get khi version < 0) như lớp HTTP (một tx), rồi chạy hết việc.
func (e *flowEnv) confirm(id uuid.UUID, version int64) (cash.ConfirmedStatement, error) {
	e.t.Helper()
	if version < 0 {
		version = e.get(id).Statement.Version
	}
	var out cash.ConfirmedStatement
	err := e.app(func(tx pgx.Tx) error {
		var err error
		out, err = e.mod.Service.ConfirmStatement(e.t.Context(), tx, e.tenant, id, version, e.user)
		return err //nolint:wrapcheck // test
	})
	if err == nil {
		e.drain()
	}
	return out, err
}

func (e *flowEnv) mustConfirm(id uuid.UUID) cash.ConfirmedStatement {
	e.t.Helper()
	c, err := e.confirm(id, -1)
	if err != nil {
		e.t.Fatalf("chốt %s: %v", id, err)
	}
	return c
}

// bal là số dư {to_collect, carrier_holding, shop_received, carrier_fee, sales_clearing} của vận đơn id.
func (e *flowEnv) bal(id uuid.UUID) [5]int64 {
	e.t.Helper()
	var b cash.Balances
	e.mustApp(func(tx pgx.Tx) error {
		var err error
		b, err = cash.BalancesFor(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	})
	return [5]int64{b.ToCollect, b.CarrierHolding, b.ShopReceived, b.CarrierFee, b.SalesClearing}
}

func (e *flowEnv) count(q string, args ...any) int {
	e.t.Helper()
	return e.staff.CountIn(e.t, e.tenant, q, args...)
}

// lineOf trả dòng có mã vận đơn tracking của bảng kê d.
func lineOf(t testing.TB, d cash.StatementDetail, tracking string) cash.StatementLine {
	t.Helper()
	for _, l := range d.Lines {
		if l.TrackingCode == tracking {
			return l
		}
	}
	t.Fatalf("bảng kê không có dòng %s", tracking)
	return cash.StatementLine{}
}

func diffOf(l cash.StatementLine) string {
	if l.DiffAmount == nil {
		return "nil"
	}
	return fmt.Sprint(*l.DiffAmount)
}

// discrepancies đọc mọi mục "Lệch đối soát" theo trang page mục (đi qua con trỏ).
func (e *flowEnv) discrepancies() []cash.Discrepancy { return e.discrepanciesBy(3) }

func (e *flowEnv) discrepanciesBy(page int) []cash.Discrepancy {
	e.t.Helper()
	var out []cash.Discrepancy
	var before *uuid.UUID
	for range 10000 {
		var items []cash.Discrepancy
		var next *uuid.UUID
		e.mustApp(func(tx pgx.Tx) error {
			var err error
			items, next, err = e.mod.Service.ListDiscrepancies(e.t.Context(), tx, before, page)
			return err //nolint:wrapcheck // test
		})
		out = append(out, items...)
		if next == nil {
			return out
		}
		before = next
	}
	e.t.Fatal("con trỏ lệch đối soát không dừng")
	return nil
}

// TI02, TI06, fee_only, kết quả khớp (xem trước): mỗi dòng đúng loại, vận đơn, số dư kỳ vọng, chênh; bảng kê matched,
// có báo hiệu; khớp chưa ghi bút toán nào.
func TestMatch_Classifies(t *testing.T) {
	t.Parallel()
	e := newFlowEnv(t)
	v1 := e.shipment(ship{tracking: "FK0001", cod: 630000, status: fulfillment.StatusDelivered})
	v2 := e.shipment(ship{tracking: "FK0002", cod: 450000, status: fulfillment.StatusDelivered})
	v3 := e.shipment(ship{tracking: "FK0003", cod: 300000, status: fulfillment.StatusShipping})
	v4 := e.shipment(ship{tracking: "FK0004", cod: 200000, status: fulfillment.StatusDelivered, partial: true})
	v5 := e.shipment(ship{tracking: "FK0005", cod: 0, status: fulfillment.StatusDelivered})
	before := e.count("SELECT count(*) FROM cash.cod_transfers")
	id := e.statement(uuid.Nil, nil,
		line{"FK0001", 630000, 32000, 0}, line{"FK0002", 420000, 30000, 0}, line{"FK0003", 300000, 20000, 0},
		line{"FK0004", 200000, 20000, 0}, line{"FK0005", 0, 25000, 0}, line{"NHANH9999", 450000, 30000, 0})
	d := e.get(id)
	if d.Statement.Status != statement.StatusMatched {
		t.Fatalf("bảng kê %s, mong matched", d.Statement.Status)
	}
	for _, c := range []struct {
		tracking, status string
		shipment         *uuid.UUID
		expected         int64
		diff             string
	}{
		{"FK0001", statement.MatchMatched, &v1, 630000, "0"},
		{"FK0002", statement.MatchAmountMismatch, &v2, 450000, "-30000"},
		{"FK0003", statement.MatchAmountMismatch, &v3, 0, "300000"},
		{"FK0004", statement.MatchAmountMismatch, &v4, 200000, "0"},
		{"FK0005", statement.MatchFeeOnly, &v5, 0, "0"},
		{"NHANH9999", statement.MatchExternal, nil, 0, "nil"},
	} {
		l := lineOf(t, d, c.tracking)
		if l.MatchStatus != c.status || diffOf(l) != c.diff {
			t.Errorf("%s: %s chênh %s, mong %s chênh %s", c.tracking, l.MatchStatus, diffOf(l), c.status, c.diff)
		}
		switch {
		case c.shipment == nil && (l.ShipmentID != nil || l.ExpectedAmount != nil):
			t.Errorf("%s: ngoài hệ thống mà có vận đơn %v, kỳ vọng %v", c.tracking, l.ShipmentID, l.ExpectedAmount)
		case c.shipment != nil && (l.ShipmentID == nil || *l.ShipmentID != *c.shipment || l.ExpectedAmount == nil ||
			*l.ExpectedAmount != c.expected):
			t.Errorf("%s: vận đơn %v kỳ vọng %v, mong %s %d", c.tracking, l.ShipmentID, l.ExpectedAmount, c.shipment, c.expected)
		}
	}
	if n := e.count("SELECT count(*) FROM cash.cod_transfers"); n != before {
		t.Errorf("khớp đã ghi %d bút toán (chỉ chốt mới ghi)", n-before)
	}
	// Task 137: dòng có vận đơn mang mã, số đơn (cột "Đơn" của màn); ngoài hệ thống không có. Tổng theo nhóm có tổng phí
	// (phí + phí khác) và tổng phần hãng trả thiếu (chênh âm) của mọi dòng nhóm.
	if l := lineOf(t, d, "FK0001"); l.OrderID == nil || *l.OrderID != e.orderOf(v1) || !strings.HasPrefix(l.OrderCode, "DH") {
		t.Errorf("FK0001: đơn %v %q, mong %s DH…", l.OrderID, l.OrderCode, e.orderOf(v1))
	}
	if l := lineOf(t, d, "NHANH9999"); l.OrderID != nil || l.OrderCode != "" {
		t.Errorf("NHANH9999: đơn %v %q, mong không có", l.OrderID, l.OrderCode)
	}
	wantTotals := "[{amount_mismatch 3 920000 70000 30000} {external 1 450000 30000 0} {fee_only 1 0 25000 0} " +
		"{matched 1 630000 32000 0}]"
	if got := fmt.Sprint(d.Totals); got != wantTotals {
		t.Errorf("tổng %s, mong %s", got, wantTotals)
	}
	if !hasSignal(e.bus, cash.SignalStatementChanged, id) {
		t.Error("không báo hiệu bảng kê đổi")
	}
}

// VC13: tài khoản A2 (hãng fake_b) có vận đơn X123; bảng kê của A có X123 → external, không khớp nhầm; bảng kê của A2
// khớp đúng vận đơn đó.
func TestMatch_ByCarrierAccount(t *testing.T) {
	t.Parallel()
	e := newFlowEnv(t)
	x := e.shipment(ship{account: e.a2, tracking: "X123", cod: 150000, status: fulfillment.StatusDelivered})
	a := e.get(e.statement(uuid.Nil, nil, line{"X123", 150000, 10000, 0}))
	if l := lineOf(t, a, "X123"); l.MatchStatus != statement.MatchExternal || l.ShipmentID != nil {
		t.Errorf("bảng kê A: %s %v, mong external không vận đơn", l.MatchStatus, l.ShipmentID)
	}
	b := e.get(e.statement(e.a2, nil, line{"X123", 150000, 10000, 0}))
	if l := lineOf(t, b, "X123"); l.MatchStatus != statement.MatchMatched || l.ShipmentID == nil || *l.ShipmentID != x {
		t.Errorf("bảng kê A2: %s %v, mong matched %s", l.MatchStatus, l.ShipmentID, x)
	}
}

// Sự kiện đã đọc xong giao lại sau khi đã khớp (hay đã chốt): không khớp lại, không đổi version.
func TestMatch_RedeliveredIsNoop(t *testing.T) {
	t.Parallel()
	e := newFlowEnv(t)
	e.shipment(ship{tracking: "FK0001", cod: 630000, status: fulfillment.StatusDelivered})
	id := e.statement(uuid.Nil, nil, line{"FK0001", 630000, 32000, 0})
	v := e.get(id).Statement.Version
	h := e.mod.Consumers()
	var c events.Consumer
	for _, x := range h {
		if x.Name() == cash.StatementsConsumerName {
			c = x
		}
	}
	if c == nil {
		t.Fatal("không có consumer cash_statements")
	}
	p, _ := json.Marshal(map[string]any{"statementId": id}) //nolint:errchkjson // test
	e.mustApp(func(tx pgx.Tx) error {
		return c.Handle(t.Context(), events.StoredEvent{
			ID: uuid.New(), TenantID: e.tenant, AggregateType: "cod_statement", AggregateID: id.String(), Seq: 99,
			Topic: cash.TopicStatementParsed, Payload: p,
		}, tx)
	})
	if got := e.get(id).Statement; got.Version != v || got.Status != statement.StatusMatched {
		t.Errorf("sau giao lại: %s v%d, mong matched v%d", got.Status, got.Version, v)
	}
	bad := []byte(`{"statementId":"x"}`)
	err := e.app(func(tx pgx.Tx) error {
		return c.Handle(t.Context(), events.StoredEvent{
			ID: uuid.New(), TenantID: e.tenant, Topic: cash.TopicStatementParsed,
			Payload: bad,
		}, tx)
	})
	if err == nil || !strings.Contains(err.Error(), "payload") {
		t.Errorf("payload hỏng: %v, mong lỗi vĩnh viễn", err)
	}
}

// Đường thật: tệp elimaz_v1 tải lên, lệnh đọc tệp xong → sự kiện đã đọc → consumer khớp → matched.
func TestMatch_AfterRealParse(t *testing.T) {
	t.Parallel()
	ie := newImportEnv(t)
	e := &flowEnv{
		t: t, staff: ie.staff, tenant: ie.tenant, user: ie.user, shop: ie.shop, bus: &realtimetest.RecordingBus{},
		now: time.Now(),
	}
	e.wire(ie.store)
	ie.mod = e.mod
	st, err := ie.importFile(ie.upload("bk.csv", csvType, testdata(t, "elimaz_v1_ba_dong.csv")), ie.shop.Account, "")
	if err != nil {
		t.Fatal(err)
	}
	e.drain()
	d := e.get(st.ID)
	if d.Statement.Status != statement.StatusMatched || len(d.Lines) != 3 {
		t.Fatalf("bảng kê %s %d dòng, mong matched 3 dòng", d.Statement.Status, len(d.Lines))
	}
	for _, l := range d.Lines {
		if l.MatchStatus != statement.MatchExternal { // tenant thử chưa có vận đơn nào mang mã này
			t.Errorf("dòng %s: %s, mong external", l.TrackingCode, l.MatchStatus)
		}
	}
}

// Đồng hồ giả: vận đơn giao 15 ngày trước chưa có tiền → trong "Quá hạn chưa có tiền" (số ngày 15, mã đơn, hãng, mã
// vận đơn); 13 ngày → không; đã trả đủ → không; đang giao → không.
func TestListOverdue(t *testing.T) {
	t.Parallel()
	e := newFlowEnv(t)
	old := e.shipment(ship{
		tracking: "OD15", cod: 450000, status: fulfillment.StatusDelivered,
		deliveredAt: e.now.Add(-15 * 24 * time.Hour),
	})
	e.shipment(ship{
		tracking: "OD13", cod: 450000, status: fulfillment.StatusDelivered,
		deliveredAt: e.now.Add(-13 * 24 * time.Hour),
	})
	paid := e.shipment(ship{
		tracking: "OD20", cod: 100000, status: fulfillment.StatusDelivered,
		deliveredAt: e.now.Add(-20 * 24 * time.Hour),
	})
	e.shipment(ship{tracking: "OD30", cod: 100000, status: fulfillment.StatusShipping})
	e.mustConfirm(e.statement(uuid.Nil, nil, line{"OD20", 100000, 10000, 0}))
	if b := e.bal(paid); b[1] != 0 {
		t.Fatalf("OD20 chưa trả đủ: %v", b)
	}
	var items []cash.Overdue
	e.mustApp(func(tx pgx.Tx) error {
		var err error
		items, _, err = e.mod.Service.ListOverdue(t.Context(), tx, nil, 50)
		return err //nolint:wrapcheck // test
	})
	if len(items) != 1 {
		t.Fatalf("quá hạn %d vận đơn %+v, mong 1 (OD15)", len(items), items)
	}
	it := items[0]
	if it.ShipmentID != old || it.TrackingCode != "OD15" || it.Days != 15 || it.CarrierHolding != 450000 ||
		it.CarrierKey != shiptest.CarrierKey || !strings.HasPrefix(it.OrderCode, "DH") {
		t.Errorf("quá hạn %+v", it)
	}
}

// TI07/AT30: hai kế toán chốt cùng bảng kê cùng version → đúng một thành công, một ErrStatementChanged; số bút toán
// remitted = số dòng khớp (không gấp đôi). Bảng kê không có → ErrStatementNotFound; chưa khớp xong → ErrStatementChanged.
func TestConfirm_Concurrent(t *testing.T) {
	t.Parallel()
	e := newFlowEnv(t)
	e.shipment(ship{tracking: "FK0001", cod: 630000, status: fulfillment.StatusDelivered})
	e.shipment(ship{tracking: "FK0002", cod: 450000, status: fulfillment.StatusDelivered})
	id := e.statement(uuid.Nil, nil, line{"FK0001", 630000, 32000, 0}, line{"FK0002", 450000, 30000, 0},
		line{"NHANH9999", 1000, 0, 0})
	v := e.get(id).Statement.Version
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() {
			errs[i] = e.app(func(tx pgx.Tx) error {
				_, err := e.mod.Service.ConfirmStatement(t.Context(), tx, e.tenant, id, v, e.user)
				return err //nolint:wrapcheck // test
			})
		})
	}
	wg.Wait()
	ok, changed := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, cash.ErrStatementChanged):
			changed++
		default:
			t.Errorf("chốt: %v", err)
		}
	}
	if ok != 1 || changed != 1 {
		t.Fatalf("%d thành công, %d bị chặn; mong 1 và 1", ok, changed)
	}
	e.drain()
	if n := e.count("SELECT count(*) FROM cash.cod_transfers WHERE kind = 'remitted' AND statement_line_id IN "+
		"(SELECT id FROM cash.cod_statement_lines WHERE statement_id = $1)", id); n != 2 {
		t.Errorf("%d bút toán remitted, mong 2", n)
	}
	if _, err := e.confirm(id, -1); !errors.Is(err, cash.ErrStatementChanged) {
		t.Errorf("chốt lại bảng kê đã chốt: %v, mong ErrStatementChanged", err)
	}
	if _, err := e.confirm(uuid.Must(uuid.NewV7()), 1); !errors.Is(err, cash.ErrStatementNotFound) {
		t.Errorf("bảng kê không có: %v, mong ErrStatementNotFound", err)
	}
}

// hasSignal báo bus đã nhận báo hiệu topic cho bảng kê id.
func hasSignal(bus *realtimetest.RecordingBus, topic string, id uuid.UUID) bool {
	for _, s := range bus.Signals() {
		if s.Topic == topic && s.Entity.ID == id.String() {
			return true
		}
	}
	return false
}
