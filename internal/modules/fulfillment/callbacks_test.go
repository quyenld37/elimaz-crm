package fulfillment_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Danh sách "đơn giao thất bại cần gọi lại" (kế hoạch M3, Task M3-19; VC07): danh sách quá hạn trước, ghi kết quả,
// giao lại qua hãng giả, giao việc.

func listCallbacks(e *shipEnv, svc *fulfillment.Service, f fulfillment.CallbackFilter) fulfillment.CallbackPage {
	e.t.Helper()
	var p fulfillment.CallbackPage
	if err := e.app(func(tx pgx.Tx) (err error) {
		p, err = svc.ListCallbacks(e.t.Context(), tx, f)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("ListCallbacks: %v", err)
	}
	return p
}

func recordOutcome(e *shipEnv, svc *fulfillment.Service, id uuid.UUID, in fulfillment.CallbackOutcomeInput,
) (fulfillment.CallbackOutcomeResult, error) {
	var r fulfillment.CallbackOutcomeResult
	err := e.app(func(tx pgx.Tx) (err error) {
		r, err = svc.RecordCallbackOutcome(e.t.Context(), tx, id, in, e.orderActor())
		return err //nolint:wrapcheck // test
	})
	return r, err
}

func mustRecord(e *shipEnv, svc *fulfillment.Service, id uuid.UUID, in fulfillment.CallbackOutcomeInput,
) fulfillment.CallbackTask {
	e.t.Helper()
	r, err := recordOutcome(e, svc, id, in)
	if err != nil {
		e.t.Fatalf("ghi kết quả %s: %v", in.Outcome, err)
	}
	return r.Task
}

// callbackOf trả việc gọi lại lần attempt của vận đơn shipmentID (đọc mọi trạng thái).
func callbackOf(e *shipEnv, svc *fulfillment.Service, shipmentID uuid.UUID, attempt int32) fulfillment.CallbackTask {
	e.t.Helper()
	for _, st := range []string{"", fulfillment.CallbackDone} {
		for _, it := range listCallbacks(e, svc, fulfillment.CallbackFilter{Status: st, Limit: 100}).Items {
			if it.ShipmentID == shipmentID && it.AttemptNo == attempt {
				return it
			}
		}
	}
	var status string
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT status FROM fulfillment.callback_tasks WHERE shipment_id = $1
			AND attempt_no = $2`, shipmentID, attempt).Scan(&status)
	})
	e.t.Fatalf("việc gọi lại lần %d của %s ở trạng thái %s, không có trong danh sách", attempt, shipmentID, status)
	return fulfillment.CallbackTask{}
}

// taskState đọc trạng thái, kết quả của việc id bằng chủ bảng (cả việc closed).
func taskState(e *shipEnv, id uuid.UUID) (status, outcome string) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT status, coalesce(outcome, '') FROM fulfillment.callback_tasks
			WHERE id = $1`, id).Scan(&status, &outcome)
	})
	return status, outcome
}

// pushAt đẩy sự kiện raw lúc at vào kiện tracking của hãng giả.
func (e *trackEnv) pushAt(tracking, raw string, at time.Time) {
	e.srv.Push(tracking, fulfillment.CarrierEvent{RawStatus: raw, OccurredAt: at})
}

// refresh báo tín hiệu eventID cho kiện tracking rồi chạy hết việc (đồng hồ tiến 2 phút để lần tra mới không bị gộp).
func (e *trackEnv) refresh(tracking, eventID string) {
	e.t.Helper()
	e.clock.add(2 * time.Minute)
	e.signal(tracking, eventID)
	e.drain()
}

