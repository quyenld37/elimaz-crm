package fulfillment_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

// Credentials không lộ qua slog JSON, %v, %+v, %#v, %s, kể cả khi nằm trong Account.
func TestCredentialsRedacted(t *testing.T) {
	const secret = "abc123secret"
	c := fulfillment.Credentials{"token": secret}
	var buf bytes.Buffer
	acc := fulfillment.Account{Creds: c, CarrierKey: "vtp", Pickup: fulfillment.Address{Name: "Kho", Phone: "0912345678", Line: "12 Lý"}}
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("thử", slog.Any("creds", c), slog.Any("acc", acc))
	if strings.Contains(buf.String(), "0912345678") || strings.Contains(buf.String(), "12 Lý") || !strings.Contains(buf.String(), `"carrier_key":"vtp"`) {
		t.Errorf("Account ghi log lộ địa chỉ lấy hàng hoặc thiếu khóa hãng: %s", buf.String())
	}
	outs := []string{
		buf.String(), fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c), c.String(), fmt.Sprint(c),
		fmt.Sprintf("%v", fulfillment.Account{Creds: c}), fmt.Sprintf("%+v", fulfillment.Account{Creds: c}),
	}
	for i, s := range outs {
		if strings.Contains(s, secret) {
			t.Errorf("đầu ra %d lộ bí mật: %s", i, s)
		}
		if !strings.Contains(s, "[Redacted]") {
			t.Errorf("đầu ra %d không có [Redacted]: %s", i, s)
		}
	}
	if c["token"] != secret {
		t.Fatal("che khi in không được đổi giá trị")
	}
}

// InputHash đổi khi COD đổi 1 đồng, cân nặng đổi 1 g, dịch vụ hay tùy chọn đổi; không đổi khi địa chỉ chỉ khác khoảng
// trắng, chữ hoa, hay khi tên, SĐT người nhận đổi (không ảnh hưởng phí).
func TestInputHash(t *testing.T) {
	to := fulfillment.Address{Name: "An", Phone: "0912345678", Line: "12 Lý Thường Kiệt", ProvinceCode: "01", WardCode: "00004"}
	p := fulfillment.Parcel{WeightGrams: 500, LengthCm: 20, WidthCm: 10, HeightCm: 5, DeclaredValue: 300000, ItemCount: 1}
	o := fulfillment.Options{Inspection: "view", Payer: "shop"}
	base := fulfillment.InputHash(to, p, 530000, "std", o)
	if len(base) != 32 {
		t.Fatalf("độ dài = %d, muốn 32 (sha256)", len(base))
	}
	same := map[string][]byte{
		"khoảng trắng, chữ hoa": fulfillment.InputHash(fulfillment.Address{
			Name: "An", Phone: "0912345678", Line: "  12   LÝ thường   KIỆT ", ProvinceCode: " 01", WardCode: "00004 ",
		}, p, 530000, "std", o),
		"tên, SĐT khác": fulfillment.InputHash(fulfillment.Address{
			Name: "Bình", Phone: "0987654321", Line: "12 Lý Thường Kiệt", ProvinceCode: "01", WardCode: "00004",
		}, p, 530000, "std", o),
		"ghi chú khác": fulfillment.InputHash(to, p, 530000, "std", fulfillment.Options{Inspection: "view", Payer: "shop", Note: "gọi trước"}),
	}
	for name, h := range same {
		if !bytes.Equal(h, base) {
			t.Errorf("%s: băm đổi", name)
		}
	}
	p2 := p
	p2.WeightGrams++
	o2 := o
	o2.Inspection = "try"
	to2 := to
	to2.WardCode = "00007"
	diff := map[string][]byte{
		"COD +1 đồng":  fulfillment.InputHash(to, p, 530001, "std", o),
		"cân nặng +1g": fulfillment.InputHash(to, p2, 530000, "std", o),
		"dịch vụ":      fulfillment.InputHash(to, p, 530000, "fast", o),
		"tùy chọn":     fulfillment.InputHash(to, p, 530000, "std", o2),
		"phường":       fulfillment.InputHash(to2, p, 530000, "std", o),
	}
	for name, h := range diff {
		if bytes.Equal(h, base) {
			t.Errorf("%s: băm không đổi", name)
		}
	}
}

