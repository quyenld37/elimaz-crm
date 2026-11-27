// Package reportseed dựng nhanh dữ liệu cho test báo cáo (kế hoạch M6, Task 227): chèn thẳng vào bảng gốc của M1–M4
// bằng role chủ bảng trong ngữ cảnh tenant (FORCE RLS áp cả cho chủ bảng), không đi qua public API của module. Chỉ
// dùng trong test, trên database thử riêng. Dữ liệu đúng các CHECK, khóa ngoại và trigger tổng tiền của bảng đơn.
package reportseed

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Seed dựng dữ liệu của một tenant. Các thứ phụ (người bán own, kho mặc định, khách, mẫu, tài khoản hãng) tạo khi
// cần lần đầu. Không an toàn khi gọi từ nhiều goroutine.
type Seed struct {
	t        testing.TB
	owner    *db.DB
	tenant   uuid.UUID
	own      uuid.UUID
	store    uuid.UUID
	customer uuid.UUID
	products map[string]uuid.UUID // "<người bán>/<tên mẫu>"
	accounts map[string]uuid.UUID // "<người bán>/<khóa hãng>"
	pages    []uuid.UUID
	n        int   // bộ đếm cho mã, tên duy nhất
	codeBase int64 // gốc ngẫu nhiên của mã đơn, mã phiếu trả thử
}

// New mở bộ dựng cho tenant trên pool chủ bảng owner (testkit.Staff.Owner).
func New(t testing.TB, owner *db.DB, tenant uuid.UUID) *Seed {
	t.Helper()
	var b [6]byte
	_, _ = rand.Read(b[:])
	base := int64(b[0])<<40 | int64(b[1])<<32 | int64(b[2])<<24 | int64(b[3])<<16 | int64(b[4])<<8 | int64(b[5])
	return &Seed{
		t: t, owner: owner, tenant: tenant, products: map[string]uuid.UUID{}, accounts: map[string]uuid.UUID{},
		codeBase: base % 1e14 * 10,
	}
}

// Tenant là tenant của bộ dựng.
func (s *Seed) Tenant() uuid.UUID { return s.tenant }

// Pages là các Page đã tạo bằng Page, theo thứ tự tạo.
func (s *Seed) Pages() []uuid.UUID { return append([]uuid.UUID(nil), s.pages...) }

// exec chạy fn bằng chủ bảng trong ngữ cảnh tenant; lỗi làm test đỏ.
func (s *Seed) exec(what string, fn func(ctx context.Context, tx pgx.Tx) error) {
	s.t.Helper()
	ctx := s.t.Context()
	if err := tenancy.WithTenant(ctx, s.owner, s.tenant, func(tx pgx.Tx) error { return fn(ctx, tx) }); err != nil {
		s.t.Fatalf("reportseed: %s: %v", what, err)
	}
}

// id chạy một câu INSERT … RETURNING id.
func (s *Seed) id(what, q string, args ...any) uuid.UUID {
	s.t.Helper()
	var id uuid.UUID
	s.exec(what, func(ctx context.Context, tx pgx.Tx) error { return tx.QueryRow(ctx, q, args...).Scan(&id) })
	return id
}

func (s *Seed) next() int { s.n++; return s.n }

// docCode sinh mã đơn (prefix DH) hay mã phiếu trả (TH) thử: prefix + "8" + 15 chữ số từ gốc ngẫu nhiên của Seed
// cộng bộ đếm. Đúng CHECK ^DH[0-9]{7,}$ / ^TH[0-9]{7,}$; không gọi sequence của M2 (chỉ queries/ord/code.sql sinh mã,
// TestCodeSequenceSingleSource) và không trùng mã thật (mã của sequence ngắn hơn nhiều).
func (s *Seed) docCode(prefix string) string {
	return prefix + "8" + fmt.Sprintf("%015d", s.codeBase+int64(s.next()))
}

// Own là người bán của chính shop (mã ELIMAZ, kind own), tạo ở lần gọi đầu.
func (s *Seed) Own() uuid.UUID {
	if s.own == uuid.Nil {
		s.own = s.Seller("ELIMAZ", "own")
	}
	return s.own
}

