package fulfillment_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/carrier"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/events"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
)

// Task 448: danh mục hãng nhập tay (carrier.ManualKeys) cho tạo tài khoản mode manual của hãng thật chưa nối API, cả
// khi bộ hãng rỗng (production: không hãng giả); mode api vẫn đòi adapter như cũ; hãng ngoài danh mục (gõ sai, hãng giả
// ở production) vẫn ErrCarrierNotSupported. Bộ adapter dựng như cmd/crm: production rỗng, dev carrier.All có hãng giả.
func TestManualCatalog_CreateAccount(t *testing.T) {
	e := newShipEnv(t)
	dev := config.Config{Env: "development", FakeCarrierURL: "http://127.0.0.1:1"}
	cases := []struct {
		name   string
		reg    *fulfillment.Registry
		ok     []string // "khóa/mode" tạo được
		refuse []string // "khóa/mode" bị ErrCarrierNotSupported
	}{
		{
			name: "production", reg: mustRegistry(t),
			ok:     []string{"spx/manual", "vtp/manual", "best/manual", "self/manual"},
			refuse: []string{"spx/api", "vtp/api", "best/api", "self/api", "fake/manual", "fake/api", "ghn/manual", "/manual"},
		},
		{
			name: "dev", reg: mustRegistry(t, carrier.All(dev)...),
			ok:     []string{"spx/manual", "vtp/manual", "best/manual", "self/manual", "fake/manual", "fake/api", "spx/api", "best/api"},
			refuse: []string{"self/api", "ghn/manual", "ghn/api"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := fulfillment.New(fulfillment.Deps{
				DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(), Bus: &realtimetest.RecordingBus{},
				Carriers: c.reg, ManualCarriers: carrier.ManualKeys(),
			}).Service
			for _, km := range c.ok {
				expectCreated(t, e, svc, km)
			}
			for _, km := range c.refuse {
				key, mode := splitKeyMode(t, km)
				if _, err := createVia(e, svc, key, mode); !errors.Is(err, fulfillment.ErrCarrierNotSupported) {
					t.Errorf("%s: %v, muốn ErrCarrierNotSupported", km, err)
				}
			}
		})
	}
	if n := e.commandJobs(); n != 0 {
		t.Fatalf("tạo tài khoản tay ghi %d việc command, muốn 0", n)
	}
}

// Task 448, cấu hình production (bộ hãng rỗng, danh mục nối): vận đơn của tài khoản SPX, VTP, BEST nhập tay đi đúng
// nhánh tay của Task 106 — dán mã → awaiting_pickup, create_state manual; hủy → cancel_requested và việc manual_cancel
// cho nhân viên; ghi tay hãng đã hủy → cancelled; "Tra lại" bị từ chối (ErrCarrierNotSupported) — không lệnh gửi, hủy,
// tra nào được ghi (platform.jobs mode command vẫn 0).
func TestManualCatalog_ProductionShipmentFlow(t *testing.T) {
	e := newShipEnv(t)
	svc := fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Events: events.NewRegistry(), Bus: &realtimetest.RecordingBus{},
		Carriers: mustRegistry(t), ManualCarriers: carrier.ManualKeys(),
	}).Service
	e.svc = svc
	for _, key := range []string{"spx", "vtp", "best"} {
		t.Run(key, func(t *testing.T) {
			acc, err := createVia(e, svc, key, fulfillment.ModeManual)
			if err != nil {
				t.Fatalf("tạo tài khoản tay: %v", err)
			}
			sv := shipManual(t, e, acc.ID, key)
			if err := e.app(func(tx pgx.Tx) error {
				_, err := svc.SyncShipment(t.Context(), tx, sv.ID, e.actor)
				return err //nolint:wrapcheck // test
			}); !errors.Is(err, fulfillment.ErrCarrierNotSupported) {
				t.Fatalf("tra lại vận đơn tay: %v, muốn ErrCarrierNotSupported", err)
			}
			cancelManual(t, e, sv.ID)
			if n := e.commandJobs(); n != 0 {
				t.Fatalf("việc command = %d, muốn 0", n)
			}
		})
	}
}

