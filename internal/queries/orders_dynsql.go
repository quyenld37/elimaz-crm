package queries

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// Giới hạn của OrderList (khớp tham số limit của listOrders trong openapi.yaml).
const (
	DefaultOrderListLimit = 50
	MaxOrderListLimit     = 100
)

// ErrInvalidOrderFilter là lỗi bộ lọc của OrderList sai (thiếu người bán, nhãn hay trạng thái COD lạ); lớp HTTP đổi sang
// sentinel của order để ra mã lỗi như M2.
var ErrInvalidOrderFilter = errors.New("bộ lọc danh sách đơn không hợp lệ")

// Trạng thái tiền COD của bộ lọc codState (kế hoạch M3, Task M3-28) trên số dư của cash.shipment_cod_v của vận đơn hiện
// hành.
const (
	CODCollecting = "collecting" // còn phải thu: to_collect > 0
	CODHolding    = "holding"    // hãng đang giữ: carrier_holding > 0
	CODReceived   = "received"   // shop đã nhận: shop_received > 0
	CODNone       = "none"       // không có vận đơn hiện hành, hay cả ba ngăn ≤ 0
)

// shipmentLabelStatuses là các trạng thái vận đơn đặt nhãn riêng cho đơn confirmed (khớp orderview.Label).
const shipmentLabelStatuses = "('creating', 'awaiting_pickup', 'shipping', 'delivery_failed', 'returning', 'returned')"

// OrderSearch là phần tìm của danh sách đơn đã giải sẵn (order.ResolveListSearch giải: cần module customer, gói này
// không nhập module). CustomerRoots, NameRoots nil: không lọc theo nhóm khách đó; khác nil (kể cả rỗng): chỉ đơn của
// khách thuộc nhóm gốc trong danh sách. Tối đa một trong Code (mã đơn viết hoa), Phone (SĐT chuẩn hóa), PhoneTail (chữ
// số đảo ngược của đuôi SĐT, từ 4 số) khác rỗng.
type OrderSearch struct {
	CustomerRoots, NameRoots []uuid.UUID
	Code, Phone, PhoneTail   string
}

// OrderFilter là bộ lọc của OrderList. SellerID bắt buộc (ADR-29). Mỗi danh sách rỗng là không lọc; nhiều giá trị
// trong một danh sách là "hoặc", các bộ lọc khác nhau là "và". CreatedFrom ≤ created_at < CreatedTo. CarrierKey,
// ShipmentStatuses: hãng, trạng thái của vận đơn hiện hành; CODState: một trong CODCollecting, CODHolding, CODReceived,
// CODNone; PackingStatus: none, picking, packed ("" không lọc). WarehouseID: kho của đơn (nil không lọc). LabelPrinted:
// vận đơn hiện hành đã (true) hay chưa (false) in nhãn; khác nil thì chỉ đơn có vận đơn hiện hành (nil không lọc).
type OrderFilter struct {
	SellerID               uuid.UUID
	Labels                 []orderview.LabelKey
	Statuses               []string
	PendingReasons         []string
	Channels               []string
	TrafficSourceIDs       []uuid.UUID
	CareBy                 *uuid.UUID
	CreatedFrom, CreatedTo *time.Time
	DuplicateSuspect       *bool
	CarrierKey             string
	ShipmentStatuses       []string
	CODState               string
	PackingStatus          string
	WarehouseID            *uuid.UUID
	LabelPrinted           *bool
	Search                 OrderSearch
}

// OrderShipment là vận đơn hiện hành của một đơn (fulfillment.shipments_v). TrackingCode "" khi hãng chưa cấp. Mode:
// api hay manual (cách nối của tài khoản lúc gửi); LabelPrintedAt nil khi chưa in nhãn của hãng.
type OrderShipment struct {
	ID, CarrierAccountID           uuid.UUID
	Code, CarrierKey, TrackingCode string
	Status, SubStatus, Mode        string
	StuckSince, LabelPrintedAt     *time.Time
	NeedsReview                    bool
}

