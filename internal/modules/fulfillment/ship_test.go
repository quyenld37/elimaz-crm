package fulfillment_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/carriers/fake"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shiptest"
)

// Test của gửi hãng (kế hoạch M3, Task M3-12): VC16, Q-5, VC15, báo phí, địa giới, chết giữa chừng.

func (e *shipEnv) orderActor() order.Actor {
	return order.Actor{TenantID: e.tenant, UserID: e.actor.UserID, Type: "staff"}
}

// ship gọi Service.Ship trong tx crm_app.
func (e *shipEnv) ship(in fulfillment.ShipInput) (fulfillment.ShipmentView, error) {
	var v fulfillment.ShipmentView
	err := e.app(func(tx pgx.Tx) (err error) {
		v, err = e.svc.Ship(e.t.Context(), tx, in, e.orderActor())
		return err //nolint:wrapcheck // test
	})
	return v, err
}

func (e *shipEnv) resend(id uuid.UUID) (fulfillment.ShipmentView, error) {
	var v fulfillment.ShipmentView
	err := e.app(func(tx pgx.Tx) (err error) {
		v, err = e.svc.Resend(e.t.Context(), tx, id, e.orderActor(), nil)
		return err //nolint:wrapcheck // test
	})
	return v, err
}

func (e *shipEnv) resolveCreate(id uuid.UUID, in fulfillment.ResolveInput) (fulfillment.ShipmentView, error) {
	var v fulfillment.ShipmentView
	err := e.app(func(tx pgx.Tx) (err error) {
		v, err = e.svc.ResolveCreate(e.t.Context(), tx, id, in, e.orderActor(), nil)
		return err //nolint:wrapcheck // test
	})
	return v, err
}

func (e *shipEnv) view(id uuid.UUID) fulfillment.ShipmentView {
	e.t.Helper()
	return readAs(e, func(ctx context.Context, tx pgx.Tx) (fulfillment.ShipmentView, error) {
		return e.svc.ShipmentView(ctx, tx, id)
	})
}

func (e *shipEnv) resolveInDoubt() (done, requeued, still int) {
	e.t.Helper()
	done, requeued, still, err := commands.ResolveInDoubt(e.t.Context(), e.staff.App, e.cmds, 50)
	if err != nil {
		e.t.Fatalf("ResolveInDoubt: %v", err)
	}
	return done, requeued, still
}

// openAnomalies đếm việc mở loại kind của vận đơn id.
func (e *shipEnv) openAnomalies(id uuid.UUID, kind string) int {
	e.t.Helper()
	return e.count("SELECT count(*) FROM fulfillment.shipment_anomalies WHERE shipment_id = $1 AND kind = $2 AND status = 'open'", id, kind)
}

// VC16: hãng tạo kiện nhưng trả lời quá giờ → lệnh in_doubt, vận đơn unknown, anomaly in_doubt_create, cần xem; chạy
// worker thêm 3 lần → không gọi tạo lần hai; ResolveInDoubt (hãng có kiện theo mã) → done, vận đơn có mã hãng,
// awaiting_pickup, đúng một kiện ở hãng, việc "không rõ" đóng; giữa lúc tra thấy kiện và lúc điền mã, vận đơn created
// (không "không tạo", không "Gửi lại").
func TestShip_TimeoutAfterCreate(t *testing.T) {
	e := newShipEnv(t)
	_, s := e.createTimedOut()
	v := e.view(s.ID)
	if v.CreateState != "unknown" || !v.NeedsReview || e.openAnomalies(s.ID, "in_doubt_create") != 1 {
		t.Fatalf("sau quá giờ: %s, cần xem %v, anomaly %d", v.CreateState, v.NeedsReview, e.openAnomalies(s.ID, "in_doubt_create"))
	}
	for range 3 {
		if _, err := e.worker.RunOnce(t.Context(), 10); err != nil {
			t.Fatal(err)
		}
	}
	if e.carrier.Calls("create") != 1 {
		t.Fatalf("gọi tạo %d lần, muốn 1", e.carrier.Calls("create"))
	}
	e.carrier.SetMode(fake.ModeOK)
	if d, r, st := e.resolveInDoubt(); d != 1 || r != 0 || st != 0 {
		t.Fatalf("ResolveInDoubt = %d %d %d", d, r, st)
	}
	// Hãng đã xác nhận có kiện, mã hãng chưa điền: created ngay, không còn "không tạo" hay "Gửi lại".
	if v := e.view(s.ID); v.CreateState != "created" || v.Status != "creating" {
		t.Fatalf("sau ResolveInDoubt: %s %s, muốn creating created", v.Status, v.CreateState)
	}
	if _, err := e.resolveCreate(s.ID, fulfillment.ResolveInput{Result: "not_created", Reason: "nhầm"}); !errors.Is(err, fulfillment.ErrCreateInProgress) {
		t.Fatalf("không tạo sau khi hãng xác nhận: %v, muốn ErrCreateInProgress", err)
	}
	if _, err := e.resend(s.ID); !errors.Is(err, fulfillment.ErrCreateInProgress) {
		t.Fatalf("gửi lại sau khi hãng xác nhận: %v, muốn ErrCreateInProgress", err)
	}
	e.drain()
	v = e.view(s.ID)
	if v.Status != "awaiting_pickup" || v.TrackingCode != e.carrier.Tracking(s.Code) || v.CreateState != "created" ||
		v.NeedsReview || v.Label != "Chờ thu gom" {
		t.Fatalf("sau tra lại: %+v", v)
	}
	if e.carrier.Calls("create") != 1 || !e.carrier.Has(s.Code) || e.carrier.Parcels() != 1 {
		t.Fatalf("hãng: gọi tạo %d, có kiện %v, %d kiện", e.carrier.Calls("create"), e.carrier.Has(s.Code), e.carrier.Parcels())
	}
	if e.openAnomalies(s.ID, "in_doubt_create") != 0 {
		t.Fatal("việc không rõ còn mở")
	}
}

