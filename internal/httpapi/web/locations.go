package web

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/cache"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
)

// Địa giới đổi hiếm (danh mục dùng chung, cập nhật bằng migration): bộ đệm 1 ngày (kế hoạch M5 Task 191).
const (
	locationsTTL = 24 * time.Hour
	locationsKey = "all"
)

// Locations trả tỉnh/thành (theo mã) kèm xã/phường (theo tên) của danh mục customer.admin_units (A19, B11), qua khóa
// "all" của SpaceLocations, có ETag; If-None-Match khớp → 304. Không có mã địa giới của hãng vận chuyển.
//
//nolint:ireturn // kiểu trả của operation do ogen sinh (gen.Handler)
func (c *Catalog) Locations(ctx context.Context, tenant uuid.UUID, p gen.WebLocationsParams) (gen.WebLocationsRes, error) {
	raw, _, err := c.rc.GetOrLoad(ctx, tenant, cache.SpaceLocations, locationsKey, locationsTTL, func(ctx context.Context) ([]byte, error) {
		var provinces, wards []customer.AdminUnit
		err := tenancy.WithTenant(ctx, c.db, tenant, func(tx pgx.Tx) error {
			var err error
			if provinces, err = customer.ListProvinces(ctx, tx); err != nil {
				return err //nolint:wrapcheck // customer đã bọc
			}
			wards, err = customer.ListAllWards(ctx, tx)
			return err //nolint:wrapcheck // customer đã bọc
		}, db.TxAPI)
		if err != nil {
			return nil, fmt.Errorf("đọc địa giới cho website: %w", err)
		}
		g := locations(provinces, wards)
		return g.MarshalJSON()
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // ReadCache đã bọc; ErrBusy giữ nguyên để đổi 503
	}
	tag := etag(raw)
	if matches(p.IfNoneMatch, tag) {
		return &gen.WebLocationsNotModified{}, nil
	}
	var out gen.WebLocations
	if err := out.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("đọc địa giới trong bộ đệm: %w", err)
	}
	return &gen.WebLocationsHeaders{ETag: gen.NewOptString(tag), Response: out}, nil
}

// locations gắn mỗi xã vào tỉnh của nó (xã của tỉnh không có trong danh sách tỉnh thì bỏ; CHECK của bảng không cho).
func locations(provinces, wards []customer.AdminUnit) gen.WebLocations {
	out := gen.WebLocations{Provinces: make([]gen.WebProvince, 0, len(provinces))}
	at := make(map[string]int, len(provinces))
	for i, p := range provinces {
		at[p.Code] = i
		out.Provinces = append(out.Provinces, gen.WebProvince{Code: p.Code, Name: p.Name, Wards: []gen.WebWard{}})
	}
	for _, w := range wards {
		if i, ok := at[w.ParentCode]; ok {
			out.Provinces[i].Wards = append(out.Provinces[i].Wards, gen.WebWard{Code: w.Code, Name: w.Name})
		}
	}
	return out
}
