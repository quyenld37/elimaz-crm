package fulfillment_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// Nhập mã tay và hãng "tự giao" (kế hoạch M3, Task M3-15): VC14 và các ca biên. Tài khoản tay dựng bằng
// EnsureCarrierAccount rồi đổi mode; Service của test không có adapter của best, self (nhánh tay không cần adapter).

// manualAccount tạo tài khoản mode manual, active của hãng key cho người bán của test ở kho MAIN.
func (e *shipEnv) manualAccount(key, ref string) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		id = shiptest.ManualAccount(e.t, tx, e.seller, e.shop.Warehouse, key, ref)
		return nil
	})
	return id
}

func (e *shipEnv) setManual(id uuid.UUID, in fulfillment.ManualStatusInput) (fulfillment.ShipmentView, error) {
	var v fulfillment.ShipmentView
	err := e.app(func(tx pgx.Tx) (err error) {
		v, err = e.svc.SetManualStatus(e.t.Context(), tx, id, in, e.orderActor())
		return err //nolint:wrapcheck // test
	})
	return v, err
}

func (e *shipEnv) mustSetManual(id uuid.UUID, in fulfillment.ManualStatusInput) fulfillment.ShipmentView {
	e.t.Helper()
	v, err := e.setManual(id, in)
	if err != nil {
		e.t.Fatalf("ghi tay %s: %v", in.To, err)
	}
	return v
}

// commandJobs đếm việc chế độ command (lệnh gọi ra ngoài) của tenant.
func (e *shipEnv) commandJobs() int {
	e.t.Helper()
	return e.count("SELECT count(*) FROM platform.jobs WHERE mode = 'command'")
}

// VC14: tài khoản Best mode manual; nhân viên dán mã → awaiting_pickup, create_state manual, phí thật, không lệnh gọi
// hãng; ghi tay shipping rồi delivered (COD thu đủ) → đơn delivered, xuất kho đúng một lần, status_changed như vận đơn
// API (có codCollected), lịch sử ghi nguồn staff và người làm; lý do che SĐT.
func TestManual_VC14(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	acc := e.manualAccount("best", "best-tay-1")
	o, v := e.order(2)
	fee := int64(32000)
	sv, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: acc, TrackingCode: " BEST123456 ", FeeActual: &fee})
	if err != nil {
		t.Fatalf("tạo vận đơn tay: %v", err)
	}
	if sv.Status != "awaiting_pickup" || sv.CreateState != "manual" || sv.TrackingCode != "BEST123456" ||
		sv.Mode != "manual" || sv.CarrierKey != "best" || sv.FeeActual == nil || *sv.FeeActual != fee ||
		sv.Label != "Chờ thu gom" || sv.Code == "" || sv.Code == sv.TrackingCode {
		t.Fatalf("vận đơn tay = %+v", sv)
	}
	if n := e.commandJobs(); n != 0 {
		t.Fatalf("việc command = %d, muốn 0", n)
	}
	if got := e.getOrder(o.ID); got.CurrentShipmentID == nil || *got.CurrentShipmentID != sv.ID {
		t.Fatalf("đơn chưa gắn vận đơn tay: %+v", got.CurrentShipmentID)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE aggregate_id = $1
		AND topic IN ('shipment.created', 'shipment.status_changed')`, sv.ID.String()); n != 2 {
		t.Fatalf("sự kiện tạo vận đơn tay = %d, muốn 2 (created, status_changed)", n)
	}
	e.stockIs(v, [3]int32{5, 2, 0})

	now := time.Now().UTC()
	e.mustSetManual(sv.ID, fulfillment.ManualStatusInput{
		To: fulfillment.StatusShipping, At: now.Add(-2 * time.Hour), Reason: "Best đã lấy hàng, gọi 0912345678",
	})
	e.stockIs(v, [3]int32{5, 0, 2})
	collected := o.CODAmount
	got := e.mustSetManual(sv.ID, fulfillment.ManualStatusInput{
		To: fulfillment.StatusDelivered, At: now.Add(-time.Hour), Reason: "Best báo giao xong", CODCollected: &collected,
	})
	if got.Status != "delivered" || got.CODCollectedAmount == nil || *got.CODCollectedAmount != collected {
		t.Fatalf("sau giao xong = %+v", got)
	}
	if st := e.getOrder(o.ID).Status; st != order.StatusDelivered {
		t.Fatalf("đơn = %s, muốn delivered", st)
	}
	e.stockIs(v, [3]int32{3, 0, 0})
	if n := e.count(`SELECT count(*) FROM inventory.stock_ledger WHERE doc_type = 'order' AND bucket = 'on_hand'
		AND delta < 0 AND variant_id = $1`, v); n != 1 {
		t.Fatalf("dòng xuất kho = %d, muốn 1", n)
	}
	if n := e.statusEvents(sv.ID); n != 3 {
		t.Fatalf("shipment.status_changed = %d, muốn 3 (tạo, lấy hàng, giao xong)", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.domain_events WHERE topic = 'shipment.status_changed'
		AND aggregate_id = $1 AND payload->>'status' = 'delivered' AND (payload->>'codCollected')::bigint = $2
		AND payload->'effects' ? 'delivered'`, sv.ID.String(), collected); n != 1 {
		t.Fatalf("status_changed delivered có codCollected = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1 AND source = 'staff'
		AND actor_id = $2 AND applied AND event_key LIKE 'staff:%'`, sv.ID, e.actor.UserID); n != 2 {
		t.Fatalf("lịch sử nguồn staff có người làm = %d, muốn 2", n)
	}
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1
		AND raw::text LIKE '%0912345678%'`, sv.ID); n != 0 {
		t.Fatalf("lịch sử chứa SĐT ở %d dòng", n)
	}
	if n := e.commandJobs(); n != 0 {
		t.Fatalf("việc command sau ghi tay = %d, muốn 0", n)
	}
}

