package customer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/logging"
)

// maxLineRunes là độ dài tối đa của số nhà, đường (khớp maxLength của AddressInput.line).
const maxLineRunes = 500

// AddressInput là địa chỉ giao nhập vào (tên trường như ShipAddress của M1: Name là người nhận, Line là số nhà,
// đường), thêm mã tỉnh và mã xã của danh mục hai cấp (Task M2-04).
type AddressInput struct {
	Name         string
	Phone        string
	Line         string
	ProvinceCode string
	WardCode     string
}

// AddressPatch là phần sửa một địa chỉ: Address nil giữ nguyên địa chỉ; IsDefault nil giữ nguyên cờ mặc định.
type AddressPatch struct {
	Address   *AddressInput
	IsDefault *bool
}

// Address là một địa chỉ (chưa xóa) của khách. ProvinceName, WardName lấy từ danh mục (rỗng khi mã không còn trong
// danh mục).
type Address struct {
	ID, CustomerID             uuid.UUID
	Name                       Name
	Phone                      logging.Phone
	Line                       logging.Address
	ProvinceCode, ProvinceName string
	WardCode, WardName         string
	IsDefault                  bool
	Version                    int64
}

// AddAddress thêm địa chỉ cho hồ sơ customerID (khóa dòng khách trước, ADR-46 nhóm 5). Tỉnh, xã kiểm bằng
// ValidateUnits (ErrUnknownAdminUnit, ErrWardNotInProvince); SĐT chuẩn hóa (ErrInvalidPhone). isDefault → các địa chỉ
// khác của hồ sơ hết mặc định; hồ sơ chưa có địa chỉ mặc định thì địa chỉ đầu tiên thành mặc định. Ghi audit
// customer.address.add (chỉ tên trường), báo hiệu customer.changed. Trả mã địa chỉ mới.
func (w Writer) AddAddress(ctx context.Context, tx pgx.Tx, customerID uuid.UUID, in AddressInput, isDefault bool, a Actor,
	now time.Time,
) (uuid.UUID, error) {
	if err := w.check(); err != nil {
		return uuid.Nil, err
	}
	v, err := validAddress(ctx, tx, in)
	if err != nil {
		return uuid.Nil, err
	}
	cust, err := lockOwner(ctx, tx, customerID)
	if err != nil {
		return uuid.Nil, err
	}
	q := store.New(tx)
	if !isDefault {
		hasDefault, err := hasDefaultAddress(ctx, tx, customerID)
		if err != nil {
			return uuid.Nil, err
		}
		isDefault = !hasDefault
	}
	if isDefault {
		if err := q.ClearDefaultAddress(ctx, store.ClearDefaultAddressParams{Now: now, CustomerID: customerID, KeepID: uuid.Nil}); err != nil {
			return uuid.Nil, fmt.Errorf("bỏ mặc định địa chỉ cũ: %w", err)
		}
	}
	row, err := q.InsertAddress(ctx, insertAddressParams(customerID, v, isDefault, now))
	if err != nil {
		return uuid.Nil, fmt.Errorf("ghi địa chỉ: %w", err)
	}
	if err := w.audit(ctx, tx, cust.TenantID, a, "customer.address.add", customerID, nil,
		addressAudit(row.ID, []string{"recipient_name", "phone", "street", "province_code", "ward_code"}, isDefault, row.Version)); err != nil {
		return uuid.Nil, err
	}
	if err := w.signal(ctx, tx, cust.TenantID, cust.RootID); err != nil {
		return uuid.Nil, err
	}
	return row.ID, nil
}

// UpdateAddress sửa địa chỉ addressID của nhóm customerID khi version còn khớp (ErrVersionConflict); địa chỉ không
// có, đã xóa hay thuộc nhóm khác → ErrNotFound. Khóa dòng hồ sơ giữ địa chỉ rồi mới khóa địa chỉ (ADR-46).
// IsDefault = true → địa chỉ khác của hồ sơ hết mặc định.
func (w Writer) UpdateAddress(ctx context.Context, tx pgx.Tx, customerID, addressID uuid.UUID, version int64, p AddressPatch,
	a Actor, now time.Time,
) error {
	if err := w.check(); err != nil {
		return err
	}
	var in *AddressInput
	if p.Address != nil {
		v, err := validAddress(ctx, tx, *p.Address)
		if err != nil {
			return err
		}
		in = &v
	}
	holder, err := lockAddressHolder(ctx, tx, customerID, addressID)
	if err != nil {
		return err
	}
	q := store.New(tx)
	cur, err := q.LockAddress(ctx, addressID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("khóa địa chỉ: %w", err)
	}
	if cur.Version != version {
		return ErrVersionConflict
	}
	next := store.UpdateAddressParams{
		RecipientName: cur.RecipientName, Phone: cur.Phone, ProvinceCode: cur.ProvinceCode, WardCode: cur.WardCode,
		Street: cur.Street, IsDefault: cur.IsDefault, Now: now, ID: addressID, Version: version,
	}
	var fields []string
	if in != nil {
		fields = applyAddress(&next, *in)
	}
	if p.IsDefault != nil && *p.IsDefault != cur.IsDefault {
		next.IsDefault = *p.IsDefault
		fields = append(fields, "is_default")
	}
	if next.IsDefault && !cur.IsDefault {
		if err := q.ClearDefaultAddress(ctx, store.ClearDefaultAddressParams{Now: now, CustomerID: holder.id, KeepID: addressID}); err != nil {
			return fmt.Errorf("bỏ mặc định địa chỉ cũ: %w", err)
		}
	}
	newVersion, err := q.UpdateAddress(ctx, next)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrVersionConflict
	}
	if err != nil {
		return fmt.Errorf("sửa địa chỉ: %w", err)
	}
	if err := w.audit(ctx, tx, holder.row.TenantID, a, "customer.address.update", holder.id,
		map[string]any{"address_id": addressID.String(), "version": version},
		addressAudit(addressID, fields, next.IsDefault, newVersion)); err != nil {
		return err
	}
	return w.signal(ctx, tx, holder.row.TenantID, holder.row.RootID)
}

