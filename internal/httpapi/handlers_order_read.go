package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/queries"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// ListOrders (GET /api/v1/orders, order.read): một trang đơn của người bán mặc định, mới nhất trước, keyset (Task
// M2-24; M3 Task M3-28: lọc theo hãng, trạng thái vận đơn, tiền COD, đóng gói; mỗi dòng có vận đơn hiện hành và số dư
// COD, mọi nhãn lọc được). Phần tìm (q, khách) giải bằng order.ResolveListSearch, câu danh sách ghép view ở
// queries.OrderList (spec 04 mục 3). Con trỏ hỏng → 400 invalid_input; q chỉ chữ số dưới 4 số → 422 query_too_short.
// Không ghi q vào log.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListOrders(ctx context.Context, params gen.ListOrdersParams) (gen.ListOrdersRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	f := listOrdersFilter(params)
	var cursor queries.OrderCursor
	if s, ok := params.Cursor.Get(); ok {
		c, err := order.ParseCursor(s)
		if err != nil {
			return nil, fmt.Errorf("danh sách đơn: %w", err)
		}
		cursor = queries.OrderCursor{CreatedAt: c.CreatedAt, ID: c.ID}
	}
	var (
		rows  []queries.OrderListRow
		next  queries.OrderCursor
		names map[uuid.UUID]string
	)
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		s, err := order.ResolveListSearch(ctx, tx, params.Q.Or(""), optUUID(params.CustomerId))
		if err != nil || s.Empty {
			return err //nolint:wrapcheck // lỗi của order đã bọc
		}
		f.SellerID = seller
		f.Search = queries.OrderSearch{
			CustomerRoots: s.CustomerRoots, NameRoots: s.NameRoots, Code: s.Code, Phone: s.Phone, PhoneTail: s.PhoneTail,
		}
		rows, next, err = queries.OrderList(ctx, tx, f, cursor, int(params.Limit.Or(0)))
		if err != nil {
			return orderListErr(err)
		}
		names, err = actorNamesOf(ctx, tx, rows, func(r queries.OrderListRow) *uuid.UUID { return r.CareBy })
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("danh sách đơn: %w", err)
	}
	out := &gen.OrderPage{Items: make([]gen.OrderListItem, 0, len(rows))}
	for _, r := range rows {
		item := gen.OrderListItem{
			ID: r.ID, Code: r.Code, Label: gen.OrderLabel(r.Label), Status: gen.OrderStatus(r.Status),
			CustomerId: r.CustomerRootID, CustomerName: r.CustomerName, ShipPhone: nonEmpty(string(r.ShipPhone)),
			GrandTotal: gen.Money(r.GrandTotal), CodAmount: gen.Money(r.CodAmount), ItemCount: int32(r.ItemCount), //nolint:gosec // tổng số lượng của một đơn
			Channel: nonEmpty(r.Channel), CreatedAt: r.CreatedAt, DuplicateSuspect: r.DuplicateSuspect,
		}
		if r.PendingReason != "" {
			item.PendingReason = gen.NewOptPendingReason(gen.PendingReason(r.PendingReason))
		}
		setOpt(&item.TrafficSourceId, r.TrafficSource)
		if r.CareBy != nil {
			item.CareBy = gen.NewOptActor(gen.Actor{ID: *r.CareBy, Name: names[*r.CareBy]})
		}
		if sh := r.Shipment; sh != nil {
			sum := gen.OrderShipmentSummary{
				Code: sh.Code, CarrierKey: sh.CarrierKey, TrackingCode: nonEmpty(sh.TrackingCode),
				Status: gen.ShipmentStatus(sh.Status), Label: gen.NewOptString(fulfillment.StatusLabel(sh.CarrierKey, sh.Status, sh.SubStatus)),
				NeedsReview: gen.NewOptBool(sh.NeedsReview),
			}
			setOptTime(&sum.StuckSince, sh.StuckSince)
			item.Shipment = gen.NewOptOrderShipmentSummary(sum)
		}
		if c := r.COD; c != nil {
			item.Cod = gen.NewOptOrderCodSummary(gen.OrderCodSummary{
				ToCollect: gen.SignedMoney(c.ToCollect), CarrierHolding: gen.SignedMoney(c.CarrierHolding),
				ShopReceived: gen.SignedMoney(c.ShopReceived),
			})
		}
		out.Items = append(out.Items, item)
	}
	if next != (queries.OrderCursor{}) {
		out.NextCursor = gen.NewOptString(order.Cursor{CreatedAt: next.CreatedAt, ID: next.ID}.String())
	}
	return out, nil
}

