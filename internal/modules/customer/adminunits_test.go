package customer_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

// seedAdminUnits chèn danh mục mẫu bằng chủ bảng (crm_app không ghi được): hai tỉnh T01, T02 và năm xã. Mã có chữ T
// để không trùng mã số của danh mục thật (migration 0203).
func seedAdminUnits(t *testing.T, s *testkit.Staff) {
	t.Helper()
	_, err := s.Owner.Pool.Exec(t.Context(), `INSERT INTO customer.admin_units
		(code, level, parent_code, name, name_search, effective_from, source) VALUES
		('T01', 'province', NULL, 'Thành phố Thử Một', 'thanh pho thu mot', DATE '2025-07-01', 'test'),
		('T02', 'province', NULL, 'Tỉnh Thử Hai', 'tinh thu hai', DATE '2025-07-01', 'test'),
		('T01001', 'ward', 'T01', 'Phường Bến Thử', 'phuong ben thu', DATE '2025-07-01', 'test'),
		('T01002', 'ward', 'T01', 'Phường Đa Kao', 'phuong da kao', DATE '2025-07-01', 'test'),
		('T01003', 'ward', 'T01', 'Xã Bến 50%', 'xa ben 50%', DATE '2025-07-01', 'test'),
		('T02001', 'ward', 'T02', 'Phường Hoàn Kiếm Thử', 'phuong hoan kiem thu', DATE '2025-07-01', 'test'),
		('T02002', 'ward', 'T02', 'Phường Bến Hai', 'phuong ben hai', DATE '2025-07-01', 'test')`)
	if err != nil {
		t.Fatalf("chèn danh mục mẫu: %v", err)
	}
}

// adminEnv mở database, chèn danh mục mẫu và trả hàm chạy fn bằng crm_app trong tenancy.WithTenant.
func adminEnv(t *testing.T) (*testkit.Staff, func(fn func(tx pgx.Tx) error)) {
	t.Helper()
	s := testkit.OpenStaff(t)
	seedAdminUnits(t, s)
	tenant := s.Tenant(t, "A")
	return s, func(fn func(tx pgx.Tx) error) {
		t.Helper()
		if err := tenancy.WithTenant(t.Context(), s.App, tenant, fn); err != nil {
			t.Fatal(err)
		}
	}
}

func codes(us []customer.AdminUnit) []string {
	out := make([]string, 0, len(us))
	for _, u := range us {
		out = append(out, u.Code)
	}
	return out
}

func TestListProvinces(t *testing.T) {
	t.Parallel()
	_, inTx := adminEnv(t)
	var got []customer.AdminUnit
	inTx(func(tx pgx.Tx) error {
		var err error
		got, err = customer.ListProvinces(t.Context(), tx)
		return err //nolint:wrapcheck // test
	})
	// Chỉ xét mã mẫu T…: migration dữ liệu thật (nếu đã có) thêm tỉnh mã số, không làm test đỏ.
	var mine []customer.AdminUnit
	for _, u := range got {
		if u.Level != customer.AdminProvince || u.ParentCode != "" {
			t.Fatalf("dòng không phải tỉnh: %+v", u)
		}
		if strings.HasPrefix(u.Code, "T") {
			mine = append(mine, u)
		}
	}
	if !slices.Equal(codes(mine), []string{"T01", "T02"}) {
		t.Fatalf("tỉnh mẫu: %v", mine)
	}
	want := customer.AdminUnit{Code: "T01", Level: customer.AdminProvince, Name: "Thành phố Thử Một"}
	if mine[0] != want {
		t.Fatalf("tỉnh T01: %+v", mine[0])
	}
	if !slices.IsSortedFunc(got, func(a, b customer.AdminUnit) int { return strings.Compare(a.Code, b.Code) }) {
		t.Fatalf("tỉnh phải theo mã: %v", codes(got))
	}
}

