package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/httpapi/web"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/modules/promotion"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// flashRetry là nhãn flash_result của lượt mua phải thử lại (không lưu).
const flashRetry = "retry"

// WebFlashBuy (POST /api/v1/web/flash-sales/{saleId}/items/{itemId}/buy, web.flash.buy, x-admission flash,
// Idempotency-Key bắt buộc; Task 203 / M5-20). Đã qua cổng nhận vào (admission.go) và khóa chống trùng. Chuẩn hóa SĐT
// (sai → 400 phone_required) → tìm hay tạo khách (web.Flash.Customer, transaction riêng TxAPI) → WriteTxWith với
// db.TxFlashBuy: món của đợt, order.BuyFlash, lưu kết quả cuối (bought, sold_out, over_limit, not_open → 200) cùng
// transaction. Không lưu, trả lại khóa: suất đang bị lượt khác khóa (ErrFlashRetry → 503 retry_later, cổng thêm
// Retry-After), món không có hay không thuộc đợt (404), đầu vào hay biến thể không hợp lệ (422); lỗi tạm của database →
// 503 (WriteTxWith). Sau commit: sold_out → dựng cờ hết suất (Deps.FlashGate, ngoài transaction, lỗi bỏ qua).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) WebFlashBuy(ctx context.Context, req *gen.WebFlashBuyRequest, p gen.WebFlashBuyParams,
) (gen.WebFlashBuyRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	started := time.Now()
	phone, err := customer.NormalizePhone(req.Phone)
	if err != nil {
		return nil, errPhoneRequired
	}
	cust, err := h.d.webFlash.Customer(ctx, u.TenantID, phone, customer.Actor{
		Type: customer.ActorService, ID: u.ServiceKeyID, RequestID: RequestID(ctx),
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // web đã bọc
	}
	in := web.FlashBuyInput{
		SaleID: p.SaleId, ItemID: p.ItemId, CustomerID: cust, Qty: int(req.Qty), ExternalRef: req.ExternalRef,
	}
	a := order.Actor{TenantID: u.TenantID, UserID: u.ServiceKeyID, Type: order.ActorService}
	txCtx, span := otel.Tracer(flashTracer).Start(ctx, "flash.buy")
	var out *gen.WebFlashBuyResult
	var refused error
	err = WriteTxWith(txCtx, h.d, u.TenantID, db.TxFlashBuy, func(tx pgx.Tx) (int, any, bool, error) {
		out, refused = nil, nil // fn có thể chạy lại (ADR-46): gán lại mỗi lần
		res, err := h.d.webFlash.Buy(txCtx, tx, in, a)
		switch {
		case flashRefusal(err):
			refused = err
			return 0, nil, false, nil
		case err != nil:
			return 0, nil, false, fmt.Errorf("lượt mua flash sale: %w", err)
		}
		out = web.FlashResultOut(res)
		return http.StatusOK, out, true, nil
	})
	result := flashRetry
	if err == nil && refused == nil && out != nil {
		result = string(out.Result)
	}
	span.SetAttributes(attribute.String("flash.result", result))
	span.End()
	h.flashMetrics(result, started)
	switch {
	case err != nil:
		return nil, err
	case refused != nil:
		return nil, fmt.Errorf("lượt mua flash sale: %w", refused)
	}
	if out.Result == gen.WebFlashBuyResultResultSoldOut && h.d.FlashGate != nil {
		if err := h.d.FlashGate.SetState(ctx, u.TenantID, p.ItemId, 0, true); err != nil {
			// Không lỗi cho khách: lượt sau xuống database rồi dựng lại; vòng dựng lại (M5-21) cũng ghi.
			h.d.Logger.LogAttrs(ctx, slog.LevelWarn, "không dựng được cờ hết suất flash sale",
				slog.String("request_id", RequestID(ctx)), logging.Err(err))
		}
	}
	return out, nil
}

// flashRefusal: lỗi của lượt mua trả lời ngay nhưng không lưu theo khóa (gửi lại cùng khóa chạy từ đầu).
func flashRefusal(err error) bool {
	return errors.Is(err, order.ErrFlashRetry) || errors.Is(err, promotion.ErrNotFound) ||
		errors.Is(err, order.ErrInvalidInput) || errors.Is(err, order.ErrNotForSale)
}

// flashMetrics ghi flash_buy_ms (nhãn kết quả) và flash_result của lượt đã qua cổng.
func (h *handler) flashMetrics(result string, started time.Time) {
	if h.d.Metrics == nil {
		return
	}
	h.d.Metrics.Observe("flash_buy_ms", float64(time.Since(started).Milliseconds()), result)
	h.d.Metrics.Inc("flash_result", result, 1)
}
