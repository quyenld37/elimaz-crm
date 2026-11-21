package httpapi

import (
	"context"
	"fmt"
	"net/http"

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
	var (
		found []order.Summary
		names map[uuid.UUID]string
	)
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		found, err = order.CheckDuplicates(ctx, tx, seller, optUUID(params.CustomerId), string(params.Phone.Or("")),
			params.VariantIds, h.now().UTC())
		if err != nil {
			return err //nolint:wrapcheck // sentinel của order
		}
		names, err = actorNamesOf(ctx, tx, found, func(s order.Summary) *uuid.UUID { return s.CareBy })
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("kiểm đơn trùng: %w", err)
	}
	out := &gen.OrderList{Items: make([]gen.OrderListItem, 0, len(found))}
	for _, s := range found {
		out.Items = append(out.Items, orderListItemOut(s, names))
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
		names map[uuid.UUID]string
	)
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		items, next, err = order.ListDuplicates(ctx, tx, seller, after, int(params.Limit.Or(0)))
		if err != nil {
			return err //nolint:wrapcheck // lỗi của order đã bọc
		}
		names, err = duplicateCareNames(ctx, tx, items...)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("danh sách đơn trùng: %w", err)
	}
	out := &gen.OrderDuplicatePage{Items: make([]gen.OrderDuplicate, 0, len(items))}
	for _, d := range items {
		out.Items = append(out.Items, *duplicateOut(d, names))
	}
	if next != nil {
		out.NextCursor = gen.NewOptString(next.String())
	}
	return out, nil
}

// ResolveOrderDuplicate (POST /api/v1/order-duplicates/{orderId}/{otherOrderId}/resolve, order.duplicates.resolve):
// nhân viên kết luận "không trùng" hoặc hủy một đơn (lý do duplicate, version của đơn bị hủy) → 200 cặp sau xử lý (đơn
// đã gửi hãng: đơn cancel_requested, cặp chưa có resolution). Lỗi nghiệp vụ (cặp đã xử lý
// 422, không có 404, đơn không hủy được) không lưu theo khóa. Audit order.duplicate.resolve chỉ ghi mã hai đơn, hành
// động.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ResolveOrderDuplicate(ctx context.Context, req *gen.ResolveOrderDuplicateRequest,
	params gen.ResolveOrderDuplicateParams,
) (gen.ResolveOrderDuplicateRes, error) {
	action := string(req.Action)
	var version *int64
	if v, ok := req.Version.Get(); ok {
		version = &v
	}
	out, err := h.refusableWrite(ctx, http.StatusOK, isOrderCancelRefusal, "xử lý đơn trùng",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			seller, err := catalog.DefaultSeller(ctx, tx)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của catalog
			}
			w := order.Writer{Events: h.d.Events, Queue: queue.NewQueue(), Realtime: h.d.Realtime}
			if err := w.ResolveDuplicate(ctx, tx, seller, params.OrderId, params.OtherOrderId, action, version,
				order.Actor{TenantID: u.TenantID, UserID: u.UserID}, h.now().UTC()); err != nil {
				return nil, err //nolint:wrapcheck // sentinel của order
			}
			d, err := order.GetDuplicate(ctx, tx, seller, params.OrderId, params.OtherOrderId)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của order
			}
			after := map[string]any{"otherOrderId": params.OtherOrderId.String(), "action": action}
			names, err := duplicateCareNames(ctx, tx, d)
			if err != nil {
				return nil, err
			}
			return duplicateOut(d, names), h.audit(ctx, tx, u, "order.duplicate.resolve", "order", params.OrderId, nil, after)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.OrderDuplicate), nil //nolint:forcetypeassert // fn trên trả *gen.OrderDuplicate
}

// duplicateCareNames đọc tên hiển thị người phụ trách của hai đơn trong mọi cặp ds.
func duplicateCareNames(ctx context.Context, tx pgx.Tx, ds ...order.Duplicate) (map[uuid.UUID]string, error) {
	sums := make([]order.Summary, 0, 2*len(ds))
	for _, d := range ds {
		sums = append(sums, d.Order, d.OtherOrder)
	}
	return actorNamesOf(ctx, tx, sums, func(s order.Summary) *uuid.UUID { return s.CareBy })
}

func duplicateOut(d order.Duplicate, names map[uuid.UUID]string) *gen.OrderDuplicate {
	out := &gen.OrderDuplicate{
		Order: orderListItemOut(d.Order, names), OtherOrder: orderListItemOut(d.OtherOrder, names),
		MatchedBy: gen.OrderDuplicateMatchedBy(d.MatchedBy), CreatedAt: d.CreatedAt,
	}
	if d.Resolution != nil {
		out.Resolution = gen.NewOptOrderDuplicateResolution(gen.OrderDuplicateResolution(*d.Resolution))
	}
	setOptTime(&out.ResolvedAt, d.ResolvedAt)
	return out
}

// orderListItemOut là một dòng OrderListItem từ tóm tắt đơn; names: tên hiển thị người phụ trách (actorNamesOf).
func orderListItemOut(s order.Summary, names map[uuid.UUID]string) gen.OrderListItem {
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
	if s.CareBy != nil {
		out.CareBy = gen.NewOptActor(gen.Actor{ID: *s.CareBy, Name: names[*s.CareBy]})
	}
	return out
}