// DeleteAddress xóa mềm địa chỉ addressID của nhóm customerID (đặt deleted_at, bỏ mặc định); không có, đã xóa hay
// thuộc nhóm khác → ErrNotFound.
func (w Writer) DeleteAddress(ctx context.Context, tx pgx.Tx, customerID, addressID uuid.UUID, a Actor, now time.Time) error {
	if err := w.check(); err != nil {
		return err
	}
	holder, err := lockAddressHolder(ctx, tx, customerID, addressID)
	if err != nil {
		return err
	}
	n, err := store.New(tx).SoftDeleteAddress(ctx, store.SoftDeleteAddressParams{Now: now, ID: addressID})
	if err != nil {
		return fmt.Errorf("xóa địa chỉ: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	if err := w.audit(ctx, tx, holder.row.TenantID, a, "customer.address.delete", holder.id,
		map[string]any{"address_id": addressID.String()}, nil); err != nil {
		return err
	}
	return w.signal(ctx, tx, holder.row.TenantID, holder.row.RootID)
}

// applyAddress chép in vào next và trả tên các trường đã đổi.
func applyAddress(next *store.UpdateAddressParams, in AddressInput) []string {
	var fields []string
	for _, f := range []struct {
		name string
		dst  *string
		val  string
	}{
		{"recipient_name", &next.RecipientName, in.Name},
		{"phone", &next.Phone, in.Phone},
		{"street", &next.Street, in.Line},
		{"province_code", &next.ProvinceCode, in.ProvinceCode},
		{"ward_code", &next.WardCode, in.WardCode},
	} {
		if *f.dst != f.val {
			*f.dst = f.val
			fields = append(fields, f.name)
		}
	}
	return fields
}

type addressHolder struct {
	id  uuid.UUID
	row store.LockCustomerRow
}

// lockAddressHolder tìm hồ sơ giữ địa chỉ addressID, khóa dòng hồ sơ đó và đòi nó cùng gốc với customerID.
func lockAddressHolder(ctx context.Context, tx pgx.Tx, customerID, addressID uuid.UUID) (addressHolder, error) {
	root, err := RootOf(ctx, tx, customerID)
	if err != nil {
		return addressHolder{}, err
	}
	holderID, err := store.New(tx).GetAddressOwner(ctx, addressID)
	if errors.Is(err, pgx.ErrNoRows) {
		return addressHolder{}, ErrNotFound
	}
	if err != nil {
		return addressHolder{}, fmt.Errorf("đọc chủ địa chỉ: %w", err)
	}
	row, err := lockOwner(ctx, tx, holderID)
	if err != nil {
		return addressHolder{}, err
	}
	if row.RootID != root {
		return addressHolder{}, ErrNotFound
	}
	return addressHolder{id: holderID, row: row}, nil
}

func hasDefaultAddress(ctx context.Context, tx pgx.Tx, customerID uuid.UUID) (bool, error) {
	rows, err := store.New(tx).ListGroupAddresses(ctx, []uuid.UUID{customerID})
	if err != nil {
		return false, fmt.Errorf("đọc địa chỉ của khách: %w", err)
	}
	for _, r := range rows {
		if r.IsDefault {
			return true, nil
		}
	}
	return false, nil
}

// validAddress kiểm và chuẩn hóa một địa chỉ: tên 1–120, số nhà 1–500 ký tự, SĐT về +84…, tỉnh và xã có trong danh
// mục, xã thuộc tỉnh.
func validAddress(ctx context.Context, tx pgx.Tx, in AddressInput) (AddressInput, error) {
	name, err := validName(in.Name)
	if err != nil {
		return AddressInput{}, err
	}
	line, err := validText(in.Line, maxLineRunes)
	if err != nil {
		return AddressInput{}, err
	}
	phone, err := NormalizePhone(in.Phone)
	if err != nil {
		return AddressInput{}, err
	}
	if _, err := ValidateUnits(ctx, tx, in.ProvinceCode, in.WardCode); err != nil {
		return AddressInput{}, err
	}
	return AddressInput{Name: name, Phone: string(phone), Line: line, ProvinceCode: in.ProvinceCode, WardCode: in.WardCode}, nil
}

func insertAddressParams(customerID uuid.UUID, in AddressInput, isDefault bool, now time.Time) store.InsertAddressParams {
	return store.InsertAddressParams{
		CustomerID: customerID, RecipientName: in.Name, Phone: in.Phone, ProvinceCode: in.ProvinceCode,
		WardCode: in.WardCode, Street: in.Line, IsDefault: isDefault, Now: now,
	}
}

// addressAudit: mã địa chỉ, tên trường đã đổi, cờ mặc định, version; không giá trị địa chỉ.
func addressAudit(id uuid.UUID, fields []string, isDefault bool, version int64) map[string]any {
	m := fieldsAudit(fields)
	m["address_id"], m["is_default"], m["version"] = id.String(), isDefault, version
	return m
}
