// Package perfseed nạp dữ liệu đo của bài thử tải M2 (kế hoạch M2 Task 88, M2-31; lệnh tools/seedperf) vào một
// database đo: tenant "Thử tải" (của loadkit seed) N khách, SĐT giả +8490xxxxxxx không trùng, đơn đã kết thúc rải
// trong nhiều tháng; tenant "Thử tải B" cùng các SĐT đó (RLS thật); rồi VACUUM ANALYZE. Ghi thẳng bảng của nhiều
// schema (customer, ord) bằng COPY của người quản trị: là hạ tầng thử như internal/testkit, không dùng trong tiến trình
// ứng dụng. Từ chối database có khách của tenant khác (ErrNotPerfDB) hay đã nạp (ErrAlreadySeeded).
//
// Khi nào không dùng: mã sản phẩm; test cần vài khách, vài đơn (dùng testkit, reportseed); database thật.
package perfseed

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Phần database của seedperf: COPY bằng người quản trị (superuser bỏ qua RLS; COPY FROM không chạy trên bảng có RLS
// với role thường) vào database đo. Như internal/testkit/loadstats, SQL nằm thẳng ở đây (hạ tầng thử, ngoài
// luật *_dynsql.go của ADR-44 chốt 3); test trên PostgreSQL thật ở seed_test.go.

// Tên hai tenant đo. tenantName trùng tenant của loadkit seed (nhân viên thử, thẻ Access của bài tải); tenant thứ hai
// chỉ có khách để câu tìm chạy dưới RLS thật (cùng SĐT với tenant một).
const (
	tenantName  = "Thử tải"
	tenant2Name = "Thử tải B"
	// sampleIDs, sampleCodes, sampleNames: cỡ mẫu ghi cho k6.
	sampleIDs   = 2000
	sampleCodes = 500
	sampleNames = 500
)

var (
	// ErrAlreadySeeded báo hai tenant đo đã có khách (seedperf chỉ nạp một lần vào database mới).
	ErrAlreadySeeded = errors.New("database đo đã được nạp")
	// ErrNotPerfDB báo database có khách của tenant khác hai tenant đo — có thể là dữ liệu thật; không bao giờ nạp vào đó.
	ErrNotPerfDB = errors.New("database có khách ngoài tenant đo: không phải database đo")
	// errNoCatalog: tenant đo chưa có biến thể hay kho (chạy loadkit m2seed trước).
	errNoCatalog = errors.New("tenant đo chưa có biến thể hay kho")
)

// Options là cỡ và hình dạng của dữ liệu nạp.
type Options struct {
	Customers          int    // khách của tenant đo
	Orders             int    // đơn (dòng đơn = 1,6 × đơn)
	Tenant2            int    // khách của tenant thứ hai
	ExtraPhonePermille int    // phần nghìn số khách có SĐT thứ hai (300 → 1,3 SĐT mỗi khách)
	Months             int    // đơn rải trong ngần ấy tháng trước Now
	Seed               uint64 // hạt giống: cùng hạt giống, cùng dữ liệu
	Now                time.Time
}

// Sample là kết quả cho k6 và báo cáo (m2-sample.json): công thức SĐT, mẫu mã khách, mã, tên. Không ghi vào repo.
type Sample struct {
	TenantID    uuid.UUID `json:"tenantId"`
	Tenant2ID   uuid.UUID `json:"tenant2Id"`
	Customers   int       `json:"customers"`
	Phones      int       `json:"phones"` // SĐT thứ k (0 ≤ k < Phones) thuộc một khách của tenant đo
	PhonePrefix string    `json:"phonePrefix"`
	PhoneSpace  int       `json:"phoneSpace"`
	PhoneMul    int       `json:"phoneMul"`
	PhoneAdd    int       `json:"phoneAdd"`
	CustomerIDs []string  `json:"customerIds"`
	Codes       []string  `json:"codes"`
	Names       []string  `json:"names"`
	Orders      int       `json:"orders"`
	Items       int       `json:"items"`
	SeedSeconds float64   `json:"seedSeconds"`
}

// refs là phần có sẵn của tenant đo mà đơn tham chiếu.
type refs struct {
	seller, warehouse uuid.UUID
	variants          []variantRef
}

// Seed nạp dữ liệu đo vào database của adminURL (người quản trị): hai tenant (tạo nếu chưa có), khách, danh tính
// SĐT, đơn, dòng đơn; rồi VACUUM ANALYZE. Đơn, dòng đơn, danh tính nạp song song trên ba kết nối; khách nạp sau cùng vì
// chỉ số của khách tính từ đơn.
func Seed(ctx context.Context, adminURL string, o Options, logger *slog.Logger) (Sample, error) {
	started := time.Now()
	admin, err := db.Open(ctx, adminURL, db.PoolOptions{MaxConns: 2})
	if err != nil {
		return Sample{}, fmt.Errorf("mở database đo: %w", err)
	}
	defer admin.Pool.Close()
	t1, err := tenancy.SeedTenant(ctx, admin, tenantName)
	if err != nil {
		return Sample{}, fmt.Errorf("tenant đo: %w", err)
	}
	t2, err := tenancy.SeedTenant(ctx, admin, tenant2Name)
	if err != nil {
		return Sample{}, fmt.Errorf("tenant thứ hai: %w", err)
	}
	if err := guard(ctx, admin, t1, t2); err != nil {
		return Sample{}, err
	}
	rf, err := readRefs(ctx, admin, t1)
	if err != nil {
		return Sample{}, err
	}
	tl := timeline{to: o.Now, from: o.Now.AddDate(0, -o.Months, 0), lead: 30 * 24 * time.Hour, n: o.Customers}
	tl2 := timeline{to: o.Now, from: o.Now.AddDate(0, -o.Months, 0), n: max(o.Tenant2, 1)}
	stats := make([]custStats, o.Customers)
	l := loader{adminURL: adminURL, o: o, rf: rf, t1: t1, t2: t2, tl: tl, tl2: tl2}

	var wg sync.WaitGroup
	errs := make([]error, 4)
	wg.Go(func() { errs[0] = l.copyOrders(ctx, stats) })
	wg.Go(func() { errs[1] = l.copyItems(ctx) })
	wg.Go(func() { errs[2] = l.copyIdentities(ctx) })
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return Sample{}, err
	}
	logger.Info("seedperf: đã nạp đơn, dòng đơn, SĐT", slog.Duration("sau", time.Since(started)))
	if err := l.copyCustomers(ctx, stats); err != nil {
		return Sample{}, err
	}
	logger.Info("seedperf: đã nạp khách; VACUUM ANALYZE", slog.Duration("sau", time.Since(started)))
	if err := vacuumAnalyze(ctx, adminURL); err != nil {
		return Sample{}, err
	}
	s := l.sample()
	s.SeedSeconds = time.Since(started).Seconds()
	return s, nil
}