// StatusFromCanonical: trạng thái chuẩn có trạng thái M1 tương ứng thì ra trạng thái đó; còn lại (M3 Task 7 thêm) → false.
func TestStatusFromCanonical(t *testing.T) {
	cases := map[fulfillment.Canonical]fulfillment.Status{
		fulfillment.CanonAwaitingPickup:     fulfillment.StatusAwaitingPickup,
		fulfillment.CanonPickedUp:           fulfillment.StatusShipping,
		fulfillment.CanonInTransit:          fulfillment.StatusShipping,
		fulfillment.CanonOutForDelivery:     fulfillment.StatusShipping,
		fulfillment.CanonDelivered:          fulfillment.StatusDelivered,
		fulfillment.CanonCancelled:          fulfillment.StatusCancelled,
		fulfillment.CanonDeliveryFailed:     "",
		fulfillment.CanonPartiallyDelivered: "",
		fulfillment.CanonReturning:          "",
		fulfillment.CanonReturnedToShop:     "",
		fulfillment.CanonLost:               "",
		fulfillment.CanonDamaged:            "",
		fulfillment.CanonUnknown:            "",
		"la":                                "",
	}
	for c, want := range cases {
		got, ok := fulfillment.StatusFromCanonical(c)
		if got != want || ok != (want != "") {
			t.Errorf("StatusFromCanonical(%q) = %q %v, muốn %q", c, got, ok, want)
		}
	}
}

// CarrierError: errors.Is tới lỗi bọc (ErrNotSent, ErrRejected của M1), errors.As lấy được lớp lỗi.
func TestCarrierErrorUnwrap(t *testing.T) {
	err := fmt.Errorf("tạo kiện: %w", &fulfillment.CarrierError{
		Class: fulfillment.ClassRateLimited, RetryAfter: 7 * time.Second, Err: fulfillment.ErrNotSent,
	})
	if !errors.Is(err, fulfillment.ErrNotSent) || errors.Is(err, fulfillment.ErrRejected) {
		t.Fatalf("errors.Is sai: %v", err)
	}
	ce, ok := errors.AsType[*fulfillment.CarrierError](err)
	if !ok || ce.Class != fulfillment.ClassRateLimited || ce.RetryAfter != 7*time.Second {
		t.Fatalf("errors.As = %+v %v", ce, ok)
	}
	if !strings.Contains(err.Error(), "rate_limited") {
		t.Fatalf("Error() = %q, muốn nêu lớp lỗi", err.Error())
	}
}

type keyOnly struct {
	fulfillment.CarrierAdapter
	key     string
	formats []fulfillment.StatementFormat
}

func (k keyOnly) Key() string { return k.key }

type withFormats struct{ keyOnly }

func (w withFormats) StatementFormats() []fulfillment.StatementFormat { return w.formats }

// Registry: trùng khóa → lỗi; Get, Keys (sắp xếp); StatementFormats gom từ adapter cài StatementFormatProvider.
func TestRegistry(t *testing.T) {
	if _, err := fulfillment.NewRegistry(keyOnly{key: "a"}, keyOnly{key: "a"}); !errors.Is(err, fulfillment.ErrDuplicateCarrier) {
		t.Fatalf("trùng khóa: %v", err)
	}
	if _, err := fulfillment.NewRegistry(keyOnly{key: ""}); err == nil {
		t.Fatal("khóa rỗng mà không lỗi")
	}
	r, err := fulfillment.NewRegistry(keyOnly{key: "vtp"},
		withFormats{keyOnly{key: "best", formats: []fulfillment.StatementFormat{{Key: "best_v1", CarrierKey: "best"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if ks := r.Keys(); len(ks) != 2 || ks[0] != "best" || ks[1] != "vtp" {
		t.Fatalf("Keys = %v", ks)
	}
	if a, ok := r.Get("vtp"); !ok || a.Key() != "vtp" {
		t.Fatal("Get vtp")
	}
	if _, ok := r.Get("spx"); ok {
		t.Fatal("Get khóa lạ phải false")
	}
	if fs := r.StatementFormats(); len(fs) != 1 || fs[0].Key != "best_v1" {
		t.Fatalf("StatementFormats = %+v", fs)
	}
}
