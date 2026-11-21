package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
)

// Vùng quotes (kế hoạch M3, Task M3-13): so phí các hãng trên đơn. fulfillment.quote là quyền theo kho: handler đọc
// đơn theo người bán mặc định (ADR-29) rồi kiểm quyền ở kho của đơn bằng canAt, trong cùng transaction. Báo phí là
// đọc ở hãng nên không ghi audit.

const permQuote = identity.Permission("fulfillment.quote")

// isQuoteRefusal: lỗi nghiệp vụ của fulfillment, hay đơn không có (order.ErrNotFound → 404).
func isQuoteRefusal(err error) bool {
	return isShipmentRefusal(err) || errors.Is(err, order.ErrNotFound)
}

// quoteOrder đọc đơn id theo người bán mặc định và kiểm u có fulfillment.quote ở kho của đơn.
func quoteOrder(ctx context.Context, tx pgx.Tx, u identity.AuthUser, id uuid.UUID) (order.Order, error) {
	seller, err := catalog.DefaultSeller(ctx, tx)
	if err != nil {
		return order.Order{}, err //nolint:wrapcheck // sentinel của catalog
	}
	o, err := order.Get(ctx, tx, seller, id)
	if err != nil {
		return order.Order{}, err //nolint:wrapcheck // sentinel của order
	}
	return o, canAt(u, o.WarehouseID, permQuote)
}

// RequestShippingQuotes (POST /api/v1/orders/{id}/shipping-quotes, fulfillment.quote ở kho của đơn) → 202 {requestId,
// accounts}: mỗi tài khoản một việc báo phí. Đơn đã có vận đơn → 409 shipment_exists; đơn lạ → 404; tài khoản chọn
// không có → 404, không báo phí được → 409 carrier_account_unavailable. Bấm lại cùng khóa → cùng requestId.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RequestShippingQuotes(ctx context.Context, req *gen.RequestShippingQuotesRequest, params gen.RequestShippingQuotesParams) (gen.RequestShippingQuotesRes, error) {
	s, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	in := fulfillment.QuoteRequest{
		WeightGrams: int(req.WeightGrams.Or(0)), DeclaredValue: int64(req.DeclaredValue.Or(0)),
		AccountIDs: req.CarrierAccountIds,
	}
	if o, ok := req.Options.Get(); ok {
		opts := carrierOptionsIn(o)
		in.Options = &opts
	}
	out, err := h.refusableWrite(ctx, http.StatusAccepted, isQuoteRefusal, "so phí",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			o, err := quoteOrder(ctx, tx, u, params.ID)
			if err != nil {
				return nil, err
			}
			acc, err := s.RequestQuotes(ctx, tx, o, in, fulfillment.Actor{TenantID: u.TenantID, UserID: u.UserID})
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			body := &gen.ShippingQuoteRequestAccepted{RequestId: acc.RequestID, Accounts: make([]gen.ShippingQuoteAccount, 0, len(acc.Accounts))}
			for _, a := range acc.Accounts {
				body.Accounts = append(body.Accounts, gen.ShippingQuoteAccount{ID: a.ID, Name: a.Name, CarrierKey: a.CarrierKey})
			}
			return body, nil
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.ShippingQuoteRequestAccepted), nil //nolint:forcetypeassert // fn trên trả *gen.ShippingQuoteRequestAccepted
}

// ListShippingQuotes (GET /api/v1/orders/{id}/shipping-quotes?requestId=, fulfillment.quote ở kho của đơn): kết quả
// một lần so phí (mặc định lần gần nhất), ok theo phí tăng dần, rồi pending, rồi lỗi; feeTotal chỉ có ở dòng ok.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListShippingQuotes(ctx context.Context, params gen.ListShippingQuotesParams) (gen.ListShippingQuotesRes, error) {
	s, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	var reqID *uuid.UUID
	if v, ok := params.RequestId.Get(); ok {
		reqID = &v
	}
	var got uuid.UUID
	var rows []fulfillment.QuoteRow
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) error {
		o, err := quoteOrder(ctx, tx, u, params.ID)
		if err != nil {
			return err
		}
		got, rows, err = s.ListQuotes(ctx, tx, o.ID, reqID)
		return err //nolint:wrapcheck // lỗi đọc của fulfillment
	}); err != nil {
		return nil, fmt.Errorf("đọc báo phí: %w", err)
	}
	out := &gen.ShippingQuoteList{Items: make([]gen.ShippingQuote, 0, len(rows))}
	if got != uuid.Nil {
		out.RequestId = gen.NewOptUUID(got)
	}
	for _, r := range rows {
		out.Items = append(out.Items, shippingQuoteOut(r))
	}
	return out, nil
}

func shippingQuoteOut(r fulfillment.QuoteRow) gen.ShippingQuote {
	q := gen.ShippingQuote{
		CarrierAccountId: r.CarrierAccountID, AccountName: r.AccountName, CarrierKey: r.CarrierKey,
		Status: gen.ShippingQuoteStatus(r.Status),
	}
	if r.Status == "ok" {
		q.QuoteId = gen.NewOptUUID(r.QuoteID)
	}
	if r.Service != "" {
		q.Service = gen.NewOptString(r.Service)
	}
	if r.ServiceName != "" {
		q.ServiceName = gen.NewOptString(r.ServiceName)
	}
	if r.FeeTotal != nil && *r.FeeTotal > 0 {
		q.FeeTotal = gen.NewOptMoney(gen.Money(*r.FeeTotal))
	}
	if r.ExpectedDays != nil {
		q.ExpectedDays = gen.NewOptInt32(int32(*r.ExpectedDays)) //nolint:gosec // cột int của PostgreSQL
	}
	if r.ValidUntil != nil {
		q.ValidUntil = gen.NewOptDateTime(*r.ValidUntil)
	}
	if r.ErrorCode != "" {
		q.ErrorCode = gen.NewOptString(r.ErrorCode)
	}
	return q
}
