package main

import (
	"testing"

	"elimaz.com.vn/crm/internal/testkit"
)

// seed-dev nạp tỉnh 79 và phường 26734 (mã của hãng giả, kịch bản "một đơn đi trọn"): chạy hai lần vẫn đúng hai dòng,
// phường thuộc tỉnh 79.
func TestSeedDevAdminUnits(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	ctx := t.Context()
	for range 2 {
		if err := seedDevAdminUnits(ctx, s.Owner); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	var parent string
	if err := s.Owner.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE code IN ('79', '26734')),
		coalesce(max(parent_code) FILTER (WHERE code = '26734'), '') FROM customer.admin_units`).Scan(&n, &parent); err != nil {
		t.Fatal(err)
	}
	if n != 2 || parent != "79" {
		t.Fatalf("dòng = %d, cha của 26734 = %q", n, parent)
	}
}

// Mã 79 đã có với tên khác (danh mục thật): không lỗi, không đè tên.
func TestSeedDevAdminUnits_KeepsExisting(t *testing.T) {
	t.Parallel()
	s := testkit.OpenStaff(t)
	ctx := t.Context()
	if _, err := s.Owner.Pool.Exec(ctx, `INSERT INTO customer.admin_units
		(code, level, parent_code, name, name_search, effective_from, source)
		VALUES ('79', 'province', NULL, 'Thành phố Thật', 'thanh pho that', DATE '2025-07-01', 'test')`); err != nil {
		t.Fatal(err)
	}
	if err := seedDevAdminUnits(ctx, s.Owner); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := s.Owner.Pool.QueryRow(ctx, `SELECT name FROM customer.admin_units WHERE code = '79'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Thành phố Thật" {
		t.Fatalf("tên tỉnh 79 = %q", name)
	}
}
