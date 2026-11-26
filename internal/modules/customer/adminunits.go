package customer

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"elimaz.com.vn/crm/internal/modules/customer/internal/store"
	"elimaz.com.vn/crm/internal/platform/textnorm"
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

// ListAllWards trả mọi xã/phường của mọi tỉnh, theo mã tỉnh rồi tên (không chặn MaxWardHits: danh mục địa giới đầy đủ
// cho website, kế hoạch M5 Task 191). Khi nào không dùng: ô chọn xã của màn hình (ListWards có lọc và giới hạn).
func ListAllWards(ctx context.Context, tx pgx.Tx) ([]AdminUnit, error) {
	rows, err := store.New(tx).ListAllAdminWards(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc mọi xã: %w", err)
	}
	out := make([]AdminUnit, 0, len(rows))
	for _, r := range rows {
		out = append(out, AdminUnit{Code: r.Code, Level: AdminWard, ParentCode: r.ParentCode, Name: r.Name})
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

// EnsureFixtureAdminUnits chèn các đơn vị hành chính thử (tỉnh trước xã) của bộ e2e website (lệnh seed-web-fixture,
// Task 218) khi mã chưa có, trả số đơn vị đã chèn; chạy lại không chèn thêm. Mã đã có mà khác cấp, tên hay tỉnh cha:
// trả các mã đó trong conflicts và không chèn gì (không đè danh mục thật; bên gọi quyết định lỗi). tx phải là
// transaction của chủ bảng: crm_app chỉ đọc bảng này (lỗi quyền 42501). Khi nào không dùng: danh mục thật (chỉ
// migration ghi, tools/adminunits).
func EnsureFixtureAdminUnits(ctx context.Context, tx pgx.Tx, units []AdminUnit) (inserted int, conflicts []string, err error) {
	q := store.New(tx)
	codes := make([]string, 0, len(units))
	for _, u := range units {
		codes = append(codes, u.Code)
	}
	rows, err := q.GetAdminUnits(ctx, codes)
	if err != nil {
		return 0, nil, fmt.Errorf("đọc đơn vị hành chính đã có: %w", err)
	}
	have := make(map[string]AdminUnit, len(rows))
	for _, r := range rows {
		have[r.Code] = AdminUnit{Code: r.Code, Level: AdminLevel(r.Level), ParentCode: r.ParentCode.String, Name: r.Name}
	}
	for _, u := range units {
		if h, ok := have[u.Code]; ok && h != u {
			conflicts = append(conflicts, u.Code)
		}
	}
	if len(conflicts) > 0 {
		return 0, conflicts, nil
	}
	for _, u := range units {
		if _, ok := have[u.Code]; ok {
			continue
		}
		n, err := q.InsertFixtureAdminUnit(ctx, store.InsertFixtureAdminUnitParams{
			Code: u.Code, Level: string(u.Level), ParentCode: pgtype.Text{String: u.ParentCode, Valid: u.ParentCode != ""},
			Name: u.Name, NameSearch: textnorm.Fold(u.Name),
		})
		if err != nil {
			return 0, nil, fmt.Errorf("chèn đơn vị hành chính thử %s: %w", u.Code, err)
		}
		inserted += int(n)
	}
	return inserted, nil, nil
}
