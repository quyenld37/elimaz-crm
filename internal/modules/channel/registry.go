package channel

import (
	"errors"
	"fmt"
	"slices"
)

// ErrDuplicateAdapter báo hai adapter cùng Key.
var ErrDuplicateAdapter = errors.New("channel: hai adapter cùng khóa")

// Registry là bộ adapter kênh của tiến trình, dựng một lần ở cmd/crm (chỉ cmd nối adapter, luật (k)).
type Registry struct {
	byKey map[string]MessagingAdapter
	keys  []string
}

// NewRegistry dựng bộ adapter từ as, bỏ phần tử nil (adapter chưa bật, như kênh Facebook khi chưa có cấu hình Meta);
// hai adapter cùng Key → ErrDuplicateAdapter.
func NewRegistry(as ...MessagingAdapter) (*Registry, error) {
	r := &Registry{byKey: map[string]MessagingAdapter{}}
	for _, a := range as {
		if a == nil {
			continue
		}
		k := a.Key()
		if _, dup := r.byKey[k]; dup {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateAdapter, k)
		}
		r.byKey[k] = a
		r.keys = append(r.keys, k)
	}
	slices.Sort(r.keys)
	return r, nil
}

// Get trả adapter của key.
func (r *Registry) Get(key string) (MessagingAdapter, bool) { //nolint:ireturn // adapter là interface của kênh
	a, ok := r.byKey[key]
	return a, ok
}

// Keys trả khóa mọi adapter, theo thứ tự chữ.
func (r *Registry) Keys() []string { return slices.Clone(r.keys) }

// Lookup trả adapter của key; r nil (kênh chưa bật ở tiến trình) → ok = false.
func (r *Registry) Lookup(key string) (MessagingAdapter, bool) { //nolint:ireturn // adapter là interface của kênh
	if r == nil {
		return nil, false
	}
	return r.Get(key)
}
