package fulfillment_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/files"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// labelCarrier là hãng giả trong bộ nhớ có in nhãn: body trả thân nhãn cho các mã hãng (nil: PDF có các mã); err
// khác nil thì Labels trả lỗi đó. Ghi các lần gọi.
type labelCarrier struct {
	*fake.Carrier
	batch bool
	mu    sync.Mutex
	body  func(codes []string) []byte
	err   error
	calls [][]string
}

func (c *labelCarrier) Capabilities() fulfillment.Capabilities {
	return fulfillment.Capabilities{Create: true, LookupByRef: true, Labels: true, LabelsBatch: c.batch}
}

func (c *labelCarrier) Labels(_ context.Context, _ fulfillment.Account, codes []string) (fulfillment.Label, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, slices.Clone(codes))
	if c.err != nil {
		return fulfillment.Label{}, c.err
	}
	if c.body != nil {
		return fulfillment.Label{PDF: c.body(codes)}, nil
	}
	return fulfillment.Label{PDF: []byte("%PDF-1.4\n" + strings.Join(codes, "\n")), Pages: len(codes)}, nil
}

func (c *labelCarrier) set(body func([]string) []byte, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.body, c.err = body, err
}

func (c *labelCarrier) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

// labelEnv: shipEnv cộng Service có hãng in nhãn và kho tệp trong bộ nhớ, worker chạy lệnh tạo, tra và lệnh nhãn.
type labelEnv struct {
	*shipEnv
	lc    *labelCarrier
	store *files.MemStore
	lsvc  *fulfillment.Service
	cmd   *fulfillment.LabelsCommand
}

func newLabelEnv(t *testing.T, batch bool) *labelEnv {
	t.Helper()
	fulfillment.RegisterFilePurposes()
	e := &labelEnv{shipEnv: newShipEnv(t), store: files.NewMemStore()}
	e.lc = &labelCarrier{Carrier: e.carrier, batch: batch}
	e.lsvc = fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(), Bus: &realtimetest.RecordingBus{},
		Carriers: mustRegistry(t, e.lc), Files: e.store,
	}).Service
	e.cmd = fulfillment.NewLabelsCommand(e.lsvc)
	e.useCommands(append(slices.Clone(e.cmds), e.cmd))
	return e
}

// shippedOf tạo đơn 1 cái và vận đơn đã có mã hãng của người bán seller.
func (e *labelEnv) shippedOf(seller uuid.UUID) (order.Order, fulfillment.Shipment) {
	e.t.Helper()
	se := *e.shipEnv
	se.seller, se.account = seller, e.shop.AccountOf(seller)
	o, _, s := se.shipped(1)
	if s.TrackingCode == "" {
		e.t.Fatalf("vận đơn %s chưa có mã hãng", s.ID)
	}
	return o, s
}

// shippedN tạo n đơn 1 cái có vận đơn đã có mã hãng của người bán chính; trả mã vận đơn.
func (e *labelEnv) shippedN(n int) []uuid.UUID {
	e.t.Helper()
	ids := make([]uuid.UUID, 0, n)
	for range n {
		_, s := e.shippedOf(e.shop.Seller)
		ids = append(ids, s.ID)
	}
	return ids
}

// manualShipment chèn (chủ bảng) vận đơn nhập tay có mã hãng cho một đơn mới của người bán chính.
func (e *labelEnv) manualShipment() uuid.UUID {
	e.t.Helper()
	o, _ := e.order(1)
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `INSERT INTO fulfillment.shipments (tenant_id, code, order_id, carrier_key,
			carrier_account_id, tracking_code, status, cod_amount, seller_id, mode, create_state)
			VALUES ($1, 'E261121MAN001', $2, 'fake', $3, 'TAY-1', 'awaiting_pickup', 0, $4, 'manual', 'manual')
			RETURNING id`, e.tenant, o.ID, e.account, e.seller).Scan(&id)
	})
	return id
}

func allowAll(uuid.UUID) error { return nil }

func (e *labelEnv) create(ids []uuid.UUID, auth fulfillment.Authorize) (fulfillment.LabelBatchCreated, error) {
	var out fulfillment.LabelBatchCreated
	err := e.app(func(tx pgx.Tx) (err error) {
		out, err = e.lsvc.CreateLabelBatch(e.t.Context(), tx, ids, e.actor, auth)
		return err //nolint:wrapcheck // test
	})
	return out, err
}