// Tự giao: mã hãng là mã vận đơn nội bộ, nhãn "Tự giao"; nhập mã cho self → ErrShipInvalid; tài khoản tay của hãng
// thật thiếu mã → ErrShipInvalid; tài khoản api có mã nhập tay → ErrShipInvalid. Lỗi không để lại vận đơn.
func TestManual_SelfAndInputs(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	self := e.manualAccount("self", "tu-giao")
	best := e.manualAccount("best", "best-tay-1")
	o, _ := e.order(1)
	if _, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: self, TrackingCode: "X1"}); !errors.Is(err, fulfillment.ErrShipInvalid) {
		t.Fatalf("self có mã nhập = %v, muốn ErrShipInvalid", err)
	}
	if _, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: best}); !errors.Is(err, fulfillment.ErrShipInvalid) {
		t.Fatalf("tay thiếu mã = %v, muốn ErrShipInvalid", err)
	}
	if _, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: e.account, TrackingCode: "FK1"}); !errors.Is(err, fulfillment.ErrShipInvalid) {
		t.Fatalf("api có mã nhập = %v, muốn ErrShipInvalid", err)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE order_id = $1", o.ID); n != 0 {
		t.Fatalf("vận đơn còn lại sau lỗi = %d", n)
	}
	sv, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: self})
	if err != nil {
		t.Fatalf("tự giao: %v", err)
	}
	if sv.TrackingCode != sv.Code || sv.Status != "awaiting_pickup" || sv.CreateState != "manual" || sv.Label != "Tự giao" ||
		sv.FeeActual != nil {
		t.Fatalf("tự giao = %+v", sv)
	}
	if n := e.commandJobs(); n != 0 {
		t.Fatalf("việc command = %d, muốn 0", n)
	}
	got := e.mustSetManual(sv.ID, fulfillment.ManualStatusInput{
		To: fulfillment.StatusDelivered, At: time.Now().Add(-time.Minute), Reason: "Shop tự giao xong",
	})
	if got.Status != "delivered" || got.Label != "Thành công" {
		t.Fatalf("tự giao xong = %+v", got)
	}
}

