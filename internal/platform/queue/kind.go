package queue

import (
	"errors"
	"fmt"
	"time"
)

// Chế độ của một loại việc (cột mode).
const (
	// ModeTransactional: handler chỉ đụng database, chạy trong một transaction; done ghi cùng transaction đó.
	ModeTransactional = "transactional"
	// ModeCommand: lệnh ra ngoài; Worker giao cho WorkerOptions.Commands (commands.Runner), không chạy trong transaction.
	ModeCommand = "command"
)

// Việc làm gì khi giữ chỗ hết hạn mà worker chưa xong (cột on_lease_expired).
const (
	// LeaseRetry: về hàng chờ, chạy lại (hết lượt thì dead).
	LeaseRetry = "retry"
	// LeaseInDoubt: thành in_doubt, không tự chạy lại (lệnh có thể đã ra ngoài).
	LeaseInDoubt = "in_doubt"
)

// Trạng thái kết thúc truyền cho TerminalHook.OnTerminal.
const (
	StatusDead    = "dead"
	StatusInDoubt = "in_doubt"
	StatusExpired = "expired"
)

// ErrInvalidJob báo Kind hoặc NewJob không hợp lệ (lỗi của bên gọi, không ghi gì).
var ErrInvalidJob = errors.New("queue: việc không hợp lệ")

// Policy là chính sách của một loại việc. Trường bằng giá trị rỗng lấy giá trị của DefaultPolicy (BlockOnDead và
// ExpiresAfter rỗng nghĩa là không chặn, không hết hạn).
type Policy struct {
	// Mode: ModeTransactional hoặc ModeCommand.
	Mode string
	// MaxAttempts: số lần chạy tối đa; lỗi ở lần thứ MaxAttempts thì dead.
	MaxAttempts int
	// BlockOnDead: việc dead chặn các việc sau cùng (tenant, OrderKey) (sự kiện nội bộ theo đối tượng, spec 04
	// mục 5.1); false cho sự kiện vào (một tin lỗi không chặn tin sau).
	BlockOnDead bool
	// OnLeaseExpired: LeaseRetry hoặc LeaseInDoubt.
	OnLeaseExpired string
	// ExpiresAfter > 0: việc còn pending quá thời gian này kể từ lúc ghi thì expired.
	ExpiresAfter time.Duration
	// Timeout: hạn của một lần Handle (spec 04 mục 5.1: tối đa 30 s, nhỏ hơn giữ chỗ 90 s).
	Timeout time.Duration
}

// DefaultPolicy là chính sách mặc định: việc transactional, 8 lần, hết giữ chỗ thì chạy lại, mỗi lần tối đa 30 s (spec 04 mục 5.1).
//
//nolint:gochecknoglobals // Interfaces của kế hoạch M0 Task 11 khai là var; chỉ đọc, không mã nào gán lại.
var DefaultPolicy = Policy{Mode: ModeTransactional, MaxAttempts: 8, OnLeaseExpired: LeaseRetry, Timeout: 30 * time.Second}

// Kind là một loại việc: tên (quyết định handler) và chính sách.
type Kind struct {
	Name   string
	Policy Policy
}

// normalized trả bản sao của p với trường rỗng lấy từ DefaultPolicy, hoặc ErrInvalidJob khi giá trị lạ.
func (p Policy) normalized() (Policy, error) {
	if p.Mode == "" {
		p.Mode = DefaultPolicy.Mode
	}
	if p.MaxAttempts == 0 {
		p.MaxAttempts = DefaultPolicy.MaxAttempts
	}
	if p.OnLeaseExpired == "" {
		p.OnLeaseExpired = DefaultPolicy.OnLeaseExpired
	}
	if p.Timeout == 0 {
		p.Timeout = DefaultPolicy.Timeout
	}
	switch {
	case p.Mode != ModeTransactional && p.Mode != ModeCommand:
		return p, fmt.Errorf("%w: mode %q", ErrInvalidJob, p.Mode)
	case p.OnLeaseExpired != LeaseRetry && p.OnLeaseExpired != LeaseInDoubt:
		return p, fmt.Errorf("%w: on_lease_expired %q", ErrInvalidJob, p.OnLeaseExpired)
	case p.MaxAttempts < 1, p.Timeout < 0, p.ExpiresAfter < 0:
		return p, fmt.Errorf("%w: MaxAttempts, Timeout, ExpiresAfter phải dương", ErrInvalidJob)
	}
	return p, nil
}
