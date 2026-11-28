package nhanh_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/adapters/nhanh"
)

// fakeClock là đồng hồ duy nhất của test: Sleep chỉ đẩy giờ tới, không chờ thật.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *fakeClock) Now() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now }
func (f *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	return nil
}

// seqHandler gọi fn với số thứ tự lời gọi (từ 1); calls trả số lời gọi đã nhận.
func seqHandler(fn func(n int, w http.ResponseWriter)) (h http.Handler, calls func() int) {
	var mu sync.Mutex
	n := 0
	h = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		n++
		fn(n, w)
	})
	return h, func() int { mu.Lock(); defer mu.Unlock(); return n }
}

func newTestClient(t *testing.T, h http.Handler) (*nhanh.Client, *fakeClock) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := nhanh.New(nhanh.Config{BaseURL: srv.URL, AppID: "app1", BusinessID: "biz1", Token: "tok-secret-123", HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	clk := &fakeClock{now: time.Unix(1_000_000, 0)}
	c.Now, c.Sleep = clk.Now, clk.Sleep
	return c, clk
}

// Đường ghi hay đường lạ: lỗi trước khi gửi, máy chủ giả không nhận lời gọi nào.
func TestCall_RejectsNonReadPathsBeforeSending(t *testing.T) {
	t.Parallel()
	var hits int
	var mu sync.Mutex
	c, _ := newTestClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { mu.Lock(); hits++; mu.Unlock() }))
	for _, p := range []string{
		"order/add", "product/add", "product/edit", "customer/add", "inventory/importother",
		"promotion/couponupdate", "product/list/../order/add", "", "/product/list", "product/list?x=1",
	} {
		_, err := c.Call(t.Context(), p, map[string]any{})
		if !errors.Is(err, nhanh.ErrPathNotAllowed) {
			t.Errorf("Call(%q) = %v, muốn ErrPathNotAllowed", p, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Fatalf("máy chủ nhận %d lời gọi, muốn 0", hits)
	}
}

// Danh sách đường cho phép không có đường ghi nào (dò theo tên động từ ghi).
func TestReadPaths_NoWriteVerb(t *testing.T) {
	t.Parallel()
	for _, p := range nhanh.ReadPaths() {
		for _, w := range []string{"add", "edit", "update", "delete", "import", "export", "cancel", "send"} {
			if strings.Contains(p, w) {
				t.Errorf("đường %q có động từ ghi %q", p, w)
			}
		}
	}
}

// Gửi đúng khuôn: POST, appId/businessId trên query, token ở Authorization không tiền tố; phân trang theo next.
func TestCall_RequestShapeAndPaging(t *testing.T) {
	t.Parallel()
	var got []string
	var mu sync.Mutex
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodPost || r.URL.Path != "/product/list" || r.URL.Query().Get("appId") != "app1" ||
			r.URL.Query().Get("businessId") != "biz1" || r.Header.Get("Authorization") != "tok-secret-123" {
			t.Errorf("lời gọi sai khuôn: %s %s %v", r.Method, r.URL.String(), r.Header)
		}
		var body struct {
			Paginator struct{ Next json.RawMessage }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, string(body.Paginator.Next))
		_, _ = w.Write([]byte(`{"code":1,"data":[{"id":1}],"paginator":{"next":{"id":1}}}`))
	}))
	p, err := c.Call(t.Context(), nhanh.PathProductList, map[string]any{"paginator": map[string]any{"next": map[string]int{"id": 9}}})
	if err != nil || string(p.Next) != `{"id":1}` {
		t.Fatalf("Call = %+v, %v", p, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != `{"id":9}` {
		t.Fatalf("next đã gửi = %v", got)
	}
}

// ERR_429: chờ tới unlockedAt, không gọi trong lúc khóa, rồi gọi lại thành công.
func TestCall_RateLimitWaitsUntilUnlockedAt(t *testing.T) {
	t.Parallel()
	var clk *fakeClock
	var c *nhanh.Client
	var at []time.Time
	h, calls := seqHandler(func(n int, w http.ResponseWriter) {
		at = append(at, clk.Now())
		if n == 1 {
			_, _ = w.Write([]byte(`{"code":0,"errorCode":"ERR_429","data":{"lockedSeconds":10,"unlockedAt":1000010}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":1,"data":[]}`))
	})
	c, clk = newTestClient(t, h)
	if _, err := c.Call(t.Context(), nhanh.PathCustomerList, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if calls() != 2 {
		t.Fatalf("số lời gọi = %d, muốn 2", calls())
	}
	if at[1].Before(time.Unix(1_000_010, 0)) {
		t.Fatalf("gọi lại lúc %v, trước unlockedAt", at[1])
	}
}

// Khóa quá trần chờ: lỗi ErrRateLimited ngay, không chờ, không gọi lần hai.
func TestCall_RateLimitBeyondMaxWaitFails(t *testing.T) {
	t.Parallel()
	h, calls := seqHandler(func(_ int, w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"code":0,"errorCode":"ERR_429","data":{"lockedSeconds":3600}}`))
	})
	c, _ := newTestClient(t, h)
	if _, err := c.Call(t.Context(), nhanh.PathProductList, map[string]any{}); !errors.Is(err, nhanh.ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	if calls() != 1 {
		t.Fatalf("số lời gọi = %d, muốn 1", calls())
	}
}

// Lỗi API và lỗi mạng không chép thân trả lời hay token vào lỗi.
func TestCall_ErrorsDoNotLeakBodyOrToken(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"errorCode":"ERR_403","messages":"0900000000 Nguyen Van A","data":{"mobile":"0900000000"}}`))
	}))
	_, err := c.Call(t.Context(), nhanh.PathCustomerList, map[string]any{})
	if !errors.Is(err, nhanh.ErrAPI) || !strings.Contains(err.Error(), "ERR_403") {
		t.Fatalf("err = %v", err)
	}
	for _, bad := range []string{"0900000000", "Nguyen", "tok-secret-123"} {
		if strings.Contains(err.Error(), bad) {
			t.Errorf("lỗi chứa %q: %v", bad, err)
		}
	}
}

// 5xx rồi thành công: thử lại (đọc nên an toàn); hết lượt thì ErrTransport.
func TestCall_RetriesServerErrors(t *testing.T) {
	t.Parallel()
	h, _ := seqHandler(func(n int, w http.ResponseWriter) {
		if n < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"code":1,"data":[]}`))
	})
	c, _ := newTestClient(t, h)
	if _, err := c.Call(t.Context(), nhanh.PathProductList, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	c2, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	if _, err := c2.Call(t.Context(), nhanh.PathProductList, map[string]any{}); !errors.Is(err, nhanh.ErrTransport) {
		t.Fatalf("err = %v", err)
	}
}

func TestRows(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		in   string
		want int
		bad  bool
	}{
		"mảng": {`[{"id":1},{"id":2}]`, 2, false}, "null": {`null`, 0, false}, "rỗng": {``, 0, false},
		"object theo mã": {`{"10":{"id":10},"9":{"id":9}}`, 2, false}, "số": {`5`, 0, true},
	} {
		got, err := nhanh.Rows(json.RawMessage(tc.in))
		if (err != nil) != tc.bad || len(got) != tc.want {
			t.Errorf("%s: %d dòng, lỗi %v", name, len(got), err)
		}
	}
	got, _ := nhanh.Rows(json.RawMessage(`{"10":{"id":10},"9":{"id":9}}`))
	if string(got[0]) != `{"id":9}` {
		t.Errorf("thứ tự khóa số sai: %s", got[0])
	}
}
