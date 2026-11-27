package reportseed

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Item là một dòng hàng: Qty chiếc biến thể Variant giá Price (đồng).
type Item struct {
	Variant uuid.UUID
	Qty     int
	Price   int64
}

// Step là một lần đổi trạng thái của đơn: sang To lúc At. Bước đầu là lúc tạo (dòng lịch sử from_status = ”).
type Step struct {
	To string
	At time.Time
}

// ShipmentSpec là một vận đơn. Account rỗng → tài khoản hãng CarrierKey của người bán đơn (tự tạo). Vận đơn cuối
// chưa hủy là vận đơn hiện hành của đơn (M3 gỡ vận đơn hiện hành khi hủy). FeeActual nil → chưa đối soát.
type ShipmentSpec struct {
	CarrierKey string
	Account    uuid.UUID
	Status     string
	Fee        int64
	FeeActual  *int64
	COD        int64
	CreatedAt  time.Time
}

// ReturnLine là một dòng phiếu trả: dòng hàng thứ Item (0 là dòng đầu của OrderSpec.Items), Qty chiếc, chia
// Sellable/Damaged/Missing (tổng 0 hoặc Qty), hoàn Refund đồng.
type ReturnLine struct {
	Item                            int
	Qty, Sellable, Damaged, Missing int
	Refund                          int64
}

// ReturnSpec là một phiếu trả (Kind: carrier_return, customer_return…; Status: requested, received…). ReceivedAt
// nil → chưa nhận. Tiền hoàn của phiếu là tổng Refund của các dòng.
type ReturnSpec struct {
	Kind, Status string
	ReceivedAt   *time.Time
	CreatedAt    time.Time
	Lines        []ReturnLine
}

// OrderSpec mô tả một đơn. Seller rỗng → người bán own. Page, Source, CareBy, CreatedBy rỗng → NULL. Channel rỗng →
// manual. Trạng thái của đơn là To của bước cuối; created_at là At của bước đầu. Discount là giảm cả đơn
// (order_discount), phân bổ vào allocated_order_discount của các dòng theo tỷ lệ tiền hàng (dòng cuối nhận phần dư).
type OrderSpec struct {
	Seller, Page, Source, CareBy, CreatedBy uuid.UUID
	Channel                                 string
	Items                                   []Item
	ShippingFee, Discount                   int64
	History                                 []Step
	Shipments                               []ShipmentSpec
	Returns                                 []ReturnSpec
}

// orderMoney là tiền của đơn tính đúng các CHECK của ord.orders và trigger check_order_totals.
type orderMoney struct {
	subtotal, grand int64
	alloc           []int64
}

func moneyOf(o OrderSpec) (orderMoney, error) {
	var m orderMoney
	for _, it := range o.Items {
		if it.Qty <= 0 || it.Price < 0 {
			return m, fmt.Errorf("dòng hàng sai: %+v", it)
		}
		m.subtotal += int64(it.Qty) * it.Price
	}
	if o.Discount < 0 || o.Discount > m.subtotal {
		return m, fmt.Errorf("giảm %d ngoài 0..%d", o.Discount, m.subtotal)
	}
	m.alloc = make([]int64, len(o.Items))
	left := o.Discount
	for i, it := range o.Items {
		if i == len(o.Items)-1 {
			m.alloc[i] = left
			break
		}
		if m.subtotal > 0 {
			m.alloc[i] = o.Discount * int64(it.Qty) * it.Price / m.subtotal
		}
		left -= m.alloc[i]
	}
	m.grand = m.subtotal - o.Discount + o.ShippingFee
	return m, nil
}

// nullID đổi mã rỗng thành NULL.
func nullID(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: id != uuid.Nil} }

// stepTimes là các mốc lấy từ lịch sử: lần cuối sang confirmed, sang cancelled.
func stepTimes(h []Step) (confirmed, cancelled *time.Time) {
	for _, st := range h {
		at := st.At
		switch st.To {
		case "confirmed":
			confirmed = &at
		case "cancelled":
			cancelled = &at
		}
	}
	return confirmed, cancelled
}

