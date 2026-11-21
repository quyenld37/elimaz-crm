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

	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// Giới hạn của OrderList (khớp tham số limit của listOrders trong openapi.yaml).
const (
	DefaultOrderListLimit = 50
	MaxOrderListLimit     = 100
)

// Lỗi của OrderList (lớp HTTP đổi sang sentinel của order để ra mã lỗi như M2).
var (
	// ErrInvalidOrderFilter: bộ lọc sai (thiếu người bán, nhãn lạ).
	ErrInvalidOrderFilter = errors.New("bộ lọc danh sách đơn không hợp lệ")
	// ErrLabelNotFilterable: nhãn của vận đơn chưa lọc được.
	ErrLabelNotFilterable = errors.New("nhãn của vận đơn chưa lọc được")
)

// OrderSearch là phần tìm của danh sách đơn đã giải sẵn (order.ResolveListSearch giải: cần module customer, gói này
// không nhập module). CustomerRoots, NameRoots nil: không lọc theo nhóm khách đó; khác nil (kể cả rỗng): chỉ đơn của
// khách thuộc nhóm gốc trong danh sách. Tối đa một trong Code (mã đơn viết hoa), Phone (SĐT chuẩn hóa), PhoneTail (chữ
// số đảo ngược của đuôi SĐT, từ 4 số) khác rỗng.
type OrderSearch struct {
	CustomerRoots, NameRoots []uuid.UUID
	Code, Phone, PhoneTail   string
}

// OrderFilter là bộ lọc của OrderList. SellerID bắt buộc (ADR-29). Mỗi danh sách rỗng là không lọc; nhiều giá trị
// trong một danh sách là "hoặc", các bộ lọc khác nhau là "và". CreatedFrom ≤ created_at < CreatedTo.
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
	Search                 OrderSearch
}

// OrderCursor là vị trí sau dòng cuối của một trang (created_at giảm, rồi id giảm). Giá trị rỗng: trang đầu (khi gửi
// vào), hết trang (khi OrderList trả).
type OrderCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// OrderListRow là một đơn của danh sách. CustomerRootID, CustomerName là của khách gốc (nhóm sau gộp); ItemCount là
// tổng số lượng các dòng; Label là orderview.Label của các trục. CustomerName, ShipPhone là dữ liệu cá nhân: LogValue
// của dòng chỉ ghi mã.
type OrderListRow struct {
	ID                    uuid.UUID
	Code                  string
	Label                 orderview.LabelKey
	Status, PendingReason string
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
}

// LogValue chỉ ghi mã đơn (không tên khách, SĐT).
func (r OrderListRow) LogValue() slog.Value {
	return slog.GroupValue(slog.String("id", r.ID.String()), slog.String("code", r.Code))
}

// OrderList trả một trang đơn của người bán f.SellerID theo f, mới nhất trước, keyset (created_at, id) giảm dần sau
// cursor (rỗng: trang đầu), và con trỏ trang sau (rỗng khi hết). limit ≤ 0: DefaultOrderListLimit; tối đa
// MaxOrderListLimit. Đơn tạo sau lần đọc trang trước không chen vào trang sau. Lỗi: ErrInvalidOrderFilter,
// ErrLabelNotFilterable. Chạy trong tenancy.WithTenant.
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
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (OrderListRow, error) {
		var o OrderListRow
		var packing string
		var items int64
		err := r.Scan(&o.ID, &o.Code, &o.Status, &o.PendingReason, &packing, &o.CustomerRootID, &o.CustomerName,
			&o.ShipPhone, &o.GrandTotal, &o.CodAmount, &items, &o.Channel, &o.TrafficSource, &o.CareBy, &o.CreatedAt,
			&o.DuplicateSuspect)
		o.ItemCount = int(items)
		o.Label = orderview.Label(orderview.Axes{Status: o.Status, PendingReason: o.PendingReason, PackingStatus: packing})
		return o, err //nolint:wrapcheck // bọc ở dưới
	})
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

func clampOrderLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultOrderListLimit
	case n > MaxOrderListLimit:
		return MaxOrderListLimit
	}
	return n
}

