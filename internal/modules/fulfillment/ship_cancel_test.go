package fulfillment_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// Đơn đã yêu cầu hủy (cancel_requested, M2) lúc lệnh tạo vận đơn đang chạy (Task 431, kế hoạch M3-12 "Bổ sung Task
// 91"): lệnh tạo kết thúc mà hãng chắc chưa tạo → vận đơn cancelled, đơn cancelled, hàng held nhả; không ai gửi lại
// được.

// requestCancel đưa đơn o (confirmed, có vận đơn hiện hành) sang cancel_requested bằng order.Writer.Cancel của M2.
func (e *shipEnv) requestCancel(o order.Order) {
	e.t.Helper()
	if err := e.tryRequestCancel(o.ID); err != nil {
		e.t.Fatalf("yêu cầu hủy đơn: %v", err)
	}
	if got := e.getOrder(o.ID); got.Status != order.StatusCancelRequested {
		e.t.Fatalf("đơn = %s, muốn cancel_requested", got.Status)
	}
}

// tryRequestCancel như requestCancel nhưng trả lỗi (gọi được từ hook của hãng giả, ngoài goroutine của test).
func (e *shipEnv) tryRequestCancel(id uuid.UUID) error {
	return e.app(func(tx pgx.Tx) error {
		cur, err := order.Get(e.t.Context(), tx, e.seller, id)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		w := order.Writer{Events: events.NewRegistry(), Queue: queue.NewQueue(), Realtime: &testkit.RecordingBus{}}
		_, err = w.Cancel(e.t.Context(), tx, e.seller, id, cur.Version, order.CancelCustomerChangedMind, "",
			e.orderActor(), time.Now())
		return err //nolint:wrapcheck // test
	})
}

// cancelDuringSend: khách yêu cầu hủy đơn o đúng lúc lệnh tạo đang gọi hãng (sau khi Execute đã đọc vận đơn).
func (e *shipEnv) cancelDuringSend(o order.Order) {
	e.carrier.SetCreateHook(func() {
		e.carrier.SetCreateHook(nil)
		if err := e.tryRequestCancel(o.ID); err != nil {
			e.t.Errorf("yêu cầu hủy trong lúc gửi: %v", err)
		}
	})
}

// cancelDuringSendWith: đơn 1 món, khách yêu cầu hủy trong lúc lệnh tạo gọi hãng ở chế độ mode; kiểm vận đơn cancelled
// (kind, state), đơn cancelled, nhả hàng, gọi hãng đúng 1 lần.
func (e *shipEnv) cancelDuringSendWith(mode, kind, state string) {
	e.t.Helper()
	o, v := e.order(1)
	s := e.mustCreateShipment(o.ID)
	e.cancelDuringSend(o)
	e.carrier.SetMode(mode)
	e.drain()
	e.cancelledWithOrder(s.ID, o, v, kind, state)
	if e.carrier.Calls("create") != 1 {
		e.t.Fatalf("gọi hãng %d lần, muốn 1", e.carrier.Calls("create"))
	}
}

// notSentShipment: đơn 1 món, vận đơn mà hãng chắc chưa tạo (fail_before_create → lệnh dead, create_state not_sent).
func (e *shipEnv) notSentShipment() (order.Order, fulfillment.Shipment) {
	e.t.Helper()
	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	e.carrier.SetMode(fake.ModeFailBeforeCreate)
	e.drain()
	return o, s
}

// cancelledWithOrder kiểm vận đơn id cancelled (cancel_kind kind, create_state state), đơn o cancelled không còn vận
// đơn hiện hành, biến thể v không còn giữ hàng.
func (e *shipEnv) cancelledWithOrder(id uuid.UUID, o order.Order, v uuid.UUID, kind, state string) {
	e.t.Helper()
	if s := e.view(id); s.Status != "cancelled" || s.CancelKind != kind || s.CreateState != state {
		e.t.Fatalf("vận đơn = %s %s %s, muốn cancelled %s %s", s.Status, s.CancelKind, s.CreateState, kind, state)
	}
	if got := e.getOrder(o.ID); got.Status != order.StatusCancelled || got.CurrentShipmentID != nil {
		e.t.Fatalf("đơn = %s gắn %v, muốn cancelled", got.Status, got.CurrentShipmentID)
	}
	e.stockIs(v, [3]int32{5, 0, 0})
}

// Đơn yêu cầu hủy trước khi lệnh gửi hãng → Execute không gọi hãng (not_sent order_cancel_requested), vận đơn
// cancelled order_cancel not_sent, đơn cancelled, nhả hàng.
func TestShip_CancelRequestedBeforeSend(t *testing.T) {
	e := newShipEnv(t)
	o, v := e.order(1)
	s := e.mustCreateShipment(o.ID)
	e.requestCancel(o)
	e.drain()
	e.cancelledWithOrder(s.ID, o, v, "order_cancel", "not_sent")
	jobID, st := e.createJob(s.ID)
	if e.carrier.Calls("create") != 0 || st != queue.StatusDead ||
		e.count("SELECT count(*) FROM platform.jobs WHERE id = $1 AND last_error LIKE '%order_cancel_requested%'", jobID) != 1 {
		t.Fatalf("gọi hãng %d lần, lệnh %s", e.carrier.Calls("create"), st)
	}
}