// Seller chèn một người bán (kind own hoặc partner) và trả mã.
func (s *Seed) Seller(code, kind string) uuid.UUID {
	s.t.Helper()
	return s.id("chèn người bán", `INSERT INTO catalog.sellers (tenant_id, code, name, kind) VALUES ($1, $2, $2, $3) RETURNING id`,
		s.tenant, code, kind)
}

// Page chèn một Page Facebook của người bán own và trả mã tài khoản kênh (phạm vi page của grant).
func (s *Seed) Page(name string) uuid.UUID {
	s.t.Helper()
	id := s.id("chèn Page", `INSERT INTO channel.channel_accounts (tenant_id, seller_id, channel, external_id, name)
		VALUES ($1, $2, 'facebook', $3, $4) RETURNING id`, s.tenant, s.Own(), strconv.Itoa(100000+s.next()), name)
	s.pages = append(s.pages, id)
	return id
}

// Source chèn một nguồn đơn (ord.traffic_sources; nguồn của M2 không gắn Page) và trả mã.
func (s *Seed) Source(name string) uuid.UUID {
	s.t.Helper()
	return s.id("chèn nguồn", `INSERT INTO ord.traffic_sources (tenant_id, name, position) VALUES ($1, $2, $3) RETURNING id`,
		s.tenant, name, s.next())
}

// Staff chèn một nhân viên (identity.users, tên hiển thị name) và trả mã.
func (s *Seed) Staff(name string) uuid.UUID {
	s.t.Helper()
	return s.id("chèn nhân viên", `INSERT INTO identity.users (tenant_id, email, display_name) VALUES ($1, $2, $3) RETURNING id`,
		s.tenant, "nv"+strconv.Itoa(s.next())+"@reportseed.test", name)
}

// Warehouse chèn một kho do người bán own vận hành và trả mã.
func (s *Seed) Warehouse(name string) uuid.UUID {
	s.t.Helper()
	return s.id("chèn kho", `INSERT INTO inventory.warehouses (tenant_id, operated_by_seller_id, code, name) VALUES ($1, $2, $3, $4) RETURNING id`,
		s.tenant, s.Own(), "K"+strconv.Itoa(s.next()), name)
}

// defaultWarehouse là kho của mọi đơn dựng bằng Seed (tạo ở lần gọi đầu).
func (s *Seed) defaultWarehouse() uuid.UUID {
	if s.store == uuid.Nil {
		s.store = s.Warehouse("Kho chính")
	}
	return s.store
}

// defaultCustomer là khách của mọi đơn dựng bằng Seed (tạo qua customer.Create: chỉ module customer ghi bảng khách;
// báo cáo không đọc tên khách).
func (s *Seed) defaultCustomer() uuid.UUID {
	if s.customer == uuid.Nil {
		s.exec("tạo khách", func(ctx context.Context, tx pgx.Tx) error {
			var err error
			s.customer, err = customer.Create(ctx, tx, "Khách thử")
			return err //nolint:wrapcheck // lỗi dựng dữ liệu thử, exec bọc
		})
	}
	return s.customer
}

// Variant chèn một biến thể mã sku của mẫu tên product (tạo mẫu khi chưa có; mã mẫu tự sinh) thuộc người bán seller;
// màu "Mặc định", size là sku, giá 100.000. Giá bán của dòng đơn đặt ở Item.
func (s *Seed) Variant(seller uuid.UUID, product, sku string) uuid.UUID {
	s.t.Helper()
	key := seller.String() + "/" + product
	pid, ok := s.products[key]
	if !ok {
		pid = s.id("chèn mẫu", `INSERT INTO catalog.products (tenant_id, seller_id, code, name, name_search, status)
			VALUES ($1, $2, $3, $4, lower($4), 'active') RETURNING id`, s.tenant, seller, "M"+strconv.Itoa(s.next()), product)
		s.products[key] = pid
	}
	return s.id("chèn biến thể", `INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color, size, price)
		VALUES ($1, $2, $3, $4, $5, 'Mặc định', $4, 100000) RETURNING id`, s.tenant, seller, pid, sku, "B"+strconv.Itoa(1000+s.next()))
}