const (
	sqlForeignCustomers = `SELECT count(*) FROM customer.customers WHERE tenant_id <> ALL ($1::uuid[])`
	sqlOwnCustomers     = `SELECT count(*) FROM customer.customers WHERE tenant_id = ANY ($1::uuid[])`
	sqlVariants         = `SELECT id, seller_id, sku, price FROM catalog.variants WHERE tenant_id = $1 ORDER BY seller_id, sku`
	sqlWarehouse        = `SELECT id FROM inventory.warehouses WHERE tenant_id = $1 ORDER BY created_at, id LIMIT 1`
)

// guard từ chối database có khách ngoài hai tenant đo (ErrNotPerfDB) hay đã nạp (ErrAlreadySeeded).
func guard(ctx context.Context, admin *db.DB, t1, t2 uuid.UUID) error {
	var foreign, own int64
	ids := []uuid.UUID{t1, t2}
	if err := admin.Pool.QueryRow(ctx, sqlForeignCustomers, ids).Scan(&foreign); err != nil {
		return fmt.Errorf("đếm khách ngoài tenant đo: %w", err)
	}
	if foreign > 0 {
		return ErrNotPerfDB
	}
	if err := admin.Pool.QueryRow(ctx, sqlOwnCustomers, ids).Scan(&own); err != nil {
		return fmt.Errorf("đếm khách của tenant đo: %w", err)
	}
	if own > 0 {
		return ErrAlreadySeeded
	}
	return nil
}