// VC07: V1 giao thất bại → việc open, hạn = lúc thất bại + 24 giờ; quá hạn → đứng đầu, overdue; no_answer kèm hẹn →
// in_progress; redeliver → done, hãng giả nhận đúng một lệnh giao lại; thất bại lần hai sau giao lại → việc mới lần 2;
// giao được → việc lần 2 closed delivered, việc lần 1 giữ done và không ghi kết quả được nữa.
func TestCallbacks_VC07(t *testing.T) {
	t.Parallel()
	e := newTrackEnv(t)
	base := time.Now().UTC().Truncate(time.Second)
	s1, v1, t1 := vc07FailedTwo(e, base)
	done := vc07CallAndRedeliver(e, t1)
	vc07FailAgainThenDeliver(e, s1, done, base)
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'order' AND bucket = 'on_hand'
		AND delta < 0 AND variant_id = $1`, v1); n != 1 {
		t.Fatalf("dòng phiếu xuất của V1 = %d, muốn 1 (giao lại không xuất thêm)", n)
	}
}

// vc07FailedTwo: V1 thất bại lúc base−2h, V2 lúc base; danh sách đúng thứ tự, sau 23 giờ chỉ V1 quá hạn. Trả V1, biến
// thể của V1 và việc của V1.
func vc07FailedTwo(e *trackEnv, base time.Time) (fulfillment.Shipment, uuid.UUID, fulfillment.CallbackTask) {
	t := e.t
	svc := e.mod.Service
	_, v1, s1 := e.shipped(1)
	_, _, s2 := e.shipped(1)
	e.pushAt(s1.TrackingCode, fake.RawPickedUp, base.Add(-3*time.Hour))
	e.pushAt(s1.TrackingCode, fake.RawDeliveryFailed, base.Add(-2*time.Hour))
	e.refresh(s1.TrackingCode, "v1-failed")
	e.pushAt(s2.TrackingCode, fake.RawPickedUp, base.Add(-time.Hour))
	e.pushAt(s2.TrackingCode, fake.RawDeliveryFailed, base)
	e.refresh(s2.TrackingCode, "v2-failed")
	p := listCallbacks(e.shipEnv, svc, fulfillment.CallbackFilter{})
	if len(p.Items) != 2 || p.OpenCount != 2 || p.Next != "" {
		t.Fatalf("danh sách = %d việc, đếm %d, con trỏ %q; muốn 2, 2, rỗng", len(p.Items), p.OpenCount, p.Next)
	}
	t1 := p.Items[0]
	if t1.ShipmentID != s1.ID || t1.AttemptNo != 1 || t1.Status != fulfillment.CallbackOpen || t1.Overdue ||
		!t1.DueAt.Equal(t1.FailedAt.Add(24*time.Hour)) || !t1.FailedAt.Equal(base.Add(-2*time.Hour)) ||
		t1.TrackingCode != s1.TrackingCode || t1.OrderCode == "" || t1.CustomerID == uuid.Nil {
		t.Fatalf("việc V1 = %+v", t1)
	}
	e.clock.add(23 * time.Hour) // V1 quá hạn (hạn base+22h), V2 chưa (base+24h)
	p = listCallbacks(e.shipEnv, svc, fulfillment.CallbackFilter{})
	if !p.Items[0].Overdue || p.Items[0].ID != t1.ID || p.Items[1].Overdue || p.Items[1].ShipmentID != s2.ID {
		t.Fatalf("sau 23 giờ: %+v", p.Items)
	}
	return s1, v1, t1
}

// vc07CallAndRedeliver: no_answer kèm hẹn → in_progress; redeliver → done, đúng một lệnh giao lại tới hãng giả, nhật ký
// đã che SĐT. Trả việc sau redeliver.
func vc07CallAndRedeliver(e *trackEnv, t1 fulfillment.CallbackTask) fulfillment.CallbackTask {
	t := e.t
	svc := e.mod.Service
	at := e.clock.Now().Add(3 * time.Hour).Truncate(time.Second)
	got := mustRecord(e.shipEnv, svc, t1.ID, fulfillment.CallbackOutcomeInput{
		Outcome: fulfillment.OutcomeNoAnswer, NextCallAt: &at, Version: t1.Version,
	})
	if got.Status != fulfillment.CallbackInProgress || got.NextCallAt == nil || !got.NextCallAt.Equal(at) ||
		got.Version != t1.Version+1 || !got.Overdue {
		t.Fatalf("sau no_answer = %+v", got)
	}
	r, err := recordOutcome(e.shipEnv, svc, t1.ID, fulfillment.CallbackOutcomeInput{
		Outcome: fulfillment.OutcomeRedeliver, Note: "Khách hẹn chiều, gọi 0912345678", Version: got.Version,
	})
	if err != nil || !r.Redeliver || r.Task.Status != fulfillment.CallbackDone || r.Task.NextCallAt != nil {
		t.Fatalf("redeliver = %+v, %v", r, err)
	}
	e.drain()
	if n := e.srv.Calls("redeliver"); n != 1 {
		t.Fatalf("hãng giả nhận %d lệnh giao lại, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1 AND status = 'done'`,
		fulfillment.RedeliverKind().Name); n != 1 {
		t.Fatalf("lệnh giao lại done = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.callback_task_logs WHERE task_id = $1 AND note LIKE '%0912345678%'`,
		t1.ID); n != 0 {
		t.Fatal("nhật ký còn SĐT chưa che")
	}
	return r.Task
}

