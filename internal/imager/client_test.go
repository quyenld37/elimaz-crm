package imager_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/imager"
)

func clientRequest() imager.Request {
	return imager.Request{
		SourceURL: "http://kho/src", MaxBytes: 10, MaxPixels: 10, Allowed: []string{"image/jpeg"},
		Outputs: []imager.Output{{Width: 320, Format: "jpeg", PutURL: "http://kho/out"}},
	}
}

// Client nói đúng hợp đồng của Server: 200 → Result (cả rejected); 503 → *BusyError có Retry-After; mã khác → lỗi;
// hết hạn ctx → lỗi.
func TestClient_Process(t *testing.T) {
	var got imager.Request
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("mode") {
		case "busy":
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		case "bad":
			w.WriteHeader(http.StatusBadRequest)
			return
		case "hang":
			select { // HTTP/1.1: server chỉ biết bên gọi bỏ khi đọc được kết nối; release mở khi test xong
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/process" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(imager.Result{Status: imager.StatusRejected, Reason: imager.ReasonNotImage})
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // chạy trước srv.Close
	ctx := t.Context()

	res, err := imager.NewClient(srv.URL, srv.Client(), 2).Process(ctx, clientRequest())
	if err != nil || res.Status != imager.StatusRejected || res.Reason != imager.ReasonNotImage || got.SourceURL != "http://kho/src" {
		t.Fatalf("200: %+v %v (gửi %+v)", res, err, got)
	}
	bctx, bcancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer bcancel()
	_, err = imager.NewClient(srv.URL+"/?mode=busy#", srv.Client(), 2).Process(bctx, clientRequest())
	if be, ok := errors.AsType[*imager.BusyError](err); !ok || be.RetryAfter != 3*time.Second || !errors.Is(err, imager.ErrBusy) {
		t.Fatalf("503: %v", err)
	}
	if _, err := imager.NewClient(srv.URL+"/?mode=bad#", srv.Client(), 2).Process(ctx, clientRequest()); err == nil || errors.Is(err, imager.ErrBusy) {
		t.Fatalf("400: %v", err)
	}
	hctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if _, err := imager.NewClient(srv.URL+"/?mode=hang#", srv.Client(), 2).Process(hctx, clientRequest()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("treo: %v", err)
	}
}

// Client giữ tối đa maxParallel việc cùng lúc (việc thừa chờ, không gửi); 503 thì chờ Retry-After rồi gửi lại trong
// cùng lần gọi; chờ chỗ trống quá hạn ctx → BusyError (chắc chắn chưa gửi).
func TestClient_LimitsParallelAndRetriesBusy(t *testing.T) {
	var mu sync.Mutex
	inflight, peak, calls := 0, 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		first := calls == 1
		inflight++
		peak = max(peak, inflight)
		mu.Unlock()
		defer func() { mu.Lock(); inflight--; mu.Unlock() }()
		if first { // lần đầu: bận, hẹn 1 s
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		time.Sleep(100 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(imager.Result{Status: imager.StatusOK})
	}))
	t.Cleanup(srv.Close)
	c := imager.NewClient(srv.URL, srv.Client(), 1)
	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range 3 {
		wg.Go(func() {
			_, errs[i] = c.Process(t.Context(), clientRequest())
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
	}
	if peak != 1 || calls != 4 {
		t.Fatalf("cao nhất %d việc cùng lúc (muốn 1), %d lần gọi (muốn 4: một 503 rồi gửi lại)", peak, calls)
	}
	// Chỗ duy nhất đang bận: lần gọi khác chờ quá hạn ctx → BusyError.
	hold := imager.NewClient(srv.URL+"/?x#", srv.Client(), 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = hold.Process(t.Context(), clientRequest())
	}()
	time.Sleep(20 * time.Millisecond)
	sctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := hold.Process(sctx, clientRequest()); !errors.Is(err, imager.ErrBusy) {
		t.Fatalf("chờ chỗ quá hạn: muốn ErrBusy, nhận %v", err)
	}
	<-done
}