// Trùng mã Best ở tài khoản tay khác cùng hãng → ErrTrackingCodeConflict, đơn thứ hai không có vận đơn nào, không để lại
// việc cần xem.
func TestManual_TrackingConflict(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	a1, a2 := e.manualAccount("best", "best-tay-1"), e.manualAccount("best", "best-tay-2")
	o1, _ := e.order(1)
	o2, _ := e.order(1)
	if _, err := e.ship(fulfillment.ShipInput{OrderID: o1.ID, CarrierAccountID: a1, TrackingCode: "BEST123456"}); err != nil {
		t.Fatal(err)
	}
	_, err := e.ship(fulfillment.ShipInput{OrderID: o2.ID, CarrierAccountID: a2, TrackingCode: "BEST123456"})
	if !errors.Is(err, fulfillment.ErrTrackingCodeConflict) {
		t.Fatalf("trùng mã = %v, muốn ErrTrackingCodeConflict", err)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE order_id = $1", o2.ID); n != 0 {
		t.Fatalf("đơn 2 còn %d vận đơn", n)
	}
	if got := e.getOrder(o2.ID); got.CurrentShipmentID != nil {
		t.Fatalf("đơn 2 vẫn gắn vận đơn %v", got.CurrentShipmentID)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipment_anomalies"); n != 0 {
		t.Fatalf("việc cần xem = %d, muốn 0", n)
	}
	// Cùng mã ở hãng khác không trùng (unique theo hãng).
	vtp := e.manualAccount("vtp", "vtp-tay-1")
	if _, err := e.ship(fulfillment.ShipInput{OrderID: o2.ID, CarrierAccountID: vtp, TrackingCode: "BEST123456"}); err != nil {
		t.Fatalf("cùng mã hãng khác: %v", err)
	}
}

// Ghi tay bị từ chối: lý do trống → ErrManualReasonRequired; returned → ErrManualNotAllowed; vận đơn api không có việc
// hủy tay → ErrManualNotAllowed; version lệch → ErrVersionConflict; cạnh không hợp lệ (delivered → shipping), giờ cũ
// hơn sự kiện đã ghi, trùng trạng thái → ErrManualNotAllowed mà không để lại lịch sử hay việc cần xem; giờ ở tương lai,
// COD âm hay COD khi không giao xong → ErrShipInvalid; không có vận đơn → ErrNotFound.
func TestManual_Refusals(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	acc := e.manualAccount("best", "best-tay-1")
	o, _ := e.order(1)
	sv, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: acc, TrackingCode: "BEST1"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	neg, cod := int64(-1), int64(1000)
	ver := sv.Version + 1
	for _, c := range []struct {
		name string
		in   fulfillment.ManualStatusInput
		want error
	}{
		{"lý do trống", fulfillment.ManualStatusInput{To: fulfillment.StatusShipping, At: now, Reason: "  "}, fulfillment.ErrManualReasonRequired},
		{"returned", fulfillment.ManualStatusInput{To: fulfillment.StatusReturned, At: now, Reason: "x"}, fulfillment.ErrManualNotAllowed},
		{"version lệch", fulfillment.ManualStatusInput{To: fulfillment.StatusShipping, At: now, Reason: "x", Version: &ver}, fulfillment.ErrVersionConflict},
		{"giờ tương lai", fulfillment.ManualStatusInput{To: fulfillment.StatusShipping, At: now.Add(time.Hour), Reason: "x"}, fulfillment.ErrShipInvalid},
		{"thiếu giờ", fulfillment.ManualStatusInput{To: fulfillment.StatusShipping, Reason: "x"}, fulfillment.ErrShipInvalid},
		{"COD âm", fulfillment.ManualStatusInput{To: fulfillment.StatusDelivered, At: now, Reason: "x", CODCollected: &neg}, fulfillment.ErrShipInvalid},
		{"COD khi chưa giao", fulfillment.ManualStatusInput{To: fulfillment.StatusShipping, At: now, Reason: "x", CODCollected: &cod}, fulfillment.ErrShipInvalid},
	} {
		if _, err := e.setManual(sv.ID, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, muốn %v", c.name, err, c.want)
		}
	}
	if _, err := e.setManual(uuid.New(), fulfillment.ManualStatusInput{To: fulfillment.StatusShipping, At: now, Reason: "x"}); !errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("không có vận đơn: %v", err)
	}
	cur := sv.Version
	e.mustSetManual(sv.ID, fulfillment.ManualStatusInput{To: fulfillment.StatusShipping, At: now.Add(-2 * time.Hour), Reason: "x", Version: &cur})
	e.mustSetManual(sv.ID, fulfillment.ManualStatusInput{To: fulfillment.StatusDelivered, At: now.Add(-time.Hour), Reason: "x"})
	events := e.count("SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1", sv.ID)
	for _, c := range []struct {
		name string
		in   fulfillment.ManualStatusInput
	}{
		{"delivered → shipping", fulfillment.ManualStatusInput{To: fulfillment.StatusShipping, At: now, Reason: "x"}},
		{"trùng delivered", fulfillment.ManualStatusInput{To: fulfillment.StatusDelivered, At: now, Reason: "x"}},
		{"giờ cũ hơn", fulfillment.ManualStatusInput{To: fulfillment.StatusReturning, At: now.Add(-3 * time.Hour), Reason: "x"}},
	} {
		if _, err := e.setManual(sv.ID, c.in); !errors.Is(err, fulfillment.ErrManualNotAllowed) {
			t.Errorf("%s: %v, muốn ErrManualNotAllowed", c.name, err)
		}
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipment_events WHERE shipment_id = $1", sv.ID); n != events {
		t.Fatalf("lịch sử sau ghi tay bị từ chối = %d, muốn %d", n, events)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipment_anomalies WHERE shipment_id = $1", sv.ID); n != 0 {
		t.Fatalf("việc cần xem = %d, muốn 0", n)
	}

	// Vận đơn api không có việc hủy tay.
	_, _, api := e.shipped(1)
	if _, err := e.setManual(api.ID, fulfillment.ManualStatusInput{To: fulfillment.StatusShipping, At: now, Reason: "x"}); !errors.Is(err, fulfillment.ErrManualNotAllowed) {
		t.Fatalf("api không việc: %v, muốn ErrManualNotAllowed", err)
	}
}