func TestListWards(t *testing.T) {
	t.Parallel()
	_, inTx := adminEnv(t)
	cases := []struct {
		name, province, q string
		limit             int
		want              []string
	}{
		{"có dấu, hoa thường", "T01", "BẾN thử", 50, []string{"T01001"}},
		{"không dấu", "T01", "ben thu", 50, []string{"T01001"}},
		{"chỉ xã của tỉnh đã chọn", "T01", "ben", 50, []string{"T01001", "T01003"}},
		{"q rỗng: mọi xã, theo tên", "T01", "  ", 50, []string{"T01001", "T01002", "T01003"}},
		{"% là chữ thường, không phải ký tự đại diện", "T01", "%", 50, []string{"T01003"}},
		{"_ là chữ thường", "T01", "ben_thu", 50, nil},
		{"giới hạn", "T01", "", 2, []string{"T01001", "T01002"}},
		{"limit 0 → MaxWardHits", "T01", "", 0, []string{"T01001", "T01002", "T01003"}},
		{"tỉnh không có", "T99", "", 50, nil},
		{"mã xã làm tỉnh", "T01001", "", 50, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []customer.AdminUnit
			inTx(func(tx pgx.Tx) error {
				var err error
				got, err = customer.ListWards(t.Context(), tx, c.province, c.q, c.limit)
				return err //nolint:wrapcheck // test
			})
			if !slices.Equal(codes(got), c.want) && (len(got) != 0 || len(c.want) != 0) {
				t.Fatalf("xã: %v, muốn %v", codes(got), c.want)
			}
			for _, u := range got {
				if u.Level != customer.AdminWard || u.ParentCode != c.province {
					t.Fatalf("dòng sai: %+v", u)
				}
			}
		})
	}
}

// seedManyWards thêm 80 xã T020100…T020179 ("Xã số 100"…) vào tỉnh T02 (nhiều hơn MaxWardHits).
func seedManyWards(t *testing.T, s *testkit.Staff) {
	t.Helper()
	if _, err := s.Owner.Pool.Exec(t.Context(), `INSERT INTO customer.admin_units
		(code, level, parent_code, name, name_search, effective_from, source)
		SELECT 'T02' || lpad(i::text, 4, '0'), 'ward', 'T02', 'Xã số ' || i, 'xa so ' || i, DATE '2025-07-01', 'test'
		FROM generate_series(100, 179) AS i`); err != nil {
		t.Fatal(err)
	}
}

func TestListWardsLimitCapped(t *testing.T) {
	t.Parallel()
	s, inTx := adminEnv(t)
	seedManyWards(t, s)
	inTx(func(tx pgx.Tx) error {
		got, err := customer.ListWards(t.Context(), tx, "T02", "xa so", 1000)
		if err != nil {
			return err //nolint:wrapcheck // test
		}
		if len(got) != customer.MaxWardHits {
			t.Fatalf("muốn tối đa %d xã, có %d", customer.MaxWardHits, len(got))
		}
		return nil
	})
}

// ListAllWards (website, Task 191): mọi xã của mọi tỉnh, không chặn MaxWardHits (T02 có 82 xã), theo tỉnh rồi tên.
func TestListAllWards(t *testing.T) {
	t.Parallel()
	s, inTx := adminEnv(t)
	seedManyWards(t, s)
	var got []customer.AdminUnit
	inTx(func(tx pgx.Tx) error {
		var err error
		got, err = customer.ListAllWards(t.Context(), tx)
		return err //nolint:wrapcheck // test
	})
	// Chỉ xét mã mẫu T… (danh mục thật, nếu có, không làm test đỏ).
	var mine []customer.AdminUnit
	for _, u := range got {
		if u.Level != customer.AdminWard || u.ParentCode == "" {
			t.Fatalf("dòng không phải xã: %+v", u)
		}
		if strings.HasPrefix(u.Code, "T") {
			mine = append(mine, u)
		}
	}
	if len(mine) != 85 {
		t.Fatalf("mong 85 xã mẫu (vượt MaxWardHits), có %d", len(mine))
	}
	want := []string{"T01001", "T01002", "T01003", "T02002", "T02001", "T020100"}
	if head := codes(mine[:6]); !slices.Equal(head, want) || mine[84].Code != "T020179" {
		t.Fatalf("thứ tự theo tỉnh rồi tên: đầu %v, cuối %s", head, mine[84].Code)
	}
	if mine[1] != (customer.AdminUnit{Code: "T01002", Level: customer.AdminWard, ParentCode: "T01", Name: "Phường Đa Kao"}) {
		t.Fatalf("xã T01002: %+v", mine[1])
	}
	if !slices.IsSortedFunc(got, func(a, b customer.AdminUnit) int { return strings.Compare(a.ParentCode, b.ParentCode) }) {
		t.Fatal("xã phải theo mã tỉnh")
	}
}