// OrderCOD là số dư COD ba ngăn của vận đơn hiện hành (cash.shipment_cod_v), đồng.
type OrderCOD struct {
	ToCollect, CarrierHolding, ShopReceived int64
}

// OrderCursor là vị trí sau dòng cuối của một trang (created_at giảm, rồi id giảm). Giá trị rỗng: trang đầu (khi gửi
// vào), hết trang (khi OrderList trả).
type OrderCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// OrderListRow là một đơn của danh sách. CustomerRootID, CustomerName là của khách gốc (nhóm sau gộp); ItemCount là
// tổng số lượng các dòng; Label là orderview.Label của bốn trục (trục vận đơn từ Shipment). Shipment nil: không có vận
// đơn hiện hành; COD nil: không có vận đơn hiện hành hay vận đơn chưa có bút toán COD. CustomerName, ShipPhone là dữ liệu
// cá nhân: LogValue của dòng chỉ ghi mã.
type OrderListRow struct {
	ID                    uuid.UUID
	Code                  string
	Label                 orderview.LabelKey
	Status, PendingReason string
	PackingStatus         string
	CustomerRootID        uuid.UUID
	CustomerName          string
	ShipPhone             logging.Phone
	GrandTotal, CodAmount int64
	ItemCount             int
	Channel               string
	TrafficSource         *uuid.UUID
	CareBy                *uuid.UUID
	CreatedAt             time.Time
	DuplicateSuspect      bool
	Shipment              *OrderShipment
	COD                   *OrderCOD
}

// LogValue chỉ ghi mã đơn (không tên khách, SĐT).
func (r OrderListRow) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", r.ID.String()), slog.String("code", r.Code))
}

// OrderList trả một trang đơn của người bán f.SellerID theo f, mới nhất trước, keyset (created_at, id) giảm dần sau
// cursor (rỗng: trang đầu), và con trỏ trang sau (rỗng khi hết). limit ≤ 0: DefaultOrderListLimit; tối đa
// MaxOrderListLimit. Đơn tạo sau lần đọc trang trước không chen vào trang sau. Ghép ord.orders_v ⟕
// fulfillment.shipments_v (vận đơn hiện hành, cùng người bán) ⟕ cash.shipment_cod_v. Lỗi: ErrInvalidOrderFilter. Chạy
// trong tenancy.WithTenant.
func OrderList(ctx context.Context, tx pgx.Tx, f OrderFilter, cursor OrderCursor, limit int) ([]OrderListRow, OrderCursor, error) {
	limit = clampOrderLimit(limit)
	sql, args, err := orderListSQL(f, cursor, limit)
	if err != nil {
		return nil, OrderCursor{}, err
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, OrderCursor{}, fmt.Errorf("đọc danh sách đơn: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanOrderListRow)
	if err != nil {
		return nil, OrderCursor{}, fmt.Errorf("đọc danh sách đơn: %w", err)
	}
	var next OrderCursor
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = OrderCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return out, next, nil
}

// scanOrderListRow đọc một dòng của orderListSQL và tính nhãn gộp bằng orderview.Label.
func scanOrderListRow(r pgx.CollectableRow) (OrderListRow, error) {
	var (
		o                                          OrderListRow
		items                                      int64
		shipID, accountID                          pgtype.UUID
		code, carrier, tracking, status, sub, mode pgtype.Text
		stuck, printed                             pgtype.Timestamptz
		review                                     pgtype.Bool
		toCollect, holding, received               pgtype.Int8
	)
	if err := r.Scan(&o.ID, &o.Code, &o.Status, &o.PendingReason, &o.PackingStatus, &o.CustomerRootID, &o.CustomerName,
		&o.ShipPhone, &o.GrandTotal, &o.CodAmount, &items, &o.Channel, &o.TrafficSource, &o.CareBy, &o.CreatedAt,
		&o.DuplicateSuspect, &shipID, &code, &carrier, &tracking, &status, &sub, &stuck, &review,
		&toCollect, &holding, &received, &accountID, &mode, &printed); err != nil {
		return OrderListRow{}, err //nolint:wrapcheck // bên gọi bọc
	}
	o.ItemCount = int(items)
	axes := orderview.Axes{Status: o.Status, PendingReason: o.PendingReason, PackingStatus: o.PackingStatus}
	if shipID.Valid {
		o.Shipment = &OrderShipment{
			ID: shipID.Bytes, CarrierAccountID: accountID.Bytes, Code: code.String, CarrierKey: carrier.String,
			TrackingCode: tracking.String, Status: status.String, SubStatus: sub.String, Mode: mode.String,
			NeedsReview: review.Bool,
		}
		if stuck.Valid {
			t := stuck.Time
			o.Shipment.StuckSince = &t
		}
		if printed.Valid {
			t := printed.Time
			o.Shipment.LabelPrintedAt = &t
		}
		axes.ShipmentStatus = o.Shipment.Status
	}
	if toCollect.Valid {
		o.COD = &OrderCOD{ToCollect: toCollect.Int64, CarrierHolding: holding.Int64, ShopReceived: received.Int64}
	}
	o.Label = orderview.Label(axes)
	return o, nil
}

func clampOrderLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultOrderListLimit
	case n > MaxOrderListLimit:
		return MaxOrderListLimit
	}
	return n
}