// Task M3-14 để lại: vận đơn api mà hãng không hủy qua API (việc manual_cancel). Nhân viên hủy trên trang hãng rồi ghi
// "hãng đã hủy" → cancelled, đơn về confirmed (đổi hãng), giữ hàng, việc đóng; vận đơn khác ghi "hãng không cho hủy"
// (shipping) → đơn confirmed, hàng in_transit, việc đóng; delivered khi đang có việc → ErrManualNotAllowed.
func TestManual_StaffCancelTask(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	e.carrier.SetCancelSupported(false)
	now := time.Now().UTC()
	o1, v1, s1 := e.shipped(1)
	if _, err := e.cancelShipment(s1.ID, false); err != nil {
		t.Fatal(err)
	}
	if e.openAnomalies(s1.ID, "manual_cancel") != 1 {
		t.Fatal("không có việc manual_cancel")
	}
	if _, err := e.setManual(s1.ID, fulfillment.ManualStatusInput{To: fulfillment.StatusDelivered, At: now, Reason: "x"}); !errors.Is(err, fulfillment.ErrManualNotAllowed) {
		t.Fatalf("delivered khi có việc hủy: %v", err)
	}
	got := e.mustSetManual(s1.ID, fulfillment.ManualStatusInput{To: fulfillment.StatusCancelled, At: now, Reason: "Đã hủy trên trang hãng"})
	if got.Status != "cancelled" || got.NeedsReview || e.openAnomalies(s1.ID, "manual_cancel") != 0 {
		t.Fatalf("sau hãng đã hủy = %+v, việc mở %d", got, e.openAnomalies(s1.ID, "manual_cancel"))
	}
	e.expectOrder(o1.ID, order.StatusConfirmed, nil)
	e.stockIs(v1, [3]int32{5, 1, 0})
	if n := e.count(`SELECT count(*) FROM fulfillment.shipment_anomalies WHERE shipment_id = $1 AND kind = 'manual_cancel'
		AND resolved_by = $2 AND resolution = 'staff_cancelled'`, s1.ID, e.actor.UserID); n != 1 {
		t.Fatalf("việc đóng có người làm = %d, muốn 1", n)
	}

	o2, v2, s2 := e.shipped(1)
	if _, err := e.cancelShipment(s2.ID, false); err != nil {
		t.Fatal(err)
	}
	got = e.mustSetManual(s2.ID, fulfillment.ManualStatusInput{To: fulfillment.StatusShipping, At: now, Reason: "Hãng đã lấy hàng, không cho hủy"})
	if got.Status != "shipping" || got.NeedsReview || e.openAnomalies(s2.ID, "manual_cancel") != 0 {
		t.Fatalf("sau hãng không cho hủy = %+v", got)
	}
	e.expectOrder(o2.ID, order.StatusConfirmed, &s2.ID)
	e.stockIs(v2, [3]int32{5, 0, 1})
}