// vc07FailAgainThenDeliver: sau giao lại, thất bại lần hai → việc lần 2; giao được → lần 2 closed delivered, lần 1 giữ
// done; ghi kết quả lên cả hai → ErrCallbackClosed; không thêm lệnh giao lại.
func vc07FailAgainThenDeliver(e *trackEnv, s1 fulfillment.Shipment, done fulfillment.CallbackTask, base time.Time) {
	t := e.t
	svc := e.mod.Service
	e.refresh(s1.TrackingCode, "v1-redelivering") // hãng giả đã chuyển kiện sang đang giao lại
	e.pushAt(s1.TrackingCode, fake.RawDeliveryFailed, base.Add(time.Hour))
	e.refresh(s1.TrackingCode, "v1-failed-2")
	t2 := callbackOf(e.shipEnv, svc, s1.ID, 2)
	if t2.Status != fulfillment.CallbackOpen {
		t.Fatalf("việc lần 2 = %+v", t2)
	}
	e.pushAt(s1.TrackingCode, fake.RawDelivered, base.Add(2*time.Hour))
	e.refresh(s1.TrackingCode, "v1-delivered")
	if st, oc := taskState(e.shipEnv, t2.ID); st != fulfillment.CallbackClosed || oc != "delivered" {
		t.Fatalf("việc lần 2 sau giao được = %s %s", st, oc)
	}
	if st, oc := taskState(e.shipEnv, done.ID); st != fulfillment.CallbackDone || oc != fulfillment.OutcomeRedeliver {
		t.Fatalf("việc lần 1 sau giao được = %s %s", st, oc)
	}
	for _, k := range []fulfillment.CallbackTask{{ID: t2.ID, Version: t2.Version + 1}, done} {
		if _, err := recordOutcome(e.shipEnv, svc, k.ID, fulfillment.CallbackOutcomeInput{
			Outcome: fulfillment.OutcomeNoAnswer, Version: k.Version,
		}); !errors.Is(err, fulfillment.ErrCallbackClosed) {
			t.Fatalf("ghi kết quả việc đã xong/đóng %s = %v, muốn ErrCallbackClosed", k.ID, err)
		}
	}
	if e.srv.Calls("redeliver") != 1 {
		t.Fatal("hãng giả nhận thêm lệnh giao lại")
	}
}

// failedD1 tạo đơn D1, vận đơn chờ thu gom rồi giao thất bại 10:00; trả env, vận đơn, việc gọi lại lần 1.
func failedD1(t *testing.T) (*applyEnv, fulfillment.Shipment, fulfillment.CallbackTask) {
	t.Helper()
	e, _, _, _, s := newD1Shipment(t)
	e.apply(s.ID, ev("p", fulfillment.CanonPickedUp, 8, 0), ev("f", fulfillment.CanonDeliveryFailed, 10, 0))
	return e, s, callbackOf(e.shipEnv, e.svc, s.ID, 1)
}

