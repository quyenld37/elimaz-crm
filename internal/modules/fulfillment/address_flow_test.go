package fulfillment_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
)

// Đổi địa chỉ sau khi đã gửi hãng (kế hoạch M3, Task M3-17): DH15 và các ca biên. Hãng giả trong bộ nhớ sửa địa chỉ
// theo chế độ (fake.UpdateMode*); lệnh sửa địa chỉ chạy trong worker cùng lệnh tạo và lệnh tra; audit của hook ghi vào
// bộ nhớ (fulfillment không nhập identity: cmd/crm truyền hàm ghi thật).

// auditLog ghi lại các dòng audit lệnh sửa địa chỉ gửi ra (an toàn khi gọi song song).
type auditLog struct {
	mu   sync.Mutex
	recs []fulfillment.AuditRecord
}

func (l *auditLog) record(_ context.Context, _ pgx.Tx, r fulfillment.AuditRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recs = append(l.recs, r)
	return nil
}

func (l *auditLog) all() []fulfillment.AuditRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]fulfillment.AuditRecord(nil), l.recs...)
}

// newAddressEnv: shipEnv có danh mục địa giới mẫu, lệnh sửa địa chỉ trong worker (audit vào log), hãng giả sửa địa chỉ
// ở chế độ mode (rỗng: hãng không sửa qua API).
func newAddressEnv(t *testing.T, mode string) (*shipEnv, *auditLog) {
	t.Helper()
	e := newShipEnv(t)
	e.staff.SeedAdminUnits(t)
	e.carrier.SetUpdateMode(mode)
	log := &auditLog{}
	e.useCommands(append(e.cmds, fulfillment.NewUpdateAddressCommand(e.svc, log.record)))
	return e, log
}

// newAddr là địa chỉ mới đủ hai cấp (danh mục mẫu), khác địa chỉ của đơn thử.
func newAddr() order.ShipAddress {
	return order.ShipAddress{
		Name: "Chị Lan", Phone: logging.Phone(" 0912345678 "), Line: "45 Trần Hưng Đạo",
		ProvinceCode: testkit.SampleProvinceCode, WardCode: testkit.SampleWardCode,
	}
}

func (e *shipEnv) requestAddress(id uuid.UUID, in fulfillment.AddressChangeInput) (fulfillment.AddressChange, error) {
	var c fulfillment.AddressChange
	err := e.app(func(tx pgx.Tx) (err error) {
		c, err = e.svc.RequestAddressChange(e.t.Context(), tx, id, in, e.orderActor())
		return err //nolint:wrapcheck // test
	})
	return c, err
}

func (e *shipEnv) mustRequestAddress(id uuid.UUID) fulfillment.AddressChange {
	e.t.Helper()
	c, err := e.requestAddress(id, fulfillment.AddressChangeInput{Address: newAddr(), Note: "Khách gọi 0987654321"})
	if err != nil {
		e.t.Fatalf("yêu cầu đổi địa chỉ: %v", err)
	}
	return c
}

func (e *shipEnv) resolveAddress(id uuid.UUID, in fulfillment.AddressResolveInput) (fulfillment.AddressChangeResult, error) {
	var r fulfillment.AddressChangeResult
	err := e.app(func(tx pgx.Tx) (err error) {
		r, err = e.svc.ResolveAddressChange(e.t.Context(), tx, id, in, e.orderActor())
		return err //nolint:wrapcheck // test
	})
	return r, err
}

// changeStatus đọc trạng thái, chế độ của yêu cầu id (crm_app trong tenant).
func (e *shipEnv) changeStatus(id uuid.UUID) (status, mode string) {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), "SELECT status, mode FROM fulfillment.address_change_requests WHERE id = $1",
			id).Scan(&status, &mode)
	}); err != nil {
		e.t.Fatalf("đọc yêu cầu đổi địa chỉ: %v", err)
	}
	return status, mode
}

// expectAddress kiểm địa chỉ giao của đơn id và ship_to của vận đơn s là địa chỉ mới (moved) hay vẫn cũ.
func (e *shipEnv) expectAddress(orderID, shipmentID uuid.UUID, moved bool) {
	e.t.Helper()
	got := e.getOrder(orderID).ShipAddress
	if (got.WardCode == testkit.SampleWardCode) != moved {
		e.t.Fatalf("địa chỉ đơn: xã %q, muốn đã đổi = %v", got.WardCode, moved)
	}
	if moved && (string(got.Phone) != "0912345678" || string(got.Line) != "45 Trần Hưng Đạo") {
		e.t.Fatalf("địa chỉ đơn sau đổi: SĐT %q, dòng %q", got.Phone, got.Line)
	}
	n := e.count(`SELECT count(*) FROM fulfillment.shipments WHERE id = $1 AND ship_to->>'wardCode' = $2
		AND ship_to->>'line' = '45 Trần Hưng Đạo' AND ship_to->>'phone' = '0912345678'`, shipmentID, testkit.SampleWardCode)
	if (n == 1) != moved {
		e.t.Fatalf("ship_to của vận đơn: khớp địa chỉ mới %d dòng, muốn đã đổi = %v", n, moved)
	}
}

