package reportseed

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// bulkCols là các cột của n đơn ngẫu nhiên, mỗi cột một mảng (chèn bằng unnest: một câu mỗi bảng).
type bulkCols struct {
	order                                       orderCols
	itemOrder, itemVariant                      []uuid.UUID
	itemQty                                     []int32
	itemPrice                                   []int64
	itemSKU, itemName                           []string
	histOrder                                   []uuid.UUID
	histFrom, histTo                            []string
	histAt                                      []time.Time
	shipID, shipOrder, shipAccount              []uuid.UUID
	shipCode, shipKey, shipStatus, shipTracking []string
	shipCOD                                     []int64
	shipAt                                      []time.Time
}

type orderCols struct {
	id                             []uuid.UUID
	page, source, care             []*uuid.UUID
	code, channel, status          []string
	subtotal, ship, grand          []int64
	created                        []time.Time
	confirmed, cancelled, draftExp []*time.Time
	reason                         []*string
}

type bulkRefs struct {
	pages, sources, staff []uuid.UUID
	variants              []uuid.UUID
	skus                  []string
	prices                []int64
	accounts              map[string]uuid.UUID
}

// Bulk dựng n đơn ngẫu nhiên (rnd quyết định mọi lựa chọn) của người bán own, lên trong [from, to): kênh, Page, nguồn,
// nhân viên, một dòng hàng, lịch sử lên → xác nhận → giao xong/hoàn/hủy, vận đơn cho đơn đã gửi hãng. Chèn bằng
// INSERT … SELECT FROM unnest (COPY FROM không chạy trên bảng có RLS) trong một transaction, rồi ANALYZE các bảng.
func (s *Seed) Bulk(n int, rnd *rand.Rand, from, to time.Time) {
	s.t.Helper()
	refs := s.bulkRefs()
	wh, cust := s.defaultWarehouse(), s.defaultCustomer()
	c := bulkCols{}
	span := to.Sub(from)
	for range n {
		s.bulkOrder(&c, refs, rnd, s.docCode("DH"), from.Add(time.Duration(rnd.Int64N(int64(span)))))
	}
	own := s.Own()
	s.exec("chèn đơn hàng loạt", func(ctx context.Context, tx pgx.Tx) error { return s.insertBulk(ctx, tx, c, own, wh, cust) })
	for _, tbl := range []string{"ord.orders", "ord.order_items", "ord.order_status_history", "fulfillment.shipments"} {
		if _, err := s.owner.Pool.Exec(s.t.Context(), "ANALYZE "+tbl); err != nil {
			s.t.Fatalf("reportseed: ANALYZE %s: %v", tbl, err)
		}
	}
}

// bulkRefs tạo hai Page, hai nguồn, hai nhân viên, ba biến thể, hai tài khoản hãng cho Bulk.
func (s *Seed) bulkRefs() bulkRefs {
	own := s.Own()
	r := bulkRefs{accounts: map[string]uuid.UUID{}}
	for range 2 {
		k := strconv.Itoa(s.next())
		r.pages = append(r.pages, s.Page("Page "+k))
		r.sources = append(r.sources, s.Source("Nguồn "+k))
		r.staff = append(r.staff, s.Staff("NV "+k))
	}
	for i, price := range []int64{150_000, 250_000, 200_000} {
		sku := "BULK" + strconv.Itoa(s.next()) + "-" + strconv.Itoa(i)
		r.variants = append(r.variants, s.Variant(own, "Mẫu hàng loạt", sku))
		r.skus = append(r.skus, sku)
		r.prices = append(r.prices, price)
	}
	for _, k := range []string{"vtp", "spx"} {
		r.accounts[k] = s.account(own, k)
	}
	return r
}

func pick[T any](rnd *rand.Rand, xs []T) *T {
	if rnd.IntN(5) == 0 {
		return nil // khoảng 1/5 để trống: nhóm "Không xác định"
	}
	v := xs[rnd.IntN(len(xs))]
	return &v
}

