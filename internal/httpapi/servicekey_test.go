package httpapi

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/realtime"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// Bộ kiểm khóa dịch vụ (Task 189): đệm theo tiền tố có hạn, xóa ngay khi có báo hiệu thu hồi, last_used_at ghi ngoài
// đường yêu cầu tối đa một lần mỗi phút mỗi khóa.

func newFakeClock() *fakeClock { return &fakeClock{now: time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)} }

// silentBus: Bus không bao giờ phát báo hiệu hệ thống (mô phỏng báo hiệu mất); đếm số người đăng ký còn lại.
type silentBus struct {
	realtime.Bus

	subs atomic.Int64
}

func (b *silentBus) SubscribeSystem(string, func(realtime.Signal)) func() {
	b.subs.Add(1)
	var once sync.Once
	return func() { once.Do(func() { b.subs.Add(-1) }) }
}

// keyFixture: tenant có Chủ, một khóa (quyền, CIDR tùy ca) tạo qua identity.CreateServiceKey.
type keyFixture struct {
	t      *testing.T
	staff  *testkit.Staff
	tenant uuid.UUID
	owner  identity.AuthUser
}

func newKeyFixture(t *testing.T) *keyFixture {
	t.Helper()
	f := &keyFixture{t: t, staff: testkit.OpenStaff(t)}
	f.tenant = f.staff.Tenant(t, "A")
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error { return identity.EnsureDefaultRoles(t.Context(), tx, f.tenant) })
	id := f.staff.User(t, f.tenant, "chu@elimaz.test", "active")
	f.staff.Grant(t, f.tenant, id, []string{"identity.service_keys.manage"}, identity.ScopeAll, "")
	u, err := identity.LoadUser(t.Context(), f.staff.App, "chu@elimaz.test")
	if err != nil {
		t.Fatal(err)
	}
	f.owner = u
	return f
}

func (f *keyFixture) key(cidrs []netip.Prefix, perms ...identity.Permission) (identity.ServiceKey, string) {
	f.t.Helper()
	var (
		k    identity.ServiceKey
		full string
	)
	if err := tenancy.WithTenant(f.t.Context(), f.staff.App, f.tenant, func(tx pgx.Tx) error {
		var err error
		k, full, err = identity.CreateServiceKey(f.t.Context(), tx, identity.CreateServiceKeyInput{
			Name: "Website", Permissions: perms, AllowedCIDRs: cidrs, Actor: f.owner,
		})
		return err //nolint:wrapcheck // test
	}); err != nil {
		f.t.Fatal(err)
	}
	return k, full
}

func (f *keyFixture) revoke(k identity.ServiceKey) {
	f.t.Helper()
	if err := tenancy.WithTenant(f.t.Context(), f.staff.App, f.tenant, func(tx pgx.Tx) error {
		_, err := identity.RevokeServiceKey(f.t.Context(), tx, k.ID, k.Version, f.owner)
		return err //nolint:wrapcheck // test
	}); err != nil {
		f.t.Fatal(err)
	}
}

// Khóa đúng → người gọi service; sai bí mật, tiền tố lạ, sai dạng → ErrInvalidKey. Báo hiệu mất: thu hồi còn có hiệu
// lực ở giây thứ 4 (bản đệm), hết ở giây thứ 5 (CacheTTL, đồng hồ giả). Run dừng thì hủy đăng ký báo hiệu.
func TestServiceKeyVerifier_TTLWithoutSignal(t *testing.T) {
	f := newKeyFixture(t)
	k, full := f.key(nil, "web.catalog.read")
	clock := newFakeClock()
	bus := &silentBus{}
	v := NewServiceKeyVerifier(f.staff.App, bus, ServiceKeyOptions{Now: clock.Now})
	stop := runVerifier(t, v)

	u, err := v.Verify(t.Context(), full, netip.Addr{})
	if err != nil || u.Kind != identity.KindService || u.ServiceKeyID != k.ID || u.TenantID != f.tenant ||
		!identity.Can(u, "web.catalog.read", nil) || identity.Can(u, "web.orders.create", nil) {
		t.Fatalf("khóa đúng: %+v %v", u, err)
	}
	wrong := []byte(full)
	wrong[len(wrong)-2] ^= 1
	for _, bad := range []string{string(wrong), "ek_aaaaaaaaaaaa_" + full[16:], "ek_x_y"} {
		if _, err := v.Verify(t.Context(), bad, netip.Addr{}); !errors.Is(err, identity.ErrInvalidKey) {
			t.Errorf("%q: mong ErrInvalidKey, có %v", bad[:6], err)
		}
	}

	f.revoke(k)
	clock.Add(4 * time.Second)
	if _, err := v.Verify(t.Context(), full, netip.Addr{}); err != nil {
		t.Fatalf("giây thứ 4 (chưa hết hạn đệm, không báo hiệu): mong còn được, có %v", err)
	}
	clock.Add(time.Second)
	if _, err := v.Verify(t.Context(), full, netip.Addr{}); !errors.Is(err, identity.ErrRevoked) {
		t.Fatalf("giây thứ 5: mong ErrRevoked, có %v", err)
	}
	stop()
	if bus.subs.Load() != 0 {
		t.Fatal("Run dừng phải hủy đăng ký báo hiệu")
	}
}