// account là tài khoản hãng carrierKey của người bán seller (tạo ở lần gọi đầu; kho lấy hàng là kho mặc định).
func (s *Seed) account(seller uuid.UUID, carrierKey string) uuid.UUID {
	key := seller.String() + "/" + carrierKey
	if id, ok := s.accounts[key]; ok {
		return id
	}
	id := s.id("chèn tài khoản hãng", `INSERT INTO fulfillment.carrier_accounts (tenant_id, seller_id, carrier_key, name, external_account_ref, warehouse_id)
		VALUES ($1, $2, $3, $3, $4, $5) RETURNING id`, s.tenant, seller, carrierKey, "acc-"+strconv.Itoa(s.next()), s.defaultWarehouse())
	s.accounts[key] = id
	return id
}

// ledgerBuckets: cột số dư của từng ngăn (bảng hằng; ngăn lạ làm test đỏ).
var ledgerBuckets = map[string]string{ //nolint:gochecknoglobals // bảng hằng chỉ đọc
	"on_hand": "on_hand", "held": "held", "in_transit": "in_transit", "damaged": "damaged",
}

// Ledger ghi một dòng sổ tồn (mốc created_at = at) và cộng delta vào ngăn bucket của số dư (tạo dòng số dư khi chưa
// có), cùng một transaction như inventory.applyDelta: số dư luôn bằng tổng sổ.
func (s *Seed) Ledger(variant, warehouse uuid.UUID, bucket string, delta int, docType string, at time.Time) {
	s.t.Helper()
	col, ok := ledgerBuckets[bucket]
	if !ok {
		s.t.Fatalf("reportseed: ngăn tồn lạ %q", bucket)
	}
	s.exec("ghi sổ tồn", func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO inventory.stock_balances (tenant_id, variant_id, warehouse_id, seller_id)
			SELECT $1, $2, $3, seller_id FROM catalog.variants WHERE id = $2 ON CONFLICT DO NOTHING`, s.tenant, variant, warehouse); err != nil {
			return fmt.Errorf("số dư: %w", err)
		}
		if _, err := tx.Exec(ctx, "UPDATE inventory.stock_balances SET "+col+" = "+col+" + $3, updated_at = $4 WHERE variant_id = $1 AND warehouse_id = $2",
			variant, warehouse, delta, at); err != nil {
			return fmt.Errorf("cộng số dư: %w", err)
		}
		_, err := tx.Exec(ctx, `INSERT INTO inventory.stock_ledger (tenant_id, variant_id, warehouse_id, bucket, delta, doc_type, doc_id, step, actor_type, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'seed', 'system', $8)`, s.tenant, variant, warehouse, bucket, delta, docType, uuid.New(), at)
		return err //nolint:wrapcheck // lỗi dựng dữ liệu thử, exec bọc
	})
}

// shipmentCode sinh mã vận đơn đúng CHECK ^E[0-9]{6}[0-9A-Z]{6}$ (cùng mẫu fulfillment.NewShipmentCode: E + yyMMdd giờ
// VN + 6 ký tự base32 Crockford). Phần đuôi lấy từ gốc ngẫu nhiên của Seed cộng bộ đếm, không lấy ngẫu nhiên như
// NewShipmentCode: Bulk(100000) sinh hàng chục nghìn mã, đuôi ngẫu nhiên 30 bit sẽ có lúc trùng (unique (tenant, code)).
func (s *Seed) shipmentCode(at time.Time) string {
	const alphabet, width = "0123456789ABCDEFGHJKMNPQRSTVWXYZ", 6
	n := (s.codeBase + int64(s.next())) % (1 << (5 * width))
	tail := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		tail[i] = alphabet[n%32]
		n /= 32
	}
	return "E" + at.In(vn).Format("060102") + string(tail)
}

// vn là giờ Việt Nam (UTC+7).
var vn = time.FixedZone("ICT", 7*3600) //nolint:gochecknoglobals // giá trị chỉ đọc, như hằng