func (e *labelEnv) mustCreate(ids ...uuid.UUID) fulfillment.LabelBatchCreated {
	e.t.Helper()
	out, err := e.create(ids, allowAll)
	if err != nil {
		e.t.Fatalf("CreateLabelBatch: %v", err)
	}
	return out
}

func (e *labelEnv) batch(id uuid.UUID) fulfillment.LabelBatchView {
	e.t.Helper()
	return readAs(e.shipEnv, func(ctx context.Context, tx pgx.Tx) (fulfillment.LabelBatchView, error) {
		return e.lsvc.LabelBatch(ctx, tx, id, allowAll)
	})
}

// labelState là phần của vận đơn và đơn mà in không được đổi (AT08).
type labelState struct {
	status, packing           string
	shipVersion, orderVersion int64
	labelFile                 *uuid.UUID
	printed                   bool
}

func (e *labelEnv) state(shipmentID uuid.UUID) labelState {
	e.t.Helper()
	var s labelState
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT s.status, o.packing_status, s.version, o.version, s.label_file_id,
			s.label_printed_at IS NOT NULL FROM fulfillment.shipments s JOIN ord.orders o ON o.id = s.order_id
			WHERE s.id = $1`, shipmentID).Scan(&s.status, &s.packing, &s.shipVersion, &s.orderVersion, &s.labelFile, &s.printed)
	})
	return s
}

// content đọc tệp nhãn của lô id trên kho tệp trong bộ nhớ (khóa priv/<tenant>/<lô>).
func (e *labelEnv) content(batchID uuid.UUID) []byte {
	e.t.Helper()
	v := e.store.Versions(files.BucketPrivate, files.ServerKey(e.tenant, batchID))
	if len(v) == 0 {
		e.t.Fatalf("không có tệp nhãn của lô %s", batchID)
	}
	return v[len(v)-1]
}

// VC04: 3 vận đơn của tài khoản A (fake-1), 1 của B (fake-2), 1 nhập tay → 2 lô (A: 3, B: 1) + 1 bỏ qua
// no_carrier_label; sau worker hai lô ready, có URL, tệp bắt đầu bằng %PDF- và có đủ mã; vận đơn có tệp nhãn và
// label_printed_at; hãng được gọi một lần mỗi lô.
func TestLabels_BatchPerAccount(t *testing.T) {
	e := newLabelEnv(t, true)
	a := e.shippedN(3)
	_, sb := e.shippedOf(e.shop.Partner)
	man := e.manualShipment()

	out := e.mustCreate(a[2], sb.ID, man, a[0], a[1])
	if len(out.Batches) != 2 || len(out.Skipped) != 1 {
		t.Fatalf("kết quả = %+v, muốn 2 lô 1 bỏ qua", out)
	}
	e.checkCreated(out, man)
	e.drain()
	e.checkReady(out)
	if n := e.lc.callCount(); n != 2 {
		t.Fatalf("hãng được gọi in %d lần, muốn 2 (mỗi lô một lần)", n)
	}
	for _, id := range append(slices.Clone(a), sb.ID) {
		if st := e.state(id); st.labelFile == nil || !st.printed {
			t.Fatalf("vận đơn %s sau in = %+v, muốn có tệp nhãn và label_printed_at", id, st)
		}
	}
	if st := e.state(man); st.labelFile != nil || st.printed {
		t.Fatalf("vận đơn nhập tay bị gắn nhãn: %+v", st)
	}
	pdfA := e.content(batchOf(t, out, e.shop.Account))
	for _, id := range a {
		if !bytes.Contains(pdfA, []byte(e.shipment(id).TrackingCode)) {
			t.Fatalf("nhãn lô A thiếu mã hãng của %s", id)
		}
	}
}

// checkCreated kiểm kết quả tạo lô của TestLabels_BatchPerAccount: lô pending không URL, A 3, B 1, vận đơn tay bỏ qua.
func (e *labelEnv) checkCreated(out fulfillment.LabelBatchCreated, man uuid.UUID) {
	e.t.Helper()
	counts := map[uuid.UUID]int{}
	for _, b := range out.Batches {
		counts[b.CarrierAccountID] = b.Count
		if v := e.batch(b.ID); v.Status != "pending" || v.URL != "" {
			e.t.Fatalf("lô vừa tạo = %+v, muốn pending không URL", v)
		}
	}
	if counts[e.shop.Account] != 3 || counts[e.shop.PartnerAccount] != 1 {
		e.t.Fatalf("số vận đơn mỗi lô = %v, muốn A 3, B 1", counts)
	}
	if out.Skipped[0] != (fulfillment.LabelSkip{ShipmentID: man, Reason: "no_carrier_label"}) {
		e.t.Fatalf("bỏ qua = %+v", out.Skipped)
	}
}

// checkReady kiểm mọi lô của out đã ready, có URL, đúng số vận đơn, tệp bắt đầu bằng %PDF-.
func (e *labelEnv) checkReady(out fulfillment.LabelBatchCreated) {
	e.t.Helper()
	for _, b := range out.Batches {
		v := e.batch(b.ID)
		if v.Status != "ready" || v.URL == "" || v.ErrorCode != "" || v.Count != b.Count {
			e.t.Fatalf("lô sau worker = %+v, muốn ready có URL", v)
		}
		if pdf := e.content(b.ID); !bytes.HasPrefix(pdf, []byte("%PDF-")) {
			e.t.Fatalf("tệp nhãn không phải PDF: %q", pdf[:min(len(pdf), 20)])
		}
	}
}

func batchOf(t *testing.T, out fulfillment.LabelBatchCreated, account uuid.UUID) uuid.UUID {
	t.Helper()
	for _, b := range out.Batches {
		if b.CarrierAccountID == account {
			return b.ID
		}
	}
	t.Fatalf("không có lô của tài khoản %s", account)
	return uuid.Nil
}

// AT08: in lại cùng 3 vận đơn (đơn đã packed) → lô mới, trạng thái vận đơn, version, packing_status không đổi (đã
// packed vẫn packed).
func TestLabels_ReprintKeepsState(t *testing.T) {
	e := newLabelEnv(t, true)
	var ids, orders []uuid.UUID
	for range 3 {
		o, s := e.shippedOf(e.shop.Seller)
		ids, orders = append(ids, s.ID), append(orders, o.ID)
	}
	first := e.mustCreate(ids...)
	e.drain()
	w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &realtimetest.RecordingBus{}}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := w.SetPackingStatus(e.t.Context(), tx, e.seller, orders, order.PackingPacked,
			order.Actor{TenantID: e.tenant, UserID: e.actor.UserID, Type: "staff"}, time.Now().UTC())
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("đóng gói: %v", err)
	}
	before := map[uuid.UUID]labelState{}
	for _, id := range ids {
		before[id] = e.state(id)
	}
	again := e.mustCreate(ids...)
	e.drain()
	if len(again.Batches) != 1 || again.Batches[0].ID == first.Batches[0].ID {
		t.Fatalf("in lại = %+v, muốn một lô mới", again)
	}
	if v := e.batch(again.Batches[0].ID); v.Status != "ready" {
		t.Fatalf("lô in lại = %+v", v)
	}
	for _, id := range ids {
		got, was := e.state(id), before[id]
		if got.status != was.status || got.packing != order.PackingPacked || got.shipVersion != was.shipVersion ||
			got.orderVersion != was.orderVersion {
			t.Fatalf("in lại đổi vận đơn/đơn: trước %+v, sau %+v", was, got)
		}
		if got.labelFile == nil || was.labelFile == nil || *got.labelFile == *was.labelFile {
			t.Fatalf("vận đơn %s phải trỏ tệp nhãn của lô mới", id)
		}
	}
}

// Hãng trả thân không phải PDF → lô failed invalid_pdf; 21 MB → too_large; hãng từ chối → carrier_rejected. Vận đơn
// không được gắn nhãn, không có tệp trên kho.
func TestLabels_BadFiles(t *testing.T) {
	e := newLabelEnv(t, true)
	big := append([]byte("%PDF-1.7\n"), make([]byte, 21<<20)...)
	for _, c := range []struct {
		name string
		body func([]string) []byte
		err  error
		want string
	}{
		{"html", func([]string) []byte { return []byte("<html>502</html>") }, nil, "invalid_pdf"},
		{"21MB", func([]string) []byte { return big }, nil, "too_large"},
		{"từ chối", nil, &fulfillment.CarrierError{Class: fulfillment.ClassRejected, Code: "bad"}, "carrier_rejected"},
	} {
		_, s := e.shippedOf(e.shop.Seller)
		e.lc.set(c.body, c.err)
		out := e.mustCreate(s.ID)
		e.drain()
		v := e.batch(out.Batches[0].ID)
		if v.Status != "failed" || v.ErrorCode != c.want || v.URL != "" {
			t.Fatalf("%s: lô = %+v, muốn failed %s", c.name, v, c.want)
		}
		if st := e.state(s.ID); st.labelFile != nil || st.printed {
			t.Fatalf("%s: vận đơn bị gắn nhãn %+v", c.name, st)
		}
		if n := len(e.store.Versions(files.BucketPrivate, files.ServerKey(e.tenant, out.Batches[0].ID))); n != 0 {
			t.Fatalf("%s: có %d tệp trên kho, muốn 0", c.name, n)
		}
	}
}

// Hãng quá tải (429) → lệnh chưa gửi, lô vẫn pending; hết lượt (dead) → OnTerminal failed timeout.
func TestLabels_RetryThenTerminal(t *testing.T) {
	e := newLabelEnv(t, true)
	_, s := e.shippedOf(e.shop.Seller)
	e.lc.set(nil, &fulfillment.CarrierError{Class: fulfillment.ClassRateLimited, Code: "429"})
	out := e.mustCreate(s.ID)
	e.drain()
	if v := e.batch(out.Batches[0].ID); v.Status != "pending" {
		t.Fatalf("lô khi hãng 429 = %+v, muốn pending (thử lại)", v)
	}
	j := e.labelJob(out.Batches[0].ID)
	if err := e.app(func(tx pgx.Tx) error {
		return e.cmd.OnTerminal(e.t.Context(), j, queue.StatusDead, tx)
	}); err != nil {
		t.Fatal(err)
	}
	if v := e.batch(out.Batches[0].ID); v.Status != "failed" || v.ErrorCode != "timeout" {
		t.Fatalf("lô sau dead = %+v, muốn failed timeout", v)
	}
	if err := e.app(func(tx pgx.Tx) error {
		return e.cmd.OnTerminal(e.t.Context(), j, queue.StatusInDoubt, tx)
	}); err != nil {
		t.Fatalf("in_doubt: %v", err)
	}
}

// labelJob đọc việc nhãn của lô batchID trong hàng đợi.
func (e *labelEnv) labelJob(batchID uuid.UUID) queue.Job {
	e.t.Helper()
	j := queue.Job{TenantID: &e.tenant, Kind: fulfillment.LabelsKind().Name, Attempts: 1}
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT id, payload FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2`,
			j.Kind, batchID.String()).Scan(&j.ID, &j.Payload)
	})
	return j
}

