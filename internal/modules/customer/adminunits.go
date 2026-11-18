package customer

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
)

// AdminLevel là cấp của đơn vị hành chính (cột admin_units.level): hai cấp hiện hành từ 01/07/2025 (kế hoạch M2, Q15).
type AdminLevel string

// Giá trị của AdminLevel; khớp CHECK của customer.admin_units.level (migration 0202).
const (
	AdminProvince AdminLevel = "province"
	AdminWard     AdminLevel = "ward"
)

// MaxWardHits là số xã tối đa ListWards trả một lần (ô chọn xã trên màn hình lọc tiếp bằng q).
const MaxWardHits = 50

// AdminUnit là một tỉnh/thành hay xã/phường của danh mục dùng chung (không theo shop). ParentCode rỗng với tỉnh.
type AdminUnit struct {
	Code       string
	Level      AdminLevel
	ParentCode string
	Name       string
}

// AdminUnitPair là tỉnh và xã đã kiểm của một địa chỉ.
type AdminUnitPair struct {
	Province AdminUnit
	Ward     AdminUnit
}

// ListProvinces trả mọi tỉnh/thành theo mã. tx là transaction của bên gọi (bảng không có tenant_id nên chạy được cả
// trong tenancy.WithTenant).
func ListProvinces(ctx context.Context, tx pgx.Tx) ([]AdminUnit, error) {
	rows, err := store.New(tx).ListAdminProvinces(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách tỉnh: %w", err)
	}
	out := make([]AdminUnit, 0, len(rows))
	for _, r := range rows {
		out = append(out, AdminUnit{Code: r.Code, Level: AdminProvince, Name: r.Name})
	}
	return out, nil
}

// ListWards trả xã/phường của tỉnh provinceCode có tên chứa q (q chuẩn hóa bằng NormalizeName: bỏ dấu, không phân
// biệt hoa thường; q rỗng là mọi xã), theo tên, tối đa limit dòng (limit ngoài 1…MaxWardHits thành MaxWardHits). Mã
// tỉnh không có trong danh mục cho danh sách rỗng.
func ListWards(ctx context.Context, tx pgx.Tx, provinceCode, q string, limit int) ([]AdminUnit, error) {
	if limit <= 0 || limit > MaxWardHits {
		limit = MaxWardHits
	}
	pattern := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(NormalizeName(q))
	rows, err := store.New(tx).ListAdminWards(ctx, store.ListAdminWardsParams{
		ProvinceCode: provinceCode, Q: pattern, MaxRows: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("đọc danh sách xã: %w", err)
	}
	out := make([]AdminUnit, 0, len(rows))
	for _, r := range rows {
		out = append(out, AdminUnit{Code: r.Code, Level: AdminWard, ParentCode: provinceCode, Name: r.Name})
	}
	return out, nil
}

// ValidateUnits kiểm cặp tỉnh, xã của một địa chỉ: provinceCode phải là một tỉnh và wardCode một xã có trong danh mục
// (không thì ErrUnknownAdminUnit), xã phải thuộc tỉnh đó (không thì ErrWardNotInProvince).
func ValidateUnits(ctx context.Context, tx pgx.Tx, provinceCode, wardCode string) (AdminUnitPair, error) {
	rows, err := store.New(tx).GetAdminUnits(ctx, []string{provinceCode, wardCode})
	if err != nil {
		return AdminUnitPair{}, fmt.Errorf("đọc đơn vị hành chính: %w", err)
	}
	var pair AdminUnitPair
	for _, r := range rows {
		u := AdminUnit{Code: r.Code, Level: AdminLevel(r.Level), ParentCode: r.ParentCode.String, Name: r.Name}
		switch {
		case u.Code == provinceCode && u.Level == AdminProvince:
			pair.Province = u
		case u.Code == wardCode && u.Level == AdminWard:
			pair.Ward = u
		}
	}
	if pair.Province.Code == "" || pair.Ward.Code == "" {
		return AdminUnitPair{}, ErrUnknownAdminUnit
	}
	if pair.Ward.ParentCode != pair.Province.Code {
		return AdminUnitPair{}, ErrWardNotInProvince
	}
	return pair, nil
}
