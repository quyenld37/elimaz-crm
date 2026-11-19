package order

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/queries/orderview"
)

// Giới hạn của List (khớp tham số limit, q của listOrders trong openapi.yaml).
const (
	DefaultListLimit  = 50
	MaxListLimit      = 100
	maxListQueryRunes = 100
	minTailDigits     = 4
)

// orderCodePattern: mã đơn DH + chữ số, không phân biệt hoa thường.
var orderCodePattern = regexp.MustCompile(`^(?i)DH[0-9]+$`)

// ListFilter là bộ lọc của List. SellerID bắt buộc (ADR-29, bên gọi luôn đặt). Mỗi danh sách rỗng là không lọc; nhiều
// giá trị trong một danh sách là "hoặc", các bộ lọc khác nhau là "và". CustomerID: cả nhóm gốc của khách đó (gồm hồ sơ
// đã gộp). CreatedFrom ≤ created_at < CreatedTo. Q: mã đơn DH…, SĐT người nhận (đủ, hay đuôi từ 4 chữ số) hoặc tên khách
// (customer.Search, tối đa MaxListLimit nhóm); Q không vào log, lỗi.
type ListFilter struct {
	Labels                 []orderview.LabelKey
	Statuses               []string
	PendingReasons         []string
	Channels               []string
	TrafficSourceIDs       []uuid.UUID
	CareBy                 *uuid.UUID
	CustomerID             *uuid.UUID
	CreatedFrom, CreatedTo *time.Time
	Q                      string
	DuplicateSuspect       *bool
	SellerID               uuid.UUID
}

// OrderRow là một đơn của danh sách. CustomerRootID, CustomerName là của khách gốc (nhóm sau gộp); ItemCount là tổng
// số lượng các dòng; Label là orderview.Label của ba trục (M2 chưa có trục vận đơn).
type OrderRow struct { //nolint:revive // tên theo Interfaces của kế hoạch M2 (Task M2-24), task sau dùng
	ID                    uuid.UUID
	Code                  string
	Label                 orderview.LabelKey
	Status, PendingReason string
	CustomerRootID        uuid.UUID
	CustomerName          customer.Name
	ShipPhone             logging.Phone
	GrandTotal, CodAmount int64
	ItemCount             int
	Channel               string
	TrafficSource         *uuid.UUID
	CareBy                *uuid.UUID
	CreatedAt             time.Time
	DuplicateSuspect      bool
}

// Cursor là vị trí sau dòng cuối của một trang danh sách đơn (created_at giảm, rồi id giảm): cùng khóa keyset và cùng
// cách mã hóa với danh sách khách nên dùng lại customer.ListCursor. Giá trị rỗng: trang đầu (khi gửi vào), hết trang
// (khi List trả).
type Cursor = customer.ListCursor

// ParseCursor đọc chuỗi do Cursor.String tạo. Hỏng → ErrInvalidInput (của order).
func ParseCursor(s string) (Cursor, error) {
	c, err := customer.ParseListCursor(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("đọc con trỏ danh sách đơn: %w", ErrInvalidInput)
	}
	return c, nil
}

// List trả một trang đơn của người bán f.SellerID theo f, mới nhất trước, keyset (created_at, id) giảm dần sau cursor
// (rỗng: trang đầu), và con trỏ trang sau (rỗng khi hết). limit ≤ 0: DefaultListLimit; tối đa MaxListLimit. Đơn tạo
// sau lần đọc trang trước không chen vào trang sau. Lỗi: ErrInvalidInput (nhãn, trạng thái lạ, q quá dài),
// ErrLabelNotFilterable (nhãn của vận đơn), customer.ErrQueryTooShort (q chỉ chữ số dưới 4 số; q tên quá ngắn).
// Chạy trong tenancy.WithTenant.
func List(ctx context.Context, tx pgx.Tx, f ListFilter, cursor Cursor, limit int) ([]OrderRow, Cursor, error) {
	m, err := classifyQ(f.Q)
	if err != nil {
		return nil, Cursor{}, err
	}
	if m.name != "" {
		hits, err := customer.Search(ctx, tx, m.name, customer.MaxListLimit)
		if err != nil {
			return nil, Cursor{}, fmt.Errorf("tìm khách của danh sách đơn: %w", err)
		}
		m.nameRoots = make([]uuid.UUID, 0, len(hits))
		for _, h := range hits {
			m.nameRoots = append(m.nameRoots, h.RootID)
		}
	}
	var roots []uuid.UUID
	if f.CustomerID != nil {
		root, err := customer.RootOf(ctx, tx, *f.CustomerID)
		if errors.Is(err, customer.ErrNotFound) {
			return []OrderRow{}, Cursor{}, nil
		}
		if err != nil {
			return nil, Cursor{}, fmt.Errorf("đọc gốc của khách lọc: %w", err)
		}
		roots = []uuid.UUID{root}
	}
	limit = clampListLimit(limit)
	sql, args, err := listSQL(f, m, roots, cursor, limit)
	if err != nil {
		return nil, Cursor{}, err
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, Cursor{}, fmt.Errorf("đọc danh sách đơn: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (OrderRow, error) {
		var o OrderRow
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
		return nil, Cursor{}, fmt.Errorf("đọc danh sách đơn: %w", err)
	}
	var next Cursor
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return out, next, nil
}

func clampListLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultListLimit
	case n > MaxListLimit:
		return MaxListLimit
	}
	return n
}

