package invtest

import (
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/catalog/catalogtest"
	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Shop là dữ liệu thử của một tenant cho test bán hàng (đơn, vận chuyển): người bán ELIMAZ (Seller), người bán P
// partner (Partner), kho MAIN, và mỗi người bán một mẫu "Áo thử" (tạo khi cần).
type Shop struct {
	Tenant, Seller, Partner uuid.UUID
	Warehouse               uuid.UUID
	mu                      sync.Mutex
	products                map[uuid.UUID]uuid.UUID
	n                       int
}

// NewShop dựng Shop cho tenant tenantID trong tx (chủ bảng, tenancy.WithTenant của tenantID; ví dụ Staff.Exec).
func NewShop(t testing.TB, tx pgx.Tx, tenantID uuid.UUID) *Shop {
	t.Helper()
	s := &Shop{Tenant: tenantID, products: map[uuid.UUID]uuid.UUID{}}
	var err error
	if s.Seller, err = catalog.EnsureDefaultSeller(t.Context(), tx, tenantID); err != nil {
		t.Fatalf("invtest: người bán mặc định: %v", err)
	}
	if s.Warehouse, err = inventory.EnsureDefaultWarehouse(t.Context(), tx, tenantID, s.Seller); err != nil {
		t.Fatalf("invtest: kho mặc định: %v", err)
	}
	s.Partner = catalogtest.Seller(t, tx, tenantID, "P", "partner")
	return s
}

// Variant tạo biến thể mới giá price của người bán seller (tên bán "Áo thử - Đỏ / S<n>", n tăng từ 1 trong Shop) và
// nạp onHand ở kho MAIN (0: không nạp), trong tx của chủ bảng. Lỗi làm test dừng (chỉ gọi từ goroutine của test).
func (s *Shop) Variant(t testing.TB, tx pgx.Tx, seller uuid.UUID, price int64, onHand int32) uuid.UUID {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	product, ok := s.products[seller]
	if !ok {
		product = catalogtest.Product(t, tx, s.Tenant, seller, fmt.Sprintf("AO%02d", len(s.products)+1))
		s.products[seller] = product
	}
	id := catalogtest.Variant(t, tx, s.Tenant, seller, product, s.n, price)
	if onHand > 0 {
		SeedStock(t, tx, id, s.Warehouse, onHand)
	}
	return id
}

// Product trả mẫu "Áo thử" của người bán seller (tạo ở lần Variant đầu tiên; chưa có → uuid.Nil).
func (s *Shop) Product(seller uuid.UUID) uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.products[seller]
}
