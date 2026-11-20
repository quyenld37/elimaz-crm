package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/httpapi/gen"
	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/identity"
	"elimaz.com.vn/crm/internal/modules/inventory"
)

// Vùng warehouse_pickup (kế hoạch M3, Task M3-09): địa chỉ lấy hàng của kho (của shop, không phải của khách).
// Quyền fulfillment.settings.manage (toàn shop, guard kiểm trước handler). Audit chỉ ghi tên trường đổi, không giá trị.

// SetWarehousePickup (PUT /api/v1/inventory/warehouses/{id}/pickup): ghi khi version còn khớp (sai → 409); SĐT chuẩn
// hóa về +84… (sai → 422 invalid_phone).
//
//nolint:ireturn // chữ ký do ogen sinh (gen.Handler)
func (h *handler) SetWarehousePickup(ctx context.Context, req *gen.SetWarehousePickupRequest, params gen.SetWarehousePickupParams) (gen.SetWarehousePickupRes, error) {
	phone, err := customer.NormalizePhone(string(req.Phone))
	if err != nil {
		return nil, fmt.Errorf("SĐT lấy hàng: %w", err)
	}
	addr := inventory.PickupAddress{Name: req.Name, Line: req.Line, ProvinceCode: req.ProvinceCode, WardCode: req.WardCode}
	out, err := h.refusableWrite(ctx, http.StatusOK, isInventoryRefusal, "ghi địa chỉ lấy hàng",
		func(tx pgx.Tx, u identity.AuthUser) (any, error) {
			before, err := inventory.GetWarehousePickup(ctx, tx, params.ID)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của inventory
			}
			version, err := inventory.SetWarehousePickup(ctx, tx, params.ID, req.Version, addr, phone)
			if err != nil {
				return nil, err //nolint:wrapcheck // sentinel của inventory
			}
			b, a := pickupChanges(before, inventory.WarehousePickup{Address: addr, Phone: phone})
			return &gen.WarehousePickup{
				WarehouseId: params.ID, Name: gen.NewOptString(addr.Name), Phone: gen.NewOptString(string(phone)),
				Line: gen.NewOptString(addr.Line), ProvinceCode: gen.NewOptString(addr.ProvinceCode),
				WardCode: gen.NewOptString(addr.WardCode), Version: version,
			}, h.audit(ctx, tx, u, "inventory.warehouse.pickup", "warehouse", params.ID, b, a)
		})
	if err != nil {
		return nil, err
	}
	return out.(*gen.WarehousePickup), nil //nolint:forcetypeassert // fn trên trả *gen.WarehousePickup
}

// pickupChanges trả phần trước/sau của audit: chỉ tên trường đổi (giá trị là dấu "old"/"new", không địa chỉ, SĐT).
func pickupChanges(before, after inventory.WarehousePickup) (b, a map[string]any) {
	b, a = map[string]any{}, map[string]any{}
	for _, f := range []struct {
		name    string
		changed bool
	}{
		{"name", before.Address.Name != after.Address.Name},
		{"line", before.Address.Line != after.Address.Line},
		{"provinceCode", before.Address.ProvinceCode != after.Address.ProvinceCode},
		{"wardCode", before.Address.WardCode != after.Address.WardCode},
		{"phone", before.Phone != after.Phone},
	} {
		if f.changed {
			b[f.name], a[f.name] = "old", "new"
		}
	}
	return b, a
}