// recTx ghi lại câu SQL chạy qua tx.
type recTx struct {
	pgx.Tx
	mu  *sync.Mutex
	log *[]string
}

func (r recTx) add(sql string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.log = append(*r.log, sql)
}

func (r recTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	r.add(sql)
	return r.Tx.Exec(ctx, sql, args...) //nolint:wrapcheck // test
}

//nolint:ireturn // chữ ký của pgx.Tx
func (r recTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	r.add(sql)
	return r.Tx.Query(ctx, sql, args...) //nolint:wrapcheck // test
}

//nolint:ireturn // chữ ký của pgx.Tx
func (r recTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	r.add(sql)
	return r.Tx.QueryRow(ctx, sql, args...)
}

// indexOf trả vị trí câu đầu tiên trong log có mọi đoạn parts (-1 nếu không có).
func indexOf(log []string, parts ...string) int {
	return slices.IndexFunc(log, func(s string) bool {
		for _, p := range parts {
			if !strings.Contains(s, p) {
				return false
			}
		}
		return true
	})
}

// OnDone khóa shipments theo id tăng dần (một câu FOR UPDATE) trước khi ghi platform.files và label_batches (ADR-46
// nhóm 8 rồi 12b); label_printed_at có giá trị.
func TestLabels_OnDoneLockOrder(t *testing.T) {
	e := newLabelEnv(t, true)
	ids := e.shippedN(2)
	out := e.mustCreate(ids...)
	j := e.labelJob(out.Batches[0].ID)
	res, err := e.cmd.Execute(e.t.Context(), j)
	if err != nil || res.Status != commands.StatusDone {
		t.Fatalf("Execute = %+v, %v", res, err)
	}
	var log []string
	if err := e.app(func(tx pgx.Tx) error {
		log = nil
		return e.cmd.OnDone(e.t.Context(), j, res.Result, recTx{Tx: tx, mu: &sync.Mutex{}, log: &log})
	}); err != nil {
		t.Fatal(err)
	}
	lock := indexOf(log, "fulfillment.shipments", "ORDER BY id", "FOR UPDATE")
	file := indexOf(log, "INSERT INTO platform.files")
	setShip := indexOf(log, "UPDATE fulfillment.shipments")
	batch := indexOf(log, "UPDATE fulfillment.label_batches")
	if lock < 0 || file < 0 || setShip < 0 || batch < 0 || lock >= file || lock >= setShip || lock >= batch {
		t.Fatalf("thứ tự câu: khóa vận đơn %d, tệp %d, gắn nhãn %d, lô %d\n%s", lock, file, setShip, batch,
			strings.Join(log, "\n---\n"))
	}
	if i := indexOf(log, "label_batches", "FOR UPDATE"); i >= 0 {
		t.Fatalf("OnDone khóa label_batches (câu %d), muốn chỉ câu có điều kiện sau shipments", i)
	}
	for _, id := range ids {
		if st := e.state(id); !st.printed {
			t.Fatalf("vận đơn %s chưa có label_printed_at", id)
		}
	}
}

