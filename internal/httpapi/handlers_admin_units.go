package httpapi

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
)

// ListAdminUnits (GET /api/v1/admin-units, customer.read): parent rỗng → mọi tỉnh; có parent → xã của tỉnh đó có tên
// chứa q (tối đa customer.MaxWardHits). Mã tỉnh không có trong danh mục cho danh sách rỗng.
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) ListAdminUnits(ctx context.Context, params gen.ListAdminUnitsParams) (gen.ListAdminUnitsRes, error) {
	var units []customer.AdminUnit
	err := h.catalogRead(ctx, func(tx pgx.Tx) error {
		var err error
		if parent := params.Parent.Value; parent != "" {
			units, err = customer.ListWards(ctx, tx, parent, params.Q.Value, customer.MaxWardHits)
		} else {
			units, err = customer.ListProvinces(ctx, tx)
		}
		return err //nolint:wrapcheck // lỗi của customer đã bọc
	})
	if err != nil {
		return nil, fmt.Errorf("đọc danh mục đơn vị hành chính: %w", err)
	}
	out := &gen.AdminUnitList{Items: make([]gen.AdminUnit, 0, len(units))}
	for _, u := range units {
		item := gen.AdminUnit{Code: u.Code, Name: u.Name, Level: gen.AdminUnitLevel(u.Level)}
		if u.ParentCode != "" {
			item.ParentCode = gen.NewOptString(u.ParentCode)
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