// orderLabelCond trả điều kiện SQL của nhãn l (khớp orderview.Label). Nhãn của vận đơn → ErrLabelNotFilterable; nhãn
// lạ → ErrInvalidOrderFilter.
func orderLabelCond(l orderview.LabelKey) (string, error) {
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
	case orderview.LabelConfirmed:
		return "(o.status = 'confirmed' AND o.packing_status NOT IN ('picking', 'packed'))", nil
	case orderview.LabelPacking:
		return "(o.status = 'confirmed' AND o.packing_status = 'picking')", nil
	case orderview.LabelPacked:
		return "(o.status = 'confirmed' AND o.packing_status = 'packed')", nil
	case orderview.LabelAwaitingPickup, orderview.LabelShipping, orderview.LabelDeliveryFailed, orderview.LabelReturning:
		return "", ErrLabelNotFilterable
	}
	return "", fmt.Errorf("nhãn đơn lạ: %w", ErrInvalidOrderFilter)
}

// orderListSQL dựng câu danh sách trên view: điều kiện seller_id luôn có (ADR-29), mọi giá trị là tham số ($n), chỉ
// chuỗi hằng của orderLabelCond ghép vào câu. Khách: hồ sơ thuộc nhóm gốc (customer.customers_v, index customers_root
// rồi orders_customer_created). Lấy limit + 1 dòng để biết còn trang sau.
func orderListSQL(f OrderFilter, c OrderCursor, limit int) (string, []any, error) {
	if f.SellerID == uuid.Nil {
		return "", nil, fmt.Errorf("danh sách đơn thiếu người bán: %w", ErrInvalidOrderFilter)
	}
	args := []any{f.SellerID}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	where := []string{"o.seller_id = $1"}
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
	if len(f.Statuses) > 0 {
		where = append(where, "o.status = ANY("+arg(f.Statuses)+"::text[])")
	}
	if len(f.PendingReasons) > 0 {
		where = append(where, "o.pending_reason = ANY("+arg(f.PendingReasons)+"::text[])")
	}
	if len(f.Channels) > 0 {
		where = append(where, "o.channel = ANY("+arg(f.Channels)+"::text[])")
	}
	if len(f.TrafficSourceIDs) > 0 {
		where = append(where, "o.traffic_source_id = ANY("+arg(f.TrafficSourceIDs)+"::uuid[])")
	}
	if f.CareBy != nil {
		where = append(where, "o.care_by = "+arg(*f.CareBy))
	}
	if f.CreatedFrom != nil {
		where = append(where, "o.created_at >= "+arg(*f.CreatedFrom))
	}
	if f.CreatedTo != nil {
		where = append(where, "o.created_at < "+arg(*f.CreatedTo))
	}
	if f.DuplicateSuspect != nil {
		where = append(where, "o.duplicate_suspect = "+arg(*f.DuplicateSuspect))
	}
	s := f.Search
	for _, group := range [][]uuid.UUID{s.CustomerRoots, s.NameRoots} {
		if group == nil {
			continue
		}
		where = append(where, "o.customer_id IN (SELECT g.id FROM customer.customers_v AS g WHERE g.root_id = ANY("+
			arg(group)+"::uuid[]))")
	}
	switch {
	case s.Code != "":
		where = append(where, "o.code = "+arg(s.Code))
	case s.Phone != "":
		where = append(where, "o.ship_phone = "+arg(s.Phone))
	case s.PhoneTail != "":
		// Khoảng [lo, hi) theo thứ tự byte (toán tử text_pattern_ops, index orders_ship_phone_reversed).
		lo := s.PhoneTail
		hi := lo[:len(lo)-1] + string(lo[len(lo)-1]+1)
		where = append(where, "o.ship_phone_reversed ~>=~ "+arg(lo)+"::text AND o.ship_phone_reversed ~<~ "+arg(hi)+"::text")
	}
	if c != (OrderCursor{}) {
		where = append(where, "(o.created_at, o.id) < ("+arg(c.CreatedAt)+"::timestamptz, "+arg(c.ID)+"::uuid)")
	}
	sql := `SELECT o.id, o.code, o.status, o.pending_reason, o.packing_status, r.id, r.full_name, o.ship_phone,
       o.grand_total, o.cod_amount,
       COALESCE((SELECT sum(i.quantity) FROM ord.order_items_v AS i
                 WHERE i.tenant_id = o.tenant_id AND i.seller_id = o.seller_id AND i.order_id = o.id), 0)::bigint,
       o.channel, o.traffic_source_id, o.care_by, o.created_at, o.duplicate_suspect
FROM ord.orders_v AS o
JOIN customer.customers_v AS c ON c.id = o.customer_id
JOIN customer.customers_v AS r ON r.id = c.root_id
WHERE ` + strings.Join(where, "\n  AND ") + `
ORDER BY o.created_at DESC, o.id DESC
LIMIT ` + arg(limit+1)
	return sql, args, nil
}