// Hai lô chung ba vận đơn, thứ tự mảng ngược nhau, OnDone chạy song song nhiều vòng: không deadlock (mỗi tx chạy đúng
// một lần, không lỗi), cả hai lô ready.
func TestLabels_ConcurrentBatchesNoDeadlock(t *testing.T) {
	e := newLabelEnv(t, true)
	ids := e.shippedN(3)
	rev := slices.Clone(ids)
	slices.Reverse(rev)
	for round := range 8 {
		e.concurrentRound(round, ids, rev)
	}
}

// concurrentRound tạo hai lô (thứ tự mảng a, b), chạy Execute rồi OnDone của hai lô song song; mỗi tx phải chạy đúng
// một lần không lỗi (deadlock sẽ là lỗi 40P01 hay một lần chạy lại), cả hai lô ready.
func (e *labelEnv) concurrentRound(round int, a, b []uuid.UUID) {
	e.t.Helper()
	var jobs [2]queue.Job
	var results [2]json.RawMessage
	var batches [2]uuid.UUID
	for i, order := range [][]uuid.UUID{a, b} {
		batches[i] = e.insertBatch(order)
		jobs[i] = queue.Job{
			ID: uuid.New(), TenantID: &e.tenant, Kind: fulfillment.LabelsKind().Name, Attempts: 1,
			Payload: json.RawMessage(fmt.Sprintf(`{"batchId":%q}`, batches[i])),
		}
		res, err := e.cmd.Execute(e.t.Context(), jobs[i])
		if err != nil || res.Status != commands.StatusDone {
			e.t.Fatalf("vòng %d Execute = %+v, %v", round, res, err)
		}
		results[i] = res.Result
	}
	var wg sync.WaitGroup
	var runs [2]int
	var errs [2]error
	start := make(chan struct{})
	for i := range 2 {
		wg.Go(func() {
			<-start
			errs[i] = e.app(func(tx pgx.Tx) error {
				runs[i]++
				return e.cmd.OnDone(e.t.Context(), jobs[i], results[i], tx)
			})
		})
	}
	close(start)
	wg.Wait()
	for i := range 2 {
		if errs[i] != nil || runs[i] != 1 {
			e.t.Fatalf("vòng %d lô %d: lỗi %v, chạy %d lần (deadlock?)", round, i, errs[i], runs[i])
		}
		if v := e.batch(batches[i]); v.Status != "ready" {
			e.t.Fatalf("vòng %d lô %d = %+v", round, i, v)
		}
	}
}