// Q-5: quá giờ mà hãng chưa tạo; tra lại nói "không có" → lệnh dead confirmed_not_sent (không tự gửi lại), vận đơn
// not_sent; "Gửi lại" → lần gửi 2 (lệnh mới), hãng nhận, awaiting_pickup; gọi tạo đúng 2 lần.
func TestShip_ConfirmedNotSentThenResend(t *testing.T) {
	e := newShipEnv(t)
	s := e.timedOutShipment()
	e.carrier.SetMode(fake.ModeOK)
	if d, r, st := e.resolveInDoubt(); d != 0 || r != 1 || st != 0 {
		t.Fatalf("ResolveInDoubt = %d %d %d", d, r, st)
	}
	jobID, st := e.createJob(s.ID)
	if st != queue.StatusDead || e.count("SELECT count(*) FROM platform.jobs WHERE id = $1 AND last_error = 'confirmed_not_sent'", jobID) != 1 {
		t.Fatalf("lệnh = %s, muốn dead confirmed_not_sent", st)
	}
	e.drain()
	if v := e.view(s.ID); v.CreateState != "not_sent" || v.Status != "creating" || e.carrier.Calls("create") != 1 || e.inDoubtMarked(s.ID) {
		t.Fatalf("sau xác nhận chưa gửi: %s %s, gọi %d, dấu %v", v.CreateState, v.Status, e.carrier.Calls("create"), e.inDoubtMarked(s.ID))
	}
	before := e.view(s.ID).Version
	v, err := e.resend(s.ID)
	if err != nil || v.CreateState != "pending" || v.Version != before+1 {
		t.Fatalf("gửi lại: %+v %v", v, err)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'fulfillment.create_shipment' AND dedupe_key = $1`,
		"create:"+s.ID.String()+":2"); n != 1 {
		t.Fatalf("lệnh lần 2 = %d, muốn 1", n)
	}
	if _, err := e.resend(s.ID); !errors.Is(err, fulfillment.ErrCreateInProgress) {
		t.Fatalf("gửi lại khi đang chờ: %v, muốn ErrCreateInProgress", err)
	}
	e.drain()
	if v := e.view(s.ID); v.Status != "awaiting_pickup" || v.TrackingCode == "" || e.carrier.Calls("create") != 2 {
		t.Fatalf("sau gửi lại: %s %q, gọi %d", v.Status, v.TrackingCode, e.carrier.Calls("create"))
	}
	if _, err := e.resend(s.ID); !errors.Is(err, fulfillment.ErrCreateInProgress) {
		t.Fatalf("gửi lại vận đơn đã tạo: %v, muốn ErrCreateInProgress", err)
	}
}

// Hãng không tra được (Inconclusive) → vẫn in_doubt; nhân viên xử lý created FK7777 → awaiting_pickup mã FK7777, lệnh
// done, việc "không rõ" đóng; xử lý lần hai → ErrCreateInProgress. Vận đơn khác: not_created → not_sent, lệnh expired
// (bỏ), gửi lại được. Dữ liệu sai (thiếu mã, thiếu lý do) → ErrShipInvalid.
func TestShip_ResolveCreateByStaff(t *testing.T) {
	e := newShipEnv(t)
	s := e.timedOutShipment()
	e.carrier.SetLookupInconclusive(true)
	if d, r, st := e.resolveInDoubt(); d != 0 || r != 0 || st != 1 {
		t.Fatalf("ResolveInDoubt = %d %d %d, muốn còn in_doubt", d, r, st)
	}
	if _, st := e.createJob(s.ID); st != queue.StatusInDoubt {
		t.Fatalf("lệnh = %s, muốn in_doubt", st)
	}
	for _, bad := range []fulfillment.ResolveInput{
		{Result: "created", Reason: "x"}, {Result: "created", TrackingCode: "FK1"}, {Result: "maybe", Reason: "x"},
	} {
		if _, err := e.resolveCreate(s.ID, bad); !errors.Is(err, fulfillment.ErrShipInvalid) {
			t.Fatalf("%+v: %v, muốn ErrShipInvalid", bad, err)
		}
	}
	v, err := e.resolveCreate(s.ID, fulfillment.ResolveInput{Result: "created", TrackingCode: "FK7777", Reason: "thấy trên trang hãng"})
	if err != nil || v.Status != "awaiting_pickup" || v.TrackingCode != "FK7777" || v.CreateState != "created" || v.NeedsReview {
		t.Fatalf("xử lý created: %+v %v", v, err)
	}
	if _, st := e.createJob(s.ID); st != "done" || e.openAnomalies(s.ID, "in_doubt_create") != 0 {
		t.Fatalf("lệnh = %s, anomaly mở %d", st, e.openAnomalies(s.ID, "in_doubt_create"))
	}
	if _, err := e.resolveCreate(s.ID, fulfillment.ResolveInput{Result: "created", TrackingCode: "FK7777", Reason: "x"}); !errors.Is(err, fulfillment.ErrCreateInProgress) {
		t.Fatalf("xử lý lần hai: %v, muốn ErrCreateInProgress", err)
	}

	o2, _ := e.order(1)
	s2 := e.mustCreateShipment(o2.ID)
	e.carrier.SetLookupInconclusive(false)
	e.carrier.SetMode(fake.ModeTimeoutNoCreate)
	e.drain()
	if _, err := e.resolveCreate(s2.ID, fulfillment.ResolveInput{Result: "created", TrackingCode: "FK7777", Reason: "nhầm"}); !errors.Is(err, fulfillment.ErrTrackingCodeConflict) {
		t.Fatalf("mã đã thuộc vận đơn khác: %v, muốn ErrTrackingCodeConflict", err)
	}
	v, err = e.resolveCreate(s2.ID, fulfillment.ResolveInput{Result: "not_created", Reason: "không có trên trang hãng"})
	if err != nil || v.CreateState != "not_sent" || v.Status != "creating" || v.NeedsReview {
		t.Fatalf("xử lý not_created: %+v %v", v, err)
	}
	if _, st := e.createJob(s2.ID); st != queue.StatusExpired {
		t.Fatalf("lệnh = %s, muốn expired (bỏ)", st)
	}
	e.carrier.SetMode(fake.ModeOK)
	if _, err := e.resend(s2.ID); err != nil {
		t.Fatalf("gửi lại sau not_created: %v", err)
	}
	e.drain()
	if v := e.view(s2.ID); v.Status != "awaiting_pickup" {
		t.Fatalf("sau gửi lại: %s", v.Status)
	}
}

// Hãng từ chối → vận đơn cancelled create_rejected, đơn không còn vận đơn hiện hành; gửi lại bằng tài khoản khác (cùng
// người bán, cùng kho) → được, hãng nhận. Mã hãng tới muộn cho vận đơn đã hủy → không gắn, tracking_conflict.
func TestShip_RejectedThenOtherAccount(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	e.carrier.SetMode(fake.ModeReject)
	e.drain()
	v := e.view(s.ID)
	if v.Status != "cancelled" || v.CancelKind != "create_rejected" || v.CreateState != "rejected" {
		t.Fatalf("sau từ chối: %+v", v)
	}
	if got := e.getOrder(o.ID); got.CurrentShipmentID != nil || got.Status != "confirmed" {
		t.Fatalf("đơn: %v %s", got.CurrentShipmentID, got.Status)
	}
	// Mã hãng tới muộn cho vận đơn đã hủy: không gắn, giữ cancelled rejected, mở tracking_conflict, cần xem.
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		r, err := e.svc.AttachTrackingCode(t.Context(), tx, s.ID, "LATE01", nil)
		if err == nil && !r.Conflict {
			t.Errorf("mã muộn cho vận đơn đã hủy: %+v, muốn Conflict", r)
		}
		return err //nolint:wrapcheck // test
	})
	if v := e.view(s.ID); v.Status != "cancelled" || v.CreateState != "rejected" || v.TrackingCode != "" ||
		!v.NeedsReview || e.openAnomalies(s.ID, "tracking_conflict") != 1 {
		t.Fatalf("sau mã muộn: %+v", v)
	}
	var other uuid.UUID
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) (err error) {
		other, err = fulfillment.EnsureCarrierAccount(t.Context(), tx, fulfillment.SeedAccountInput{
			SellerID: e.seller, WarehouseID: e.shop.Warehouse, CarrierKey: shiptest.CarrierKey, Name: "Hãng giả 3",
			ExternalAccountRef: "fake-3",
		})
		return err //nolint:wrapcheck // test
	})
	e.carrier.SetMode(fake.ModeOK)
	v2, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: other})
	if err != nil || v2.CarrierAccountID != other {
		t.Fatalf("gửi bằng tài khoản khác: %+v %v", v2, err)
	}
	e.drain()
	if v := e.view(v2.ID); v.Status != "awaiting_pickup" {
		t.Fatalf("vận đơn mới = %s", v.Status)
	}
}

// VC15: tài khoản auth_failed → ErrAccountUnavailable, không vận đơn; tài khoản bị thu hồi khi lệnh đang chờ → không gọi
// hãng, lệnh dead lý do account_unavailable, vận đơn cancelled create_rejected. Hãng chưa hỗ trợ tạo (Task 94) →
// rejected not_supported (chắc chưa gửi), không in_doubt.
func TestShip_AccountUnavailable(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(1)
	e.ownerExec("UPDATE fulfillment.carrier_accounts SET status = 'auth_failed' WHERE id = $1", e.account)
	if _, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: e.account}); !errors.Is(err, fulfillment.ErrAccountUnavailable) {
		t.Fatalf("auth_failed: %v, muốn ErrAccountUnavailable", err)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE order_id = $1", o.ID); n != 0 {
		t.Fatalf("%d vận đơn, muốn 0", n)
	}
	e.ownerExec("UPDATE fulfillment.carrier_accounts SET status = 'active' WHERE id = $1", e.account)
	s := e.mustCreateShipment(o.ID)
	e.ownerExec("UPDATE fulfillment.carrier_accounts SET status = 'revoked' WHERE id = $1", e.account)
	e.drain()
	jobID, st := e.createJob(s.ID)
	if st != queue.StatusDead || e.count("SELECT count(*) FROM platform.jobs WHERE id = $1 AND last_error LIKE '%account_unavailable%'", jobID) != 1 {
		t.Fatalf("lệnh = %s, muốn dead account_unavailable", st)
	}
	if v := e.view(s.ID); v.Status != "cancelled" || v.CancelKind != "create_rejected" || e.carrier.Calls("create") != 0 {
		t.Fatalf("vận đơn %s %s, gọi hãng %d", v.Status, v.CancelKind, e.carrier.Calls("create"))
	}
	e.ownerExec("UPDATE fulfillment.carrier_accounts SET status = 'active' WHERE id = $1", e.account)

	un := &unsupportedCarrier{}
	e.useCommands([]commands.Handler{fulfillment.NewCreateCommand(fulfillment.CommandOptions{
		DB: e.staff.App, Adapters: []fulfillment.CarrierAdapter{un}, Events: events.NewRegistry(), Queue: queue.NewQueue(),
		Realtime: &testkit.RecordingBus{},
	})})
	o2, _ := e.order(1)
	s2 := e.mustCreateShipment(o2.ID)
	e.drain()
	if _, st := e.createJob(s2.ID); st != queue.StatusDead {
		t.Fatalf("hãng chưa hỗ trợ: lệnh %s, muốn dead", st)
	}
	if v := e.view(s2.ID); v.Status != "cancelled" || v.CreateState != "rejected" {
		t.Fatalf("hãng chưa hỗ trợ: %s %s", v.Status, v.CreateState)
	}
}

// unsupportedCarrier là hãng khóa "fake" chưa làm tạo vận đơn (mọi khả năng false, như khung best/spx).
type unsupportedCarrier struct{ fulfillment.Unsupported }

func (*unsupportedCarrier) Key() string { return fake.Key }

// Báo phí: còn hạn và khớp → gửi được, vận đơn nhớ quote; hết hạn, địa chỉ đổi sau báo phí, của tài khoản khác, không
// có → ErrQuoteStale, không vận đơn.
func TestShip_Quote(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(2)
	to := fulfillment.Address{Name: "Chị Lan", Line: "12 Lê Lợi, Q1"}
	parcel := fulfillment.Parcel{WeightGrams: fulfillment.DefaultSettings().DefaultWeightGramsPerItem * 2, ItemCount: 2}
	good := fulfillment.InputHash(to, parcel, o.CODAmount, "", fulfillment.Options{})
	moved := fulfillment.InputHash(fulfillment.Address{Line: "99 Hai Bà Trưng"}, parcel, o.CODAmount, "", fulfillment.Options{})
	quote := func(account uuid.UUID, hash []byte, valid string) uuid.UUID {
		var id uuid.UUID
		e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(t.Context(), `INSERT INTO fulfillment.shipping_quotes
				(tenant_id, order_id, request_id, carrier_account_id, carrier_key, status, fee_total, input_hash, valid_until)
				VALUES ($1, $2, gen_random_uuid(), $3, 'fake', 'ok', 30000, $4, now() + $5::interval) RETURNING id`,
				e.tenant, o.ID, account, hash, valid).Scan(&id)
		})
		return id
	}
	for name, id := range map[string]uuid.UUID{
		"hết hạn":          quote(e.account, good, "-1 minute"),
		"địa chỉ đã đổi":   quote(e.account, moved, "1 hour"),
		"tài khoản khác":   quote(e.shop.PartnerAccount, good, "1 hour"),
		"không có báo phí": uuid.New(),
	} {
		if _, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: e.account, QuoteID: &id}); !errors.Is(err, fulfillment.ErrQuoteStale) {
			t.Fatalf("%s: %v, muốn ErrQuoteStale", name, err)
		}
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE order_id = $1", o.ID); n != 0 {
		t.Fatalf("%d vận đơn, muốn 0", n)
	}
	ok := quote(e.account, good, "1 hour")
	v, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: e.account, QuoteID: &ok})
	if err != nil {
		t.Fatalf("báo phí còn khớp: %v", err)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE id = $1 AND quote_id = $2 AND weight_grams = $3",
		v.ID, ok, parcel.WeightGrams); n != 1 {
		t.Fatal("vận đơn không nhớ báo phí, cân nặng mặc định")
	}
}

// locCarrier là hãng giả có bảng địa giới riêng (Capabilities.Locations): địa chỉ phải khớp carrier_locations.
type locCarrier struct{ *fake.Carrier }

func (locCarrier) Capabilities() fulfillment.Capabilities {
	return fulfillment.Capabilities{Create: true, LookupByRef: true, Locations: true}
}

// Hãng có bảng địa giới mà địa chỉ giao chưa khớp → ErrAddressUnmapped trước khi gọi mạng, không vận đơn.
func TestShip_AddressUnmapped(t *testing.T) {
	e := newShipEnv(t)
	lc := locCarrier{Carrier: e.carrier}
	e.svc = fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(), Carriers: mustRegistry(t, lc),
	}).Service
	o, _ := e.order(1)
	if _, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: e.account}); !errors.Is(err, fulfillment.ErrAddressUnmapped) {
		t.Fatalf("địa chỉ chưa khớp: %v, muốn ErrAddressUnmapped", err)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE order_id = $1", o.ID); n != 0 || e.carrier.Calls("create") != 0 {
		t.Fatalf("%d vận đơn, gọi hãng %d", n, e.carrier.Calls("create"))
	}
}

// Đơn không confirmed → ErrOrderNotShippable; đơn lạ → ErrOrderNotFound; Authorize từ chối kho của đơn → lỗi của nó,
// không vận đơn; mã, phí nhập tay cho tài khoản api → ErrShipInvalid; database chặn vận đơn người bán khác tài khoản.
func TestShip_Refusals(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(1)
	errScope := errors.New("không có quyền ở kho")
	var seen uuid.UUID
	deny := func(wh uuid.UUID) error { seen = wh; return errScope }
	if _, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: e.account, Authorize: deny}); !errors.Is(err, errScope) || seen != o.WarehouseID {
		t.Fatalf("Authorize: %v, kho %s muốn %s", err, seen, o.WarehouseID)
	}
	if _, err := e.ship(fulfillment.ShipInput{OrderID: uuid.New(), CarrierAccountID: e.account}); !errors.Is(err, fulfillment.ErrOrderNotFound) {
		t.Fatalf("đơn lạ: %v", err)
	}
	if _, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: e.account, TrackingCode: "X1"}); !errors.Is(err, fulfillment.ErrShipInvalid) {
		t.Fatalf("mã nhập tay: %v", err)
	}
	if _, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: uuid.New()}); !errors.Is(err, fulfillment.ErrAccountNotFound) {
		t.Fatalf("tài khoản lạ: %v", err)
	}
	e.ownerExec("UPDATE ord.orders SET status = 'cancelled', cancelled_at = now() WHERE id = $1", o.ID)
	if _, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: e.account}); !errors.Is(err, fulfillment.ErrOrderNotShippable) {
		t.Fatalf("đơn đã hủy: %v, muốn ErrOrderNotShippable", err)
	}
	if n := e.count("SELECT count(*) FROM fulfillment.shipments WHERE order_id = $1", o.ID); n != 0 {
		t.Fatalf("%d vận đơn, muốn 0", n)
	}
	o2, _ := e.order(1)
	err := e.appExec(`INSERT INTO fulfillment.shipments (tenant_id, seller_id, code, order_id, carrier_key, carrier_account_id,
		status, cod_amount) VALUES ($1, $2, 'E261121ABCDEF', $3, 'fake', $4, 'creating', 0)`,
		e.tenant, e.seller, o2.ID, e.shop.PartnerAccount)
	if err == nil {
		t.Fatal("database cho vận đơn của người bán này dùng tài khoản của người bán kia")
	}
}

// Worker chết sau khi Execute đã gọi hãng (giữ chỗ hết hạn trong lúc chờ trả lời, worker khác quét) → in_doubt, vận
// đơn unknown; kết quả muộn của lần chạy cũ không được ghi; chạy worker thêm → không gọi tạo lần hai.
func TestShip_WorkerDiesAfterSend(t *testing.T) {
	e := newShipEnv(t)
	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	sweeper := queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{
		WorkerID: "sweeper/" + uuid.NewString(), Commands: commands.NewRunner(e.staff.App, e.cmds),
	})
	e.carrier.SetCreateHook(func() {
		e.carrier.SetCreateHook(nil)
		e.ownerExec(`UPDATE platform.jobs SET lease_until = now() - interval '1 second'
			WHERE kind = 'fulfillment.create_shipment' AND payload->>'shipmentId' = $1`, s.ID.String())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := sweeper.RunOnce(ctx, 1); err != nil {
			t.Errorf("worker quét: %v", err)
		}
	})
	e.drain()
	if _, st := e.createJob(s.ID); st != queue.StatusInDoubt {
		t.Fatalf("lệnh = %s, muốn in_doubt", st)
	}
	if v := e.view(s.ID); v.CreateState != "unknown" || v.Status != "creating" || v.TrackingCode != "" {
		t.Fatalf("vận đơn = %s %s %q", v.CreateState, v.Status, v.TrackingCode)
	}
	e.drain()
	if e.carrier.Calls("create") != 1 {
		t.Fatalf("gọi tạo %d lần, muốn 1", e.carrier.Calls("create"))
	}
	if !strings.HasPrefix(e.carrier.Tracking(s.Code), "FK") {
		t.Fatal("hãng phải có kiện của lần gửi đầu")
	}
}

// timedOutShipment: đơn 1 món, vận đơn gửi hãng mà hãng quá giờ chưa tạo (lệnh in_doubt sau drain).
func (e *shipEnv) timedOutShipment() fulfillment.Shipment {
	e.t.Helper()
	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	e.carrier.SetMode(fake.ModeTimeoutNoCreate)
	e.drain()
	return s
}
