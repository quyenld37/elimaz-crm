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
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// Vùng callbacks (kế hoạch M3, Task M3-19): đơn giao thất bại cần gọi lại. Quyền fulfillment.callbacks.work (toàn shop,
// guard kiểm). Danh sách không có SĐT, địa chỉ (màn đọc SĐT qua getOrder khi bấm "Hiện số", tệp task 110 Quyết định
// 1). Giao lại địa chỉ mới cần thêm fulfillment.ship ở kho của vận đơn (Quyết định 2). Audit chỉ có trạng thái, kết
// quả, mã (không ghi chú: chữ tự do có thể có SĐT).

const permCallbacksWork = identity.Permission("fulfillment.callbacks.work")

// ListCallbackTasks (GET /api/v1/callback-tasks): việc gọi lại, quá hạn trước rồi hạn gần nhất, kèm số việc còn mở,
// tên khách, số nhận/hoàn của khách.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListCallbackTasks(ctx context.Context, params gen.ListCallbackTasksParams) (gen.ListCallbackTasksRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	f := fulfillment.CallbackFilter{
		Status: string(params.Status.Or("")), Cursor: params.Cursor.Or(""), Limit: int(params.Limit.Or(0)),
	}
	if v, ok := params.DueBefore.Get(); ok {
		f.DueBefore = &v
	}
	var out *gen.CallbackTaskPage
	if err := h.inventoryRead(ctx, func(tx pgx.Tx, u identity.AuthUser) error {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của catalog
		}
		f.SellerID = seller
		if params.Assignee.Or(gen.ListCallbackTasksAssigneeAny) == gen.ListCallbackTasksAssigneeMe {
			f.AssigneeID = &u.UserID
		}
		p, err := svc.ListCallbacks(ctx, tx, f)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của fulfillment
		}
		out, err = callbackPageOut(ctx, tx, p)
		return err
	}); err != nil {
		return nil, fmt.Errorf("đọc việc gọi lại: %w", err)
	}
	return out, nil
}

// callbackPageOut đổi trang việc gọi lại sang hợp đồng, kèm tên người được giao và số nhận/hoàn của khách (mỗi khách
// đọc một lần).
func callbackPageOut(ctx context.Context, tx pgx.Tx, p fulfillment.CallbackPage) (*gen.CallbackTaskPage, error) {
	out := &gen.CallbackTaskPage{Items: make([]gen.CallbackTask, 0, len(p.Items)), OpenCount: gen.NewOptInt32(int32n(p.OpenCount))}
	enr, err := newCallbackEnricher(ctx, tx, p.Items)
	if err != nil {
		return nil, err
	}
	for _, t := range p.Items {
		it, err := enr.out(ctx, tx, t)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *it)
	}
	if p.Next != "" {
		out.NextCursor = gen.NewOptString(p.Next)
	}
	return out, nil
}

// callbackEnricher thêm tên người được giao, tên khách và số nhận/hoàn của khách (ngưỡng theo cài đặt vận chuyển) vào
// việc gọi lại; mỗi khách đọc một lần (customer.Brief, customer.DeliveryStatsFor).
type callbackEnricher struct {
	names     map[uuid.UUID]string
	rr        customer.ReturnRisk
	customers map[uuid.UUID]callbackCustomer
}

// callbackCustomer là tên (của hồ sơ gốc) và số nhận/hoàn của một khách; khách không còn → rỗng.
type callbackCustomer struct {
	name  string
	stats *gen.DeliveryStats
}

func newCallbackEnricher(ctx context.Context, tx pgx.Tx, items []fulfillment.CallbackTask) (*callbackEnricher, error) {
	names, err := actorNamesOf(ctx, tx, items, func(t fulfillment.CallbackTask) *uuid.UUID { return t.AssigneeID })
	if err != nil {
		return nil, err
	}
	set, _, err := fulfillment.LoadSettings(ctx, tx)
	if err != nil {
		return nil, err //nolint:wrapcheck // fulfillment đã bọc
	}
	rr := set.ReturnRisk
	return &callbackEnricher{
		names: names, customers: map[uuid.UUID]callbackCustomer{},
		rr: customer.ReturnRisk{WatchMinReturned: rr.WatchMinReturned, HighMinReturned: rr.HighMinReturned, HighRateBP: rr.HighRateBP},
	}, nil
}

