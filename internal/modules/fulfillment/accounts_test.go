package fulfillment_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/adapters/carrier/manual"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/commands"
	"elimaz.com.vn/crm/internal/platform/fieldcrypt"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/tools/fakecarrier/server"
)

const (
	accSecret = "accounts-test-fake-secret-0123456789abcdef"
	liveToken = "tk-live-123"
)

// recNotifier ghi lại khóa của mọi cảnh báo.
type recNotifier struct {
	mu   sync.Mutex
	keys []string
}

func (n *recNotifier) Notify(_ context.Context, key, _, _ string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.keys = append(n.keys, key)
	return nil
}

func (n *recNotifier) has(key string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, k := range n.keys {
		if k == key {
			return true
		}
	}
	return false
}

// accEnv: shipEnv (tenant A, kho MAIN, hai người bán) cộng máy chủ hãng giả thật (tools/fakecarrier/server qua
// httptest), Service với bộ adapter {fake, self}, khóa mã hóa spec và worker chạy lệnh của module + lệnh cảnh báo.
type accEnv struct {
	*shipEnv
	hook  *checkHook
	srv   *server.Server
	reg   *fulfillment.Registry
	svc   *fulfillment.Service
	notes *recNotifier
}

// checkHook là hãng giả fake có chỗ cắm: before (nếu đặt) chạy một lần ngay trước lần gọi CheckConnection kế tiếp,
// khi lệnh kiểm đã đọc tài khoản xong (để ghi khóa mới giữa lúc kiểm và lúc ghi kết quả).
type checkHook struct {
	*fake.Client
	mu     sync.Mutex
	before func()
}

func (h *checkHook) CheckConnection(ctx context.Context, a fulfillment.Account) (fulfillment.Health, error) {
	h.mu.Lock()
	f := h.before
	h.before = nil
	h.mu.Unlock()
	if f != nil {
		f()
	}
	return h.Client.CheckConnection(ctx, a) //nolint:wrapcheck // chuyển thẳng của hãng giả
}

func (h *checkHook) set(f func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.before = f
}

func keySpec(id string, b byte) string {
	return id + ":" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32))
}

func newAccEnv(t *testing.T) *accEnv {
	t.Helper()
	e := &accEnv{shipEnv: newShipEnv(t), srv: server.New(server.Options{Secret: accSecret, Hang: time.Second}), notes: &recNotifier{}}
	hs := httptest.NewServer(e.srv)
	t.Cleanup(hs.Close)
	e.hook = &checkHook{Client: fake.NewClient(hs.URL, accSecret, hs.Client())}
	reg, err := fulfillment.NewRegistry(e.hook, manual.New(fulfillment.SelfCarrierKey))
	if err != nil {
		t.Fatal(err)
	}
	e.reg = reg
	e.useKeys(keySpec("k1", 1))
	return e
}

// useKeys dựng lại Service, worker với FIELD_ENC_KEYS = spec (xoay khóa).
func (e *accEnv) useKeys(spec string) {
	e.t.Helper()
	keys, err := fieldcrypt.ParseKeyring(logging.Secret(spec))
	if err != nil {
		e.t.Fatal(err)
	}
	m := fulfillment.New(fulfillment.Deps{
		DB: e.staff.App, Queue: queue.NewQueue(), Carriers: e.reg, Keys: keys,
		Bus: &testkit.RecordingBus{},
	})
	e.svc = m.Service
	hs := append(m.CommandHandlers(), fulfillment.NewAuthAlertCommand(e.notes, nil))
	e.worker = queue.NewWorker(e.staff.App, nil, queue.WorkerOptions{
		WorkerID: "test/" + uuid.NewString(), Commands: commands.NewRunner(e.staff.App, hs),
	})
}

func (e *accEnv) input(mode string, creds fulfillment.Credentials) fulfillment.AccountInput {
	seller := e.shop.Seller
	return fulfillment.AccountInput{
		SellerID: &seller, CarrierKey: fake.CarrierKey, Mode: mode, Name: "GHN giả", WarehouseID: e.shop.Warehouse,
		Credentials: creds,
	}
}

func (e *accEnv) create(in fulfillment.AccountInput) (fulfillment.AccountView, error) {
	var v fulfillment.AccountView
	err := e.app(func(tx pgx.Tx) (err error) {
		v, err = e.svc.CreateAccount(e.t.Context(), tx, in, e.actor)
		return err //nolint:wrapcheck // test
	})
	return v, err
}