// insertBatch chèn (chủ bảng) lô pending của tài khoản chính với đúng thứ tự mảng ids.
func (e *labelEnv) insertBatch(ids []uuid.UUID) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `INSERT INTO fulfillment.label_batches (tenant_id, carrier_account_id,
			shipment_ids, status, created_by) VALUES ($1, $2, $3, 'pending', $4) RETURNING id`,
			e.tenant, e.account, ids, e.actor.UserID).Scan(&id)
	})
	return id
}

// Hãng không in nhiều vận đơn một lần (Q-18) → mỗi vận đơn một lô, mỗi lô một lần gọi một mã.
func TestLabels_SinglePerShipmentWhenNoBatch(t *testing.T) {
	e := newLabelEnv(t, false)
	ids := e.shippedN(2)
	out := e.mustCreate(ids...)
	if len(out.Batches) != 2 || out.Batches[0].Count != 1 || out.Batches[1].Count != 1 {
		t.Fatalf("lô = %+v, muốn hai lô một vận đơn", out.Batches)
	}
	e.drain()
	e.lc.mu.Lock()
	defer e.lc.mu.Unlock()
	if len(e.lc.calls) != 2 || len(e.lc.calls[0]) != 1 || len(e.lc.calls[1]) != 1 {
		t.Fatalf("lần gọi hãng = %v, muốn hai lần một mã", e.lc.calls)
	}
}