func TestValidateUnits(t *testing.T) {
	t.Parallel()
	_, inTx := adminEnv(t)
	inTx(func(tx pgx.Tx) error {
		pair, err := customer.ValidateUnits(t.Context(), tx, "T01", "T01002")
		if err != nil {
			t.Fatalf("cặp đúng: %v", err)
		}
		if pair.Province.Code != "T01" || pair.Province.Name != "Thành phố Thử Một" ||
			pair.Ward.Code != "T01002" || pair.Ward.Name != "Phường Đa Kao" || pair.Ward.ParentCode != "T01" {
			t.Fatalf("cặp: %+v", pair)
		}
		for _, c := range []struct {
			name, province, ward string
			want                 error
		}{
			{"xã của tỉnh khác", "T01", "T02001", customer.ErrWardNotInProvince},
			{"tỉnh lạ", "T99", "T01001", customer.ErrUnknownAdminUnit},
			{"xã lạ", "T01", "T01999", customer.ErrUnknownAdminUnit},
			{"tỉnh rỗng", "", "T01001", customer.ErrUnknownAdminUnit},
			{"xã rỗng", "T01", "", customer.ErrUnknownAdminUnit},
			{"mã xã ở ô tỉnh", "T01001", "T01001", customer.ErrUnknownAdminUnit},
			{"mã tỉnh ở ô xã", "T01", "T02", customer.ErrUnknownAdminUnit},
		} {
			if _, err := customer.ValidateUnits(t.Context(), tx, c.province, c.ward); !errors.Is(err, c.want) {
				t.Fatalf("%s: lỗi %v, muốn %v", c.name, err, c.want)
			}
		}
		return nil
	})
}

// crm_app chỉ đọc danh mục: ghi, sửa, xóa đều lỗi quyền 42501. Bảng không có tenant_id nên đọc được cả ngoài
// ngữ cảnh tenant.
func TestAdminUnitsReadOnlyForApp(t *testing.T) {
	t.Parallel()
	s, _ := adminEnv(t)
	for _, q := range []string{
		`INSERT INTO customer.admin_units (code, level, parent_code, name, name_search, effective_from, source)
			VALUES ('T03', 'province', NULL, 'X', 'x', DATE '2025-07-01', 'test')`,
		`UPDATE customer.admin_units SET name = 'Y' WHERE code = 'T01'`,
		`DELETE FROM customer.admin_units WHERE code = 'T01002'`,
		`TRUNCATE customer.admin_units CASCADE`,
	} {
		_, err := s.App.Pool.Exec(t.Context(), q)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("%s: muốn lỗi quyền 42501, được %v", q, err)
		}
	}
	var n int
	if err := s.App.Pool.QueryRow(t.Context(), "SELECT count(*) FROM customer.admin_units WHERE code LIKE 'T%'").Scan(&n); err != nil || n != 7 {
		t.Fatalf("crm_app đọc danh mục: n=%d err=%v", n, err)
	}
}

// Ràng buộc của bảng: cấp chỉ province/ward, tỉnh không có cha, xã phải có cha, cha phải tồn tại; sau migrate mọi xã
// có cha là một tỉnh có trong bảng (đúng cả khi migration dữ liệu thật đã chạy).
func TestAdminUnitsConstraints(t *testing.T) {
	t.Parallel()
	s, _ := adminEnv(t)
	for _, c := range []struct{ name, values, code string }{
		{"cấp lạ", "('T09', 'district', NULL", "23514"},
		{"tỉnh có cha", "('T09', 'province', 'T01'", "23514"},
		{"xã không cha", "('T09001', 'ward', NULL", "23514"},
		{"cha không có", "('T09001', 'ward', 'T09'", "23503"},
		{"trùng mã", "('T01', 'province', NULL", "23505"},
	} {
		_, err := s.Owner.Pool.Exec(t.Context(), `INSERT INTO customer.admin_units
			(code, level, parent_code, name, name_search, effective_from, source) VALUES `+c.values+
			`, 'X', 'x', DATE '2025-07-01', 'test')`)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != c.code {
			t.Fatalf("%s: muốn %s, được %v", c.name, c.code, err)
		}
	}
	if n := s.Count(t, `SELECT count(*) FROM customer.admin_units w
		LEFT JOIN customer.admin_units p ON p.code = w.parent_code AND p.level = 'province'
		WHERE w.level = 'ward' AND p.code IS NULL`); n != 0 {
		t.Fatalf("%d xã không có tỉnh cha", n)
	}
}
