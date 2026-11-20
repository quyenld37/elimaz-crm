package imager_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/imager"
)

// startGateGuard: chốt chống treo khi chờ script giả ghi pidfile (không phải tiêu chí đúng/sai).
const startGateGuard = 30 * time.Second

// startGate là ctx đưa cho lệnh con: hết (Done đóng, Err = lỗi của ctx cha, thường là DeadlineExceeded) khi ctx cha
// đã hết VÀ script giả đã ghi xong pidfile. Nhờ vậy "lệnh treo bị giết khi hết hạn" luôn được kiểm trên một lệnh đã
// thật sự chạy, kể cả khi máy tải cao làm sh khởi động chậm hơn hạn (Task 440: hạn 1 s hết trước khi script ghi
// pidfile → "script giả chưa chạy"). Hạn vẫn do bên tạo ctx cha đặt (Process hay test); cửa này chỉ lùi lúc giết tới
// khi tiến trình đã khởi động, không bao giờ giết sớm hơn hạn.
type startGate struct {
	context.Context //nolint:containedctx // ctx cha: Deadline, Value
	done            chan struct{}
}

func (g *startGate) Done() <-chan struct{} { return g.done }

func (g *startGate) Err() error {
	select {
	case <-g.done:
		return g.Context.Err() //nolint:wrapcheck // giữ nguyên lỗi của ctx cha, như context chuẩn
	default:
		return nil
	}
}

// gateOnPidfile trả ctx chỉ hết sau khi parent hết và pidfile đã có số tiến trình (dòng kết thúc bằng "\n": echo đã
// ghi xong, không đọc nhầm tệp vừa mở chưa có nội dung). Chờ quá startGateGuard thì vẫn mở cửa, để test đỏ ở câu
// kiểm pidfile thay vì treo. Goroutine chờ thuộc về t: dọn dẹp của test dừng nó và chờ nó kết thúc.
func gateOnPidfile(parent context.Context, t *testing.T, pidfile string) context.Context {
	t.Helper()
	g := &startGate{Context: parent, done: make(chan struct{})}
	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		<-g.done
	})
	go func() {
		defer close(g.done)
		select {
		case <-parent.Done():
		case <-stop:
			return
		}
		guard := time.NewTimer(startGateGuard)
		defer guard.Stop()
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			if b, err := os.ReadFile(pidfile); err == nil && bytes.HasSuffix(b, []byte("\n")) {
				return
			}
			select {
			case <-tick.C:
			case <-guard.C:
				return
			case <-stop:
				return
			}
		}
	}()
	return g
}

// gatedRunner bọc Runner thật: lệnh hang (script giả) chạy với gateOnPidfile(ctx của bước); lệnh khác chạy nguyên ctx.
// Ghi lại hạn của ctx bước mà Process đưa cho lệnh hang (để test kiểm Process có đặt hạn bước).
type gatedRunner struct {
	t         *testing.T
	inner     imager.Runner
	hang      string
	pidfile   string
	deadlines chan time.Duration // còn lại bao lâu tới hạn, đo lúc Process gọi Run lệnh hang
}

func (r gatedRunner) Run(ctx context.Context, name string, args []string, memLimit int64) ([]byte, error) {
	if name != r.hang {
		return r.inner.Run(ctx, name, args, memLimit) //nolint:wrapcheck // test: trả nguyên lỗi của Runner thật
	}
	left := time.Duration(-1)
	if d, ok := ctx.Deadline(); ok {
		left = time.Until(d)
	}
	select {
	case r.deadlines <- left:
	default:
	}
	return r.inner.Run(gateOnPidfile(ctx, r.t, r.pidfile), name, args, memLimit) //nolint:wrapcheck // như trên
}

// TestStartGate: cửa chỉ mở khi cả hạn cha đã hết và pidfile đã ghi xong; Err trước khi mở là nil, sau là lỗi cha.
func TestStartGate(t *testing.T) {
	pidfile := t.TempDir() + "/pid"
	parent, cancel := context.WithCancel(t.Context())
	g := gateOnPidfile(parent, t, pidfile)
	closed := func() bool {
		select {
		case <-g.Done():
			return true
		default:
			return false
		}
	}
	if err := os.WriteFile(pidfile, []byte("12"), 0o600); err != nil { // chưa xong dòng
		t.Fatal(err)
	}
	cancel()
	time.Sleep(50 * time.Millisecond) // chỉ để cửa có dịp mở sai; đúng/sai không phụ thuộc độ dài này
	if closed() || g.Err() != nil {
		t.Fatalf("cửa mở khi pidfile chưa ghi xong: %v", g.Err())
	}
	if err := os.WriteFile(pidfile, []byte("12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-g.Done():
	case <-time.After(startGateGuard):
		t.Fatal("cửa không mở sau khi hạn hết và pidfile đã ghi")
	}
	if !errors.Is(g.Err(), context.Canceled) {
		t.Fatalf("Err = %v, muốn lỗi của ctx cha", g.Err())
	}
}
