// Package schedules là việc định kỳ (spec 04 mục 5.1) và dọn dữ liệu: lịch nằm trong platform.schedules; tới kỳ,
// Ticker của vai trò worker ghi một việc (lịch toàn hệ thống) hoặc mỗi tenant một việc (lịch theo tenant) lên hàng đợi
// chung, với DedupeKey theo kỳ nên nhiều worker không tạo trùng. Việc chạy bằng queue.Handler như mọi việc khác.
package schedules

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/platform/queue"
)

// minEvery là chu kỳ ngắn nhất (CHECK every_seconds >= 15 của platform.schedules).
const minEvery = 15 * time.Second

// ErrInvalidSchedule báo Schedule có tên rỗng, trùng tên, chu kỳ dưới 15 s hoặc không tròn giây, hoặc thiếu Kind.
var ErrInvalidSchedule = errors.New("schedules: lịch không hợp lệ")

// Schedule là một lịch.
type Schedule struct {
	// Name là tên cố định, khóa của dòng platform.schedules và tiền tố của DedupeKey "<Name>:<kỳ RFC3339>".
	Name string
	// Every: chu kỳ, tròn giây, tối thiểu 15 s.
	Every time.Duration
	// PerTenant: mỗi tenant một việc (chạy trong WithTenant); false: một việc không tenant.
	PerTenant bool
	// Kind: loại việc được ghi; vai trò worker phải có handler của Kind.Name.
	Kind queue.Kind
}

// Registry là sổ lịch của tiến trình, nạp một lần lúc khởi động (cmd/crm). Không an toàn khi Define chạy đồng thời
// với Ticker.
type Registry struct {
	defs []Schedule
}

// NewRegistry trả sổ rỗng.
func NewRegistry() *Registry { return &Registry{} }

// Define thêm s vào sổ.
func (r *Registry) Define(s Schedule) error {
	switch {
	case s.Name == "":
		return fmt.Errorf("%w: thiếu tên", ErrInvalidSchedule)
	case slices.ContainsFunc(r.defs, func(o Schedule) bool { return o.Name == s.Name }):
		return fmt.Errorf("%w: trùng tên %q", ErrInvalidSchedule, s.Name)
	case s.Every < minEvery || s.Every%time.Second != 0:
		return fmt.Errorf("%w: %q chu kỳ %v (tròn giây, tối thiểu %v)", ErrInvalidSchedule, s.Name, s.Every, minEvery)
	case s.Kind.Name == "":
		return fmt.Errorf("%w: %q thiếu Kind", ErrInvalidSchedule, s.Name)
	}
	r.defs = append(r.defs, s)
	// Theo tên tăng dần: sync khóa dòng lịch theo khóa chính tăng dần (ADR-46 điểm 4).
	slices.SortFunc(r.defs, func(a, b Schedule) int { return strings.Compare(a.Name, b.Name) })
	return nil
}

func (r *Registry) find(name string) (Schedule, bool) {
	i := slices.IndexFunc(r.defs, func(s Schedule) bool { return s.Name == name })
	if i < 0 {
		return Schedule{}, false
	}
	return r.defs[i], true
}

func (r *Registry) names() []string {
	out := make([]string, len(r.defs))
	for i, s := range r.defs {
		out[i] = s.Name
	}
	return out
}

func (r *Registry) anyPerTenant() bool {
	return slices.ContainsFunc(r.defs, func(s Schedule) bool { return s.PerTenant })
}

// slotAt trả kỳ của lịch tới hạn (mốc next + k·every lớn nhất ≤ now) và mốc kế tiếp (kỳ + every, luôn > now). Lịch
// trễ nhiều kỳ chỉ chạy kỳ mới nhất.
func slotAt(next, now time.Time, every time.Duration) (slot, following time.Time) {
	k := now.Sub(next) / every
	slot = next.Add(k * every)
	return slot, slot.Add(every)
}
