package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/queue"
)

// Phiếu trả, đổi hàng (kế hoạch M2, Task M2-22): sáu operation của người bán mặc định. Từ chối nghiệp vụ (đơn chưa
// giao, vượt số, phiếu sai trạng thái, version cũ, hết hàng của đơn thay thế…) không lưu theo khóa. Audit chỉ ghi mã,
// số, trạng thái (lý do trả: chỉ có hay không).

// exchangeKeyPrefix tách không gian tên khóa nghiệp vụ của đơn thay thế khỏi khóa của createOrder.
const exchangeKeyPrefix = "elimaz-crm/order.return.exchange:"

// exchangeBusinessKey là khóa nghiệp vụ của đơn thay thế suy ra từ nhân viên và Idempotency-Key của createReturn.
func exchangeBusinessKey(userID uuid.UUID, requestKey string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(exchangeKeyPrefix+userID.String()+":"+requestKey))
}

func (h *handler) orderWriter() order.Writer {
	return order.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
}

// orderWriteAsDefaultSeller chạy change (một đường ghi của order) trong refusableWrite với người bán mặc định của shop
// (ADR-29) và người làm dựng từ nhân viên đã xác thực (quyền duyệt giảm giá tính từ grant, không từ thân yêu cầu).
func (h *handler) orderWriteAsDefaultSeller(ctx context.Context, status int, refusal func(error) bool, what string,
	change func(tx pgx.Tx, w order.Writer, u identity.AuthUser, seller uuid.UUID, a order.Actor) (any, error),
) (any, error) {
	w := h.orderWriter()
	return h.refusableWrite(ctx, status, refusal, what, func(tx pgx.Tx, u identity.AuthUser) (any, error) {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của catalog
		}
		return change(tx, w, u, seller, orderActor(u))
	})
}

// isReturnRefusal: lỗi nghiệp vụ của order, inventory (đã ánh xạ ở errors.go) và lỗi của lần tạo đơn thay thế.
func isReturnRefusal(err error) bool {
	if _, _, ok := orderError(err); ok {
		return true
	}
	if _, _, ok := inventoryError(err); ok {
		return true
	}
	return isOrderRefusal(err)
}

// CreateReturn (POST /api/v1/orders/{id}/returns, order.return): phiếu khách trả theo dòng, hoặc đổi hàng kèm đơn thay
// thế (cùng transaction, giữ hàng của đơn mới) → 201 phiếu requested; chưa đổi tồn. Audit order.return.create.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CreateReturn(ctx context.Context, req *gen.CreateReturnRequest, params gen.CreateReturnParams) (gen.CreateReturnRes, error) {
	in := order.CreateReturnInput{Kind: string(req.Kind), Reason: req.Reason.Or("")}
	for _, l := range req.Lines {
		in.Lines = append(in.Lines, order.ReturnLineInput{OrderItemID: l.OrderItemId, Quantity: int(l.Qty)})
	}
	out, err := h.orderWriteAsDefaultSeller(ctx, http.StatusCreated, isReturnRefusal, "tạo phiếu trả", func(tx pgx.Tx,
		w order.Writer, u identity.AuthUser, seller uuid.UUID, a order.Actor,
	) (any, error) {
		in.Replacement = replacementInput(req, u.UserID, params.IdempotencyKey)
		r, err := w.CreateReturn(ctx, tx, seller, params.ID, in, a, time.Now().UTC())
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của order
		}
		return returnOut(r), h.audit(ctx, tx, u, "order.return.create", "return", r.ID, nil, returnAudit(r))
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Return), nil //nolint:forcetypeassert // fn trên trả *gen.Return
}