// customer đọc (một lần mỗi khách) tên và số nhận/hoàn của khách id.
func (e *callbackEnricher) customer(ctx context.Context, tx pgx.Tx, id uuid.UUID) (callbackCustomer, error) {
	if c, seen := e.customers[id]; seen {
		return c, nil
	}
	var c callbackCustomer
	b, err := customer.Brief(ctx, tx, id)
	switch {
	case errors.Is(err, customer.ErrNotFound):
	case err != nil:
		return callbackCustomer{}, fmt.Errorf("đọc khách của việc gọi lại: %w", err)
	default:
		s, err := customer.DeliveryStatsFor(ctx, tx, id, e.rr)
		if err != nil {
			return callbackCustomer{}, fmt.Errorf("đọc số nhận/hoàn của khách: %w", err)
		}
		c = callbackCustomer{name: string(b.FullName), stats: deliveryStatsOut(s)}
	}
	e.customers[id] = c
	return c, nil
}

// out đổi t sang hợp đồng kèm tên người được giao, tên khách, số nhận/hoàn.
func (e *callbackEnricher) out(ctx context.Context, tx pgx.Tx, t fulfillment.CallbackTask) (*gen.CallbackTask, error) {
	it := callbackTaskOut(t, e.names)
	c, err := e.customer(ctx, tx, t.CustomerID)
	if err != nil {
		return nil, err
	}
	if c.name != "" {
		it.CustomerName = gen.NewOptString(c.name)
	}
	if c.stats != nil {
		it.DeliveryStats = gen.NewOptDeliveryStats(*c.stats)
	}
	return it, nil
}

// callbackTaskOut đổi t sang hợp đồng (không tên khách, deliveryStats); names: tên hiển thị của người được giao.
func callbackTaskOut(t fulfillment.CallbackTask, names map[uuid.UUID]string) *gen.CallbackTask {
	out := &gen.CallbackTask{
		ID: t.ID, ShipmentId: t.ShipmentID, OrderId: t.OrderID, OrderCode: t.OrderCode, AttemptNo: t.AttemptNo,
		FailedAt: t.FailedAt, DueAt: t.DueAt, Overdue: t.Overdue, Status: gen.CallbackTaskStatus(t.Status),
		Version: t.Version,
	}
	for _, f := range []struct {
		dst *gen.OptString
		v   string
	}{
		{&out.CarrierKey, t.CarrierKey},
		{&out.TrackingCode, t.TrackingCode},
		{&out.FailedReason, t.FailedReason},
		{&out.Outcome, t.Outcome},
	} {
		if f.v != "" {
			*f.dst = gen.NewOptString(f.v)
		}
	}
	if t.NextCallAt != nil {
		out.NextCallAt = gen.NewOptDateTime(*t.NextCallAt)
	}
	if t.AssigneeID != nil {
		out.Assignee = gen.NewOptActor(gen.Actor{ID: *t.AssigneeID, Name: names[*t.AssigneeID]})
	}
	return out
}

