package formats_test

import (
	"errors"
	"testing"

	"elimaz.com.vn/crm/internal/modules/cash/internal/formats"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

func carrierFormat(key, carrier string) fulfillment.StatementFormat {
	return fulfillment.StatementFormat{
		Key: key, CarrierKey: carrier, Version: 1, HeaderRow: 3,
		Columns: map[fulfillment.StatementField][]string{
			fulfillment.FieldTrackingCode: {"Mã vận đơn", "ma_don"},
			fulfillment.FieldCOD:          {"Tiền thu hộ"},
			fulfillment.FieldNet:          {"Thực nhận"},
		},
		Required: []fulfillment.StatementField{fulfillment.FieldTrackingCode, fulfillment.FieldCOD},
	}
}

// Tên cột so sau khi bỏ dấu, chữ thường, mọi chuỗi ký tự không phải chữ, số thành một dấu "_".
func TestColumnName(t *testing.T) {
	for in, want := range map[string]string{
		"Mã vận đơn": "ma_van_don", " TIỀN  COD (đ) ": "tien_cod_d", "ma_van_don": "ma_van_don", "": "",
		"Phí\tkhác": "phi_khac", "__x__": "x",
	} {
		if got := formats.ColumnName(in); got != want {
			t.Errorf("ColumnName(%q) = %q, mong %q", in, got, want)
		}
	}
}

// Registry luôn có elimaz_v1 (cột Q-16, 4 cột bắt buộc); định dạng hãng cộng thêm; trùng Key (kể cả trùng elimaz_v1)
// → ErrDuplicateKey; định dạng sai (thiếu Key, cột bắt buộc không có tên, hai trường chung một tên) → ErrInvalid.
func TestRegistry(t *testing.T) {
	r, err := formats.NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	v1, ok := r.Get(formats.ElimazV1Key)
	if !ok || v1.HeaderRow != 1 || len(v1.Required) != 4 || v1.Columns[fulfillment.FieldCOD][0] != "tien_cod" ||
		v1.Columns[fulfillment.FieldPaidOn][0] != "ngay_chuyen" || v1.Columns[fulfillment.FieldOtherFee][0] != "phi_khac" {
		t.Fatalf("elimaz_v1: %+v", v1)
	}
	if _, err := formats.NewRegistry([]fulfillment.StatementFormat{carrierFormat("a_v1", "a"), carrierFormat("a_v1", "a")}); !errors.Is(err, formats.ErrDuplicateKey) {
		t.Errorf("trùng khóa: %v", err)
	}
	if _, err := formats.NewRegistry([]fulfillment.StatementFormat{carrierFormat(formats.ElimazV1Key, "a")}); !errors.Is(err, formats.ErrDuplicateKey) {
		t.Errorf("trùng elimaz_v1: %v", err)
	}
	bad := []fulfillment.StatementFormat{carrierFormat("", "a"), carrierFormat("b_v1", "b"), carrierFormat("c_v1", "c"), carrierFormat("d_v1", "")}
	bad[1].Required = append(bad[1].Required, fulfillment.FieldFee)
	bad[2].Columns[fulfillment.FieldNet] = []string{"MÃ VẬN ĐƠN"}
	for i, f := range bad {
		if _, err := formats.NewRegistry([]fulfillment.StatementFormat{f}); !errors.Is(err, formats.ErrInvalid) {
			t.Errorf("định dạng sai %d: %v", i, err)
		}
	}
}

// For: rỗng → định dạng đầu tiên của hãng (theo thứ tự khai), hãng không có định dạng → elimaz_v1; chỉ định được
// elimaz_v1 cho mọi hãng; định dạng của hãng khác hay khóa lạ → ErrUnknown.
func TestRegistryFor(t *testing.T) {
	r, err := formats.NewRegistry([]fulfillment.StatementFormat{carrierFormat("a_v2", "a"), carrierFormat("a_v1", "a"), carrierFormat("b_v1", "b")})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ carrier, key, want string }{
		{"a", "", "a_v2"},
		{"a", "a_v1", "a_v1"},
		{"b", "", "b_v1"},
		{"fake", "", formats.ElimazV1Key},
		{"a", formats.ElimazV1Key, formats.ElimazV1Key},
	} {
		f, err := r.For(c.carrier, c.key)
		if err != nil || f.Key != c.want {
			t.Errorf("For(%q, %q) = %q, %v; mong %q", c.carrier, c.key, f.Key, err, c.want)
		}
	}
	for _, c := range [][2]string{{"a", "b_v1"}, {"a", "zzz"}} {
		if _, err := r.For(c[0], c[1]); !errors.Is(err, formats.ErrUnknown) {
			t.Errorf("For(%q, %q): %v, mong ErrUnknown", c[0], c[1], err)
		}
	}
}
