package web

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries/webcatalog"
)

// Đợt flash sale cho website (Task 204, M5-21): phần tĩnh (đợt, món, giá) đệm flashTTL trong SpaceFlash (báo hiệu
// flash_sale, flash_item làm mới sớm hơn); số còn lấy từ cổng flash sale, thiếu thì đếm ở PostgreSQL.
const (
	flashTTL = 2 * time.Second
	flashKey = "current"
)

// WithFlashGate đặt cổng flash sale để đọc số còn của món (AdmissionGate.Remaining); nil → luôn đếm ở PostgreSQL.
// Trả c.
func (c *Catalog) WithFlashGate(g cache.AdmissionGate) *Catalog {
	c.gate = g
	return c
}

// FlashCurrent trả các đợt đã lên lịch hay đang mở còn hạn của tenant lúc c.clock(): trạng thái hiệu lực tính lại theo
// giờ hiện tại (phần đệm có thể cũ tới flashTTL), đợt đã quá endsAt bị bỏ; remaining của từng món từ cổng, món cổng
// không biết thì đếm ở PostgreSQL (một transaction cho mọi món thiếu); soldOut = remaining 0.
func (c *Catalog) FlashCurrent(ctx context.Context, tenant uuid.UUID) (*gen.WebFlashCurrent, error) {
	raw, _, err := c.rc.GetOrLoad(ctx, tenant, cache.SpaceFlash, flashKey, flashTTL, func(ctx context.Context) ([]byte, error) {
		return c.loadFlash(ctx, tenant)
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // như Sold30
	}
	var cached gen.WebFlashCurrent
	if err := cached.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc đợt flash sale trong bộ đệm: %w", err)
	}
	now := c.clock().UTC()
	out := &gen.WebFlashCurrent{ServerTime: now, Sales: make([]gen.WebFlashSaleState, 0, len(cached.Sales))}
	var ids []uuid.UUID
	for _, s := range cached.Sales {
		st := webcatalog.FlashStatusAt(s.StartsAt, s.EndsAt, now)
		if st == "" {
			continue
		}
		s.Status = gen.WebFlashSaleStateStatus(st)
		for _, it := range s.Items {
			ids = append(ids, it.ID)
		}
		out.Sales = append(out.Sales, s)
	}
	remaining, err := c.flashRemaining(ctx, tenant, ids)
	if err != nil {
		return nil, err
	}
	for i := range out.Sales {
		for j := range out.Sales[i].Items {
			it := &out.Sales[i].Items[j]
			n := remaining[it.ID]
			it.Remaining = int32(n) //nolint:gosec // G115: số suất ≤ 20000 (CHECK của bảng)
			it.SoldOut = n == 0
		}
	}
	return out, nil
}

// loadFlash đọc phần tĩnh của các đợt (remaining 0, điền lúc trả lời) và mã hóa JSON để đệm.
func (c *Catalog) loadFlash(ctx context.Context, tenant uuid.UUID) ([]byte, error) {
	now := c.clock().UTC()
	var out gen.WebFlashCurrent
	err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
		sales, err := webcatalog.FlashCurrent(ctx, tx, now)
		if err != nil {
			return err //nolint:wrapcheck // webcatalog đã bọc
		}
		out = gen.WebFlashCurrent{ServerTime: now, Sales: make([]gen.WebFlashSaleState, len(sales))} // gán lại mỗi lần
		for i, s := range sales {
			out.Sales[i] = flashSaleOut(s)
		}
		return nil
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("đọc đợt flash sale cho website: %w", err)
	}
	b, err := out.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("mã hóa đợt flash sale cho website: %w", err)
	}
	return b, nil
}

func flashSaleOut(s webcatalog.WebFlashSale) gen.WebFlashSaleState {
	out := gen.WebFlashSaleState{
		ID: s.ID, Name: s.Name, StartsAt: s.StartsAt, EndsAt: s.EndsAt, Status: gen.WebFlashSaleStateStatus(s.Status),
		Items: make([]gen.WebFlashItemState, len(s.Items)),
	}
	for i, it := range s.Items {
		out.Items[i] = gen.WebFlashItemState{
			ID: it.ID, VariantId: it.VariantID, ProductId: it.ProductID, SalePrice: gen.Money(it.SalePrice),
			RegularPrice: gen.Money(it.RegularPrice), Quantity: int32(it.Quantity), //nolint:gosec // G115: ≤ 20000 (CHECK)
			PerCustomerLimit: int32(it.PerCustomerLimit), HoldMinutes: int32(it.HoldMinutes), //nolint:gosec // G115: ≤ 30
		}
	}
	return out
}

// flashRemaining: số còn của ids từ cổng; món cổng không biết (hay cổng nil, Valkey lỗi) đếm ở PostgreSQL.
func (c *Catalog) flashRemaining(ctx context.Context, tenant uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(ids) == 0 {
		return out, nil
	}
	if c.gate != nil {
		out = c.gate.Remaining(ctx, tenant, ids)
	}
	var missing []uuid.UUID
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return out, nil
	}
	var counted map[uuid.UUID]int
	if err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
		var err error
		counted, err = webcatalog.FlashAvailable(ctx, tx, missing) // gán lại mỗi lần chạy
		return err                                                 //nolint:wrapcheck // webcatalog đã bọc
	}, db.TxAPI); err != nil {
		return nil, fmt.Errorf("đếm suất còn trống cho website: %w", err)
	}
	for id, n := range counted {
		out[id] = n
	}
	return out, nil
}