// bareDeps là Deps của module chỉ có database, hàng đợi, sổ sự kiện, bus ghi lại; không bộ hãng, không danh mục nhập
// tay (như production lúc bộ hãng rỗng). Test thêm trường mình cần.
func (e *shipEnv) bareDeps() fulfillment.Deps {
	return fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(), Bus: &realtimetest.RecordingBus{},
	}
}

// Task M3-09 để lại (tệp task 100): ở production bộ hãng rỗng nên mọi tài khoản bị từ chối, kể cả tự giao. Tài khoản
// self mode manual không gọi mạng nên luôn tạo được (active ngay) và gửi được đơn; self mode api, hãng thật chưa có
// adapter vẫn ErrCarrierNotSupported khi Deps không có danh mục nhập tay (có danh mục: TestManualCatalog_*, Task 448).
func TestManual_SelfAccountWithoutCarriers(t *testing.T) {
	t.Parallel()
	e := newShipEnv(t)
	svc := fulfillment.New(e.bareDeps()).Service
	seller := e.seller
	in := func(key, mode string) fulfillment.AccountInput {
		return fulfillment.AccountInput{SellerID: &seller, CarrierKey: key, Mode: mode, Name: "Tự giao", WarehouseID: e.shop.Warehouse}
	}
	create := func(a fulfillment.AccountInput) (fulfillment.AccountView, error) {
		var v fulfillment.AccountView
		err := e.app(func(tx pgx.Tx) (err error) {
			v, err = svc.CreateAccount(t.Context(), tx, a, e.actor)
			return err //nolint:wrapcheck // test
		})
		return v, err
	}
	for _, c := range []struct{ key, mode string }{
		{fulfillment.SelfCarrierKey, fulfillment.ModeAPI}, {"best", fulfillment.ModeManual}, {"best", fulfillment.ModeAPI},
	} {
		if _, err := create(in(c.key, c.mode)); !errors.Is(err, fulfillment.ErrCarrierNotSupported) {
			t.Errorf("%s %s không có adapter: %v, muốn ErrCarrierNotSupported", c.key, c.mode, err)
		}
	}
	acc, err := create(in(fulfillment.SelfCarrierKey, fulfillment.ModeManual))
	if err != nil || acc.Status != fulfillment.AccountActive {
		t.Fatalf("self manual không có bộ hãng: %v %+v", err, acc)
	}
	o, _ := e.order(1)
	var sv fulfillment.ShipmentView
	if err := e.app(func(tx pgx.Tx) (err error) {
		sv, err = svc.Ship(t.Context(), tx, fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: acc.ID}, e.orderActor())
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatalf("tự giao không có bộ hãng: %v", err)
	}
	if sv.Status != "awaiting_pickup" || sv.TrackingCode != sv.Code || e.commandJobs() != 0 {
		t.Fatalf("tự giao = %+v, việc command %d", sv, e.commandJobs())
	}
}
