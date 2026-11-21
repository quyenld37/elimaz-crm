// Tệp mẫu của webexempt_test: handler của route service_key khai x-idempotency: exempt.
package httpapi

import "context"

type handler struct{}

// WebQuote (webQuote) ghi qua một hàm phụ cùng gói: phải bị báo.
func (h *handler) WebQuote(ctx context.Context) error {
	return h.saveQuote(ctx)
}

func (h *handler) saveQuote(ctx context.Context) error {
	return persist(ctx)
}

func persist(ctx context.Context) error {
	return WriteTx(ctx, nil, func() error { return nil })
}

// WebAudit (webAudit) truyền hàm ghi làm giá trị: phải bị báo.
func (h *handler) WebAudit(ctx context.Context) error {
	return run(ctx, audit)
}

func audit(ctx context.Context) error { return identity.RecordAudit(ctx) }

func run(ctx context.Context, f func(context.Context) error) error { return f(ctx) }

// WebPrices (webPrices) chỉ đọc: không bị báo.
func (h *handler) WebPrices(ctx context.Context) error {
	return h.readPrices(ctx)
}

func (h *handler) readPrices(ctx context.Context) error { return nil }
