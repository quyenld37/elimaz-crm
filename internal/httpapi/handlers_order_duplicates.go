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

// CheckOrderDuplicates (GET /api/v1/order-duplicate-check, order.create; Task M2-23): cảnh báo ở form trước khi tạo —
// đơn chưa hủy của người bán mặc định trong cửa sổ cài đặt, cùng nhóm khách hoặc cùng SĐT giao, có biến thể chung.
// Chỉ đọc; SĐT không vào log. Khách không có → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) CheckOrderDuplicates(ctx context.Context, params gen.CheckOrderDuplicatesParams) (gen.CheckOrderDuplicatesRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var found []order.Summary
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		found, err = order.CheckDuplicates(ctx, tx, seller, optUUID(params.CustomerId), string(params.Phone.Or("")),
			params.VariantIds, time.Now().UTC())
		return err //nolint:wrapcheck // sentinel của order
	})
	if err != nil {
		return nil, fmt.Errorf("kiểm đơn trùng: %w", err)
	}
	out := &gen.OrderList{Items: make([]gen.OrderListItem, 0, len(found))}
	for _, s := range found {
		out.Items = append(out.Items, orderListItemOut(s))
	}
	return out, nil
}

// ListOrderDuplicates (GET /api/v1/order-duplicates, order.read): cặp nghi trùng chưa xử lý của người bán mặc định,
// mới nhất trước, trang theo nextCursor; con trỏ hỏng → 400.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListOrderDuplicates(ctx context.Context, params gen.ListOrderDuplicatesParams) (gen.ListOrderDuplicatesRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var after *order.DuplicateCursor
	if s, ok := params.Cursor.Get(); ok {
		c, err := order.ParseDuplicateCursor(s)
		if err != nil {
			return nil, fmt.Errorf("danh sách đơn trùng: %w", err)
		}
		after = &c
	}
	var (
		items []order.Duplicate
		next  *order.DuplicateCursor
	)
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		items, next, err = order.ListDuplicates(ctx, tx, seller, after, int(params.Limit.Or(0)))
		return err //nolint:wrapcheck // lỗi của order đã bọc
	})
	if err != nil {
		return nil, fmt.Errorf("danh sách đơn trùng: %w", err)
	}
	out := &gen.OrderDuplicatePage{Items: make([]gen.OrderDuplicate, 0, len(items))}
	for _, d := range items {
		out.Items = append(out.Items, *duplicateOut(d))
	}
	if next != nil {
		out.NextCursor = gen.NewOptString(next.String())
	}
	return out, nil
}

// ResolveOrderDuplicate (POST /api/v1/order-duplicates/{orderId}/{otherOrderId}/resolve, order.duplicates.resolve):
// nhân viên kết luận "không trùng" hoặc hủy một đơn (lý do duplicate) → 200 cặp sau xử lý. Lỗi nghiệp vụ (cặp đã xử lý
// 422, không có 404, đơn không hủy được) không lưu theo khóa. Audit order.duplicate.resolve chỉ ghi mã hai đơn, hành
// động.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ResolveOrderDuplicate(ctx context.Context, req *gen.ResolveOrderDuplicateRequest,
	params gen.ResolveOrderDuplicateParams,
) (gen.ResolveOrderDuplicateRes, error) {
	action := string(req.Action)
	out, err := h.refusableWrite(ctx, http.StatusOK, isOrderCancelRefusal, "xử lý đơn trùng",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			seller, err := catalog.DefaultSeller(ctx, tx)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của catalog
			}
			w := order.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
			if err := w.ResolveDuplicate(ctx, tx, seller, params.OrderId, params.OtherOrderId, action,
				order.Actor{TenantID: u.TenantID, UserID: u.UserID}, time.Now().UTC()); err != nil {
				return nil, err //nolint:wrapcheck // sentinel của order
			}
			d, err := order.GetDuplicate(ctx, tx, seller, params.OrderId, params.OtherOrderId)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của order
			}
			after := map[string]any{"otherOrderId": params.OtherOrderId.String(), "action": action}
			return duplicateOut(d), h.audit(ctx, tx, u, "order.duplicate.resolve", "order", params.OrderId, nil, after)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.OrderDuplicate), nil //nolint:forcetypeassert // fn trên trả *gen.OrderDuplicate
}

func duplicateOut(d order.Duplicate) *gen.OrderDuplicate {
	out := &gen.OrderDuplicate{
		Order: orderListItemOut(d.Order), OtherOrder: orderListItemOut(d.OtherOrder),
		MatchedBy: gen.OrderDuplicateMatchedBy(d.MatchedBy), CreatedAt: d.CreatedAt,
	}
	if d.Resolution != nil {
		out.Resolution = gen.NewOptOrderDuplicateResolution(gen.OrderDuplicateResolution(*d.Resolution))
	}
	setOptTime(&out.ResolvedAt, d.ResolvedAt)
	return out
}

// orderListItemOut là một dòng OrderListItem từ tóm tắt đơn (người phụ trách: Task M2-24).
func orderListItemOut(s order.Summary) gen.OrderListItem {
	out := gen.OrderListItem{
		ID: s.ID, Code: s.Code, Label: gen.OrderLabel(s.Label), Status: gen.OrderStatus(s.Status),
		CustomerId: s.CustomerID, CustomerName: s.CustomerName, ShipPhone: nonEmpty(string(s.ShipPhone)),
		GrandTotal: gen.Money(s.GrandTotal), CodAmount: gen.Money(s.CODAmount), ItemCount: s.ItemCount,
		Channel: nonEmpty(s.Channel), CreatedAt: s.CreatedAt, DuplicateSuspect: s.DuplicateSuspect,
	}
	if s.PendingReason != "" {
		out.PendingReason = gen.NewOptPendingReason(gen.PendingReason(s.PendingReason))
	}
	setOpt(&out.TrafficSourceId, s.TrafficSourceID)
	return out
}
