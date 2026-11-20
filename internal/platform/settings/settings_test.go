package settings_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/settings"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit"
)

type sample struct {
	Hours map[string]int `json:"hours"`
	Days  int            `json:"days"`
	Name  string         `json:"name"`
}

func def() sample {
	return sample{Hours: map[string]int{"a": 1, "b": 2}, Days: 14, Name: "mặc định"}
}

// app chạy fn bằng crm_app trong ngữ cảnh tenant (RLS thật).
func app(t *testing.T, s *testkit.Staff, tenant uuid.UUID, fn func(pgx.Tx) error) error {
	t.Helper()
	return tenancy.WithTenant(t.Context(), s.App, tenant, fn) //nolint:wrapcheck // test
}

// get đọc khóa "thu" của tenant bằng crm_app (mặc định def()).
func get(t *testing.T, s *testkit.Staff, tenant uuid.UUID) (sample, int64) {
	t.Helper()
	var got sample
	var v int64
	if err := app(t, s, tenant, func(tx pgx.Tx) (err error) {
		got, v, err = settings.Get(t.Context(), tx, "thu", def())
		return err //nolint:wrapcheck // test
	}); err != nil {
		t.Fatal(err)
	}
	return got, v
}

// Chưa có dòng: Get trả bản sao của mặc định, version 0. Put version 0 chèn (version 1); Get đọc lại, khóa thiếu trong
// JSON đã lưu lấy mặc định (map gộp theo khóa); sửa mặc định sau đó không đổi giá trị đã trả.
func TestGetPut_DefaultsAndMerge(t *testing.T) {
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	actor := uuid.NewString()
	got, v := get(t, s, a)
	if v != 0 || got.Days != 14 || got.Hours["b"] != 2 || got.Name != "mặc định" {
		t.Errorf("chưa có dòng: mong mặc định, version 0; có %+v, %d", got, v)
	}
	got.Hours["a"] = 99 // không được sửa ngược vào mặc định của lần gọi sau
	if err := app(t, s, a, func(tx pgx.Tx) error {
		return settings.Put(t.Context(), tx, "thu", 0, map[string]any{"hours": map[string]int{"a": 5}, "name": "mới"}, actor)
	}); err != nil {
		t.Fatal(err)
	}
	if got, v = get(t, s, a); v != 1 || got.Hours["a"] != 5 || got.Hours["b"] != 2 || got.Days != 14 || got.Name != "mới" {
		t.Errorf("sau Put: mong a=5, b=2 (mặc định), days 14, version 1; có %+v, %d", got, v)
	}
	if n := s.CountIn(t, a, "SELECT count(*) FROM platform.settings WHERE key = 'thu' AND updated_by = $1", actor); n != 1 {
		t.Errorf("updated_by phải là người ghi, có %d dòng khớp", n)
	}
}

// version sai → ErrVersionConflict, không đổi gì: chèn lần hai (version 0) khi đã có dòng, sửa với version cũ.
// Version đúng tăng 1. Tenant khác không thấy dòng của A (RLS).
func TestPut_VersionConflictAndTenant(t *testing.T) {
	s := testkit.OpenStaff(t)
	a, b := s.Tenant(t, "A"), s.Tenant(t, "B")
	put := func(tenant uuid.UUID, version int64, days int) error {
		return app(t, s, tenant, func(tx pgx.Tx) error {
			return settings.Put(t.Context(), tx, "thu", version, sample{Days: days}, "")
		})
	}
	if err := put(a, 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := put(a, 0, 2); !errors.Is(err, settings.ErrVersionConflict) {
		t.Fatalf("chèn lần hai: mong ErrVersionConflict, có %v", err)
	}
	if err := put(a, 1, 3); err != nil {
		t.Fatal(err)
	}
	if err := put(a, 1, 4); !errors.Is(err, settings.ErrVersionConflict) {
		t.Fatalf("version cũ: mong ErrVersionConflict, có %v", err)
	}
	if err := put(a, 7, 4); !errors.Is(err, settings.ErrVersionConflict) {
		t.Fatalf("version lạ: mong ErrVersionConflict, có %v", err)
	}
	if got, v := get(t, s, a); v != 2 || got.Days != 3 {
		t.Errorf("mong days 3 version 2, có %d version %d", got.Days, v)
	}
	if got, v := get(t, s, b); v != 0 || got.Days != 14 {
		t.Errorf("tenant B thấy dòng của A: %+v version %d", got, v)
	}
	if err := put(b, 0, 9); err != nil {
		t.Fatalf("tenant B chèn khóa cùng tên: %v", err)
	}
}

// Khóa sai dạng, giá trị không phải object JSON, actor không phải uuid → lỗi, không ghi.
func TestPut_Invalid(t *testing.T) {
	s := testkit.OpenStaff(t)
	a := s.Tenant(t, "A")
	for name, fn := range map[string]func(pgx.Tx) error{
		"khóa rỗng":    func(tx pgx.Tx) error { return settings.Put(t.Context(), tx, "", 0, sample{}, "") },
		"khóa hoa":     func(tx pgx.Tx) error { return settings.Put(t.Context(), tx, "Thu", 0, sample{}, "") },
		"không object": func(tx pgx.Tx) error { return settings.Put(t.Context(), tx, "thu", 0, []int{1}, "") },
		"actor lạ":     func(tx pgx.Tx) error { return settings.Put(t.Context(), tx, "thu", 0, sample{}, "khong-phai-uuid") },
		"version âm":   func(tx pgx.Tx) error { return settings.Put(t.Context(), tx, "thu", -1, sample{}, "") },
		"get khóa rỗng": func(tx pgx.Tx) error {
			_, _, err := settings.Get(t.Context(), tx, "", def())
			return err //nolint:wrapcheck // test
		},
	} {
		if err := app(t, s, a, fn); err == nil || errors.Is(err, settings.ErrVersionConflict) {
			t.Errorf("%s: mong lỗi đầu vào, có %v", name, err)
		}
	}
	if n := s.CountIn(t, a, "SELECT count(*) FROM platform.settings"); n != 0 {
		t.Errorf("đầu vào sai không được ghi, có %d dòng", n)
	}
}
