package cache_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/valkey-io/valkey-go"

	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/testkit"
)

func dial(t *testing.T, url string, reg *metrics.Registry) *cache.Conn {
	t.Helper()
	c, err := cache.Dial(t.Context(), url, cache.DialOptions{Name: "cache", Metrics: reg})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func set(ctx context.Context, c *cache.Conn, key, val string) error {
	return c.Do(ctx, "set", func( //nolint:wrapcheck // Do của gói đang test
		ctx context.Context, cl valkey.Client,
	) error {
		return cl.Do(ctx, cl.B().Set().Key(key).Value(val).Build()).Error()
	})
}

func get(ctx context.Context, c *cache.Conn, key string) (string, error) {
	var v string
	err := c.Do(ctx, "get", func(ctx context.Context, cl valkey.Client) error {
		var err error
		v, err = cl.Do(ctx, cl.B().Get().Key(key).Build()).ToString()
		return err //nolint:wrapcheck // Do phân loại lỗi thật của valkey-go
	})
	return v, err //nolint:wrapcheck // Do của gói đang test
}

func TestConn_PingSetGet(t *testing.T) {
	vk := testkit.StartValkey(t)
	reg := metrics.NewRegistry()
	c := dial(t, vk.URL("crm_cache"), reg)
	if err := c.Ping(t.Context()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := set(t.Context(), c, "c:x", "1"); err != nil {
		t.Fatalf("SET c:x: %v", err)
	}
	if v, err := get(t.Context(), c, "c:x"); err != nil || v != "1" {
		t.Fatalf("GET c:x = %q, %v", v, err)
	}
	// Khóa không có: lỗi nil của Valkey, không phải ngừng; không đếm valkey_errors.
	if _, err := get(t.Context(), c, "c:khong-co"); !valkey.IsValkeyNil(err) || errors.Is(err, cache.ErrUnavailable) {
		t.Fatalf("GET khóa không có: %v", err)
	}
	var sawMs, sawErr bool
	for _, r := range reg.Drain() {
		switch {
		case r.Name == "valkey_ms" && r.Label == "cache:get" && r.Count == 2:
			sawMs = true
		case r.Name == "valkey_errors":
			sawErr = true
		}
	}
	if !sawMs || sawErr {
		t.Fatalf("chỉ số: valkey_ms cache:get 2 lần = %v, valkey_errors = %v", sawMs, sawErr)
	}
	if h := c.Health(t.Context()); h != cache.HealthOK {
		t.Fatalf("Health = %q", h)
	}
}

// CLIENT PAUSE 1000 ALL: lệnh treo ở máy chủ, Do trả lỗi theo hạn riêng của lệnh (OpTimeout), là lỗi ngừng. Không lấy
// ngưỡng giờ thật làm tiêu chí (Task 400, 454): ctx cha không có hạn, nên DeadlineExceeded chỉ có thể đến từ hạn riêng
// của Do; không có hạn đó thì SET chờ hết lúc tạm dừng rồi thành công (err nil). Hạn mà Do đặt cho fn đo bằng một đồng
// hồ (deadline − lúc fn chạy ≤ OpTimeout mặc định 50 ms).
func TestConn_OpTimeout(t *testing.T) {
	vk := testkit.StartValkey(t)
	reg := metrics.NewRegistry()
	c := dial(t, vk.URL("crm_cache"), reg)
	admin := dial(t, vk.URL("crm_admin"), nil)
	if err := c.Ping(t.Context()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := admin.Do(t.Context(), "pause", func(ctx context.Context, cl valkey.Client) error {
		return cl.Do(ctx, cl.B().ClientPause().Timeout(1000).All().Build()).Error()
	}); err != nil {
		t.Fatalf("CLIENT PAUSE: %v", err)
	}
	if _, ok := t.Context().Deadline(); ok {
		t.Fatal("ctx của test có hạn: không phân biệt được hạn riêng của Do")
	}
	var left time.Duration
	err := c.Do(t.Context(), "set", func(ctx context.Context, cl valkey.Client) error {
		dl, ok := ctx.Deadline()
		if !ok {
			return errors.New("ctx của lệnh không có hạn")
		}
		left = time.Until(dl)
		return cl.Do(ctx, cl.B().Set().Key("c:x").Value("1").Build()).Error()
	})
	if err == nil {
		t.Fatal("SET khi máy chủ tạm dừng thành công: Do không đặt hạn riêng cho lệnh")
	}
	if left > 50*time.Millisecond {
		t.Fatalf("hạn của lệnh còn %s lúc fn chạy, muốn ≤ 50 ms (OpTimeout mặc định; máy bận có thể đã hết hạn, ≤ 0)", left)
	}
	if !errors.Is(err, cache.ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lỗi hạn giờ phải là ErrUnavailable và DeadlineExceeded: %v", err)
	}
	var errs int64
	for _, r := range reg.Drain() {
		if r.Name == "valkey_errors" && r.Label == "cache:set" {
			errs = r.Count
		}
	}
	if errs != 1 {
		t.Fatalf("valkey_errors cache:set = %d, muốn 1", errs)
	}
}

func TestConn_Nil(t *testing.T) {
	var c *cache.Conn
	called := false
	err := c.Do(t.Context(), "get", func(context.Context, valkey.Client) error { called = true; return nil })
	if !errors.Is(err, cache.ErrUnavailable) || called {
		t.Fatalf("Conn nil: lỗi %v, fn gọi %v", err, called)
	}
	if err := c.Ping(t.Context()); !errors.Is(err, cache.ErrUnavailable) {
		t.Fatalf("Ping trên Conn nil: %v", err)
	}
	if h := c.Health(t.Context()); h != cache.HealthOff {
		t.Fatalf("Health trên Conn nil = %q, muốn off", h)
	}
	c.Close()
}

// Valkey ngừng: Dial vẫn được (api chạy khi Valkey ngừng), lệnh trả ErrUnavailable, 5 lần thì cầu dao mở.
func TestConn_ServerDown(t *testing.T) {
	vk := testkit.StartValkey(t)
	url := vk.URL("crm_cache")
	vk.Stop(t)
	reg := metrics.NewRegistry()
	c := dial(t, url, reg)
	for i := range 5 {
		if err := set(t.Context(), c, "c:x", "1"); !errors.Is(err, cache.ErrUnavailable) {
			t.Fatalf("lần %d khi Valkey ngừng: %v", i+1, err)
		}
	}
	if h := c.Health(t.Context()); h != cache.HealthDown {
		t.Fatalf("Health = %q, muốn down", h)
	}
	if g := gauge(reg, "cache"); g != 1 {
		t.Fatalf("valkey_breaker_open = %v, muốn 1", g)
	}
}

func TestDial_BadInput(t *testing.T) {
	for _, tc := range []struct{ url, name, op string }{
		{"http://u:p@127.0.0.1:1/0", "cache", ""},
		{"valkey://u:matkhau@[::1/0", "cache", ""},
		{"valkey://u:p@127.0.0.1:1/0", "khac", ""},
	} {
		c, err := cache.Dial(t.Context(), tc.url, cache.DialOptions{Name: tc.name})
		if err == nil {
			c.Close()
			t.Errorf("Dial(%q, %q) không lỗi", tc.url, tc.name)
			continue
		}
		if strings.Contains(err.Error(), "matkhau") || strings.Contains(err.Error(), ":p@") {
			t.Errorf("lỗi Dial lộ mật khẩu: %v", err)
		}
	}
	c, err := cache.Dial(t.Context(), "valkey://u:p@127.0.0.1:1/0", cache.DialOptions{Name: "gate"})
	if err != nil {
		t.Fatalf("Dial tới cổng không có ai nghe phải được: %v", err)
	}
	defer c.Close()
	for _, op := range []string{"", "GET", "a b", "phone:0912345678", strings.Repeat("a", 41)} {
		err := c.Do(t.Context(), op, func(context.Context, valkey.Client) error { return nil })
		if !errors.Is(err, cache.ErrBadOp) {
			t.Errorf("op %q: lỗi %v, muốn ErrBadOp", op, err)
		}
	}
}

// Lệnh lỗi với khóa băm từ SĐT: lỗi (log ở mép) không chứa SĐT gốc; lỗi quyền là lỗi của lệnh, không phải ngừng.
func TestConn_ErrorLogHasNoPhone(t *testing.T) {
	vk := testkit.StartValkey(t)
	c := dial(t, vk.URL("crm_cache"), nil)
	const phone = "0912345678"
	key := cache.SubjectKey([]byte("khoa-bi-mat-0123456789abcdef-0123"), "phone", phone)
	err := set(t.Context(), c, key, phone) // crm_cache không được ghi phone:* → NOPERM
	if err == nil || errors.Is(err, cache.ErrUnavailable) {
		t.Fatalf("SET %s: lỗi %v, muốn NOPERM (không phải ngừng)", key, err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Error("valkey", "err", err)
	if strings.Contains(buf.String(), phone) || strings.Contains(buf.String(), "912345678") {
		t.Fatalf("dòng log chứa SĐT: %s", buf.String())
	}
}