// qMatch là q đã phân loại: đúng một trong code (mã đơn viết hoa), phone (SĐT chuẩn hóa), tail (chữ số đảo ngược của
// đuôi SĐT), name (chuỗi tìm tên; nameRoots là gốc khách khớp, List điền).
type qMatch struct {
	code, phone, tail, name string
	nameRoots               []uuid.UUID
}

// classifyQ phân loại q như Task M2-10 (tìm khách) nhưng cho đơn: "DH" + chữ số → mã; chỉ chữ số (bỏ khoảng trắng,
// chấm, gạch, "+") chuẩn hóa được thành SĐT → SĐT đủ, không thì từ 4 chữ số → đuôi, dưới 4 → customer.ErrQueryTooShort;
// còn lại → tên. Không đưa q vào lỗi.
func classifyQ(q string) (qMatch, error) {
	q = strings.TrimSpace(q)
	switch {
	case q == "":
		return qMatch{}, nil
	case utf8.RuneCountInString(q) > maxListQueryRunes:
		return qMatch{}, fmt.Errorf("chuỗi tìm đơn quá dài: %w", ErrInvalidInput)
	case orderCodePattern.MatchString(q):
		return qMatch{code: strings.ToUpper(q)}, nil
	}
	digits, ok := onlyDigits(q)
	if !ok {
		return qMatch{name: q}, nil
	}
	if p, err := customer.NormalizePhone(q); err == nil {
		return qMatch{phone: string(p)}, nil
	}
	if len(digits) < minTailDigits {
		return qMatch{}, fmt.Errorf("đuôi SĐT của đơn: %w", customer.ErrQueryTooShort)
	}
	return qMatch{tail: customer.ReverseDigits(logging.Phone(digits))}, nil
}

// onlyDigits bỏ khoảng trắng, chấm, gạch, "+" của s và báo phần còn lại có phải toàn chữ số (ít nhất một).
func onlyDigits(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '.' || r == '-' || r == '+' || r == '\t':
		default:
			return "", false
		}
	}
	return b.String(), b.Len() > 0
}

// labelCond trả điều kiện SQL của nhãn l trên ba trục M2 (khớp orderview.Label với ShipmentStatus rỗng). Nhãn của
// vận đơn → ErrLabelNotFilterable; nhãn lạ → ErrInvalidInput.
func labelCond(l orderview.LabelKey) (string, error) {
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
	return "", fmt.Errorf("nhãn đơn lạ: %w", ErrInvalidInput)
}

// listSQL dựng câu danh sách: điều kiện seller_id luôn có (ADR-29), mọi giá trị là tham số ($n), chỉ chuỗi hằng của
// labelCond ghép vào câu. Khách: hồ sơ thuộc nhóm roots (customer.customers_v, index customers_root rồi
// orders_customer_created). Lấy limit + 1 dòng để biết còn trang sau.
func listSQL(f ListFilter, m qMatch, roots []uuid.UUID, c Cursor, limit int) (string, []any, error) {
	if f.SellerID == uuid.Nil {
		return "", nil, fmt.Errorf("danh sách đơn thiếu người bán: %w", ErrInvalidInput)
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
			cond, err := labelCond(l)
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
	for _, group := range [][]uuid.UUID{roots, m.nameRoots} {
		if group == nil {
			continue
		}
		where = append(where, "o.customer_id IN (SELECT g.id FROM customer.customers_v AS g WHERE g.root_id = ANY("+
			arg(group)+"::uuid[]))")
	}
	switch {
	case m.code != "":
		where = append(where, "o.code = "+arg(m.code))
	case m.phone != "":
		where = append(where, "o.ship_phone = "+arg(m.phone))
	case m.tail != "":
		// Khoảng [lo, hi) theo thứ tự byte (toán tử text_pattern_ops, index orders_ship_phone_reversed).
		lo := m.tail
		hi := lo[:len(lo)-1] + string(lo[len(lo)-1]+1)
		where = append(where, "o.ship_phone_reversed ~>=~ "+arg(lo)+"::text AND o.ship_phone_reversed ~<~ "+arg(hi)+"::text")
	}
	if c != (Cursor{}) {
		where = append(where, "(o.created_at, o.id) < ("+arg(c.CreatedAt)+"::timestamptz, "+arg(c.ID)+"::uuid)")
	}
	sql := `SELECT o.id, o.code, o.status, o.pending_reason, o.packing_status, r.id, r.full_name, o.ship_phone,
       o.grand_total, o.cod_amount,
       COALESCE((SELECT sum(i.quantity) FROM ord.order_items AS i
                 WHERE i.tenant_id = o.tenant_id AND i.seller_id = o.seller_id AND i.order_id = o.id), 0)::bigint,
       o.channel, o.traffic_source_id, o.care_by, o.created_at, o.duplicate_suspect
FROM ord.orders AS o
JOIN customer.customers_v AS c ON c.id = o.customer_id
JOIN customer.customers_v AS r ON r.id = c.root_id
WHERE ` + strings.Join(where, "\n  AND ") + `
ORDER BY o.created_at DESC, o.id DESC
LIMIT ` + arg(limit+1)
	return sql, args, nil
}
