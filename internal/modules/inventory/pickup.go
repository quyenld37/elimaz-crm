package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/inventory/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// PickupAddress là địa chỉ lấy hàng của kho theo mã địa giới của CRM (kế hoạch M3, Task M3-09): người liên hệ, dòng
// địa chỉ, mã tỉnh, mã phường. Là địa chỉ của shop, không phải của khách. JSON khớp cột inventory.warehouses.pickup_address.
type PickupAddress struct {
	Name         string `json:"name"`
	Line         string `json:"line"`
	ProvinceCode string `json:"province_code"`
	WardCode     string `json:"ward_code"`
}

// WarehousePickup là địa chỉ lấy hàng, SĐT của shop ở kho, cùng version của dòng kho.
type WarehousePickup struct {
	Address PickupAddress
	Phone   logging.Phone
	Version int64
}

// GetWarehousePickup đọc địa chỉ lấy hàng của kho id trong tx (chưa ghi lần nào: giá trị rỗng). Không có → ErrNotFound.
func GetWarehousePickup(ctx context.Context, tx pgx.Tx, id uuid.UUID) (WarehousePickup, error) {
	r, err := store.New(tx).GetWarehousePickup(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return WarehousePickup{}, ErrNotFound
	}
	if err != nil {
		return WarehousePickup{}, fmt.Errorf("đọc địa chỉ lấy hàng: %w", err)
	}
	p := WarehousePickup{Phone: logging.Phone(r.PickupPhone), Version: r.Version}
	if err := json.Unmarshal(r.PickupAddress, &p.Address); err != nil {
		return WarehousePickup{}, fmt.Errorf("đọc địa chỉ lấy hàng: %w", err)
	}
	return p, nil
}

// SetWarehousePickup ghi địa chỉ lấy hàng addr và SĐT phone (đã chuẩn hóa, ≤ 20 ký tự) của kho id khi version còn khớp,
// trong tx của bên gọi; trả version mới. Một câu UPDATE có điều kiện (không đọc ra Go rồi ghi lại). Lỗi: trường rỗng hay
// SĐT quá dài → ErrInvalidInput; kho không có (hay tenant khác) → ErrNotFound; version lệch → ErrVersionConflict.
// Audit do bên gọi ghi (chỉ tên trường).
func SetWarehousePickup(ctx context.Context, tx pgx.Tx, id uuid.UUID, version int64, addr PickupAddress, phone logging.Phone,
) (int64, error) {
	for _, f := range []string{addr.Name, addr.Line, addr.ProvinceCode, addr.WardCode} {
		if strings.TrimSpace(f) == "" {
			return 0, fmt.Errorf("địa chỉ lấy hàng thiếu trường: %w", ErrInvalidInput)
		}
	}
	if len(phone) > maxPickupPhone {
		return 0, fmt.Errorf("SĐT lấy hàng quá dài: %w", ErrInvalidInput)
	}
	raw, err := json.Marshal(addr)
	if err != nil {
		return 0, fmt.Errorf("mã hóa địa chỉ lấy hàng: %w", err)
	}
	q := store.New(tx)
	v, err := q.SetWarehousePickup(ctx, store.SetWarehousePickupParams{
		PickupAddress: raw, PickupPhone: string(phone), ID: id, Version: version,
	})
	if err == nil {
		return v, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("ghi địa chỉ lấy hàng: %w", err)
	}
	if _, err := q.GetWarehousePickup(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	} else if err != nil {
		return 0, fmt.Errorf("đọc kho: %w", err)
	}
	return 0, ErrVersionConflict
}

// maxPickupPhone khớp CHECK length(pickup_phone) <= 20 của migration 0301.
const maxPickupPhone = 20
