package formats

import "fmt"

// Registry là sổ định dạng bảng kê của tiến trình: elimaz_v1 cộng định dạng của các adapter hãng, chỉ đọc sau khi
// dựng.
type Registry struct {
	byKey map[string]Format
	order []string // thứ tự khai: định dạng mặc định của hãng là định dạng đầu tiên của hãng đó
}

// NewRegistry dựng sổ từ elimaz_v1 và carrier (fulfillment.Registry.StatementFormats, theo thứ tự khóa hãng rồi thứ
// tự adapter khai). Trùng Key → ErrDuplicateKey; định dạng khai sai → ErrInvalid. Lỗi là lỗi khởi động.
func NewRegistry(carrier []Format) (*Registry, error) {
	r := &Registry{byKey: map[string]Format{}}
	for _, f := range append([]Format{ElimazV1()}, carrier...) {
		v, err := validate(f)
		if err != nil {
			return nil, err
		}
		if _, dup := r.byKey[v.Key]; dup {
			return nil, fmt.Errorf("định dạng %q: %w", v.Key, ErrDuplicateKey)
		}
		r.byKey[v.Key] = v
		r.order = append(r.order, v.Key)
	}
	return r, nil
}

// Get trả định dạng theo khóa (tên cột đã chuẩn hóa theo ColumnName, HeaderRow ≥ 1).
func (r *Registry) Get(key string) (Format, bool) {
	f, ok := r.byKey[key]
	return f, ok
}

// For chọn định dạng cho tài khoản của hãng carrierKey: key rỗng → định dạng đầu tiên của hãng, hãng không khai định
// dạng nào → elimaz_v1; key khác rỗng phải là elimaz_v1 hoặc định dạng của đúng hãng đó, không thì ErrUnknown.
func (r *Registry) For(carrierKey, key string) (Format, error) {
	if key == "" {
		for _, k := range r.order {
			if f := r.byKey[k]; f.CarrierKey == carrierKey {
				return f, nil
			}
		}
		return r.byKey[ElimazV1Key], nil
	}
	f, ok := r.byKey[key]
	if !ok || (f.CarrierKey != "" && f.CarrierKey != carrierKey) {
		return Format{}, fmt.Errorf("định dạng %q cho hãng %q: %w", key, carrierKey, ErrUnknown)
	}
	return f, nil
}
