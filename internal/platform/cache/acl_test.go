package cache_test

import (
	"context"
	"strings"
	"testing"

	"github.com/valkey-io/valkey-go"

	"elimaz.com.vn/crm/internal/testkit"
)

// Danh sách quyền của docker/valkey/users.acl.tmpl: mỗi người dùng chỉ chạm tiền tố khóa của mình, không lệnh nguy hiểm.
func TestACL(t *testing.T) {
	vk := testkit.StartValkey(t)
	cacheC := dial(t, vk.URL("crm_cache"), nil)
	gateC := dial(t, vk.URL("crm_gate"), nil)
	adminC := dial(t, vk.URL("crm_admin"), nil)
	run := func(c interface {
		Do(context.Context, string, func(context.Context, valkey.Client) error) error
	}, cmd ...string,
	) error {
		return c.Do(t.Context(), "acl", func(ctx context.Context, cl valkey.Client) error {
			return cl.Do(ctx, cl.B().Arbitrary(cmd[0]).Keys(cmd[1:]...).Build()).Error()
		})
	}
	noperm := func(name string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "NOPERM") {
			t.Errorf("%s: lỗi %v, muốn NOPERM", name, err)
		}
	}
	noperm("crm_cache FLUSHALL", run(cacheC, "FLUSHALL"))
	noperm("crm_gate FLUSHALL", run(gateC, "FLUSHALL"))
	noperm("crm_cache CONFIG", run(cacheC, "CONFIG", "GET", "maxmemory"))
	noperm("crm_cache KEYS", run(cacheC, "KEYS", "c:*"))
	noperm("crm_cache SET gt:x", set(t.Context(), cacheC, "gt:x", "1"))
	noperm("crm_cache SET rl:x", set(t.Context(), cacheC, "rl:x", "1"))
	noperm("crm_gate SET c:x", set(t.Context(), gateC, "c:x", "1"))
	noperm("crm_gate SET g:x", set(t.Context(), gateC, "g:x", "1"))
	for _, k := range []string{"c:x", "g:x"} {
		if err := set(t.Context(), cacheC, k, "1"); err != nil {
			t.Errorf("crm_cache SET %s: %v", k, err)
		}
	}
	for _, k := range []string{"rl:x", "gt:x", "fs:x"} {
		if err := set(t.Context(), gateC, k, "1"); err != nil {
			t.Errorf("crm_gate SET %s: %v", k, err)
		}
	}
	if err := gateC.Do(t.Context(), "hset", func(ctx context.Context, cl valkey.Client) error {
		return cl.Do(ctx, cl.B().Hset().Key("gt:h").FieldValue().FieldValue("a", "1").Build()).Error()
	}); err != nil {
		t.Errorf("crm_gate HSET gt:h: %v", err)
	}
	if err := run(adminC, "FLUSHALL"); err != nil {
		t.Errorf("crm_admin FLUSHALL: %v", err)
	}
	// Người dùng default tắt: không mật khẩu thì không vào được.
	if _, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{vk.Addr()}, DisableCache: true}); err == nil {
		t.Error("nối không mật khẩu (người dùng default) phải bị từ chối")
	}
}
