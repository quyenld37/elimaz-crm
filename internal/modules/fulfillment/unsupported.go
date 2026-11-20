package fulfillment

import "context"

// Unsupported là phần "không hỗ trợ" của CarrierAdapter để nhúng vào adapter còn là khung hay chỉ làm một phần: mọi
// phương thức gọi hãng trả ErrNotSupported, Capabilities toàn false, mã thô nào cũng lạ, chữ ký webhook luôn sai.
// Adapter nhúng nó rồi tự cài Key và những phương thức mình làm được (khai true trong Capabilities).
//
// Khi nào không dùng: adapter đã làm đủ mọi khả năng thì không nhúng (để trình biên dịch báo khi interface thêm
// phương thức).
type Unsupported struct{}

// Capabilities trả toàn false.
func (Unsupported) Capabilities() Capabilities { return Capabilities{} }

// CreateShipment trả ErrNotSupported.
func (Unsupported) CreateShipment(context.Context, Account, CreateRequest) (CreateResult, error) {
	return CreateResult{}, ErrNotSupported
}

// LookupByCode trả ErrNotSupported.
func (Unsupported) LookupByCode(context.Context, Account, string) (LookupResult, error) {
	return LookupResult{}, ErrNotSupported
}

// GetStatus trả ErrNotSupported.
func (Unsupported) GetStatus(context.Context, Account, string) (StatusResult, error) {
	return StatusResult{}, ErrNotSupported
}

// MapStatus coi mọi mã là lạ.
func (Unsupported) MapStatus(string) (Status, bool) { return "", false }

// MapCanonical coi mọi mã là lạ.
func (Unsupported) MapCanonical(string) (Canonical, bool) { return CanonUnknown, false }

// VerifyWebhook luôn false.
func (Unsupported) VerifyWebhook([]byte, Header, string) bool { return false }

// SplitWebhook trả ErrNotSupported.
func (Unsupported) SplitWebhook([]byte) ([]WebhookSignal, error) { return nil, ErrNotSupported }

// CheckConnection trả ErrNotSupported.
func (Unsupported) CheckConnection(context.Context, Account) (Health, error) {
	return Health{}, ErrNotSupported
}

// Quote trả ErrNotSupported.
func (Unsupported) Quote(context.Context, Account, QuoteInput) ([]QuoteResult, error) {
	return nil, ErrNotSupported
}

// Cancel trả ErrNotSupported.
func (Unsupported) Cancel(context.Context, Account, string) (CancelResult, error) {
	return "", ErrNotSupported
}

// Track trả ErrNotSupported.
func (Unsupported) Track(context.Context, Account, string, string) (Tracking, error) {
	return Tracking{}, ErrNotSupported
}

// Labels trả ErrNotSupported.
func (Unsupported) Labels(context.Context, Account, []string) (Label, error) {
	return Label{}, ErrNotSupported
}

// UpdateAddress trả ErrNotSupported.
func (Unsupported) UpdateAddress(context.Context, Account, string, Address) (UpdateResult, error) {
	return "", ErrNotSupported
}

// Redeliver trả ErrNotSupported.
func (Unsupported) Redeliver(context.Context, Account, string, string) (UpdateResult, error) {
	return "", ErrNotSupported
}

// Locations trả ErrNotSupported.
func (Unsupported) Locations(context.Context, Account, string, string) ([]Location, error) {
	return nil, ErrNotSupported
}