func (e *accEnv) mustCreate(in fulfillment.AccountInput) fulfillment.AccountView {
	e.t.Helper()
	v, err := e.create(in)
	if err != nil {
		e.t.Fatalf("CreateAccount: %v", err)
	}
	return v
}

func (e *accEnv) check(id uuid.UUID) {
	e.t.Helper()
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.svc.CheckAccount(e.t.Context(), tx, id, e.actor)
		return err //nolint:wrapcheck // test
	}); err != nil {
		e.t.Fatalf("CheckAccount: %v", err)
	}
	e.drain()
}

func (e *accEnv) get(id uuid.UUID) fulfillment.AccountView {
	e.t.Helper()
	return readAs(e.shipEnv, func(ctx context.Context, tx pgx.Tx) (fulfillment.AccountView, error) {
		return e.svc.GetAccount(ctx, tx, id)
	})
}

func (e *accEnv) resolve(id uuid.UUID) (fulfillment.Account, error) {
	var a fulfillment.Account
	err := e.app(func(tx pgx.Tx) (err error) {
		a, err = e.svc.ResolveAccount(e.t.Context(), tx, id)
		return err //nolint:wrapcheck // test
	})
	return a, err
}

func (e *accEnv) setStatus(id uuid.UUID, to string) (fulfillment.AccountView, error) {
	var v fulfillment.AccountView
	err := e.app(func(tx pgx.Tx) (err error) {
		v, err = e.svc.SetStatus(e.t.Context(), tx, id, to, nil, e.actor)
		return err //nolint:wrapcheck // test
	})
	return v, err
}

func (e *accEnv) credentialRow(id uuid.UUID) (enc []byte, kid string) {
	e.t.Helper()
	e.staff.Exec(e.t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(e.t.Context(), `SELECT credential_enc, credential_key_id FROM fulfillment.carrier_accounts WHERE id = $1`,
			id).Scan(&enc, &kid)
	})
	return enc, kid
}