// Báo hiệu thu hồi (hay resync) xóa bản đệm ngay: lần kiểm kế tiếp đọc lại database, kể cả khi đồng hồ chưa trôi.
func TestServiceKeyVerifier_SignalInvalidates(t *testing.T) {
	f := newKeyFixture(t)
	k, full := f.key(nil, "web.catalog.read")
	bus := &signalBus{}
	clock := newFakeClock()
	v := NewServiceKeyVerifier(f.staff.App, bus, ServiceKeyOptions{Now: clock.Now})
	if _, err := v.Verify(t.Context(), full, netip.Addr{}); err != nil {
		t.Fatal(err)
	}
	f.revoke(k)
	bus.send(realtime.Signal{
		TenantID: f.tenant, Topic: "identity.service_key_revoked_x", System: true,
		Entity: realtime.Entity{Type: "service_key", ID: k.KeyPrefix},
	}) // chủ đề khác: không xóa
	if _, err := v.Verify(t.Context(), full, netip.Addr{}); err != nil {
		t.Fatalf("báo hiệu chủ đề khác không được xóa đệm: %v", err)
	}
	bus.send(realtime.Signal{
		TenantID: f.tenant, Topic: identity.TopicServiceKeyRevoked, System: true,
		Entity: realtime.Entity{Type: "service_key", ID: k.KeyPrefix},
	})
	if _, err := v.Verify(t.Context(), full, netip.Addr{}); !errors.Is(err, identity.ErrRevoked) {
		t.Fatalf("sau báo hiệu thu hồi: mong ErrRevoked, có %v", err)
	}

	k2, full2 := f.key(nil, "web.catalog.read")
	if _, err := v.Verify(t.Context(), full2, netip.Addr{}); err != nil {
		t.Fatal(err)
	}
	f.revoke(k2)
	bus.send(realtime.Signal{Topic: realtime.TopicSystemResync, System: true})
	if _, err := v.Verify(t.Context(), full2, netip.Addr{}); !errors.Is(err, identity.ErrRevoked) {
		t.Fatalf("sau resync: mong ErrRevoked, có %v", err)
	}
}

// signalBus: Bus giả gửi báo hiệu hệ thống cho người đăng ký có tiền tố khớp (như PGHub.dispatchSystem; resync gửi
// mọi người).
type signalBus struct {
	realtime.Bus

	mu   sync.Mutex
	subs map[string]func(realtime.Signal)
}

func (b *signalBus) SubscribeSystem(prefix string, fn func(realtime.Signal)) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs == nil {
		b.subs = map[string]func(realtime.Signal){}
	}
	b.subs[prefix] = fn
	return func() {}
}

func (b *signalBus) send(s realtime.Signal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for p, fn := range b.subs {
		if s.Topic == realtime.TopicSystemResync || (len(s.Topic) >= len(p) && s.Topic[:len(p)] == p) {
			fn(s)
		}
	}
}

// 100 yêu cầu trong một phút → last_used_at ghi đúng một lần (mốc của yêu cầu đầu); sang phút sau → ghi lần hai.
// Ghi bằng goroutine của Run, không chặn yêu cầu.
func TestServiceKeyVerifier_LastUsedOncePerMinute(t *testing.T) {
	f := newKeyFixture(t)
	k, full := f.key(nil, "web.catalog.read")
	clock := newFakeClock()
	v := NewServiceKeyVerifier(f.staff.App, &silentBus{}, ServiceKeyOptions{Now: clock.Now})
	defer runVerifier(t, v)()

	first := clock.Now()
	for range 100 {
		if _, err := v.Verify(t.Context(), full, netip.Addr{}); err != nil {
			t.Fatal(err)
		}
		clock.Add(500 * time.Millisecond) // 100 × 0,5 s = 50 s < 1 phút
	}
	waitTouches(t, v, 1)
	v.flush(t.Context()) // không còn gì chờ ghi
	if got := v.touches.Load(); got != 1 {
		t.Fatalf("100 yêu cầu trong 1 phút phải ghi đúng 1 lần, có %d", got)
	}
	if got := lastUsed(t, f, k.ID); got == nil || !got.Equal(first) {
		t.Fatalf("last_used_at phải là mốc yêu cầu đầu %v, có %v", first, got)
	}
	clock.Add(11 * time.Second) // 61 s sau yêu cầu đầu
	if _, err := v.Verify(t.Context(), full, netip.Addr{}); err != nil {
		t.Fatal(err)
	}
	waitTouches(t, v, 2)
}

// runVerifier chạy v.Run trong goroutine của test; hàm trả về dừng nó và chờ nó xong (gọi nhiều lần được).
func runVerifier(t *testing.T, v *KeyVerifier) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { v.Run(ctx); close(done) }()
	return func() { cancel(); <-done }
}

// waitTouches chờ (theo sự kiện ghi; giờ thật chỉ là chốt chống treo) tới khi bộ kiểm đã ghi n lần.
func waitTouches(t *testing.T, v *KeyVerifier, n int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for v.touches.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("chờ %d lần ghi last_used_at, mới có %d", n, v.touches.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func lastUsed(t *testing.T, f *keyFixture, id uuid.UUID) *time.Time {
	t.Helper()
	var at *time.Time
	f.staff.Exec(t, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(t.Context(), `SELECT last_used_at FROM identity.service_keys WHERE id = $1`, id).Scan(&at)
	})
	return at
}

// Lỗi database của bộ kiểm không mang khóa và không được đệm (lần sau đọc lại).
func TestServiceKeyVerifier_ErrorNotCached(t *testing.T) {
	f := newKeyFixture(t)
	_, full := f.key(nil, "web.catalog.read")
	v := NewServiceKeyVerifier(f.staff.App, nil, ServiceKeyOptions{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := v.Verify(ctx, full, netip.Addr{})
	if err == nil || errors.Is(err, identity.ErrInvalidKey) || strings.Contains(err.Error(), full[16:]) {
		t.Fatalf("ctx hủy: mong lỗi database không mang khóa, có %v", err)
	}
	if _, err := v.Verify(t.Context(), full, netip.Addr{}); err != nil {
		t.Fatalf("lỗi trước không được đệm: %v", err)
	}
}
