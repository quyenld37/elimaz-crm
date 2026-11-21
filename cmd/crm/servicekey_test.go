package main

import (
	"bytes"
	"errors"
	"log/slog"
	"net/netip"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/realtime/realtimetest"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// Lệnh crm service-key (Task 189): chặn production, đọc tham số; revoke tìm đúng tenant, phát báo hiệu thu hồi.
func TestServiceKeyCommand_ArgsAndProduction(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	var out bytes.Buffer
	if err := runServiceKey(t.Context(), config.Config{Env: "production"}, []string{"create"}, &out, log); !errors.Is(err, errServiceKeyProduction) {
		t.Fatalf("production: %v", err)
	}
	for _, args := range [][]string{{}, {"list"}, {"create", "--la", "x"}, {"create", "thừa"}} {
		if err := runServiceKey(t.Context(), config.Config{Env: "dev"}, args, &out, log); !errors.Is(err, errServiceKeyUsage) {
			t.Errorf("%v: mong errServiceKeyUsage, có %v", args, err)
		}
	}
	in, err := serviceKeyInput("Web", "web.catalog.read, web.orders.create,", "10.0.0.0/8")
	if err != nil || len(in.Permissions) != 2 || len(in.AllowedCIDRs) != 1 || in.AllowedCIDRs[0] != netip.MustParsePrefix("10.0.0.0/8") {
		t.Fatalf("đọc tham số: %+v %v", in, err)
	}
	if _, err := serviceKeyInput("Web", "web.catalog.read", "không-phải-cidr"); err == nil {
		t.Fatal("CIDR sai phải lỗi")
	}
}

// RATE_LIMITS_JSON sai → không dựng được (không khởi động); đúng → giới hạn theo khóa nối vào lớp HTTP.
func TestNewWebKeys_RateLimits(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	if _, err := newWebKeys(t.Context(), config.Config{RateLimitsJSON: `{"web_key":{"max":0}}`}, nil, nil, nil, log); !errors.Is(err, cache.ErrBadLimits) {
		t.Fatalf("RATE_LIMITS_JSON sai: %v", err)
	}
	w, err := newWebKeys(t.Context(), config.Config{RateLimitsJSON: `{"web_key":{"max":2,"fallback_max":1}}`}, nil, nil, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	defer w.stop()
	// Không có Valkey: bộ đếm trong tiến trình với fallback_max = 1.
	if ok, _ := w.limiter.AllowWebKey(t.Context(), "svc:"+uuid.NewString()); !ok {
		t.Fatal("lượt đầu phải được")
	}
	subject := "svc:" + uuid.NewString()
	w.limiter.AllowWebKey(t.Context(), subject)
	if ok, wait := w.limiter.AllowWebKey(t.Context(), subject); ok || wait <= 0 {
		t.Fatalf("lượt thứ hai khi fallback_max = 1 phải bị từ chối có thời gian chờ: %v %v", ok, wait)
	}
}

func TestRevokeServiceKeyAnyTenant(t *testing.T) {
	s := testkit.OpenStaff(t)
	s.Tenant(t, "A") // tenant không có khóa: lệnh phải thử qua tenant này rồi mới tới B
	b := s.Tenant(t, "B")
	s.Exec(t, b, func(tx pgx.Tx) error { return identity.EnsureDefaultRoles(t.Context(), tx, b) })
	uid := s.User(t, b, "chu@elimaz.test", "active")
	s.Grant(t, b, uid, []string{"identity.service_keys.manage"}, identity.ScopeAll, "")
	actor, err := identity.LoadUser(t.Context(), s.App, "chu@elimaz.test")
	if err != nil {
		t.Fatal(err)
	}
	var k identity.ServiceKey
	if err := tenancy.WithTenant(t.Context(), s.App, b, func(tx pgx.Tx) error {
		var err error
		k, _, err = identity.CreateServiceKey(t.Context(), tx, identity.CreateServiceKeyInput{
			Name: "Web", Permissions: []identity.Permission{"web.catalog.read"}, Actor: actor,
		})
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	bus := &realtimetest.RecordingBus{}
	if err := revokeServiceKeyAnyTenant(t.Context(), s.App, bus, uuid.New()); !errors.Is(err, errServiceKeyUnknown) {
		t.Fatalf("mã lạ: %v", err)
	}
	if err := revokeServiceKeyAnyTenant(t.Context(), s.App, bus, k.ID); err != nil {
		t.Fatal(err)
	}
	sig := bus.Signals()
	if len(sig) != 1 || sig[0].Topic != identity.TopicServiceKeyRevoked || !sig[0].System || sig[0].Entity.ID != k.KeyPrefix || sig[0].TenantID != b {
		t.Fatalf("báo hiệu thu hồi: %+v", sig)
	}
	if n := s.CountIn(t, b, `SELECT count(*) FROM identity.service_keys WHERE id = $1 AND revoked_at IS NOT NULL AND revoked_by IS NULL`, k.ID); n != 1 {
		t.Fatalf("khóa phải đã thu hồi bởi hệ thống: %d", n)
	}
	if err := revokeServiceKeyAnyTenant(t.Context(), s.App, bus, k.ID); !errors.Is(err, identity.ErrVersionConflict) {
		t.Fatalf("thu hồi lần hai: %v", err)
	}
}
