package testkit

import (
	"context"
	"errors"
	"slices"
	"sync"
)

// Chế độ của FakeExternal.
const (
	// FakeOK: bên ngoài nhận lệnh và trả lời "xong".
	FakeOK = "ok"
	// FakeFailBeforeSend: lỗi trước khi gửi (bên ngoài chắc chắn chưa nhận), trả ErrFakeNotSent.
	FakeFailBeforeSend = "fail_before_send"
	// FakeTimeoutAfterSend: bên ngoài đã nhận nhưng câu trả lời không về, trả ErrFakeTimeout.
	FakeTimeoutAfterSend = "timeout_after_send"
	// FakeHangAfterSend: bên ngoài đã nhận, rồi treo tới khi ctx hủy, trả ctx.Err().
	FakeHangAfterSend = "hang_after_send"
	// FakeReject: bên ngoài từ chối (chính sách, dữ liệu sai), không ghi nhận, trả ErrFakeRejected.
	FakeReject = "reject"
)

// Lỗi FakeExternal.Send trả theo chế độ.
var (
	ErrFakeNotSent  = errors.New("fake external: chưa gửi")
	ErrFakeTimeout  = errors.New("fake external: quá thời gian sau khi gửi")
	ErrFakeRejected = errors.New("fake external: từ chối")
)

// FakeExternal là một "bên ngoài" giả cho test lệnh ra ngoài (Task 12): ghi lại mọi ref nó đã thật sự nhận. Mode
// rỗng là FakeOK. Đặt Mode trước khi dùng; đổi chế độ trong lúc đang có lời gọi thì dùng SetMode. Dùng được từ nhiều
// goroutine.
type FakeExternal struct {
	Mode string

	mu    sync.Mutex
	calls []string
}

// SetMode đổi chế độ cho các lời gọi sau.
func (f *FakeExternal) SetMode(m string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Mode = m
}

// Calls trả các ref bên ngoài đã ghi nhận, theo thứ tự (một ref xuất hiện hai lần nghĩa là đã nhận hai lần).
func (f *FakeExternal) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// Has báo bên ngoài đã ghi nhận ref chưa (dùng cho Lookup nói thật).
func (f *FakeExternal) Has(ref string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.calls, ref)
}

// Record ghi nhận ref như bên ngoài vừa nhận, không qua Send (giả lập worker đã gửi rồi chết trước khi ghi kết quả).
func (f *FakeExternal) Record(ref string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ref)
}

// Send gửi ref theo chế độ hiện tại. ctx đã hủy trước khi gửi thì không ghi nhận, trả ctx.Err().
func (f *FakeExternal) Send(ctx context.Context, ref string) error {
	if err := ctx.Err(); err != nil {
		return err //nolint:wrapcheck // lỗi của ctx trả nguyên cho bên gọi kiểm bằng errors.Is
	}
	f.mu.Lock()
	mode := f.Mode
	switch mode {
	case FakeFailBeforeSend, FakeReject:
	default:
		f.calls = append(f.calls, ref)
	}
	f.mu.Unlock()
	switch mode {
	case FakeFailBeforeSend:
		return ErrFakeNotSent
	case FakeReject:
		return ErrFakeRejected
	case FakeTimeoutAfterSend:
		return ErrFakeTimeout
	case FakeHangAfterSend:
		<-ctx.Done()
		return ctx.Err() //nolint:wrapcheck // như trên
	}
	return nil
}
