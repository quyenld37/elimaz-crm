package carrier_test

import (
	"errors"
	"slices"
	"testing"

	"elimaz.com.vn/crm/internal/adapters/carrier"
	"elimaz.com.vn/crm/internal/adapters/carrier/fake"
	"elimaz.com.vn/crm/internal/modules/fulfillment"
	"elimaz.com.vn/crm/internal/platform/config"
)

func keys(as []fulfillment.CarrierAdapter) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.Key())
	}
	slices.Sort(out)
	return out
}

// All: luôn có self (giao tay), vtp, best, spx; hãng giả chỉ khi khác production và có FAKECARRIER_URL (như M1);
// danh sách dựng được Registry (không trùng khóa).
func TestAll(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.Config
		want []string
	}{
		{"dev không URL", config.Config{Env: "dev"}, []string{"best", "self", "spx", "vtp"}},
		{"dev có URL", config.Config{Env: "dev", FakeCarrierURL: "http://127.0.0.1:4194"}, []string{"best", "fake", "self", "spx", "vtp"}},
		{"production có URL", config.Config{Env: "production", FakeCarrierURL: "http://127.0.0.1:4194"}, []string{"best", "self", "spx", "vtp"}},
	}
	for _, c := range cases {
		as := carrier.All(c.cfg)
		if got := keys(as); !slices.Equal(got, c.want) {
			t.Errorf("%s: khóa = %v, muốn %v", c.name, got, c.want)
		}
		if _, err := fulfillment.NewRegistry(as...); err != nil {
			t.Errorf("%s: NewRegistry: %v", c.name, err)
		}
	}
}

// Hai adapter hãng giả cùng khóa → lỗi trùng khóa.
func TestRegistryDuplicateFake(t *testing.T) {
	_, err := fulfillment.NewRegistry(fake.NewClient("http://x", "s", nil), fake.NewClient("http://y", "s", nil))
	if !errors.Is(err, fulfillment.ErrDuplicateCarrier) {
		t.Fatalf("trùng khóa fake: %v", err)
	}
	if _, err := fulfillment.NewRegistry(fake.NewClient("http://x", "s", nil), fake.NewClientWithKey("fake_b", "http://y", "s", nil)); err != nil {
		t.Fatalf("fake và fake_b: %v", err)
	}
}

// ManualKeys là danh mục hãng tạo được tài khoản nhập mã tay dù chưa nối API (Task 448): đúng tập khóa khung của All
// khi không có hãng giả (self, vtp, best, spx), không bao giờ có hãng giả; bản trả về là bản sao (bên gọi sửa không ảnh
// hưởng lần gọi sau).
func TestManualKeys(t *testing.T) {
	got := carrier.ManualKeys()
	slices.Sort(got)
	if want := keys(carrier.All(config.Config{Env: "production"})); !slices.Equal(got, want) {
		t.Fatalf("ManualKeys = %v, muốn đúng khóa khung của All = %v", got, want)
	}
	if slices.Contains(got, fake.CarrierKey) {
		t.Fatalf("ManualKeys có hãng giả: %v", got)
	}
	got[0] = "sửa"
	if slices.Contains(carrier.ManualKeys(), "sửa") {
		t.Fatal("ManualKeys trả chung một mảng giữa các lần gọi")
	}
}