// bulkOrder sinh một đơn vào c.
func (s *Seed) bulkOrder(c *bulkCols, r bulkRefs, rnd *rand.Rand, code string, placed time.Time) {
	id := uuid.New()
	channel := []string{"facebook", "facebook", "facebook", "web", "manual"}[rnd.IntN(5)]
	var page *uuid.UUID
	if channel == "facebook" {
		p := r.pages[rnd.IntN(len(r.pages))]
		page = &p
	}
	v := rnd.IntN(len(r.variants))
	qty := 1 + rnd.Int32N(3)
	subtotal := int64(qty) * r.prices[v]
	ship := []int64{0, 30_000}[rnd.IntN(2)]
	path := [][]string{
		{"new"},
		{"new", "cancelled"},
		{"new", "confirmed"},
		{"new", "confirmed", "delivered"},
		{"new", "confirmed", "delivered"},
		{"new", "confirmed", "delivered"},
		{"new", "confirmed", "returned"},
		{"draft", "confirmed", "delivered"},
	}[rnd.IntN(8)]
	times := make([]time.Time, len(path))
	for k := range path {
		times[k] = placed.Add(time.Duration(k) * 26 * time.Hour)
	}
	o := &c.order
	o.id, o.page, o.source, o.care = append(o.id, id), append(o.page, page), append(o.source, pick(rnd, r.sources)), append(o.care, pick(rnd, r.staff))
	o.code, o.channel, o.status = append(o.code, code), append(o.channel, channel), append(o.status, path[len(path)-1])
	o.subtotal, o.ship, o.grand = append(o.subtotal, subtotal), append(o.ship, ship), append(o.grand, subtotal+ship)
	o.created = append(o.created, placed)
	var confirmed, cancelled *time.Time
	var reason *string
	for k, st := range path {
		switch st {
		case "confirmed":
			confirmed = &times[k]
		case "cancelled":
			cancelled, reason = &times[k], new("customer_changed_mind")
		}
	}
	o.confirmed, o.cancelled, o.reason, o.draftExp = append(o.confirmed, confirmed), append(o.cancelled, cancelled), append(o.reason, reason), append(o.draftExp, nil)
	c.itemOrder, c.itemVariant, c.itemQty = append(c.itemOrder, id), append(c.itemVariant, r.variants[v]), append(c.itemQty, qty)
	c.itemPrice, c.itemSKU, c.itemName = append(c.itemPrice, r.prices[v]), append(c.itemSKU, r.skus[v]), append(c.itemName, "Mẫu hàng loạt")
	from := ""
	for k, st := range path {
		c.histOrder, c.histFrom, c.histTo, c.histAt = append(c.histOrder, id), append(c.histFrom, from), append(c.histTo, st), append(c.histAt, times[k])
		from = st
	}
	if len(path) == 3 { // đã gửi hãng
		key := []string{"vtp", "spx"}[rnd.IntN(2)]
		at := times[1].Add(time.Hour)
		c.shipID, c.shipOrder, c.shipAccount = append(c.shipID, uuid.New()), append(c.shipOrder, id), append(c.shipAccount, r.accounts[key])
		c.shipCode, c.shipKey, c.shipStatus = append(c.shipCode, s.shipmentCode(at)), append(c.shipKey, key), append(c.shipStatus, path[2])
		c.shipTracking, c.shipCOD, c.shipAt = append(c.shipTracking, key+"-"+id.String()), append(c.shipCOD, subtotal+ship), append(c.shipAt, at)
	}
}