// orderListErr đổi lỗi bộ lọc của queries.OrderList sang sentinel của order (errors.go ánh xạ sentinel của order).
func orderListErr(err error) error {
	if errors.Is(err, queries.ErrInvalidOrderFilter) {
		return fmt.Errorf("%w: %w", order.ErrInvalidInput, err)
	}
	return err
}

// listOrdersFilter dịch tham số của listOrders sang queries.OrderFilter (chưa có SellerID, Search).
func listOrdersFilter(p gen.ListOrdersParams) queries.OrderFilter {
	f := queries.OrderFilter{
		TrafficSourceIDs: p.TrafficSourceId, CareBy: optUUID(p.CareBy), CarrierKey: p.CarrierKey.Or(""),
		CODState: string(p.CodState.Or("")), PackingStatus: string(p.PackingStatus.Or("")),
	}
	for _, s := range p.ShipmentStatus {
		f.ShipmentStatuses = append(f.ShipmentStatuses, string(s))
	}
	for _, l := range p.Label {
		f.Labels = append(f.Labels, orderview.LabelKey(l))
	}
	for _, s := range p.Status {
		f.Statuses = append(f.Statuses, string(s))
	}
	for _, r := range p.PendingReason {
		f.PendingReasons = append(f.PendingReasons, string(r))
	}
	for _, c := range p.Channel {
		f.Channels = append(f.Channels, string(c))
	}
	if v, ok := p.CreatedFrom.Get(); ok {
		f.CreatedFrom = &v
	}
	if v, ok := p.CreatedTo.Get(); ok {
		f.CreatedTo = &v
	}
	if v, ok := p.DuplicateSuspect.Get(); ok {
		f.DuplicateSuspect = &v
	}
	return f
}

// GetOrderTimeline (GET /api/v1/orders/{id}/timeline, order.read): lịch sử thao tác của đơn (đổi trạng thái, tên trường
// đã sửa, ghi chú, phiếu trả của order.Timeline; sự kiện vận đơn, phiếu thu của queries.OrderShippingTimeline — M3 Task
// M3-28), mới nhất trước (order.SortTimeline), kèm nguồn (actorType) và tên người làm; đơn không có (hay của tenant,
// người bán khác) → 404.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) GetOrderTimeline(ctx context.Context, params gen.GetOrderTimelineParams) (gen.GetOrderTimelineRes, error) {
	u, ok := authUser(ctx)
	if !ok {
		return nil, errNoAuthUser
	}
	var (
		items []order.OrderTimelineItem
		names map[uuid.UUID]string
	)
	err := h.readAsDefaultSeller(ctx, u, func(tx pgx.Tx, seller uuid.UUID) error {
		var err error
		items, err = order.Timeline(ctx, tx, seller, params.ID, orderAudit)
		if err != nil {
			return err //nolint:wrapcheck // sentinel của order
		}
		ship, err := queries.OrderShippingTimeline(ctx, tx, seller, params.ID)
		if err != nil {
			return err //nolint:wrapcheck // lỗi của queries đã bọc
		}
		for _, s := range ship {
			items = append(items, order.OrderTimelineItem{
				At: s.At, Kind: s.Kind, RefID: s.RefID, ActorID: s.ActorID, ActorType: s.ActorType, Summary: s.Summary,
			})
		}
		order.SortTimeline(items)
		names, err = actorNamesOf(ctx, tx, items, func(it order.OrderTimelineItem) *uuid.UUID { return it.ActorID })
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("lịch sử thao tác của đơn: %w", err)
	}
	out := &gen.OrderTimeline{Items: make([]gen.TimelineItem, 0, len(items))}
	for _, it := range items {
		out.Items = append(out.Items, timelineItemOut(timelineEntry{
			At: it.At, Kind: it.Kind, RefID: it.RefID, ActorID: it.ActorID, ActorType: it.ActorType, Summary: it.Summary,
			Note: string(it.Note), IsNote: it.Kind == order.TimelineNote,
		}, names))
	}
	return out, nil
}

// orderAudit là order.AuditSource: audit của đơn đọc bằng identity.ListEntityAudit (order không nhập identity).
func orderAudit(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]order.AuditChange, error) {
	recs, err := identity.ListEntityAudit(ctx, tx, "order", orderID.String())
	if err != nil {
		return nil, err //nolint:wrapcheck // lỗi của identity đã bọc
	}
	out := make([]order.AuditChange, 0, len(recs))
	for _, r := range recs {
		c := order.AuditChange{ID: r.ID, Action: r.Action, Edited: r.ListedFields, At: r.CreatedAt}
		if id, err := uuid.Parse(r.ActorID); err == nil {
			c.ActorID = &id
		}
		out = append(out, c)
	}
	return out, nil
}

