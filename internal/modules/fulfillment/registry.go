package fulfillment

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// ErrDuplicateCarrier là lỗi khi hai adapter cùng Key trong một Registry.
var ErrDuplicateCarrier = errors.New("fulfillment: trùng khóa hãng")

var errEmptyCarrierKey = errors.New("fulfillment: adapter có khóa rỗng")

// Registry là bộ adapter hãng theo Key, dựng một lần ở cmd/crm (internal/adapters/carrier.All), chỉ đọc sau đó.
type Registry struct {
	byKey map[string]CarrierAdapter
}

// NewRegistry dựng Registry; trùng Key → ErrDuplicateCarrier, Key rỗng → lỗi.
func NewRegistry(as ...CarrierAdapter) (*Registry, error) {
	r := &Registry{byKey: make(map[string]CarrierAdapter, len(as))}
	for _, a := range as {
		k := a.Key()
		if k == "" {
			return nil, errEmptyCarrierKey
		}
		if _, dup := r.byKey[k]; dup {
			return nil, fmt.Errorf("dựng bộ adapter hãng %q: %w", k, ErrDuplicateCarrier)
		}
		r.byKey[k] = a
	}
	return r, nil
}

// Get trả adapter theo khóa hãng.
func (r *Registry) Get(key string) (CarrierAdapter, bool) { //nolint:ireturn // bộ adapter theo khóa: kiểu cụ thể khác nhau từng hãng
	a, ok := r.byKey[key]
	return a, ok
}

// Keys trả mọi khóa hãng, sắp xếp.
func (r *Registry) Keys() []string { return slices.Sorted(maps.Keys(r.byKey)) }

// StatementFormats gom định dạng bảng kê từ mọi adapter cài StatementFormatProvider, theo thứ tự khóa hãng.
func (r *Registry) StatementFormats() []StatementFormat {
	var out []StatementFormat
	for _, k := range r.Keys() {
		if p, ok := r.byKey[k].(StatementFormatProvider); ok {
			out = append(out, p.StatementFormats()...)
		}
	}
	return out
}