// expectCreated tạo tài khoản "khóa/mode" km bằng svc và đòi trạng thái đúng (manual: active; api: draft), không khóa.
func expectCreated(t *testing.T, e *shipEnv, svc *fulfillment.Service, km string) {
	t.Helper()
	key, mode := splitKeyMode(t, km)
	v, err := createVia(e, svc, key, mode)
	if err != nil {
		t.Errorf("%s: %v, muốn tạo được", km, err)
		return
	}
	want := fulfillment.AccountDraft
	if mode == fulfillment.ModeManual {
		want = fulfillment.AccountActive
	}
	if v.Status != want || v.Mode != mode || v.CarrierKey != key || v.HasCredentials {
		t.Errorf("%s: tài khoản = %+v, muốn %s, không khóa", km, v, want)
	}
}

// shipManual gửi một đơn 1 món bằng tài khoản tay acc với mã dán tay; đòi awaiting_pickup, create_state manual.
func shipManual(t *testing.T, e *shipEnv, acc uuid.UUID, key string) fulfillment.ShipmentView {
	t.Helper()
	o, _ := e.order(1)
	tracking := "TAY-" + key + "-001"
	sv, err := e.ship(fulfillment.ShipInput{OrderID: o.ID, CarrierAccountID: acc, TrackingCode: tracking})
	if err != nil {
		t.Fatalf("tạo vận đơn tay: %v", err)
	}
	if sv.Status != "awaiting_pickup" || sv.CreateState != "manual" || sv.Mode != "manual" ||
		sv.CarrierKey != key || sv.TrackingCode != tracking {
		t.Fatalf("vận đơn tay = %+v", sv)
	}
	return sv
}

// cancelManual hủy vận đơn tay id: cancel_requested và việc manual_cancel; ghi tay hãng đã hủy → cancelled.
func cancelManual(t *testing.T, e *shipEnv, id uuid.UUID) {
	t.Helper()
	got, err := e.cancelShipment(id, false)
	if err != nil {
		t.Fatalf("hủy vận đơn tay: %v", err)
	}
	if n := e.openAnomalies(id, "manual_cancel"); got.Status != "cancel_requested" || n != 1 {
		t.Fatalf("sau hủy = %+v, việc manual_cancel %d", got, n)
	}
	got = e.mustSetManual(id, fulfillment.ManualStatusInput{
		To: fulfillment.StatusCancelled, At: time.Now().Add(-time.Minute), Reason: "Đã hủy trên trang hãng",
	})
	if got.Status != "cancelled" {
		t.Fatalf("sau ghi tay đã hủy = %+v", got)
	}
}

func splitKeyMode(t *testing.T, km string) (key, mode string) {
	t.Helper()
	key, mode, ok := strings.Cut(km, "/")
	if !ok {
		t.Fatalf("ca %q thiếu /", km)
	}
	return key, mode
}

// createVia tạo tài khoản key/mode (không khóa, không mặc định, mã ở hãng local:<id>) bằng svc cho người bán của test ở
// kho MAIN, trong tx crm_app.
func createVia(e *shipEnv, svc *fulfillment.Service, key, mode string) (fulfillment.AccountView, error) {
	seller := e.seller
	in := fulfillment.AccountInput{
		SellerID: &seller, CarrierKey: key, Mode: mode, Name: "Tài khoản " + key + " " + uuid.NewString()[:8],
		WarehouseID: e.shop.Warehouse,
	}
	var v fulfillment.AccountView
	err := e.app(func(tx pgx.Tx) (err error) {
		v, err = svc.CreateAccount(e.t.Context(), tx, in, e.actor)
		return err //nolint:wrapcheck // test
	})
	return v, err
}