// AddOrderNote (POST /api/v1/orders/{id}/notes, order.update): thêm ghi chú nội bộ (chỉ ghi thêm, không gửi hãng) cho
// đơn của người bán mặc định; audit order.add_note không chép nội dung. Thân rỗng → 400 invalid_input; đơn không có →
// 404. Trả ghi chú kèm tên người viết, 201.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) AddOrderNote(ctx context.Context, req *gen.AddOrderNoteRequest, params gen.AddOrderNoteParams) (gen.AddOrderNoteRes, error) {
	return h.writeNote(ctx, isOrderChangeRefusal, "ghi chú đơn", func(tx pgx.Tx, u identity.AuthUser) (writtenNote, error) {
		seller, err := catalog.DefaultSeller(ctx, tx)
		if err != nil {
			return writtenNote{}, fmt.Errorf("người bán mặc định: %w", err)
		}
		n, err := order.AddNote(ctx, tx, seller, params.ID, req.Body, order.Actor{TenantID: u.TenantID, UserID: u.UserID},
			time.Now().UTC())
		if err != nil {
			return writtenNote{}, err //nolint:wrapcheck // sentinel của order
		}
		if err := identity.RecordAudit(ctx, tx, identity.AuditEntry{
			TenantID: u.TenantID, ActorType: "staff", ActorID: u.UserID.String(), Action: "order.add_note",
			EntityType: "order", EntityID: n.OrderID.String(), After: map[string]any{"noteId": n.ID.String()},
			RequestID: RequestID(ctx),
		}); err != nil {
			return writtenNote{}, fmt.Errorf("ghi audit order.add_note: %w", err)
		}
		return writtenNote{ID: n.ID, Body: string(n.Body), AuthorID: n.AuthorID, CreatedAt: n.CreatedAt}, nil
	})
}

// orderDetailOut là thân getOrder (Task M2-24): orderOut của đơn cộng nhãn, phạm vi sửa, khách, mã, cảnh báo, phiếu trả,
// lý do hủy, người phụ trách, người tạo (tên từ names).
func orderDetailOut(d order.OrderDetail, names map[uuid.UUID]string) *gen.Order {
	out := orderOut(d.Order)
	out.Label = gen.NewOptOrderLabel(gen.OrderLabel(d.Label))
	s := d.EditScope
	out.EditScope = gen.NewOptEditScope(gen.EditScope{
		Lines: s.Lines, Discounts: s.Discounts, Coupon: s.Coupon, Address: s.Address, Shipping: s.ShippingFee,
		CarrierNote: s.CarrierNote, CareBy: s.CareBy, TrafficSource: s.TrafficSource,
	})
	c := d.Customer
	st := gen.CustomerStats{
		OrderCount: int32(c.Stats.OrderCount), DeliveredCount: int32(c.Stats.DeliveredCount), //nolint:gosec // đếm đơn của một khách
		ReturnedCount: int32(c.Stats.ReturnedCount), TotalSpent: gen.Money(c.Stats.TotalSpent), //nolint:gosec // như trên
		ReturnRateBp: int32(c.Stats.ReturnRateBP), //nolint:gosec // phần vạn ≤ 10000
	}
	setOptTime(&st.LastOrderAt, c.Stats.LastOrderAt)
	out.Customer = gen.NewOptOrderCustomer(gen.OrderCustomer{
		ID: c.ID, RootId: c.RootID, Code: c.Code, FullName: string(c.FullName), Stats: gen.NewOptCustomerStats(st),
	})
	out.CouponCode = nonEmpty(d.CouponCode)
	out.Warnings = make([]gen.OrderWarning, 0, len(d.Warnings))
	for _, w := range d.Warnings {
		out.Warnings = append(out.Warnings, gen.OrderWarning{Code: w.Code, RefId: gen.NewOptUUID(w.RefID)})
	}
	out.Returns = make([]gen.ReturnSummary, 0, len(d.Returns))
	for _, r := range d.Returns {
		out.Returns = append(out.Returns, gen.ReturnSummary{
			ID: r.ID, Code: r.Code, Kind: gen.ReturnKind(r.Kind), Status: gen.ReturnStatus(r.Status),
		})
	}
	if d.CancelReason != nil {
		out.CancelReason = gen.NewOptCancelReason(gen.CancelReason(*d.CancelReason))
	}
	if d.CancelNote != nil {
		out.CancelNote = gen.NewOptString(*d.CancelNote)
	}
	if d.CareBy != nil {
		out.CareBy = gen.NewOptActor(gen.Actor{ID: *d.CareBy, Name: names[*d.CareBy]})
	}
	if d.CreatedBy != nil {
		out.CreatedBy = gen.NewOptActor(gen.Actor{ID: *d.CreatedBy, Name: names[*d.CreatedBy]})
	}
	return out
}