// readRefs đọc biến thể (của người bán đầu tiên) và kho chính của tenant đo.
func readRefs(ctx context.Context, admin *db.DB, tenant uuid.UUID) (refs, error) {
	var rf refs
	rows, err := admin.Pool.Query(ctx, sqlVariants, tenant)
	if err != nil {
		return rf, fmt.Errorf("đọc biến thể của tenant đo: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v variantRef
		var seller uuid.UUID
		if err := rows.Scan(&v.ID, &seller, &v.SKU, &v.Price); err != nil {
			return rf, fmt.Errorf("đọc biến thể của tenant đo: %w", err)
		}
		if rf.seller == uuid.Nil {
			rf.seller = seller
		}
		if seller == rf.seller {
			rf.variants = append(rf.variants, v)
		}
	}
	if err := rows.Err(); err != nil {
		return rf, fmt.Errorf("đọc biến thể của tenant đo: %w", err)
	}
	err = admin.Pool.QueryRow(ctx, sqlWarehouse, tenant).Scan(&rf.warehouse)
	if errors.Is(err, pgx.ErrNoRows) || len(rf.variants) == 0 {
		return rf, errNoCatalog
	}
	if err != nil {
		return rf, fmt.Errorf("đọc kho của tenant đo: %w", err)
	}
	return rf, nil
}

// loader giữ tham số chung của các lượt COPY.
type loader struct {
	adminURL string
	o        Options
	rf       refs
	t1, t2   uuid.UUID
	tl, tl2  timeline
}

// Mã tỉnh, xã trong địa chỉ đơn nạp sẵn: đơn vị hành chính thử mà loadkit m2seed chèn (m2ProvinceCode, m2WardCode).
const (
	provinceCode = "T01"
	wardCode     = "T01001"
)

// Muối hạt giống của hồ sơ khách từng tenant (cùng c, khác tenant → khác mã, tên).
const (
	salt1 = 0x6b6831
	salt2 = 0x6b6832
)

// copyConn mở một kết nối người quản trị cho một lượt COPY: tắt trigger và khóa ngoại (session_replication_role =
// replica, QĐ-88-2 — trigger kiểm tổng của đơn, trigger root_id của khách, khóa ngoại; dữ liệu tự đúng do seedperf
// dựng, test kiểm lại), bỏ hạn câu lệnh, commit không chờ đĩa. Chỉ trong phiên này.
func copyConn(ctx context.Context, url string) (*pgx.Conn, error) {
	c, err := pgx.Connect(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("nối database đo: %w", err)
	}
	for _, q := range []string{
		"SET session_replication_role = replica", "SET statement_timeout = 0", "SET synchronous_commit = off",
	} {
		if _, err := c.Exec(ctx, q); err != nil {
			_ = c.Close(ctx)
			return nil, fmt.Errorf("đặt phiên nạp (%s): %w", q, err)
		}
	}
	return c, nil
}

// copyRows chạy một COPY vào bảng table (schema, tên) với các cột cols, dòng lấy từ next (nil, nil: hết).
func (l loader) copyRows(ctx context.Context, table []string, cols []string, next func() ([]any, error)) (int64, error) {
	c, err := copyConn(ctx, l.adminURL)
	if err != nil {
		return 0, err
	}
	defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
	n, err := c.CopyFrom(ctx, pgx.Identifier(table), cols, pgx.CopyFromFunc(next))
	if err != nil {
		return n, fmt.Errorf("COPY %s.%s: %w", table[0], table[1], err)
	}
	return n, nil
}

// Cột của từng lượt COPY (hàm thay biến cấp gói).
func orderCols() []string {
	return []string{
		"id", "tenant_id", "seller_id", "code", "customer_id", "channel", "idempotency_key", "status",
		"ship_address", "subtotal", "discount_total", "shipping_fee_charged", "cod_amount", "grand_total", "cancel_reason",
		"created_at", "updated_at", "payment_status", "warehouse_id", "ship_phone", "ship_phone_reversed",
		"address_complete", "confirmed_at", "cancelled_at",
	}
}

func itemCols() []string {
	return []string{
		"id", "tenant_id", "seller_id", "order_id", "position", "variant_id", "sku_snapshot",
		"name_snapshot", "quantity", "unit_price", "net_amount", "created_at", "updated_at",
	}
}

func identityCols() []string {
	return []string{
		"id", "tenant_id", "customer_id", "kind", "scope", "value", "value_reversed", "source",
		"created_at",
	}
}

func customerCols() []string {
	return []string{
		"id", "tenant_id", "root_id", "full_name", "created_at", "updated_at", "code", "name_search",
		"lifecycle", "order_count", "delivered_count", "returned_count", "total_spent", "last_order_at",
		"stats_updated_at",
	}
}

// copyOrders nạp đơn và cộng chỉ số của từng khách vào stats (chỉ goroutine này ghi stats).
func (l loader) copyOrders(ctx context.Context, stats []custStats) error {
	g := newOrderGen(l.o.Seed, l.o.Orders, l.tl, l.rf.variants)
	_, err := l.copyRows(ctx, []string{"ord", "orders"}, orderCols(), func() ([]any, error) {
		o, ok := g.next()
		if !ok {
			return nil, nil
		}
		stats[o.Customer].add(o)
		p := customerProfile(l.o.Seed, salt1, l.tl, o.Customer)
		phone := fakePhone(o.Customer)
		addr := map[string]string{"name": p.Name, "phone": phone, "line": "Số 1 đường Thử, phường Thử", "provinceCode": provinceCode, "wardCode": wardCode}
		var reason any
		var confirmed, cancelled any
		payment := "unpaid"
		switch o.Status {
		case "cancelled":
			reason, cancelled = "customer_changed_mind", o.CreatedAt.Add(2*time.Hour)
		case "delivered":
			confirmed, payment = o.CreatedAt.Add(time.Hour), "paid"
		default:
			confirmed = o.CreatedAt.Add(time.Hour)
		}
		return []any{
			o.ID, l.t1, l.rf.seller, o.Code, p.ID, "manual", o.IdemKey, o.Status, addr, o.Subtotal, int64(0),
			int64(shippingFee), o.GrandTotal, o.GrandTotal, reason, o.CreatedAt, o.CreatedAt.Add(72 * time.Hour), payment,
			l.rf.warehouse, phone, reversedDigits(phone), true, confirmed, cancelled,
		}, nil
	})
	return err
}

// copyItems nạp dòng đơn bằng một bộ sinh đơn thứ hai cùng hạt giống (cùng dãy đơn với copyOrders).
func (l loader) copyItems(ctx context.Context) error {
	g := newOrderGen(l.o.Seed, l.o.Orders, l.tl, l.rf.variants)
	var cur genOrder
	k := 0
	_, err := l.copyRows(ctx, []string{"ord", "order_items"}, itemCols(), func() ([]any, error) {
		for k >= len(cur.Items) {
			var ok bool
			if cur, ok = g.next(); !ok {
				return nil, nil
			}
			k = 0
		}
		it := cur.Items[k]
		k++
		return []any{
			it.ID, l.t1, l.rf.seller, cur.ID, it.Position, it.Variant.ID, it.Variant.SKU, it.Variant.SKU,
			int32(1), it.Variant.Price, it.Variant.Price, cur.CreatedAt, cur.CreatedAt,
		}, nil
	})
	return err
}

// phoneSlot là một danh tính SĐT: khách c của tenant giữ SĐT thứ k.
type phoneSlot struct{ c, k int }

// phoneSlots trả các danh tính của n khách: khách c giữ SĐT c; khách có c mod 1000 < permille giữ thêm SĐT n, n+1…
// theo thứ tự c tăng dần.
func phoneSlots(n, permille int, yield func(phoneSlot) bool) {
	for c := range n {
		if !yield(phoneSlot{c, c}) {
			return
		}
	}
	k := n
	for c := range n {
		if c%1000 < permille {
			if !yield(phoneSlot{c, k}) {
				return
			}
			k++
		}
	}
}

// identityRows sinh dòng danh tính SĐT của tenant (tenant thứ hai: mỗi khách một SĐT, đúng các số của tenant một).
func (l loader) identityRows(yield func([]any) bool) {
	emit := func(tenant uuid.UUID, salt uint64, tl timeline, s phoneSlot) bool {
		p := customerProfile(l.o.Seed, salt, tl, s.c)
		phone := fakePhone(s.k)
		id := uuidV7At(p.CreatedAt, randFor(l.o.Seed^salt, uint64(s.k))) //nolint:gosec // s.k ≥ 0
		return yield([]any{id, tenant, p.ID, "phone", "", phone, reversedDigits(phone), "order", p.CreatedAt})
	}
	ok := true
	phoneSlots(l.o.Customers, l.o.ExtraPhonePermille, func(s phoneSlot) bool {
		ok = emit(l.t1, salt1, l.tl, s)
		return ok
	})
	for c := 0; ok && c < l.o.Tenant2; c++ {
		ok = emit(l.t2, salt2, l.tl2, phoneSlot{c, c})
	}
}

func (l loader) copyIdentities(ctx context.Context) error {
	next, stop := pullRows(l.identityRows)
	defer stop()
	_, err := l.copyRows(ctx, []string{"customer", "customer_identities"}, identityCols(), next)
	return err
}

// copyCustomers nạp khách của hai tenant; chỉ số của tenant một lấy từ stats (đã đủ sau copyOrders).
func (l loader) copyCustomers(ctx context.Context, stats []custStats) error {
	rows := func(yield func([]any) bool) {
		row := func(tenant uuid.UUID, salt uint64, tl timeline, c int, s custStats) []any {
			p := customerProfile(l.o.Seed, salt, tl, c)
			lifecycle, last := "lead", any(nil)
			if s.orders > 0 {
				lifecycle, last = "customer", s.last
			}
			return []any{
				p.ID, tenant, p.ID, p.Name, p.CreatedAt, p.CreatedAt, customerCode(c), foldName(p.Name),
				lifecycle, s.orders, s.delivered, s.returned, s.spent, last, l.o.Now,
			}
		}
		for c := range l.o.Customers {
			if !yield(row(l.t1, salt1, l.tl, c, stats[c])) {
				return
			}
		}
		for c := range l.o.Tenant2 {
			if !yield(row(l.t2, salt2, l.tl2, c, custStats{})) {
				return
			}
		}
	}
	next, stop := pullRows(rows)
	defer stop()
	_, err := l.copyRows(ctx, []string{"customer", "customers"}, customerCols(), next)
	return err
}

// sample dựng mẫu cho k6: mã, mã khách, tên rải đều trên dải khách của tenant đo.
func (l loader) sample() Sample {
	s := Sample{
		TenantID: l.t1, Tenant2ID: l.t2, Customers: l.o.Customers, PhonePrefix: phonePrefix, PhoneSpace: PhoneSpace,
		PhoneMul: phoneMul, PhoneAdd: phoneAdd, Orders: l.o.Orders,
	}
	phoneSlots(l.o.Customers, l.o.ExtraPhonePermille, func(phoneSlot) bool { s.Phones++; return true })
	// Đơn một dòng ở j mod 5 < 2, hai dòng ở còn lại: 8 dòng mỗi 5 đơn, phần dư tính đúng.
	s.Items = l.o.Orders/5*8 + min(l.o.Orders%5, 2) + 2*max(l.o.Orders%5-2, 0)
	for i := range min(sampleIDs, l.o.Customers) {
		c := i * l.o.Customers / min(sampleIDs, l.o.Customers)
		s.CustomerIDs = append(s.CustomerIDs, customerProfile(l.o.Seed, salt1, l.tl, c).ID.String())
	}
	for i := range min(sampleCodes, l.o.Customers) {
		s.Codes = append(s.Codes, customerCode(i*l.o.Customers/min(sampleCodes, l.o.Customers)))
	}
	for i := range min(sampleNames, l.o.Customers) {
		c := (i*l.o.Customers/min(sampleNames, l.o.Customers) + 7) % l.o.Customers
		s.Names = append(s.Names, customerProfile(l.o.Seed, salt1, l.tl, c).Name)
	}
	return s
}

// vacuumAnalyze dọn và lấy thống kê các bảng đã nạp (như autovacuum sẽ làm với dữ liệu thật: bản đồ hiển thị cho
// index-only scan, thống kê cho planner).
func vacuumAnalyze(ctx context.Context, url string) error {
	c, err := copyConn(ctx, url)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close(context.WithoutCancel(ctx)) }()
	const q = `VACUUM (ANALYZE) customer.customers, customer.customer_identities, ord.orders, ord.order_items, platform.tenants`
	if _, err := c.Exec(ctx, q); err != nil {
		return fmt.Errorf("VACUUM ANALYZE: %w", err)
	}
	return nil
}

// pullRows đổi bộ sinh dòng kiểu đẩy (yield) thành hàm kéo cho pgx.CopyFromFunc: (nil, nil) khi hết. stop dừng bộ sinh
// (gọi bằng defer, cả khi COPY lỗi giữa chừng).
func pullRows(seq iter.Seq[[]any]) (next func() ([]any, error), stop func()) {
	pull, stop := iter.Pull(seq)
	return func() ([]any, error) {
		row, ok := pull()
		if !ok {
			return nil, nil
		}
		return row, nil
	}, stop
}