// VC01: tạo tài khoản fake mode api có {"token": "tk-live-123"} → draft, có tuyến; kiểm → active, last_check_result ok;
// bản mã không chứa chuỗi khóa, credential_key_id là khóa đầu (k1); ResolveAccount trả đúng khóa đã giải mã và
// địa chỉ lấy hàng của kho; việc trong platform.jobs (payload, kết quả, lỗi) không có chuỗi khóa.
func TestAccounts_CreateCheckActive(t *testing.T) {
	e := newAccEnv(t)
	v := e.mustCreate(e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": liveToken}))
	if v.Status != fulfillment.AccountDraft || !v.HasCredentials || v.NeedsRewrap || v.Environment != "sandbox" ||
		!strings.HasPrefix(v.WebhookURL, "/hooks/fake/") || len(strings.TrimPrefix(v.WebhookURL, "/hooks/fake/")) != 32 ||
		!v.Capabilities.Quote || v.Version != 1 {
		t.Fatalf("tạo: %+v", v)
	}
	enc, kid := e.credentialRow(v.ID)
	if bytes.Contains(enc, []byte(liveToken)) || kid != "k1" || len(enc) < 28 {
		t.Fatalf("bản mã: kid %q, chứa khóa %v", kid, bytes.Contains(enc, []byte(liveToken)))
	}
	if n := e.staff.CountIn(t, e.tenant, `SELECT count(*) FROM fulfillment.carrier_accounts WHERE id = $1
		AND external_account_ref = 'local:' || id::text`, v.ID); n != 1 {
		t.Errorf("external_account_ref rỗng phải ghi local:<id>")
	}
	if _, err := e.resolve(v.ID); !errors.Is(err, fulfillment.ErrAccountUnavailable) {
		t.Fatalf("ResolveAccount khi draft: %v, muốn ErrAccountUnavailable", err)
	}
	e.ownerExec(`UPDATE inventory.warehouses SET pickup_address = '{"name":"Kho MAIN","line":"1 Hàng Bài","province_code":"01","ward_code":"00070"}', pickup_phone = '+84901234567' WHERE id = $1`, e.shop.Warehouse)
	e.check(v.ID)
	got := e.get(v.ID)
	if got.Status != fulfillment.AccountActive || got.LastCheckResult != "ok" || got.LastCheckAt == nil || got.Version != 2 {
		t.Fatalf("sau kiểm: %+v", got)
	}
	a, err := e.resolve(v.ID)
	if err != nil || a.Creds["token"] != liveToken || a.Pickup.Line != "1 Hàng Bài" || a.Pickup.WardCode != "00070" ||
		a.Pickup.Phone != "+84901234567" || len(a.RouteKey) != 32 {
		t.Fatalf("ResolveAccount: %v %+v", err, a.Pickup)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'carrier.check' AND status = 'done'`); n != 1 {
		t.Fatalf("lệnh kiểm done: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs j WHERE j::text LIKE '%' || $1 || '%'`, liveToken); n != 0 {
		t.Fatalf("platform.jobs có chuỗi khóa ở %d dòng", n)
	}
}

// Đổi một byte credential_enc bằng chủ bảng → lệnh kiểm dead lý do decrypt_failed, last_error không có khóa; tài
// khoản giữ nguyên trạng thái.
func TestAccounts_TamperedCiphertext(t *testing.T) {
	e := newAccEnv(t)
	v := e.mustCreate(e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": liveToken}))
	e.ownerExec(`UPDATE fulfillment.carrier_accounts SET credential_enc = set_byte(credential_enc, 20,
		get_byte(credential_enc, 20) # 1) WHERE id = $1`, v.ID)
	e.check(v.ID)
	var lastErr string
	e.staff.Exec(t, e.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT last_error FROM platform.jobs WHERE kind = 'carrier.check' AND status = 'dead'`).
			Scan(&lastErr)
	})
	if !strings.Contains(lastErr, "decrypt_failed") || strings.Contains(lastErr, liveToken) {
		t.Fatalf("last_error = %q", lastErr)
	}
	if got := e.get(v.ID); got.Status != fulfillment.AccountDraft || got.LastCheckAt != nil {
		t.Fatalf("bản mã hỏng không được đổi tài khoản: %+v", got)
	}
}

// Chép credential_enc của tài khoản X sang Y (chủ bảng) → giải mã Y thất bại (AAD gắn mã dòng); X vẫn mở được.
func TestAccounts_CopiedCiphertext(t *testing.T) {
	e := newAccEnv(t)
	x := e.mustCreate(e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": liveToken}))
	inY := e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": "tk-other"})
	inY.Name = "Tài khoản Y"
	y := e.mustCreate(inY)
	e.ownerExec(`UPDATE fulfillment.carrier_accounts y SET credential_enc = x.credential_enc, status = 'active'
		FROM fulfillment.carrier_accounts x WHERE x.id = $1 AND y.id = $2`, x.ID, y.ID)
	e.ownerExec(`UPDATE fulfillment.carrier_accounts SET status = 'active' WHERE id = $1`, x.ID)
	if _, err := e.resolve(y.ID); !errors.Is(err, fulfillment.ErrCredentialsUnreadable) || strings.Contains(err.Error(), liveToken) {
		t.Fatalf("giải mã Y: %v, muốn ErrCredentialsUnreadable", err)
	}
	if a, err := e.resolve(x.ID); err != nil || a.Creds["token"] != liveToken {
		t.Fatalf("giải mã X: %v", err)
	}
}

// Xoay khóa: FIELD_ENC_KEYS = "k2:…,k1:…" → tài khoản cũ (k1) vẫn giải mã được, NeedsRewrap = true; PutCredentials →
// credential_key_id = k2, NeedsRewrap = false, trạng thái checking và có lệnh kiểm.
func TestAccounts_KeyRotation(t *testing.T) {
	e := newAccEnv(t)
	v := e.mustCreate(e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": liveToken}))
	e.useKeys(keySpec("k2", 2) + "," + keySpec("k1", 1))
	if got := e.get(v.ID); !got.NeedsRewrap {
		t.Fatalf("khóa k1 sau khi thêm k2: NeedsRewrap = false")
	}
	if _, err := e.setStatus(v.ID, fulfillment.AccountActive); err != nil {
		t.Fatal(err)
	}
	if a, err := e.resolve(v.ID); err != nil || a.Creds["token"] != liveToken {
		t.Fatalf("giải mã bằng k1 sau xoay khóa: %v", err)
	}
	var after fulfillment.AccountView
	if err := e.app(func(tx pgx.Tx) (err error) {
		after, err = e.svc.PutCredentials(t.Context(), tx, v.ID, fulfillment.Credentials{"token": "tk-live-456"}, e.actor)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	if _, kid := e.credentialRow(v.ID); kid != "k2" || after.NeedsRewrap || after.Status != fulfillment.AccountChecking {
		t.Fatalf("sau PutCredentials: kid %q %+v", kid, after)
	}
	if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'carrier.check' AND status = 'pending'`); n != 1 {
		t.Fatalf("PutCredentials phải ghi một lệnh kiểm, có %d", n)
	}
	e.drain()
	if a, err := e.resolve(v.ID); err != nil || a.Creds["token"] != "tk-live-456" {
		t.Fatalf("giải mã khóa mới: %v", err)
	}
}

// VC15: hãng giả chế độ auth_revoked (health) → kiểm → auth_failed, last_check_result http_401, có cảnh báo
// carrier-auth:<id>; ResolveAccount → ErrAccountUnavailable, kể cả khi có tài khoản fake thứ hai active cùng kho (không
// tự đổi). MarkAuthFailed (lệnh khác báo ErrAuth) trên tài khoản active → auth_failed, cảnh báo; trên tài khoản paused
// → giữ paused.
func TestAccounts_AuthRevoked(t *testing.T) {
	e := newAccEnv(t)
	v := e.mustCreate(e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": liveToken}))
	inB := e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": "tk-b"})
	inB.Name = "Tài khoản B"
	b := e.mustCreate(inB)
	e.check(b.ID)
	e.srv.SetMode("health", "auth_revoked")
	e.check(v.ID)
	got := e.get(v.ID)
	if got.Status != fulfillment.AccountAuthFailed || got.LastCheckResult != "http_401" {
		t.Fatalf("sau kiểm auth_revoked: %+v", got)
	}
	if !e.notes.has("carrier-auth:" + v.ID.String()) {
		t.Fatalf("thiếu cảnh báo carrier-auth:%s (có %v)", v.ID, e.notes.keys)
	}
	if e.get(b.ID).Status != fulfillment.AccountActive {
		t.Fatal("tài khoản B phải active")
	}
	if _, err := e.resolve(v.ID); !errors.Is(err, fulfillment.ErrAccountUnavailable) {
		t.Fatalf("ResolveAccount auth_failed: %v, muốn ErrAccountUnavailable", err)
	}
	e.notes = &recNotifier{}
	e.useKeys(keySpec("k1", 1))
	if err := e.app(func(tx pgx.Tx) error { return fulfillment.MarkAuthFailed(t.Context(), tx, b.ID, "HTTP_401") }); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if got := e.get(b.ID); got.Status != fulfillment.AccountAuthFailed || got.LastCheckResult != "http_401" ||
		!e.notes.has("carrier-auth:"+b.ID.String()) {
		t.Fatalf("MarkAuthFailed: %+v, cảnh báo %v", got, e.notes.keys)
	}
	if _, err := e.setStatus(v.ID, fulfillment.AccountPaused); err != nil {
		t.Fatal(err)
	}
	if err := e.app(func(tx pgx.Tx) error { return fulfillment.MarkAuthFailed(t.Context(), tx, v.ID, "x") }); err != nil {
		t.Fatal(err)
	}
	if got := e.get(v.ID); got.Status != fulfillment.AccountPaused {
		t.Fatalf("MarkAuthFailed trên paused: %s, muốn paused", got.Status)
	}
}

// Tuyến: tạo tài khoản trong WithTenant(A) → tenancy.Resolve("carrier_account", route_key) = A; crm_app chèn thẳng
// platform.tenant_routes → lỗi quyền. revoked → tuyến vẫn còn, vận đơn cũ vẫn đọc được, ResolveAccount →
// ErrAccountUnavailable, không bật lại được.
func TestAccounts_RouteAndRevoked(t *testing.T) {
	e := newAccEnv(t)
	v := e.mustCreate(e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": liveToken}))
	rk := strings.TrimPrefix(v.WebhookURL, "/hooks/fake/")
	if tid, ok, err := tenancy.Resolve(t.Context(), e.staff.App, fulfillment.RouteKindCarrierAccount, rk); err != nil || !ok || tid != e.tenant {
		t.Fatalf("Resolve: %v %v %v", tid, ok, err)
	}
	err := e.app(func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `INSERT INTO platform.tenant_routes (kind, external_key, tenant_id) VALUES ('carrier_account', 'x', $1)`, e.tenant)
		return err //nolint:wrapcheck // test
	})
	if err == nil || !strings.Contains(err.Error(), "42501") {
		t.Fatalf("crm_app chèn thẳng tenant_routes: %v, muốn lỗi quyền 42501", err)
	}

	o, _ := e.order(1)
	s := e.mustCreateShipment(o.ID)
	if _, err := e.setStatus(e.account, fulfillment.AccountRevoked); err != nil {
		t.Fatal(err)
	}
	if _, err := e.setStatus(v.ID, fulfillment.AccountRevoked); err != nil {
		t.Fatal(err)
	}
	if got := e.shipment(s.ID); got.CarrierAccountID != e.account {
		t.Fatalf("vận đơn cũ: %+v", got)
	}
	if _, ok, _ := tenancy.Resolve(t.Context(), e.staff.App, fulfillment.RouteKindCarrierAccount, rk); !ok {
		t.Fatal("tài khoản revoked phải giữ tuyến")
	}
	if _, err := e.resolve(v.ID); !errors.Is(err, fulfillment.ErrAccountUnavailable) {
		t.Fatalf("ResolveAccount revoked: %v", err)
	}
	for _, to := range []string{fulfillment.AccountActive, fulfillment.AccountPaused} {
		if _, err := e.setStatus(v.ID, to); !errors.Is(err, fulfillment.ErrAccountTransition) {
			t.Errorf("revoked → %s: %v, muốn ErrAccountTransition", to, err)
		}
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.svc.CheckAccount(t.Context(), tx, v.ID, e.actor)
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, fulfillment.ErrAccountTransition) {
		t.Errorf("kiểm tài khoản revoked: %v", err)
	}
}

// Kiểm đầu vào: hãng không có adapter → ErrCarrierNotSupported; self với api → ErrCarrierNotSupported; self manual →
// active ngay; manual có khóa → ErrAccountInvalid; SetStatus active cho api chưa có khóa → ErrAccountTransition; hai
// tài khoản mặc định cùng kho cùng hãng → ErrAccountTaken; kho lạ → ErrAccountTargetNotFound; sửa với version cũ →
// ErrVersionConflict; đổi mã ở hãng → ErrAccountInvalid.
func TestAccounts_Validation(t *testing.T) {
	e := newAccEnv(t)
	bad := e.input(fulfillment.ModeAPI, nil)
	bad.CarrierKey = "ghn"
	if _, err := e.create(bad); !errors.Is(err, fulfillment.ErrCarrierNotSupported) {
		t.Errorf("ghn: %v", err)
	}
	self := e.input(fulfillment.ModeAPI, nil)
	self.CarrierKey = fulfillment.SelfCarrierKey
	if _, err := e.create(self); !errors.Is(err, fulfillment.ErrCarrierNotSupported) {
		t.Errorf("self api: %v", err)
	}
	self.Mode = fulfillment.ModeManual
	if v, err := e.create(self); err != nil || v.Status != fulfillment.AccountActive || v.HasCredentials {
		t.Errorf("self manual: %v %+v", err, v)
	}
	if _, err := e.create(e.input(fulfillment.ModeManual, fulfillment.Credentials{"token": "x"})); !errors.Is(err, fulfillment.ErrAccountInvalid) {
		t.Errorf("manual có khóa: %v", err)
	}
	noSeller := e.input(fulfillment.ModeAPI, nil)
	noSeller.SellerID = nil
	if _, err := e.create(noSeller); !errors.Is(err, fulfillment.ErrAccountInvalid) {
		t.Errorf("thiếu người bán: %v", err)
	}
	other := e.input(fulfillment.ModeAPI, nil)
	other.WarehouseID = uuid.New()
	if _, err := e.create(other); !errors.Is(err, fulfillment.ErrAccountTargetNotFound) {
		t.Errorf("kho lạ: %v", err)
	}
	plain := e.mustCreate(e.input(fulfillment.ModeAPI, nil))
	if _, err := e.setStatus(plain.ID, fulfillment.AccountActive); !errors.Is(err, fulfillment.ErrAccountTransition) {
		t.Errorf("active chưa có khóa: %v", err)
	}
	def := e.input(fulfillment.ModeAPI, nil)
	def.IsDefaultForWarehouse = true
	e.mustCreate(def)
	if _, err := e.create(def); !errors.Is(err, fulfillment.ErrAccountTaken) {
		t.Errorf("mặc định thứ hai: %v", err)
	}
	name, ref := "Tên mới", "shop-123"
	update := func(p fulfillment.AccountPatch, version int64) (fulfillment.AccountView, error) {
		var v fulfillment.AccountView
		err := e.app(func(tx pgx.Tx) (err error) {
			v, err = e.svc.UpdateAccount(t.Context(), tx, plain.ID, p, version, e.actor)
			return err //nolint:wrapcheck // test
		})
		return v, err
	}
	if v, err := update(fulfillment.AccountPatch{Name: &name}, 1); err != nil || v.Name != name || v.Version != 2 {
		t.Fatalf("sửa tên: %v %+v", err, v)
	}
	if _, err := update(fulfillment.AccountPatch{Name: &name}, 1); !errors.Is(err, fulfillment.ErrVersionConflict) {
		t.Errorf("version cũ: %v", err)
	}
	if _, err := update(fulfillment.AccountPatch{ExternalAccountRef: &ref}, 2); !errors.Is(err, fulfillment.ErrAccountInvalid) {
		t.Errorf("đổi mã ở hãng: %v", err)
	}
	yes := true
	if _, err := update(fulfillment.AccountPatch{IsDefaultForWarehouse: &yes}, 2); !errors.Is(err, fulfillment.ErrAccountTaken) {
		t.Errorf("sửa thành mặc định thứ hai: %v", err)
	}
}

// ActiveAccounts(ShowOnWeb: true) chỉ trả tài khoản active có cờ; lọc khả năng; Creds qua slog ra [Redacted].
func TestAccounts_ActiveAccounts(t *testing.T) {
	e := newAccEnv(t)
	web := e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": liveToken})
	web.ShowOnWeb, web.Name = true, "Trên web"
	w := e.mustCreate(web)
	hidden := e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": "tk-hidden"})
	hidden.Name = "Không trên web"
	h := e.mustCreate(hidden)
	draftWeb := e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": "tk-draft"})
	draftWeb.ShowOnWeb, draftWeb.Name = true, "Nháp trên web"
	e.mustCreate(draftWeb)
	e.check(w.ID)
	e.check(h.ID)
	yes := true
	list := func(f fulfillment.AccountFilter) []fulfillment.Account {
		return readAs(e.shipEnv, func(ctx context.Context, tx pgx.Tx) ([]fulfillment.Account, error) {
			return e.svc.ActiveAccounts(ctx, tx, f)
		})
	}
	got := list(fulfillment.AccountFilter{ShowOnWeb: &yes})
	if len(got) != 1 || got[0].ID != w.ID || got[0].Creds["token"] != liveToken {
		t.Fatalf("ShowOnWeb: %d tài khoản", len(got))
	}
	if n := len(list(fulfillment.AccountFilter{WarehouseID: &e.shop.Warehouse, Capability: "quote"})); n < 2 {
		t.Fatalf("lọc khả năng quote: %d", n)
	}
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.svc.ActiveAccounts(t.Context(), tx, fulfillment.AccountFilter{Capability: "bay"})
		return err //nolint:wrapcheck // test
	}); !errors.Is(err, fulfillment.ErrAccountInvalid) {
		t.Fatalf("khả năng lạ: %v", err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", slog.Any("creds", got[0].Creds), slog.Any("account", got[0]))
	if strings.Contains(buf.String(), liveToken) || !strings.Contains(buf.String(), "[Redacted]") {
		t.Fatalf("log: %s", buf.String())
	}
}

// Nhánh kết quả kiểm (Quyết định 7 của tệp task): hãng từ chối (422) → auth_failed với mã http_422, không cảnh báo;
// 429 → lệnh hẹn lại (pending), tài khoản giữ draft; 503 chưa gửi → hẹn lại.
func TestAccounts_CheckOutcomes(t *testing.T) {
	for _, c := range []struct {
		mode, status, result, job string
	}{
		{"reject", fulfillment.AccountAuthFailed, "http_422", "done"},
		{"rate_limited", fulfillment.AccountDraft, "", "pending"},
		{"fail_before_send", fulfillment.AccountDraft, "", "pending"},
	} {
		t.Run(c.mode, func(t *testing.T) {
			e := newAccEnv(t)
			v := e.mustCreate(e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": liveToken}))
			e.srv.SetMode("health", c.mode)
			e.check(v.ID)
			got := e.get(v.ID)
			if got.Status != c.status || got.LastCheckResult != c.result {
				t.Fatalf("tài khoản: %s %q, muốn %s %q", got.Status, got.LastCheckResult, c.status, c.result)
			}
			if n := e.count(`SELECT count(*) FROM platform.jobs WHERE kind = 'carrier.check' AND status = $1`, c.job); n != 1 {
				t.Fatalf("lệnh kiểm %s: %d", c.job, n)
			}
			if len(e.notes.keys) != 0 {
				t.Fatalf("không được cảnh báo: %v", e.notes.keys)
			}
		})
	}
}

// Soát thiết kế 6.4: lệnh kiểm đã đọc khóa cũ, khóa mới được ghi trước khi lệnh xong → kết quả của lệnh cũ bị bỏ
// (tài khoản giữ checking, chưa có last_check_*); lệnh kiểm mới (theo khóa mới) mới quyết định trạng thái.
func TestAccounts_StaleCheckIgnored(t *testing.T) {
	e := newAccEnv(t)
	v := e.mustCreate(e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": liveToken}))
	if err := e.app(func(tx pgx.Tx) error {
		_, err := e.svc.CheckAccount(t.Context(), tx, v.ID, e.actor)
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	e.hook.set(func() {
		if err := e.app(func(tx pgx.Tx) error {
			_, err := e.svc.PutCredentials(context.Background(), tx, v.ID, fulfillment.Credentials{"token": "tk-new"}, e.actor)
			return err //nolint:wrapcheck // test
		}); err != nil {
			t.Errorf("PutCredentials giữa lúc kiểm: %v", err)
		}
	})
	if _, err := e.worker.RunOnce(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if got := e.get(v.ID); got.Status != fulfillment.AccountChecking || got.LastCheckAt != nil || got.LastCheckResult != "" {
		t.Fatalf("kết quả của lệnh kiểm cũ không được ghi: %+v", got)
	}
	e.srv.SetMode("health", "auth_revoked")
	e.drain()
	if got := e.get(v.ID); got.Status != fulfillment.AccountAuthFailed || got.LastCheckResult != "http_401" {
		t.Fatalf("lệnh kiểm theo khóa mới: %+v", got)
	}
}

// Tài khoản paused: kiểm OK hay 401 chỉ ghi last_check_*, giữ paused, không cảnh báo; ghi khóa mới cũng giữ paused.
func TestAccounts_PausedStaysPaused(t *testing.T) {
	e := newAccEnv(t)
	v := e.mustCreate(e.input(fulfillment.ModeAPI, fulfillment.Credentials{"token": liveToken}))
	if _, err := e.setStatus(v.ID, fulfillment.AccountPaused); err != nil {
		t.Fatal(err)
	}
	e.check(v.ID)
	if got := e.get(v.ID); got.Status != fulfillment.AccountPaused || got.LastCheckResult != "ok" || got.LastCheckAt == nil {
		t.Fatalf("kiểm OK khi paused: %+v", got)
	}
	e.srv.SetMode("health", "auth_revoked")
	e.check(v.ID)
	if got := e.get(v.ID); got.Status != fulfillment.AccountPaused || got.LastCheckResult != "http_401" || len(e.notes.keys) != 0 {
		t.Fatalf("kiểm 401 khi paused: %+v, cảnh báo %v", got, e.notes.keys)
	}
	var after fulfillment.AccountView
	if err := e.app(func(tx pgx.Tx) (err error) {
		after, err = e.svc.PutCredentials(t.Context(), tx, v.ID, fulfillment.Credentials{"token": "tk-new"}, e.actor)
		return err //nolint:wrapcheck // test
	}); err != nil || after.Status != fulfillment.AccountPaused {
		t.Fatalf("ghi khóa khi paused: %v %+v", err, after)
	}
	e.srv.SetMode("health", "ok")
	e.drain()
	if got := e.get(v.ID); got.Status != fulfillment.AccountPaused || got.LastCheckResult != "ok" {
		t.Fatalf("kiểm sau ghi khóa khi paused: %+v", got)
	}
}