// orderLabelCond trả điều kiện SQL của nhãn l, khớp từng nhánh của orderview.Label (trục vận đơn: ss là trạng thái vận
// đơn hiện hành, ” khi không có). Nhãn lạ → ErrInvalidOrderFilter.
func orderLabelCond(l orderview.LabelKey) (string, error) {
	const (
		confirmed = "o.status = 'confirmed' AND "
		ss        = "COALESCE(s.status, '')"
		noShipLbl = ss + " NOT IN " + shipmentLabelStatuses
	)
	switch l {
	case orderview.LabelDraft, orderview.LabelCancelRequested, orderview.LabelCancelled, orderview.LabelDelivered,
		orderview.LabelReturned:
		return "o.status = '" + string(l) + "'", nil
	case orderview.LabelNew:
		return "(o.status = 'new' AND o.pending_reason NOT IN ('discount_approval', 'awaiting_customer', 'out_of_stock'))", nil
	case orderview.LabelAwaitingDiscountApproval:
		return "(o.status = 'new' AND o.pending_reason = 'discount_approval')", nil
	case orderview.LabelAwaitingCustomer, orderview.LabelOutOfStock:
		return "(o.status = 'new' AND o.pending_reason = '" + string(l) + "')", nil
	case orderview.LabelSending:
		return "(" + confirmed + ss + " = 'creating')", nil
	case orderview.LabelAwaitingPickup, orderview.LabelShipping, orderview.LabelDeliveryFailed:
		return "(" + confirmed + ss + " = '" + string(l) + "')", nil
	case orderview.LabelReturning:
		return "(" + confirmed + ss + " IN ('returning', 'returned'))", nil
	case orderview.LabelPacking:
		return "(" + confirmed + noShipLbl + " AND o.packing_status = 'picking')", nil
	case orderview.LabelPacked:
		return "(" + confirmed + "o.packing_status = 'packed' AND " + ss + " = '')", nil
	case orderview.LabelConfirmed:
		return "(" + confirmed + noShipLbl + " AND o.packing_status <> 'picking' AND NOT (o.packing_status = 'packed' AND " +
			ss + " = ''))", nil
	}
	return "", fmt.Errorf("nhãn đơn lạ: %w", ErrInvalidOrderFilter)
}

// codCond trả điều kiện SQL của trạng thái COD st trên số dư cod (LATERAL, NULL khi chưa có bút toán).
func codCond(st string) (string, error) {
	switch st {
	case CODCollecting:
		return "cod.to_collect > 0", nil
	case CODHolding:
		return "cod.carrier_holding > 0", nil
	case CODReceived:
		return "cod.shop_received > 0", nil
	case CODNone:
		return "(cod.shipment_id IS NULL OR (cod.to_collect <= 0 AND cod.carrier_holding <= 0 AND cod.shop_received <= 0))", nil
	}
	return "", fmt.Errorf("trạng thái COD lạ: %w", ErrInvalidOrderFilter)
}