// Phạm vi: một vận đơn ngoài phạm vi (Authorize từ chối kho) → cả lô ErrNotFound, không ghi lô hay việc nào; mã lạ →
// ErrNotFound; 0 hay 201 mã → ErrLabelBatchInvalid; đọc lô ngoài phạm vi → ErrNotFound.
func TestLabels_ScopeAndInput(t *testing.T) {
	e := newLabelEnv(t, true)
	_, s := e.shippedOf(e.shop.Seller)
	deny := func(uuid.UUID) error { return fulfillment.ErrNotFound }
	if _, err := e.create([]uuid.UUID{s.ID}, deny); !errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("ngoài phạm vi: %v, muốn ErrNotFound", err)
	}
	if _, err := e.create([]uuid.UUID{s.ID, uuid.New()}, allowAll); !errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("mã lạ: %v, muốn ErrNotFound", err)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.label_batches"); n != 0 {
		t.Fatalf("có %d lô sau các lần bị từ chối", n)
	}
	many := make([]uuid.UUID, 201)
	for i := range many {
		many[i] = uuid.New()
	}
	for _, ids := range [][]uuid.UUID{nil, many} {
		if _, err := e.create(ids, allowAll); !errors.Is(err, fulfillment.ErrLabelBatchInvalid) {
			t.Fatalf("%d mã: %v, muốn ErrLabelBatchInvalid", len(ids), err)
		}
	}
	out := e.mustCreate(s.ID)
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.lsvc.LabelBatch(e.t.Context(), tx, out.Batches[0].ID, deny)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("đọc lô ngoài phạm vi: %v", err)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.lsvc.LabelBatch(e.t.Context(), tx, uuid.New(), allowAll)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("lô lạ: %v", err)
	}
}

// Lệnh tải nhãn chỉ được đăng ký khi module có database, kho tệp và adapter hãng (wire_m3 nối Files, Task M3-16).
func TestLabels_CommandWiring(t *testing.T) {
	e := newLabelEnv(t, true)
	has := func(d fulfillment.Deps) bool {
		m := fulfillment.New(d)
		if err := m.Check(); err != nil {
			t.Fatalf("Check: %v", err)
		}
		return slices.ContainsFunc(m.CommandHandlers(), func(h commands.Handler) bool {
			return h.Kind().Name == fulfillment.LabelsKindName
		})
	}
	full := fulfillment.Deps{DB: e.staff.App, Queue: queue.NewQueue(), Carriers: mustRegistry(t, e.lc), Files: e.store}
	noFiles := full
	noFiles.Files = nil
	if !has(full) || has(noFiles) {
		t.Fatalf("đủ phụ thuộc: %v, thiếu kho tệp: %v; muốn true, false", has(full), has(noFiles))
	}
}