// insertBulk chèn các cột của c, mỗi bảng một câu, rồi gắn vận đơn hiện hành.
func (s *Seed) insertBulk(ctx context.Context, tx pgx.Tx, c bulkCols, own, wh, cust uuid.UUID) error {
	o := c.order
	if _, err := tx.Exec(ctx, `INSERT INTO ord.orders (id, tenant_id, seller_id, code, customer_id, channel, idempotency_key, status,
			ship_address, subtotal, shipping_fee_charged, cod_amount, grand_total, care_by, created_by, channel_account_id,
			traffic_source_id, warehouse_id, created_at, updated_at, confirmed_at, cancelled_at, cancel_reason, draft_expires_at)
		SELECT u.id, $1, $2, u.code, $3, u.channel, gen_random_uuid(), u.status, '{}', u.subtotal, u.ship, u.grand, u.grand,
			u.care, u.care, u.page, u.source, $4, u.created, u.created, u.confirmed, u.cancelled, u.reason, u.draft_exp
		FROM unnest($5::uuid[], $6::text[], $7::text[], $8::text[], $9::bigint[], $10::bigint[], $11::bigint[], $12::uuid[],
			$13::uuid[], $14::uuid[], $15::timestamptz[], $16::timestamptz[], $17::timestamptz[], $18::text[], $19::timestamptz[])
			AS u(id, code, channel, status, subtotal, ship, grand, care, page, source, created, confirmed, cancelled, reason, draft_exp)`,
		s.tenant, own, cust, wh, o.id, o.code, o.channel, o.status, o.subtotal, o.ship, o.grand, o.care, o.page, o.source,
		o.created, o.confirmed, o.cancelled, o.reason, o.draftExp); err != nil {
		return fmt.Errorf("đơn: %w", err)
	}
	if err := analyze(ctx, tx, "ord.orders"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ord.order_items (tenant_id, seller_id, order_id, position, variant_id, sku_snapshot,
			name_snapshot, quantity, unit_price, net_amount)
		SELECT $1, $2, u.o, 1, u.v, u.sku, u.name, u.q, u.p, u.q::bigint * u.p
		FROM unnest($3::uuid[], $4::uuid[], $5::int[], $6::bigint[], $7::text[], $8::text[]) AS u(o, v, q, p, sku, name)`,
		s.tenant, own, c.itemOrder, c.itemVariant, c.itemQty, c.itemPrice, c.itemSKU, c.itemName); err != nil {
		return fmt.Errorf("dòng hàng: %w", err)
	}
	if err := analyze(ctx, tx, "ord.order_items"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ord.order_status_history (tenant_id, order_id, from_status, to_status, actor_type,
			occurred_at, recorded_at)
		SELECT $1, u.o, u.f, u.t, 'system', u.at, u.at FROM unnest($2::uuid[], $3::text[], $4::text[], $5::timestamptz[]) AS u(o, f, t, at)`,
		s.tenant, c.histOrder, c.histFrom, c.histTo, c.histAt); err != nil {
		return fmt.Errorf("lịch sử: %w", err)
	}
	if err := analyze(ctx, tx, "ord.order_status_history"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO fulfillment.shipments (id, tenant_id, seller_id, code, order_id, carrier_key,
			carrier_account_id, tracking_code, status, cod_amount, fee, create_state, created_at, updated_at, last_status_at)
		SELECT u.id, $1, $2, u.code, u.o, u.k, u.a, u.tr, u.st, u.cod, 22000, 'created', u.at, u.at, u.at
		FROM unnest($3::uuid[], $4::text[], $5::uuid[], $6::text[], $7::uuid[], $8::text[], $9::text[], $10::bigint[], $11::timestamptz[])
			AS u(id, code, o, k, a, tr, st, cod, at)`,
		s.tenant, own, c.shipID, c.shipCode, c.shipOrder, c.shipKey, c.shipAccount, c.shipTracking, c.shipStatus, c.shipCOD, c.shipAt); err != nil {
		return fmt.Errorf("vận đơn: %w", err)
	}
	_, err := tx.Exec(ctx, `UPDATE ord.orders o SET current_shipment_id = u.s
		FROM unnest($1::uuid[], $2::uuid[]) AS u(o, s) WHERE o.id = u.o AND o.seller_id = $3`, c.shipOrder, c.shipID, own)
	if err != nil {
		return fmt.Errorf("gắn vận đơn: %w", err)
	}
	return nil
}

// analyze cập nhật thống kê của bảng ngay trong transaction: kế hoạch kiểm khóa ngoại của câu chèn sau thấy đúng cỡ bảng
// (bảng mới không có thống kê thì kiểm khóa ngoại có thể quét cả bảng, chèn 100.000 dòng thành bậc hai).
func analyze(ctx context.Context, tx pgx.Tx, table string) error {
	if _, err := tx.Exec(ctx, "ANALYZE "+table); err != nil {
		return fmt.Errorf("ANALYZE %s: %w", table, err)
	}
	return nil
}