// Order chèn một đơn đủ dòng hàng, lịch sử, vận đơn, phiếu trả trong một transaction và trả mã đơn.
func (s *Seed) Order(o OrderSpec) uuid.UUID {
	s.t.Helper()
	if len(o.History) == 0 || len(o.Items) == 0 {
		s.t.Fatalf("reportseed: đơn cần ít nhất một bước lịch sử và một dòng hàng")
	}
	m, err := moneyOf(o)
	if err != nil {
		s.t.Fatalf("reportseed: %v", err)
	}
	if o.Seller == uuid.Nil {
		o.Seller = s.Own()
	}
	if o.Channel == "" {
		o.Channel = "manual"
	}
	for _, sh := range o.Shipments {
		if sh.Account == uuid.Nil {
			s.account(o.Seller, sh.CarrierKey)
		}
	}
	wh, cust := s.defaultWarehouse(), s.defaultCustomer()
	var id uuid.UUID
	s.exec("chèn đơn", func(ctx context.Context, tx pgx.Tx) error {
		var err error
		if id, err = s.insertOrder(ctx, tx, o, m, wh, cust); err != nil {
			return err
		}
		if err := s.insertItems(ctx, tx, o, m, id); err != nil {
			return err
		}
		if err := s.insertHistory(ctx, tx, o.History, id); err != nil {
			return err
		}
		if err := s.insertShipments(ctx, tx, o, id); err != nil {
			return err
		}
		return s.insertReturns(ctx, tx, o, id)
	})
	return id
}

func (s *Seed) insertOrder(ctx context.Context, tx pgx.Tx, o OrderSpec, m orderMoney, wh, cust uuid.UUID) (uuid.UUID, error) {
	status, created := o.History[len(o.History)-1].To, o.History[0].At
	confirmed, cancelled := stepTimes(o.History)
	var draftExpires *time.Time
	if status == "draft" {
		e := created.Add(24 * time.Hour)
		draftExpires = &e
	}
	var cancelReason *string
	if status == "cancelled" {
		r := "customer_changed_mind"
		cancelReason = &r
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO ord.orders (tenant_id, seller_id, code, customer_id, channel, idempotency_key, status,
			ship_address, subtotal, discount_total, order_discount, shipping_fee_charged, cod_amount, grand_total, created_by,
			care_by, channel_account_id, traffic_source_id, warehouse_id, created_at, updated_at, confirmed_at, cancelled_at,
			cancel_reason, draft_expires_at)
		VALUES ($1, $2, $21, $3, $4, $5, $6, '{}', $7, $8, $8, $9, $10, $10,
			$11, $12, $13, $14, $15, $16, $16, $17, $18, $19, $20)
		RETURNING id`,
		s.tenant, o.Seller, cust, o.Channel, uuid.New(), status, m.subtotal, o.Discount, o.ShippingFee, m.grand,
		nullID(o.CreatedBy), nullID(o.CareBy), nullID(o.Page), nullID(o.Source), wh, created, confirmed, cancelled,
		cancelReason, draftExpires, s.docCode("DH")).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("đơn: %w", err)
	}
	return id, nil
}

func (s *Seed) insertItems(ctx context.Context, tx pgx.Tx, o OrderSpec, m orderMoney, id uuid.UUID) error {
	for i, it := range o.Items {
		if _, err := tx.Exec(ctx, `INSERT INTO ord.order_items (tenant_id, seller_id, order_id, position, variant_id, sku_snapshot,
				name_snapshot, quantity, unit_price, allocated_order_discount, net_amount)
			SELECT $1, $2, $3, $4, v.id, v.sku, p.name, $5, $6, $7, $8
			FROM catalog.variants v JOIN catalog.products p ON p.id = v.product_id AND p.seller_id = $2
			WHERE v.id = $9 AND v.seller_id = $2`,
			s.tenant, o.Seller, id, i+1, it.Qty, it.Price, m.alloc[i], int64(it.Qty)*it.Price-m.alloc[i], it.Variant); err != nil {
			return fmt.Errorf("dòng hàng %d: %w", i+1, err)
		}
	}
	return nil
}

func (s *Seed) insertHistory(ctx context.Context, tx pgx.Tx, h []Step, id uuid.UUID) error {
	from := ""
	for _, st := range h {
		if _, err := tx.Exec(ctx, `INSERT INTO ord.order_status_history (tenant_id, order_id, from_status, to_status, actor_type,
				occurred_at, recorded_at) VALUES ($1, $2, $3, $4, 'staff', $5, $5)`, s.tenant, id, from, st.To, st.At); err != nil {
			return fmt.Errorf("lịch sử %s → %s: %w", from, st.To, err)
		}
		from = st.To
	}
	return nil
}

func (s *Seed) insertShipments(ctx context.Context, tx pgx.Tx, o OrderSpec, id uuid.UUID) error {
	var current *uuid.UUID
	for _, sh := range o.Shipments {
		acc := sh.Account
		if acc == uuid.Nil {
			acc = s.accounts[o.Seller.String()+"/"+sh.CarrierKey]
		}
		var tracking, cancelKind *string
		if sh.Status != "creating" {
			tr := sh.CarrierKey + "-" + strconv.Itoa(s.next()) + "-" + uuid.NewString()[:8]
			tracking = &tr
		}
		if sh.Status == "cancelled" {
			k := "manual"
			cancelKind = &k
		}
		createState := "created"
		if sh.Status == "creating" {
			createState = "pending"
		}
		var sid uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO fulfillment.shipments (tenant_id, seller_id, code, order_id, carrier_key,
				carrier_account_id, tracking_code, status, cod_amount, fee, fee_actual, cancel_kind, create_state, created_at,
				updated_at, last_status_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $14, $14) RETURNING id`,
			s.tenant, o.Seller, s.shipmentCode(sh.CreatedAt), id, sh.CarrierKey, acc, tracking, sh.Status, sh.COD, sh.Fee,
			sh.FeeActual, cancelKind, createState, sh.CreatedAt).Scan(&sid); err != nil {
			return fmt.Errorf("vận đơn %s: %w", sh.CarrierKey, err)
		}
		if sh.Status != "cancelled" {
			current = &sid
		}
	}
	if current == nil {
		return nil
	}
	_, err := tx.Exec(ctx, "UPDATE ord.orders SET current_shipment_id = $2 WHERE id = $1 AND seller_id = $3", id, *current, o.Seller)
	if err != nil {
		return fmt.Errorf("gắn vận đơn hiện hành: %w", err)
	}
	return nil
}

