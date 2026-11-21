package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/modules/order/internal/store"
)

// ApplyAddressChange ghi địa chỉ giao addr cho đơn orderID của người bán sellerID khi hãng đã nhận đổi địa chỉ của vận
// đơn shipmentID (DH15; gọi ở Task M3-17), trong tx của bên gọi. Đây là đường duy nhất đổi địa chỉ khi đơn có vận đơn
// hiện hành: bỏ qua khóa Editable của Edit (Edit vẫn trả ErrLockedByShipment). Ghi ship_address, ship_phone (chuẩn
// hóa), ship_phone_reversed, address_complete như Edit; version + 1, order.updated (seq = version mới), báo hiệu.
// Địa chỉ như hiện tại → changed=false, không ghi. Audit do bên gọi ghi với tên trường EditFieldShipAddress (order
// không nhập identity).
//
// Lỗi (không đổi gì): addr sai dạng → ErrInvalidInput; thiếu tỉnh hay xã → ErrAddressIncomplete; mã tỉnh, xã không có
// → lỗi của customer.ValidateUnits; không có đơn → ErrNotFound; shipmentID không phải vận đơn hiện hành, hay đơn không ở
// confirmed/cancel_requested → ErrInvalidTransition.
func (w Writer) ApplyAddressChange(ctx context.Context, tx pgx.Tx, sellerID, orderID, shipmentID uuid.UUID,
	addr ShipAddress, _ Actor, now time.Time,
) (changed bool, err error) {
	clean, err := checkShape(addr)
	if err != nil {
		return false, err
	}
	q, o, current, err := w.lockCurrentShipment(ctx, tx, sellerID, orderID, shipmentID)
	if err != nil {
		return false, err
	}
	if !current || (o.Status != StatusConfirmed && o.Status != StatusCancelRequested) {
		return false, fmt.Errorf("%w: đổi địa chỉ đơn %s qua vận đơn không hiện hành hay đơn đã xong", ErrInvalidTransition,
			o.Status)
	}
	cur, err := Get(ctx, tx, sellerID, orderID)
	if err != nil {
		return false, err
	}
	if cur.ShipAddress == clean {
		return false, nil
	}
	if err := validateUnits(ctx, tx, clean); err != nil {
		return false, err
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return false, fmt.Errorf("ghi địa chỉ giao: %w", err)
	}
	phone, reversed := normalizedShipPhone(clean.Phone)
	version, err := q.SetShipAddress(ctx, store.SetShipAddressParams{
		ShipAddress: raw, ShipPhone: string(phone), ShipPhoneReversed: reversed, AddressComplete: true, Now: now,
		SellerID: sellerID, ID: orderID, Version: o.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrVersionConflict // đã khóa: không xảy ra
	}
	if err != nil {
		return false, fmt.Errorf("ghi địa chỉ giao: %w", err)
	}
	if err := w.emitUpdated(ctx, tx, o, version); err != nil {
		return false, err
	}
	return true, nil
}

// CheckShipAddress kiểm và chuẩn hóa địa chỉ giao addr theo đúng luật của ApplyAddressChange (trong tx của bên gọi, chỉ
// đọc danh mục địa giới): bên gọi kiểm trước khi gửi yêu cầu đổi địa chỉ ra hãng (Task M3-17). Lỗi: addr sai dạng →
// ErrInvalidInput; thiếu tỉnh hay xã → ErrAddressIncomplete; mã tỉnh, xã không có → lỗi của customer.ValidateUnits.
func CheckShipAddress(ctx context.Context, tx pgx.Tx, addr ShipAddress) (ShipAddress, error) {
	clean, err := checkShape(addr)
	if err != nil {
		return ShipAddress{}, err
	}
	if err := validateUnits(ctx, tx, clean); err != nil {
		return ShipAddress{}, err
	}
	return clean, nil
}

// checkShape: phần không cần database của kiểm địa chỉ giao (dạng, đủ tỉnh và xã).
func checkShape(addr ShipAddress) (ShipAddress, error) {
	clean, err := cleanAddress(addr)
	if err != nil {
		return ShipAddress{}, err
	}
	if clean.ProvinceCode == "" || clean.WardCode == "" {
		return ShipAddress{}, ErrAddressIncomplete
	}
	return *clean, nil
}

// validateUnits: mã tỉnh, xã của addr có trong danh mục địa giới.
func validateUnits(ctx context.Context, tx pgx.Tx, addr ShipAddress) error {
	if _, err := customer.ValidateUnits(ctx, tx, addr.ProvinceCode, addr.WardCode); err != nil {
		return fmt.Errorf("kiểm địa chỉ giao: %w", err)
	}
	return nil
}