// Hãng chuyển hoàn khi việc còn open → closed carrier_returning, ghi kết quả → ErrCallbackClosed, không thêm nhật ký.
func TestCallbacks_ReturningClosesTask(t *testing.T) {
	t.Parallel()
	e, s, k := failedD1(t)
	e.apply(s.ID, ev("r", fulfillment.CanonReturning, 12, 0))
	if st, oc := taskState(e.shipEnv, k.ID); st != fulfillment.CallbackClosed || oc != "carrier_returning" {
		t.Fatalf("việc sau hãng hoàn = %s %s", st, oc)
	}
	if _, err := recordOutcome(e.shipEnv, e.svc, k.ID, fulfillment.CallbackOutcomeInput{
		Outcome: fulfillment.OutcomeRedeliver, Version: k.Version + 1,
	}); !errors.Is(err, fulfillment.ErrCallbackClosed) {
		t.Fatalf("ghi kết quả = %v, muốn ErrCallbackClosed", err)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.callback_task_logs WHERE task_id = $1`, k.ID); n != 0 {
		t.Fatalf("nhật ký = %d, muốn 0", n)
	}
	if p := listCallbacks(e.shipEnv, e.svc, fulfillment.CallbackFilter{}); len(p.Items) != 0 || p.OpenCount != 0 {
		t.Fatalf("danh sách việc mở = %+v", p)
	}
}

// Các ca từ chối không đổi gì; hãng không giao lại qua API → done kèm câu "nhân viên báo hãng"; khách hủy → done.
func TestCallbacks_OutcomeRules(t *testing.T) {
	t.Parallel()
	e, _, k := failedD1(t)
	bad := []struct {
		name string
		in   fulfillment.CallbackOutcomeInput
		want error
	}{
		{"kết quả lạ", fulfillment.CallbackOutcomeInput{Outcome: "delivered", Version: k.Version}, fulfillment.ErrShipInvalid},
		{"hẹn thiếu giờ", fulfillment.CallbackOutcomeInput{Outcome: fulfillment.OutcomeReschedule, Version: k.Version}, fulfillment.ErrShipInvalid},
		{"địa chỉ mới thiếu địa chỉ", fulfillment.CallbackOutcomeInput{Outcome: fulfillment.OutcomeRedeliverNewAddress, Version: k.Version}, fulfillment.ErrShipInvalid},
		{"ghi chú quá dài", fulfillment.CallbackOutcomeInput{Outcome: fulfillment.OutcomeNoAnswer, Note: strings.Repeat("a", 1001), Version: k.Version}, fulfillment.ErrShipInvalid},
		{"lệch version", fulfillment.CallbackOutcomeInput{Outcome: fulfillment.OutcomeNoAnswer, Version: k.Version + 1}, fulfillment.ErrVersionConflict},
	}
	for _, c := range bad {
		if _, err := recordOutcome(e.shipEnv, e.svc, k.ID, c.in); !errors.Is(err, c.want) {
			t.Fatalf("%s: lỗi %v, muốn %v", c.name, err, c.want)
		}
	}
	if _, err := recordOutcome(e.shipEnv, e.svc, uuid.New(), fulfillment.CallbackOutcomeInput{
		Outcome: fulfillment.OutcomeNoAnswer,
	}); !errors.Is(err, fulfillment.ErrCallbackNotFound) {
		t.Fatalf("việc lạ = %v", err)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.callback_task_logs`); n != 0 {
		t.Fatalf("nhật ký sau các lần từ chối = %d", n)
	}
	r, err := recordOutcome(e.shipEnv, e.svc, k.ID, fulfillment.CallbackOutcomeInput{
		Outcome: fulfillment.OutcomeRedeliver, Note: "giao sau 17h", Version: k.Version,
	})
	if err != nil || r.Redeliver || r.Task.Status != fulfillment.CallbackDone || r.Task.Outcome != fulfillment.OutcomeRedeliver {
		t.Fatalf("redeliver không API = %+v, %v", r, err)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.callback_task_logs WHERE task_id = $1 AND outcome = 'redeliver'
		AND note = 'Nhân viên báo hãng giao lại (hãng không nhận yêu cầu qua API): giao sau 17h'`, k.ID); n != 1 {
		t.Fatalf("nhật ký giao lại tay = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = $1`, fulfillment.RedeliverKind().Name); n != 0 {
		t.Fatalf("lệnh giao lại = %d, muốn 0", n)
	}
}