func (e *shipEnv) addressJobs(id uuid.UUID) int {
	e.t.Helper()
	return e.count("SELECT count(*) FROM platform.jobs WHERE kind = $1 AND dedupe_key = $2",
		fulfillment.UpdateAddressKind().Name, "address:"+id.String())
}

// expectOrderAudit kiểm đúng một dòng audit đổi địa chỉ đơn orderID trong recs: tên trường ship_address ở cả hai phía,
// người làm actorType.
func expectOrderAudit(t *testing.T, recs []fulfillment.AuditRecord, orderID uuid.UUID, actorType string) {
	t.Helper()
	if len(recs) != 1 {
		t.Fatalf("audit = %d dòng, muốn 1: %+v", len(recs), recs)
	}
	r := recs[0]
	_, before := r.Before["ship_address"]
	_, after := r.After["ship_address"]
	if r.EntityType != "order" || r.EntityID != orderID || r.Action != "order.address_change" ||
		r.ActorType != actorType || !before || !after {
		t.Fatalf("audit = %+v", r)
	}
}

// DH15 (hãng sửa địa chỉ qua API): yêu cầu → pending api, một lệnh, địa chỉ đơn và ship_to vẫn cũ, payload lệnh không
// có địa chỉ; worker → hãng nhận địa chỉ mới (SĐT đã chuẩn hóa) một lần → confirmed, địa chỉ đơn và ship_to mới, một
// order.updated, audit ship_address (người làm system); yêu cầu thứ hai sau khi xong được nhận.
func TestAddress_DH15CarrierAccepts(t *testing.T) {
	e, log := newAddressEnv(t, fake.UpdateModeDone)
	o, _, s := e.shipped(1)
	c := e.mustRequestAddress(s.ID)
	if c.Status != "pending" || c.Mode != "api" || c.ShipmentID != s.ID || c.OrderID != o.ID || c.ResolvedAt != nil {
		t.Fatalf("yêu cầu = %+v", c)
	}
	if c.Note == "" || c.Note == "Khách gọi 0987654321" {
		t.Fatalf("ghi chú chưa che SĐT: %q", c.Note)
	}
	e.expectAddress(o.ID, s.ID, false)
	if n := e.addressJobs(c.ID); n != 1 {
		t.Fatalf("lệnh sửa địa chỉ = %d, muốn 1", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE dedupe_key = $1 AND (payload::text LIKE '%Trần%'
		OR payload::text LIKE '%0912%')`, "address:"+c.ID.String()); n != 0 {
		t.Fatal("payload lệnh có địa chỉ, SĐT")
	}
	updated := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = 'order.updated' AND aggregate_id = $1", o.ID.String())
	e.drain()
	if st, _ := e.changeStatus(c.ID); st != "confirmed" {
		t.Fatalf("yêu cầu sau khi hãng nhận = %s, muốn confirmed", st)
	}
	got, ok := e.carrier.Updated(s.TrackingCode)
	if !ok || e.carrier.Calls("update_address") != 1 || got.WardCode != testkit.SampleWardCode || string(got.Phone) != "0912345678" {
		t.Fatalf("hãng nhận %+v (%v), gọi %d lần", got, ok, e.carrier.Calls("update_address"))
	}
	e.expectAddress(o.ID, s.ID, true)
	if n := e.count("SELECT count(*) FROM platform.domain_events WHERE topic = 'order.updated' AND aggregate_id = $1", o.ID.String()); n != updated+1 {
		t.Fatalf("order.updated = %d, muốn %d", n, updated+1)
	}
	expectOrderAudit(t, log.all(), o.ID, "system")
	if n := e.count("SELECT count(*) FROM fulfillment.address_change_requests WHERE id = $1 AND resolved_at IS NOT NULL", c.ID); n != 1 {
		t.Fatal("yêu cầu confirmed chưa có resolved_at")
	}
	if _, err := e.requestAddress(s.ID, fulfillment.AddressChangeInput{Address: newAddr()}); err != nil {
		t.Fatalf("yêu cầu mới sau khi xong: %v", err)
	}
}

// Hãng không sửa địa chỉ qua API → manual pending, không lệnh, không gọi hãng; địa chỉ đơn chỉ đổi khi nhân viên xác
// nhận confirmed (kèm audit để bên gọi ghi); xác nhận lần hai → ErrAddressChangeNotAllowed.
func TestAddress_ManualResolve(t *testing.T) {
	e, log := newAddressEnv(t, "")
	o, _, s := e.shipped(1)
	c := e.mustRequestAddress(s.ID)
	if c.Status != "pending" || c.Mode != "manual" || e.addressJobs(c.ID) != 0 {
		t.Fatalf("yêu cầu = %+v, lệnh %d", c, e.addressJobs(c.ID))
	}
	e.drain()
	e.expectAddress(o.ID, s.ID, false)
	if e.carrier.Calls("update_address") != 0 {
		t.Fatal("gọi hãng khi hãng không sửa qua API")
	}
	if _, err := e.resolveAddress(c.ID, fulfillment.AddressResolveInput{Result: "sent"}); !errors.Is(err, fulfillment.ErrShipInvalid) {
		t.Fatalf("kết quả lạ: %v, muốn ErrShipInvalid", err)
	}
	r, err := e.resolveAddress(c.ID, fulfillment.AddressResolveInput{Result: "confirmed", Note: "Đã gọi hãng"})
	if err != nil {
		t.Fatalf("xác nhận: %v", err)
	}
	if r.Change.Status != "confirmed" || r.Change.ResolvedAt == nil || r.Change.Note != "Đã gọi hãng" {
		t.Fatalf("sau xác nhận = %+v", r.Change)
	}
	if r.OrderAudit == nil {
		t.Fatal("xác nhận đổi địa chỉ đơn mà không có audit")
	}
	expectOrderAudit(t, []fulfillment.AuditRecord{*r.OrderAudit}, o.ID, "staff")
	if len(log.all()) != 0 {
		t.Fatal("hook lệnh ghi audit khi không có lệnh")
	}
	e.expectAddress(o.ID, s.ID, true)
	if n := e.count("SELECT count(*) FROM fulfillment.address_change_requests WHERE id = $1 AND resolved_by = $2", c.ID, e.actor.UserID); n != 1 {
		t.Fatal("yêu cầu chưa ghi người xác nhận")
	}
	if _, err := e.resolveAddress(c.ID, fulfillment.AddressResolveInput{Result: "confirmed"}); !errors.Is(err, fulfillment.ErrAddressChangeNotAllowed) {
		t.Fatalf("xác nhận lần hai: %v, muốn ErrAddressChangeNotAllowed", err)
	}
	if _, err := e.resolveAddress(uuid.New(), fulfillment.AddressResolveInput{Result: "confirmed"}); !errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("yêu cầu lạ: %v, muốn ErrNotFound", err)
	}
}

// Nhân viên báo hãng không nhận (manual rejected) → rejected, địa chỉ cũ, không audit đơn.
func TestAddress_ManualRejected(t *testing.T) {
	e, _ := newAddressEnv(t, "")
	o, _, s := e.shipped(1)
	c := e.mustRequestAddress(s.ID)
	r, err := e.resolveAddress(c.ID, fulfillment.AddressResolveInput{Result: "rejected"})
	if err != nil || r.Change.Status != "rejected" || r.OrderAudit != nil {
		t.Fatalf("từ chối = %+v %v", r, err)
	}
	e.expectAddress(o.ID, s.ID, false)
}

// Hãng từ chối (UpdateRejected) → rejected, địa chỉ cũ, không audit; hãng chắc chưa nhận (not_sent, không tự gửi lại)
// → lệnh dead → rejected. Sau đó tạo được yêu cầu mới.
func TestAddress_CarrierRejects(t *testing.T) {
	for _, mode := range []string{fake.UpdateModeRejected, fake.UpdateModeNotSent} {
		t.Run(mode, func(t *testing.T) {
			e, log := newAddressEnv(t, mode)
			o, _, s := e.shipped(1)
			c := e.mustRequestAddress(s.ID)
			e.drain()
			if st, _ := e.changeStatus(c.ID); st != "rejected" {
				t.Fatalf("yêu cầu = %s, muốn rejected", st)
			}
			e.expectAddress(o.ID, s.ID, false)
			if len(log.all()) != 0 || e.carrier.Calls("update_address") != 1 {
				t.Fatalf("audit %d, gọi hãng %d", len(log.all()), e.carrier.Calls("update_address"))
			}
			if _, err := e.requestAddress(s.ID, fulfillment.AddressChangeInput{Address: newAddr()}); err != nil {
				t.Fatalf("yêu cầu mới sau khi bị từ chối: %v", err)
			}
		})
	}
}

// Hãng nhận yêu cầu nhưng chưa trả lời chắc (UpdatePending) → sent, địa chỉ cũ; tra lại không biết được → nhân viên
// xác nhận confirmed (lệnh done đã bị dọn khỏi platform.jobs) → địa chỉ mới.
func TestAddress_PendingThenStaff(t *testing.T) {
	e, _ := newAddressEnv(t, fake.UpdateModePending)
	o, _, s := e.shipped(1)
	c := e.mustRequestAddress(s.ID)
	e.drain()
	if st, _ := e.changeStatus(c.ID); st != "sent" {
		t.Fatalf("yêu cầu = %s, muốn sent", st)
	}
	e.expectAddress(o.ID, s.ID, false)
	if _, err := e.requestAddress(s.ID, fulfillment.AddressChangeInput{Address: newAddr()}); !errors.Is(err, fulfillment.ErrAddressChangeOpen) {
		t.Fatalf("yêu cầu thứ hai khi còn sent: %v, muốn ErrAddressChangeOpen", err)
	}
	// Lệnh đã done bị dọn khỏi platform.jobs (DeleteFinishedJobs sau 7 ngày): vẫn xác nhận được.
	e.ownerExec("DELETE FROM platform.jobs WHERE dedupe_key = $1", "address:"+c.ID.String())
	if _, err := e.resolveAddress(c.ID, fulfillment.AddressResolveInput{Result: "confirmed"}); err != nil {
		t.Fatalf("xác nhận sau khi lệnh bị dọn: %v", err)
	}
	e.expectAddress(o.ID, s.ID, true)
}

// Hãng nhận rồi quá giờ → lệnh in_doubt → unknown; tra lại (Looker) không biết → vẫn unknown, không gọi hãng lần hai;
// nhân viên xác nhận rejected → rejected, lệnh bị bỏ (expired), địa chỉ cũ.
func TestAddress_InDoubtThenStaff(t *testing.T) {
	e, _ := newAddressEnv(t, fake.UpdateModeTimeoutAfterSend)
	o, _, s := e.shipped(1)
	c := e.mustRequestAddress(s.ID)
	e.drain()
	if st, _ := e.changeStatus(c.ID); st != "unknown" {
		t.Fatalf("yêu cầu = %s, muốn unknown", st)
	}
	if done, _, still := e.resolveInDoubt(); done != 0 || still != 1 {
		t.Fatalf("tra lại: done %d, còn %d", done, still)
	}
	e.drain()
	if e.carrier.Calls("update_address") != 1 {
		t.Fatalf("gọi hãng %d lần, muốn 1", e.carrier.Calls("update_address"))
	}
	if _, err := e.resolveAddress(c.ID, fulfillment.AddressResolveInput{Result: "rejected"}); err != nil {
		t.Fatalf("từ chối: %v", err)
	}
	if n := e.count("SELECT count(*) FROM platform.jobs WHERE dedupe_key = $1 AND status = 'expired'", "address:"+c.ID.String()); n != 1 {
		t.Fatalf("lệnh bị bỏ = %d, muốn 1", n)
	}
	e.expectAddress(o.ID, s.ID, false)
}

// Một yêu cầu mở mỗi vận đơn: thứ hai → ErrAddressChangeOpen; hai yêu cầu song song → đúng một được nhận.
func TestAddress_OneOpen(t *testing.T) {
	e, _ := newAddressEnv(t, "")
	_, _, s := e.shipped(1)
	e.mustRequestAddress(s.ID)
	if _, err := e.requestAddress(s.ID, fulfillment.AddressChangeInput{Address: newAddr()}); !errors.Is(err, fulfillment.ErrAddressChangeOpen) {
		t.Fatalf("yêu cầu thứ hai: %v, muốn ErrAddressChangeOpen", err)
	}
	_, _, s2 := e.shipped(1)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = e.requestAddress(s2.ID, fulfillment.AddressChangeInput{Address: newAddr()})
		})
	}
	wg.Wait()
	ok, open := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, fulfillment.ErrAddressChangeOpen):
			open++
		default:
			t.Fatalf("song song: %v", err)
		}
	}
	if ok != 1 || open != 1 {
		t.Fatalf("song song: %d nhận, %d ErrAddressChangeOpen", ok, open)
	}
}

// Vận đơn đã giao (delivered), đang tạo → ErrAddressChangeNotAllowed; vận đơn đang giao (shipping) → nhận; địa chỉ thiếu
// xã → order.ErrAddressIncomplete; xã không có trong danh mục → lỗi, không yêu cầu; Authorize từ chối → lỗi của nó;
// vận đơn lạ → ErrNotFound.
func TestAddress_Refusals(t *testing.T) {
	e, _ := newAddressEnv(t, "")
	_, _, s := e.shipped(1)
	e.carrier.Advance(s.TrackingCode, fake.RawDelivered)
	e.syncNow(s.ID)
	if _, err := e.requestAddress(s.ID, fulfillment.AddressChangeInput{Address: newAddr()}); !errors.Is(err, fulfillment.ErrAddressChangeNotAllowed) {
		t.Fatalf("đã giao: %v, muốn ErrAddressChangeNotAllowed", err)
	}
	o2, _ := e.order(1)
	creating := e.mustCreateShipment(o2.ID)
	if _, err := e.requestAddress(creating.ID, fulfillment.AddressChangeInput{Address: newAddr()}); !errors.Is(err, fulfillment.ErrAddressChangeNotAllowed) {
		t.Fatalf("đang tạo: %v, muốn ErrAddressChangeNotAllowed", err)
	}
	e.drain()
	_, _, s3 := e.shipped(1)
	e.carrier.Advance(s3.TrackingCode, fake.RawPickedUp)
	e.syncNow(s3.ID)
	bad := newAddr()
	bad.WardCode = ""
	if _, err := e.requestAddress(s3.ID, fulfillment.AddressChangeInput{Address: bad}); !errors.Is(err, order.ErrAddressIncomplete) {
		t.Fatalf("thiếu xã: %v, muốn order.ErrAddressIncomplete", err)
	}
	bad.WardCode = "T09999"
	if _, err := e.requestAddress(s3.ID, fulfillment.AddressChangeInput{Address: bad}); err == nil {
		t.Fatal("xã không có trong danh mục mà vẫn nhận")
	}
	errScope := errors.New("không có quyền ở kho")
	if _, err := e.requestAddress(s3.ID, fulfillment.AddressChangeInput{
		Address: newAddr(), Authorize: func(uuid.UUID) error { return errScope },
	}); !errors.Is(err, errScope) {
		t.Fatalf("Authorize: %v", err)
	}
	if _, err := e.requestAddress(uuid.New(), fulfillment.AddressChangeInput{Address: newAddr()}); !errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("vận đơn lạ: %v", err)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.address_change_requests"); n != 0 {
		t.Fatalf("%d yêu cầu sau các lần bị từ chối", n)
	}
	if c, err := e.requestAddress(s3.ID, fulfillment.AddressChangeInput{Address: newAddr()}); err != nil || c.Status != "pending" {
		t.Fatalf("đang giao: %+v %v", c, err)
	}
}

// Hãng có bảng địa giới mà địa chỉ mới chưa khớp carrier_locations → ErrAddressUnmapped, không yêu cầu.
func TestAddress_Unmapped(t *testing.T) {
	e, _ := newAddressEnv(t, fake.UpdateModeDone)
	_, _, s := e.shipped(1)
	e.svc = fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(),
		Carriers: mustRegistry(t, locCarrier{Carrier: e.carrier}),
	}).Service
	if _, err := e.requestAddress(s.ID, fulfillment.AddressChangeInput{Address: newAddr()}); !errors.Is(err, fulfillment.ErrAddressUnmapped) {
		t.Fatalf("chưa khớp: %v, muốn ErrAddressUnmapped", err)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.address_change_requests"); n != 0 {
		t.Fatalf("%d yêu cầu", n)
	}
}

// Lệnh sửa địa chỉ: không tự gửi lại khi chắc chưa gửi; Module.AddressCommand đòi hàm audit, không có hãng → nil.
func TestUpdateAddressCommand_Policy(t *testing.T) {
	e, log := newAddressEnv(t, fake.UpdateModeDone)
	if c := fulfillment.NewUpdateAddressCommand(e.svc, log.record); c.AutoRetryNotSent() || c.Kind().Name != "command.shipment.update_address" {
		t.Fatalf("lệnh = %v %s", c.AutoRetryNotSent(), c.Kind().Name)
	}
	m := fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(),
		Carriers: mustRegistry(t, e.carrier),
	})
	if _, err := m.AddressCommand(nil); err == nil {
		t.Fatal("AddressCommand(nil) không lỗi")
	}
	if h, err := m.AddressCommand(log.record); err != nil || h == nil {
		t.Fatalf("AddressCommand = %v %v", h, err)
	}
	empty := fulfillment.New(fulfillment.Deps{DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry()})
	if h, err := empty.AddressCommand(log.record); err != nil || h != nil {
		t.Fatalf("không có hãng: %v %v", h, err)
	}
}
