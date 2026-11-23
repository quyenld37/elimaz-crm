package web

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/queries/webcatalog"
)

// Hạn sống trong bộ đệm (kế hoạch M5 Task 191, QĐ14): còn/hết 5 s (lưới an toàn khi báo hiệu stock.changed mất;
// webcache.Invalidator xóa sớm hơn), đã bán 30 ngày 600 s.
const (
	availabilityTTL = 5 * time.Second
	sold30TTL       = 600 * time.Second
	sold30Key       = "all"
)

// Availability trả còn/hết của các biến thể (bỏ trùng), theo mã tăng dần; mỗi biến thể một khóa SpaceAvailability
// (mã biến thể, cùng dạng với báo hiệu của webcache), đọc bằng eachCached.
//
//nolint:ireturn // kiểu trả của operation do ogen sinh (gen.Handler)
func (c *Catalog) Availability(ctx context.Context, tenant uuid.UUID, p gen.WebAvailabilityParams) (gen.WebAvailabilityRes, error) {
	raws, err := c.eachCached(ctx, tenant, cache.SpaceAvailability, sortedUnique(p.VariantIds), availabilityTTL, c.loadAvailability)
	if err != nil {
		return nil, err
	}
	out := gen.WebAvailability{Items: make([]gen.WebAvailabilityItem, 0, len(raws)), TtlS: int32(availabilityTTL / time.Second)}
	for _, raw := range raws {
		var it gen.WebAvailabilityItem
		if err := it.UnmarshalJSON(raw); err != nil {
			return nil, fmt.Errorf("đọc còn/hết trong bộ đệm: %w", err)
		}
		out.Items = append(out.Items, it)
	}
	return &out, nil
}

// loadAvailability đọc ids trong một transaction và trả JSON của từng biến thể có (mã lạ không có trong map).
func (c *Catalog) loadAvailability(ctx context.Context, tenant uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]byte, error) {
	m := make(map[uuid.UUID][]byte, len(ids))
	err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
		clear(m) // RetryTx có thể chạy lại thân hàm
		as, err := webcatalog.Availability(ctx, tx, ids)
		if err != nil {
			return err //nolint:wrapcheck // webcatalog đã bọc
		}
		for _, a := range as {
			g := gen.WebAvailabilityItem{VariantId: a.VariantID, Available: a.Available, Low: a.Low}
			b, err := g.MarshalJSON()
			if err != nil {
				return fmt.Errorf("mã hóa còn/hết: %w", err)
			}
			m[a.VariantID] = b
		}
		return nil
	}, db.TxAPI)
	if err != nil {
		return nil, fmt.Errorf("đọc còn/hết cho website: %w", err)
	}
	return m, nil
}

// Sold30 trả số đã bán 30 ngày theo biến thể qua khóa "all" của SpaceSold30 (600 s).
//
//nolint:ireturn // kiểu trả của operation do ogen sinh (gen.Handler)
func (c *Catalog) Sold30(ctx context.Context, tenant uuid.UUID) (gen.WebSold30dRes, error) {
	raw, _, err := c.rc.GetOrLoad(ctx, tenant, cache.SpaceSold30, sold30Key, sold30TTL, func(ctx context.Context) ([]byte, error) {
		now := time.Now().UTC()
		var out gen.WebSold30
		err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
			ss, err := webcatalog.Sold30(ctx, tx, now)
			if err != nil {
				return err //nolint:wrapcheck // webcatalog đã bọc
			}
			out = gen.WebSold30{Items: make([]gen.WebSoldItem, 0, len(ss)), AsOf: now}
			for _, s := range ss {
				out.Items = append(out.Items, gen.WebSoldItem{ProductId: s.ProductID, VariantId: s.VariantID, Sold: int64(s.Sold)})
			}
			return nil
		}, db.TxAPI)
		if err != nil {
			return nil, fmt.Errorf("đọc số đã bán 30 ngày cho website: %w", err)
		}
		return out.MarshalJSON()
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // như Availability
	}
	var out gen.WebSold30
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc số đã bán trong bộ đệm: %w", err)
	}
	return &out, nil
}

// sortedUnique trả bản sao ids đã sắp tăng dần, bỏ trùng.
func sortedUnique(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return slices.Compact(out)
}
