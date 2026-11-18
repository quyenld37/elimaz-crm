package fulfillment_test

import (
	"bytes"
	"crypto/rand"
	"regexp"
	"testing"
	"time"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment"
)

var codePattern = regexp.MustCompile(`^E[0-9]{6}[0-9A-Z]{6}$`)

// NewShipmentCode với rnd cố định: E + yyMMdd giờ Việt Nam + 6 ký tự base32 Crockford viết hoa; 23:30 UTC là ngày hôm
// sau ở Việt Nam.
func TestNewShipmentCode_Fixed(t *testing.T) {
	now := time.Date(2026, 11, 18, 23, 30, 0, 0, time.UTC)
	got := fulfillment.NewShipmentCode(now, bytes.NewReader(make([]byte, 16)))
	if got != "E261119000000" {
		t.Fatalf("NewShipmentCode = %q, muốn E261119000000", got)
	}
	got = fulfillment.NewShipmentCode(time.Date(2026, 11, 18, 16, 59, 0, 0, time.UTC), bytes.NewReader(bytes.Repeat([]byte{0xff}, 16)))
	if got != "E261118ZZZZZZ" {
		t.Fatalf("NewShipmentCode = %q, muốn E261118ZZZZZZ", got)
	}
	if !codePattern.MatchString(got) {
		t.Fatalf("%q không khớp mẫu", got)
	}
}

// Mọi giờ, mọi chuỗi ngẫu nhiên → mã khớp ^E[0-9]{6}[0-9A-Z]{6}$, không có I, L, O, U (Crockford).
func TestNewShipmentCode_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		sec := rapid.Int64Range(0, 4102444800).Draw(t, "sec")
		seed := rapid.SliceOfN(rapid.Byte(), 8, 8).Draw(t, "seed")
		got := fulfillment.NewShipmentCode(time.Unix(sec, 0), bytes.NewReader(seed))
		if !codePattern.MatchString(got) || regexp.MustCompile(`[ILOU]`).MatchString(got[7:]) {
			t.Fatalf("mã %q sai mẫu", got)
		}
	})
	if a, b := fulfillment.NewShipmentCode(time.Now(), rand.Reader), fulfillment.NewShipmentCode(time.Now(), rand.Reader); a == b {
		t.Fatalf("hai mã ngẫu nhiên trùng: %s", a)
	}
}