// RecordCallbackOutcome (POST /api/v1/callback-tasks/{id}/outcomes) → 200: việc sau khi ghi. Việc không có → 404; đã
// xong hay đã đóng → 409 invalid_transition; lệch version → 409 version_conflict; thiếu hẹn/địa chỉ → 400 invalid_input;
// giao lại địa chỉ mới mà thiếu fulfillment.ship ở kho của vận đơn → 403; lỗi của requestAddressChange như operation đó.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) RecordCallbackOutcome(ctx context.Context, req *gen.RecordCallbackOutcomeRequest, params gen.RecordCallbackOutcomeParams) (gen.RecordCallbackOutcomeRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	in := fulfillment.CallbackOutcomeInput{Outcome: string(req.Outcome), Note: req.Note.Or(""), Version: req.Version}
	if v, ok := req.NextCallAt.Get(); ok {
		in.NextCallAt = &v
	}
	if a, ok := req.NewAddress.Get(); ok {
		in.NewAddress = &order.ShipAddress{
			Name: a.Name, Phone: logging.Phone(a.Phone), Line: logging.Address(a.Line), ProvinceCode: a.ProvinceCode,
			WardCode: a.WardCode,
		}
	}
	out, err := h.refusableWrite(ctx, http.StatusOK, isAddressChangeRefusal, "ghi kết quả gọi lại",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			if err := sellerOwns(ctx, tx, fulfillment.ScopeCallback, params.ID); err != nil {
				return nil, err
			}
			in.Authorize = shipAuthorize(u, permShip, nil)
			r, err := svc.RecordCallbackOutcome(ctx, tx, params.ID, in, orderActor(u))
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment, order
			}
			if r.AddressChange != nil {
				c := r.AddressChange
				if err := h.audit(ctx, tx, u, "fulfillment.address_change.request", "address_change", c.ID, nil,
					c.AuditFields()); err != nil {
					return nil, err
				}
			}
			after := map[string]any{"status": r.Task.Status, "outcome": r.Task.Outcome, "redeliverByApi": r.Redeliver}
			if err := h.audit(ctx, tx, u, "fulfillment.callback.outcome", "callback_task", r.Task.ID,
				map[string]any{"status": r.Before}, after); err != nil {
				return nil, err
			}
			return callbackOneOut(ctx, tx, r.Task)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CallbackTask), nil //nolint:forcetypeassert // fn trên trả *gen.CallbackTask
}

// AssignCallbackTask (POST /api/v1/callback-tasks/{id}/assign) → 200: việc sau khi giao. Người được giao không phải
// nhân viên đang hoạt động có fulfillment.callbacks.work (đọc lại trong tx) → 422 invalid_input; việc không có → 404;
// đã xong hay đã đóng → 409 invalid_transition; version có mà lệch → 409 version_conflict.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AssignCallbackTask(ctx context.Context, req *gen.AssignCallbackTaskRequest, params gen.AssignCallbackTaskParams) (gen.AssignCallbackTaskRes, error) {
	svc, err := h.fulfillmentService()
	if err != nil {
		return nil, err
	}
	var version *int64
	if v, ok := req.Version.Get(); ok {
		version = &v
	}
	out, err := h.refusableWrite(ctx, http.StatusOK, isShipmentRefusal, "giao việc gọi lại",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			if err := sellerOwns(ctx, tx, fulfillment.ScopeCallback, params.ID); err != nil {
				return nil, err
			}
			ok, err := identity.StaffHas(ctx, tx, req.AssigneeId, permCallbacksWork)
			if err != nil {
				return nil, fmt.Errorf("kiểm quyền người được giao: %w", err)
			}
			if !ok {
				return nil, fulfillment.ErrCallbackAssignee
			}
			r, err := svc.AssignCallback(ctx, tx, params.ID, req.AssigneeId, version,
				fulfillment.Actor{TenantID: u.TenantID, UserID: u.UserID})
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của fulfillment
			}
			before := map[string]any{"assigneeId": nil}
			if r.BeforeAssignee != nil {
				before["assigneeId"] = r.BeforeAssignee.String()
			}
			if err := h.audit(ctx, tx, u, "fulfillment.callback.assign", "callback_task", r.Task.ID, before,
				map[string]any{"assigneeId": req.AssigneeId.String()}); err != nil {
				return nil, err
			}
			return callbackOneOut(ctx, tx, r.Task)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.CallbackTask), nil //nolint:forcetypeassert // fn trên trả *gen.CallbackTask
}

// callbackOneOut đổi một việc sang hợp đồng kèm tên người được giao, số nhận/hoàn của khách.
func callbackOneOut(ctx context.Context, tx pgx.Tx, t fulfillment.CallbackTask) (*gen.CallbackTask, error) {
	enr, err := newCallbackEnricher(ctx, tx, []fulfillment.CallbackTask{t})
	if err != nil {
		return nil, err
	}
	return enr.out(ctx, tx, t)
}