// sqlArgs gom tham số $n của một câu dựng lúc chạy.
type sqlArgs struct{ args []any }

// add thêm v và trả chỗ giữ $n của nó.
func (a *sqlArgs) add(v any) string {
	a.args = append(a.args, v)
	return "$" + strconv.Itoa(len(a.args))
}

// orderListSQL dựng câu danh sách trên view: điều kiện seller_id luôn có (ADR-29), mọi giá trị là tham số ($n), chỉ
// chuỗi hằng của orderLabelCond, codCond ghép vào câu. Số dư COD đọc theo từng vận đơn (LATERAL: điều kiện shipment_id
// đẩy xuống dưới GROUP BY của view, index cod_ledger_entries_shipment_account). Khách: hồ sơ thuộc nhóm gốc
// (customer.customers_v, index customers_root rồi orders_customer_created). Lấy limit + 1 dòng để biết còn trang sau.
func orderListSQL(f OrderFilter, c OrderCursor, limit int) (string, []any, error) {
	if f.SellerID == uuid.Nil {
		return "", nil, fmt.Errorf("danh sách đơn thiếu người bán: %w", ErrInvalidOrderFilter)
	}
	a := &sqlArgs{}
	where := []string{"o.seller_id = " + a.add(f.SellerID)}
	if len(f.Labels) > 0 {
		conds := make([]string, 0, len(f.Labels))
		for _, l := range f.Labels {
			cond, err := orderLabelCond(l)
			if err != nil {
				return "", nil, err
			}
			conds = append(conds, cond)
		}
		where = append(where, "("+strings.Join(conds, " OR ")+")")
	}
	where = append(where, orderFieldConds(f, a)...)
	ship, err := shipmentConds(f, a)
	if err != nil {
		return "", nil, err
	}
	where = append(append(where, ship...), searchConds(f.Search, a)...)
	if c != (OrderCursor{}) {
		where = append(where, "(o.created_at, o.id) < ("+a.add(c.CreatedAt)+"::timestamptz, "+a.add(c.ID)+"::uuid)")
	}
	return orderListSelect + strings.Join(where, "\n  AND ") + `
ORDER BY o.created_at DESC, o.id DESC
LIMIT ` + a.add(limit+1), a.args, nil
}

// orderFieldConds: điều kiện trên cột của đơn (trạng thái, lý do chờ, kênh, nguồn, người phụ trách, ngày, nghi trùng,
// đóng gói).
func orderFieldConds(f OrderFilter, a *sqlArgs) []string {
	var where []string
	if len(f.Statuses) > 0 {
		where = append(where, "o.status = ANY("+a.add(f.Statuses)+"::text[])")
	}
	if len(f.PendingReasons) > 0 {
		where = append(where, "o.pending_reason = ANY("+a.add(f.PendingReasons)+"::text[])")
	}
	if len(f.Channels) > 0 {
		where = append(where, "o.channel = ANY("+a.add(f.Channels)+"::text[])")
	}
	if len(f.TrafficSourceIDs) > 0 {
		where = append(where, "o.traffic_source_id = ANY("+a.add(f.TrafficSourceIDs)+"::uuid[])")
	}
	if f.CareBy != nil {
		where = append(where, "o.care_by = "+a.add(*f.CareBy))
	}
	if f.CreatedFrom != nil {
		where = append(where, "o.created_at >= "+a.add(*f.CreatedFrom))
	}
	if f.CreatedTo != nil {
		where = append(where, "o.created_at < "+a.add(*f.CreatedTo))
	}
	if f.DuplicateSuspect != nil {
		where = append(where, "o.duplicate_suspect = "+a.add(*f.DuplicateSuspect))
	}
	if f.PackingStatus != "" {
		where = append(where, "o.packing_status = "+a.add(f.PackingStatus))
	}
	if f.WarehouseID != nil {
		where = append(where, "o.warehouse_id = "+a.add(*f.WarehouseID))
	}
	return where
}