// Khách hủy: việc done, ghi chú cho kho nằm ở nhật ký; báo hiệu callback_task.changed.
func TestCallbacks_CustomerCancel(t *testing.T) {
	t.Parallel()
	e, _, k := failedD1(t)
	before := len(e.bus.Signals())
	got := mustRecord(e.shipEnv, e.svc, k.ID, fulfillment.CallbackOutcomeInput{
		Outcome: fulfillment.OutcomeCustomerCancel, Note: "Khách không nhận, kho chờ hàng hoàn", Version: k.Version,
	})
	if got.Status != fulfillment.CallbackDone || got.Outcome != fulfillment.OutcomeCustomerCancel {
		t.Fatalf("khách hủy = %+v", got)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.callback_task_logs WHERE task_id = $1
		AND note = 'Khách không nhận, kho chờ hàng hoàn'`, k.ID); n != 1 {
		t.Fatalf("nhật ký khách hủy = %d", n)
	}
	sigs := e.bus.Signals()[before:]
	if len(sigs) != 1 || sigs[0].Topic != "callback_task.changed" || sigs[0].Entity.ID != k.ID.String() ||
		sigs[0].Requires != "fulfillment.callbacks.work" {
		t.Fatalf("báo hiệu = %+v", sigs)
	}
}

// Hai lần ghi kết quả cùng version chạy song song → đúng một lần qua, lần kia ErrVersionConflict; một dòng nhật ký.
func TestCallbacks_ConcurrentOutcome(t *testing.T) {
	t.Parallel()
	e, _, k := failedD1(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = recordOutcome(e.shipEnv, e.svc, k.ID, fulfillment.CallbackOutcomeInput{
				Outcome: fulfillment.OutcomeNoAnswer, Version: k.Version,
			})
		})
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, fulfillment.ErrVersionConflict):
			t.Fatalf("lỗi lạ: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("số lần qua = %d, muốn 1 (%v)", ok, errs)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.callback_task_logs WHERE task_id = $1`, k.ID); n != 1 {
		t.Fatalf("nhật ký = %d, muốn 1", n)
	}
}

