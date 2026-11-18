package tenancy_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// putAs ghi tuyến bằng crm_app trong ngữ cảnh tenant.
func putAs(ctx context.Context, s *testkit.Staff, tenant uuid.UUID, kind, key string) error {
	return tenancy.WithTenant(ctx, s.App, tenant, func(tx pgx.Tx) error { return tenancy.PutRoute(ctx, tx, kind, key) }) //nolint:wrapcheck // test
}

func routeCount(t *testing.T, s *testkit.Staff, kind, key string) int {
	t.Helper()
	var n int
	if err := s.Owner.Pool.QueryRow(t.Context(),
		"SELECT count(*) FROM platform.tenant_routes WHERE kind = $1 AND external_key = $2", kind, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func mustResolve(t *testing.T, s *testkit.Staff, kind, key string, want uuid.UUID) {
	t.Helper()
	got, ok, err := tenancy.Resolve(t.Context(), s.App, kind, key)
	if err != nil || !ok || got != want {
		t.Fatalf("Resolve(%s, %s) = %v %v %v, muốn %v", kind, key, got, ok, err, want)
	}
}

// PutRoute (M3 carrier_account, M4 fb_page, M5 service_key): ghi tuyến cho tenant trong ngữ cảnh; gọi lại không lỗi,
// một dòng; khóa đã thuộc tenant khác → ErrRouteTaken và giữ dòng cũ.
func TestPutRoute(t *testing.T) {
	s := testkit.OpenStaff(t)
	ctx := t.Context()
	a, b := s.Tenant(t, "a"), s.Tenant(t, "b")
	for _, c := range []struct{ kind, key string }{
		{"fb_page", "123"}, {"service_key", "ab12cd34"}, {"carrier_account", "AbCdEfGh0123456789AbCdEfGh012345"},
	} {
		for range 2 {
			if err := putAs(ctx, s, a, c.kind, c.key); err != nil {
				t.Fatalf("A ghi %s/%s: %v", c.kind, c.key, err)
			}
		}
		if n := routeCount(t, s, c.kind, c.key); n != 1 {
			t.Fatalf("%s/%s có %d dòng, muốn 1", c.kind, c.key, n)
		}
		mustResolve(t, s, c.kind, c.key, a)
		if err := putAs(ctx, s, b, c.kind, c.key); !errors.Is(err, tenancy.ErrRouteTaken) {
			t.Fatalf("B ghi %s/%s của A: %v, muốn ErrRouteTaken", c.kind, c.key, err)
		}
		mustResolve(t, s, c.kind, c.key, a)
	}
}

// Ngoài ngữ cảnh tenant, kind ngoài ba loại được phép, khóa rỗng → lỗi (không phải ErrRouteTaken), không ghi gì.
// crm_app vẫn không INSERT thẳng được vào platform.tenant_routes (như M1).
func TestPutRouteRejects(t *testing.T) {
	s := testkit.OpenStaff(t)
	ctx := t.Context()
	a := s.Tenant(t, "a")
	err := s.App.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error { return tenancy.PutRoute(ctx, tx, "fb_page", "999") })
	if err == nil || errors.Is(err, tenancy.ErrRouteTaken) {
		t.Fatalf("ngoài ngữ cảnh tenant: %v, muốn lỗi khác ErrRouteTaken", err)
	}
	for _, kind := range []string{"staff_email", "selftest", "abc", ""} {
		if err := putAs(ctx, s, a, kind, "k1"); err == nil || errors.Is(err, tenancy.ErrRouteTaken) {
			t.Fatalf("kind %q: %v, muốn lỗi", kind, err)
		}
	}
	for _, key := range []string{"", "   "} {
		if err := putAs(ctx, s, a, "fb_page", key); err == nil {
			t.Fatalf("khóa %q: không lỗi", key)
		}
	}
	var n int
	if err := s.Owner.Pool.QueryRow(ctx, "SELECT count(*) FROM platform.tenant_routes").Scan(&n); err != nil || n != 0 {
		t.Fatalf("không được ghi dòng nào: %d %v", n, err)
	}
	err = tenancy.WithTenant(ctx, s.App, a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO platform.tenant_routes (kind, external_key, tenant_id) VALUES ('fb_page', '1', $1)", a)
		return err //nolint:wrapcheck // test
	})
	if sqlState(err) != "42501" {
		t.Fatalf("crm_app INSERT thẳng: %v, muốn 42501", err)
	}
}

// Hai tenant cùng lúc ghi một khóa: đúng một thắng, người kia ErrRouteTaken; tuyến trỏ về người thắng.
func TestPutRouteConcurrent(t *testing.T) {
	s := testkit.OpenStaff(t)
	ctx := t.Context()
	tenants := []uuid.UUID{s.Tenant(t, "a"), s.Tenant(t, "b"), s.Tenant(t, "c"), s.Tenant(t, "d")}
	errs := make([]error, len(tenants))
	var wg sync.WaitGroup
	for i, tn := range tenants {
		wg.Go(func() { errs[i] = putAs(ctx, s, tn, "fb_page", "race-1") })
	}
	wg.Wait()
	var winner uuid.UUID
	wins := 0
	for i, err := range errs {
		switch {
		case err == nil:
			wins++
			winner = tenants[i]
		case !errors.Is(err, tenancy.ErrRouteTaken):
			t.Fatalf("tenant %d: %v, muốn nil hoặc ErrRouteTaken", i, err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d tenant thắng, muốn 1 (%v)", wins, errs)
	}
	mustResolve(t, s, "fb_page", "race-1", winner)
}