// Khách yêu cầu hủy trong lúc gửi, hãng chắc chưa tạo (fail_before_create → dead) → vận đơn cancelled order_cancel
// not_sent, đơn cancelled, nhả hàng; gọi hãng đúng 1 lần.
func TestShip_CancelRequestedNotSent(t *testing.T) {
	e := newShipEnv(t)
	e.cancelDuringSendWith(fake.ModeFailBeforeCreate, "order_cancel", "not_sent")
}

// Hãng từ chối khi khách yêu cầu hủy trong lúc gửi → vận đơn cancelled create_rejected, DetachShipment hủy đơn, nhả hàng.
func TestShip_CancelRequestedRejected(t *testing.T) {
	e := newShipEnv(t)
	e.cancelDuringSendWith(fake.ModeReject, "create_rejected", "rejected")
}

// "Không rõ" mà nhân viên xác nhận hãng chưa tạo khi đơn cancel_requested → vận đơn cancelled order_cancel, đơn
// cancelled, nhả hàng, lệnh bỏ (expired), việc in_doubt_create đóng.
func TestShip_CancelRequestedResolvedNotCreated(t *testing.T) {
	e := newShipEnv(t)
	o, v := e.order(1)
	s := e.mustCreateShipment(o.ID)
	e.carrier.SetMode(fake.ModeTimeoutNoCreate)
	e.drain()
	e.requestCancel(o)
	if _, err := e.resolveCreate(s.ID, fulfillment.ResolveInput{Result: "not_created", Reason: "Tra trang hãng không có"}); err != nil {
		t.Fatalf("xử lý không rõ: %v", err)
	}
	e.cancelledWithOrder(s.ID, o, v, "order_cancel", "not_sent")
	if _, st := e.createJob(s.ID); st != queue.StatusExpired || e.openAnomalies(s.ID, "in_doubt_create") != 0 {
		t.Fatalf("lệnh = %s, việc mở %d", st, e.openAnomalies(s.ID, "in_doubt_create"))
	}
}

// Vận đơn đã not_sent rồi đơn mới yêu cầu hủy → "Gửi lại" bị chặn (ErrOrderNotShippable), không gọi hãng lần hai.
func TestShip_ResendBlockedWhenOrderNotConfirmed(t *testing.T) {
	e := newShipEnv(t)
	o, s := e.notSentShipment()
	e.requestCancel(o)
	e.carrier.SetMode(fake.ModeOK)
	if _, err := e.resend(s.ID); !errors.Is(err, fulfillment.ErrOrderNotShippable) {
		t.Fatalf("gửi lại khi đơn cancel_requested: %v, muốn ErrOrderNotShippable", err)
	}
	e.drain()
	if e.carrier.Calls("create") != 1 || e.view(s.ID).CreateState != "not_sent" {
		t.Fatalf("gọi hãng %d lần, vận đơn %s", e.carrier.Calls("create"), e.view(s.ID).CreateState)
	}
}

// Lệnh "không rõ" vừa được người chạy lại (queue.RetryJob → pending) mà nhân viên bấm "đã tạo" → ErrCreateInProgress
// (409; ResolveManually chỉ nhận lệnh in_doubt, dead), transaction lùi: vận đơn giữ unknown, không gắn mã. "Không tạo"
// vẫn được: Abandon bỏ lệnh pending (expired, không chạy nữa), vận đơn not_sent.
func TestShip_ResolveWhileRetried(t *testing.T) {
	e := newShipEnv(t)
	s := e.timedOutShipment()
	e.retryCreate(s.ID)
	in := fulfillment.ResolveInput{Result: "created", TrackingCode: "FK8888", Reason: "Tra trang hãng có kiện"}
	if _, err := e.resolveCreate(s.ID, in); !errors.Is(err, fulfillment.ErrCreateInProgress) {
		t.Fatalf("đã tạo khi lệnh đang chờ chạy lại: %v, muốn ErrCreateInProgress", err)
	}
	if v := e.view(s.ID); v.CreateState != "unknown" || v.TrackingCode != "" || v.Status != "creating" {
		t.Fatalf("vận đơn = %s %s %q", v.Status, v.CreateState, v.TrackingCode)
	}
	if _, err := e.resolveCreate(s.ID, fulfillment.ResolveInput{Result: "not_created", Reason: "Tra trang hãng không có"}); err != nil {
		t.Fatalf("không tạo khi lệnh đang chờ chạy lại: %v", err)
	}
	if _, st := e.createJob(s.ID); st != queue.StatusExpired || e.view(s.ID).CreateState != "not_sent" {
		t.Fatalf("lệnh = %s, vận đơn %s", st, e.view(s.ID).CreateState)
	}
}