func (s *Seed) insertReturns(ctx context.Context, tx pgx.Tx, o OrderSpec, id uuid.UUID) error {
	for _, r := range o.Returns {
		var total int64
		for _, l := range r.Lines {
			total += l.Refund
		}
		var rid uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO ord.returns (tenant_id, code, order_id, kind, status, refund_amount, received_at,
				created_at, updated_at)
			VALUES ($1, $8, $2, $3, $4, $5, $6, $7, $7) RETURNING id`,
			s.tenant, id, r.Kind, r.Status, total, r.ReceivedAt, r.CreatedAt, s.docCode("TH")).Scan(&rid); err != nil {
			return fmt.Errorf("phiếu trả: %w", err)
		}
		for _, l := range r.Lines {
			cond := "sellable"
			switch {
			case l.Damaged == l.Qty:
				cond = "damaged"
			case l.Missing == l.Qty:
				cond = "missing"
			case l.Sellable != l.Qty && l.Sellable+l.Damaged+l.Missing == l.Qty:
				cond = "mixed"
			}
			if _, err := tx.Exec(ctx, `INSERT INTO ord.return_items (tenant_id, return_id, order_item_id, quantity, condition,
					refund_amount, sellable_qty, damaged_qty, missing_qty)
				SELECT $1, $2, i.id, $3, $4, $5, $6, $7, $8 FROM ord.order_items i
				WHERE i.order_id = $9 AND i.seller_id = $10 AND i.position = $11`,
				s.tenant, rid, l.Qty, cond, l.Refund, l.Sellable, l.Damaged, l.Missing, id, o.Seller, l.Item+1); err != nil {
				return fmt.Errorf("dòng phiếu trả: %w", err)
			}
		}
	}
	return nil
}