// replacementInput dịch phần replacement của createReturn sang order.CreateInput (nil khi không gửi).
func replacementInput(req *gen.CreateReturnRequest, userID uuid.UUID, key string) *order.CreateInput {
	r, ok := req.Replacement.Get()
	if !ok {
		return nil
	}
	in := &order.CreateInput{
		IdempotencyKey: exchangeBusinessKey(userID, key), ShippingFee: int64(r.ShippingFee.Or(0)),
	}
	if a, ok := r.ShipAddress.Get(); ok {
		in.ShipAddress = shipAddressIn(a)
	}
	for _, it := range r.Items {
		in.Lines = append(in.Lines, order.LineInput{
			VariantID: it.VariantId, Quantity: it.Qty, LineDiscount: int64(it.LineDiscount.Or(0)),
		})
	}
	return in
}

// ListReturns (GET /api/v1/returns, order.read): phiếu của người bán mặc định, mới nhất trước.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListReturns(ctx context.Context, params gen.ListReturnsParams) (gen.ListReturnsRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	f := order.ReturnFilter{
		Status: string(params.Status.Or("")), Kind: string(params.Kind.Or("")), Limit: int(params.Limit.Or(0)),
	}
	if c, ok := params.Cursor.Get(); ok {
		cur, err := order.ParseReturnCursor(c)
		if err != nil {
			return nil, fmt.Errorf("đọc con trỏ: %w", err)
		}
		f.After = &cur
	}
	var rs []order.Return
	var next *order.ReturnCursor
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		rs, next, err = order.ListReturns(ctx, tx, seller, f)
		return err //nolint:wrapcheck // sentinel của order
	})
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách phiếu trả: %w", err)
	}
	out := &gen.ReturnPage{Items: make([]gen.Return, 0, len(rs))}
	for _, r := range rs {
		out.Items = append(out.Items, *returnOut(r))
	}
	if next != nil {
		out.NextCursor = gen.NewOptString(next.String())
	}
	return out, nil
}

// GetReturn (GET /api/v1/returns/{returnId}, order.read): một phiếu kèm dòng; phiếu của người bán khác → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetReturn(ctx context.Context, params gen.GetReturnParams) (gen.GetReturnRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var r order.Return
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		r, err = order.GetReturn(ctx, tx, seller, params.ReturnId)
		return err //nolint:wrapcheck // sentinel của order
	})
	if err != nil {
		return nil, fmt.Errorf("đọc phiếu trả: %w", err)
	}
	return returnOut(r), nil
}

// ReceiveReturn (POST /api/v1/returns/{returnId}/receive, order.return.receive): kho ghi tình trạng từng dòng và nhập
// lại kho cùng lúc → 200 phiếu restocked. Audit order.return.receive (số dòng theo tình trạng, tiền hoàn).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ReceiveReturn(ctx context.Context, req *gen.ReceiveReturnRequest, params gen.ReceiveReturnParams) (gen.ReceiveReturnRes, error) {
	lines := make([]order.ReceiveLine, 0, len(req.Lines))
	for _, l := range req.Lines {
		lines = append(lines, order.ReceiveLine{OrderItemID: l.OrderItemId, Condition: string(l.Condition)})
	}
	return h.returnChange(ctx, "order.return.receive", "nhận phiếu trả", func(tx pgx.Tx, w order.Writer, seller uuid.UUID, a order.Actor) (order.Return, error) {
		return w.ReceiveReturn(ctx, tx, seller, params.ReturnId, req.Version, lines, a, time.Now().UTC())
	}, params.ReturnId)
}

// CloseReturn (POST /api/v1/returns/{returnId}/close, order.return): phiếu restocked → closed.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CloseReturn(ctx context.Context, req *gen.VersionRequest, params gen.CloseReturnParams) (gen.CloseReturnRes, error) {
	return h.returnChange(ctx, "order.return.close", "đóng phiếu trả", func(tx pgx.Tx, w order.Writer, seller uuid.UUID, a order.Actor) (order.Return, error) {
		return w.CloseReturn(ctx, tx, seller, params.ReturnId, req.Version, a, time.Now().UTC())
	}, params.ReturnId)
}

