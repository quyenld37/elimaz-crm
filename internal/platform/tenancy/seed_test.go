package tenancy_test

import (
	"errors"
	"testing"

	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// SeedTenant chạy lại trả cùng tenant; AddRoute chạy lại không lỗi, trỏ tenant khác thì ErrRouteConflict và giữ dòng cũ.
func TestSeedTenantAndAddRoute(t *testing.T) {
	s := testkit.OpenStaff(t)
	ctx := t.Context()
	a, err := tenancy.SeedTenant(ctx, s.Owner, "ELIMAZ")
	if err != nil {
		t.Fatal(err)
	}
	again, err := tenancy.SeedTenant(ctx, s.Owner, "ELIMAZ")
	if err != nil || again != a {
		t.Fatalf("SeedTenant lần hai: %v, %v (mong %v)", again, err, a)
	}
	b, err := tenancy.SeedTenant(ctx, s.Owner, "Khac")
	if err != nil || b == a {
		t.Fatalf("tên khác phải tạo tenant khác: %v, %v", b, err)
	}
	for range 2 {
		if err := tenancy.AddRoute(ctx, s.Owner, "staff_email", "x@elimaz.test", a); err != nil {
			t.Fatal(err)
		}
	}
	if err := tenancy.AddRoute(ctx, s.Owner, "staff_email", "x@elimaz.test", b); !errors.Is(err, tenancy.ErrRouteConflict) {
		t.Fatalf("trỏ tenant khác phải ErrRouteConflict, có %v", err)
	}
	got, ok, err := tenancy.Resolve(ctx, s.App, "staff_email", "x@elimaz.test")
	if err != nil || !ok || got != a {
		t.Fatalf("định tuyến phải giữ tenant cũ: %v %v %v", got, ok, err)
	}
}