// shipmentConds: điều kiện trên vận đơn hiện hành (hãng, trạng thái) và số dư COD. Trạng thái COD lạ →
// ErrInvalidOrderFilter.
func shipmentConds(f OrderFilter, a *sqlArgs) ([]string, error) {
	var where []string
	if f.CarrierKey != "" {
		where = append(where, "s.carrier_key = "+a.add(f.CarrierKey))
	}
	if len(f.ShipmentStatuses) > 0 {
		where = append(where, "s.status = ANY("+a.add(f.ShipmentStatuses)+"::text[])")
	}
	switch {
	case f.LabelPrinted == nil:
	case *f.LabelPrinted:
		where = append(where, "s.label_printed_at IS NOT NULL")
	default:
		where = append(where, "s.id IS NOT NULL AND s.label_printed_at IS NULL")
	}
	if f.CODState != "" {
		cond, err := codCond(f.CODState)
		if err != nil {
			return nil, err
		}
		where = append(where, cond)
	}
	return where, nil
}

// searchConds: điều kiện của phần tìm (nhóm khách, mã đơn, SĐT đủ, đuôi SĐT).
func searchConds(s OrderSearch, a *sqlArgs) []string {
	var where []string
	for _, group := range [][]uuid.UUID{s.CustomerRoots, s.NameRoots} {
		if group == nil {
			continue
		}
		where = append(where, "o.customer_id IN (SELECT g.id FROM customer.customers_v AS g WHERE g.root_id = ANY("+
			a.add(group)+"::uuid[]))")
	}
	switch {
	case s.Code != "":
		where = append(where, "o.code = "+a.add(s.Code))
	case s.Phone != "":
		where = append(where, "o.ship_phone = "+a.add(s.Phone))
	case s.PhoneTail != "":
		// Khoảng [lo, hi) theo thứ tự byte (toán tử text_pattern_ops, index orders_ship_phone_reversed).
		lo := s.PhoneTail
		hi := lo[:len(lo)-1] + string(lo[len(lo)-1]+1)
		where = append(where, "o.ship_phone_reversed ~>=~ "+a.add(lo)+"::text AND o.ship_phone_reversed ~<~ "+a.add(hi)+"::text")
	}
	return where
}

// orderListSelect là phần SELECT … WHERE của orderListSQL (điều kiện nối sau).
const orderListSelect = `SELECT o.id, o.code, o.status, o.pending_reason, o.packing_status, r.id, r.full_name, o.ship_phone,
       o.grand_total, o.cod_amount,
       COALESCE((SELECT sum(i.quantity) FROM ord.order_items_v AS i
                 WHERE i.tenant_id = o.tenant_id AND i.seller_id = o.seller_id AND i.order_id = o.id), 0)::bigint,
       o.channel, o.traffic_source_id, o.care_by, o.created_at, o.duplicate_suspect,
       s.id, s.code, s.carrier_key, s.tracking_code, s.status, s.sub_status, s.stuck_since, s.needs_review,
       cod.to_collect, cod.carrier_holding, cod.shop_received, s.carrier_account_id, s.mode, s.label_printed_at
FROM ord.orders_v AS o
JOIN customer.customers_v AS c ON c.id = o.customer_id
JOIN customer.customers_v AS r ON r.id = c.root_id
LEFT JOIN fulfillment.shipments_v AS s ON s.id = o.current_shipment_id AND s.seller_id = o.seller_id
LEFT JOIN LATERAL (
    SELECT b.shipment_id, b.to_collect, b.carrier_holding, b.shop_received
    FROM cash.shipment_cod_v AS b
    WHERE b.shipment_id = s.id
) AS cod ON true
WHERE `