// CancelReturn (POST /api/v1/returns/{returnId}/cancel, order.return): phiếu requested → cancelled; phiếu đổi hủy luôn
// đơn thay thế chưa gửi hãng.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CancelReturn(ctx context.Context, req *gen.VersionRequest, params gen.CancelReturnParams) (gen.CancelReturnRes, error) {
	return h.returnChange(ctx, "order.return.cancel", "hủy phiếu trả", func(tx pgx.Tx, w order.Writer, seller uuid.UUID, a order.Actor) (order.Return, error) {
		return w.CancelReturn(ctx, tx, seller, params.ReturnId, req.Version, a, time.Now().UTC())
	}, params.ReturnId)
}

// returnChange chạy một lần đổi phiếu id (nhận, đóng, hủy) của người bán mặc định trong WriteTx, ghi audit action với
// phần trước/sau của phiếu, trả 200 phiếu sau đổi.
func (h *handler) returnChange(ctx context.Context, action, what string,
	change func(tx pgx.Tx, w order.Writer, seller uuid.UUID, a order.Actor) (order.Return, error), id uuid.UUID,
) (*gen.Return, error) {
	out, err := h.orderWriteAsDefaultSeller(ctx, http.StatusOK, isReturnRefusal, what, func(tx pgx.Tx, w order.Writer,
		u identity.AuthUser, seller uuid.UUID, a order.Actor,
	) (any, error) {
		before, err := order.GetReturn(ctx, tx, seller, id)
		if err != nil {
			return nil, err //nolint:wrapcheck // sentinel của order
		}
		r, err := change(tx, w, seller, a)
		if err != nil {
			return nil, err
		}
		return returnOut(r), h.audit(ctx, tx, u, action, "return", r.ID, returnAudit(before), returnAudit(r))
	})
	if err != nil {
		return nil, err
	}
	return out.(*gen.Return), nil //nolint:forcetypeassert // fn trên trả *gen.Return
}

// returnAudit: mã, loại, trạng thái, tiền hoàn, số dòng theo tình trạng; lý do trả chỉ có hay không.
func returnAudit(r order.Return) map[string]any {
	m := map[string]any{
		"code": r.Code, "kind": r.Kind, "status": r.Status, "refundAmount": r.RefundAmount, "orderId": r.OrderID.String(),
		"items": len(r.Items), "version": r.Version, "hasReason": r.Reason != "",
	}
	if r.ReplacementOrderID != nil {
		m["replacementOrderId"] = r.ReplacementOrderID.String()
	}
	if r.ReceivedAt != nil {
		conds := map[string]int{}
		for _, it := range r.Items {
			conds[it.Condition]++
		}
		m["conditions"] = conds
	}
	return m
}

// returnOut là thân Return; condition của dòng chỉ có sau khi kho nhận.
func returnOut(r order.Return) *gen.Return {
	out := &gen.Return{
		ID: r.ID, Code: r.Code, OrderId: r.OrderID, Kind: gen.ReturnKind(r.Kind), Status: gen.ReturnStatus(r.Status),
		RefundAmount: gen.Money(r.RefundAmount), Items: make([]gen.ReturnItem, 0, len(r.Items)), CreatedAt: r.CreatedAt,
		Version: r.Version,
	}
	if r.Reason != "" {
		out.Reason = gen.NewOptString(r.Reason)
	}
	if r.ShipmentID != nil {
		out.ShipmentId = gen.NewOptUUID(*r.ShipmentID)
	}
	if r.ReplacementOrderID != nil {
		out.ReplacementOrderId = gen.NewOptUUID(*r.ReplacementOrderID)
	}
	if r.ReceivedAt != nil {
		out.ReceivedAt = gen.NewOptDateTime(*r.ReceivedAt)
	}
	for _, it := range r.Items {
		item := gen.ReturnItem{
			ID: it.ID, OrderItemId: it.OrderItemID, Qty: int32(it.Quantity), //nolint:gosec // ≤ 10.000 (hợp đồng)
			RefundAmount: gen.Money(it.RefundAmount),
		}
		if r.ReceivedAt != nil {
			item.Condition = gen.NewOptReturnItemCondition(gen.ReturnItemCondition(it.Condition))
		}
		out.Items = append(out.Items, item)
	}
	return out
}