// Giao lại địa chỉ mới: việc done và một yêu cầu đổi địa chỉ (manual: hãng giả không sửa qua API) trong cùng
// transaction; kho ngoài phạm vi → lỗi của Authorize, không đổi gì.
func TestCallbacks_RedeliverNewAddress(t *testing.T) {
	t.Parallel()
	e, _ := newAddressEnv(t, "")
	_, _, s := e.shipped(1)
	in := fulfillment.ApplyInput{
		ShipmentID: s.ID, Source: fulfillment.SrcCarrierAPI, SyncedAt: testDay().Add(23 * time.Hour),
		Actor: order.Actor{TenantID: e.tenant, Type: "carrier"},
		Tracking: fulfillment.Tracking{Events: []fulfillment.CarrierEvent{
			ev("p", fulfillment.CanonPickedUp, 8, 0), ev("f", fulfillment.CanonDeliveryFailed, 10, 0),
		}},
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.svc.ApplyTracking(t.Context(), tx, in)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	k := callbackOf(e, e.svc, s.ID, 1)
	addr := newAddr()
	errScope := errors.New("ngoài phạm vi kho")
	if _, err := recordOutcome(e, e.svc, k.ID, fulfillment.CallbackOutcomeInput{
		Outcome: fulfillment.OutcomeRedeliverNewAddress, NewAddress: &addr, Version: k.Version,
		Authorize: func(uuid.UUID) error { return errScope },
	}); !errors.Is(err, errScope) {
		t.Fatalf("kho ngoài phạm vi = %v", err)
	}
	if st, _ := taskState(e, k.ID); st != fulfillment.CallbackOpen {
		t.Fatalf("việc sau lần bị từ chối = %s, muốn open", st)
	}
	r, err := recordOutcome(e, e.svc, k.ID, fulfillment.CallbackOutcomeInput{
		Outcome: fulfillment.OutcomeRedeliverNewAddress, NewAddress: &addr, Note: "đổi sang nhà chị gái", Version: k.Version,
	})
	if err != nil || r.Task.Status != fulfillment.CallbackDone || r.AddressChange == nil ||
		r.AddressChange.ShipmentID != s.ID || r.AddressChange.Mode != "manual" || r.AddressChange.Status != "pending" {
		t.Fatalf("giao lại địa chỉ mới = %+v, %v", r, err)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.address_change_requests WHERE shipment_id = $1`, s.ID); n != 1 {
		t.Fatalf("yêu cầu đổi địa chỉ = %d, muốn 1", n)
	}
}

// Giao việc: người được giao ghi lên việc, version tăng; lệch version → ErrVersionConflict; lọc "của tôi"; việc đã
// xong → ErrCallbackClosed.
func TestCallbacks_Assign(t *testing.T) {
	t.Parallel()
	e, _, k := failedD1(t)
	other := e.staff.User(t, e.tenant, "sale@elimaz.test", "active")
	assign := func(version *int64) (fulfillment.CallbackAssignment, error) {
		var a fulfillment.CallbackAssignment
		err := e.app(func(tx pgx.Tx) (err error) {
			a, err = e.svc.AssignCallback(t.Context(), tx, k.ID, other, version, e.actor)
			return err //nolint:wrapcheck // test
		})
		return a, err
	}
	stale := k.Version + 5
	if _, err := assign(&stale); !errors.Is(err, fulfillment.ErrVersionConflict) {
		t.Fatalf("lệch version = %v", err)
	}
	a, err := assign(&k.Version)
	if err != nil || a.BeforeAssignee != nil || a.Task.AssigneeID == nil || *a.Task.AssigneeID != other ||
		a.Task.Version != k.Version+1 {
		t.Fatalf("giao việc = %+v, %v", a, err)
	}
	if p := listCallbacks(e.shipEnv, e.svc, fulfillment.CallbackFilter{AssigneeID: &other}); len(p.Items) != 1 {
		t.Fatalf("việc của người được giao = %d", len(p.Items))
	}
	me := e.actor.UserID
	if p := listCallbacks(e.shipEnv, e.svc, fulfillment.CallbackFilter{AssigneeID: &me}); len(p.Items) != 0 {
		t.Fatalf("việc của người khác = %d", len(p.Items))
	}
	mustRecord(e.shipEnv, e.svc, k.ID, fulfillment.CallbackOutcomeInput{
		Outcome: fulfillment.OutcomeCustomerCancel, Version: a.Task.Version,
	})
	if _, err := assign(nil); !errors.Is(err, fulfillment.ErrCallbackClosed) {
		t.Fatalf("giao việc đã xong = %v", err)
	}
	if p := listCallbacks(e.shipEnv, e.svc, fulfillment.CallbackFilter{Status: fulfillment.CallbackDone}); len(p.Items) != 1 ||
		p.Items[0].Overdue || p.OpenCount != 0 {
		t.Fatalf("danh sách done = %+v", p)
	}
}

// Phân trang theo (due_at, id), lọc hạn trước mốc, con trỏ hỏng → ErrInvalidPage, trạng thái lạ → ErrShipInvalid.
func TestCallbacks_ListPages(t *testing.T) {
	t.Parallel()
	te := newTrackEnv(t)
	e := te.shipEnv
	_, _, tmpl := te.shipped(1)
	te.clone(tmpl, 7, 5, "delivery_failed", nil, nil)
	day := testDay()
	e.ownerExec(`INSERT INTO fulfillment.callback_tasks (tenant_id, shipment_id, order_id, attempt_no, failed_at, due_at)
		SELECT tenant_id, id, order_id, 1, $1::timestamptz, $1::timestamptz + (right(code, 1)::int) * interval '1 hour'
		FROM fulfillment.shipments WHERE code LIKE 'E7%'`, day)
	svc := te.mod.Service
	var seen []time.Time
	cursor := ""
	for range 3 {
		p := listCallbacks(e, svc, fulfillment.CallbackFilter{Cursor: cursor, Limit: 2})
		for _, it := range p.Items {
			seen = append(seen, it.DueAt)
		}
		if cursor = p.Next; cursor == "" {
			break
		}
	}
	if len(seen) != 5 {
		t.Fatalf("đọc qua trang = %d việc, muốn 5", len(seen))
	}
	for i := 1; i < len(seen); i++ {
		if seen[i].Before(seen[i-1]) {
			t.Fatalf("thứ tự hạn sai: %v", seen)
		}
	}
	cut := day.Add(3 * time.Hour)
	if p := listCallbacks(e, svc, fulfillment.CallbackFilter{DueBefore: &cut}); len(p.Items) != 2 {
		t.Fatalf("hạn trước %v = %d việc, muốn 2", cut, len(p.Items))
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := svc.ListCallbacks(t.Context(), tx, fulfillment.CallbackFilter{Cursor: "hỏng"})
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, fulfillment.ErrInvalidPage) {
		t.Fatalf("con trỏ hỏng = %v", err)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := svc.ListCallbacks(t.Context(), tx, fulfillment.CallbackFilter{Status: "closed"})
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, fulfillment.ErrShipInvalid) {
		t.Fatalf("trạng thái closed = %v", err)
	}
}

// 2.000 việc mở và 25.000 việc đã xong: câu danh sách và câu đếm không Seq Scan.
func TestCallbacks_PlanUsesIndexes(t *testing.T) {
	t.Parallel()
	te := newTrackEnv(t)
	_, _, tmpl := te.shipped(1)
	old := time.Now().Add(-200 * time.Hour)
	for p := range 4 { // 4 × 6.250 = 25.000 vận đơn đã giao, mỗi cái một việc đã xong
		te.clone(tmpl, p+1, 6250, "delivered", &old, &old)
	}
	te.clone(tmpl, 9, 2000, "delivery_failed", &old, nil)
	e := te.shipEnv
	e.ownerBulkExec(
		`INSERT INTO fulfillment.callback_tasks (tenant_id, shipment_id, order_id, attempt_no, failed_at, due_at, status)
			SELECT tenant_id, id, order_id, 1, now(), now(), 'done' FROM fulfillment.shipments
			WHERE code LIKE 'E1%' OR code LIKE 'E2%' OR code LIKE 'E3%' OR code LIKE 'E4%'`,
		`INSERT INTO fulfillment.callback_tasks (tenant_id, shipment_id, order_id, attempt_no, failed_at, due_at)
			SELECT tenant_id, id, order_id, 1, now(), now() + random() * interval '24 hours'
			FROM fulfillment.shipments WHERE code LIKE 'E9%'`,
	)
	e.ownerBulkExec(`ANALYZE fulfillment.callback_tasks, fulfillment.shipments`)
	e.requireNoSeqScan("callbacks",
		explainCase{"listCallbackTasks", []any{[]string{"open", "in_progress"}, nil, nil, nil, nil, int32(51)}},
		explainCase{"countOpenCallbacks", nil},
	)
	if p := listCallbacks(e, te.mod.Service, fulfillment.CallbackFilter{}); p.OpenCount != 1000 || len(p.Items) != 50 {
		t.Fatalf("đếm có trần = %d, trang = %d; muốn 1000, 50", p.OpenCount, len(p.Items))
	}
}
