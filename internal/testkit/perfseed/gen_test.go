package perfseed

import (
	"regexp"
	"testing"
	"time"
)

var phoneRe = regexp.MustCompile(`^\+8490[0-9]{7}$`)

// TestFakePhone_UniqueAtFullSize: 1.300.000 SĐT của cỡ thật (1.000.000 khách, 30% có SĐT thứ hai) khác nhau từng đôi,
// đúng dạng +8490xxxxxxx; đuôi 4 số rải đều (không đuôi nào quá 3 lần trung bình).
func TestFakePhone_UniqueAtFullSize(t *testing.T) {
	t.Parallel()
	n := 0
	seen := make(map[string]struct{}, 1_300_000)
	tails := map[string]int{}
	phoneSlots(1_000_000, 300, func(s phoneSlot) bool {
		p := fakePhone(s.k)
		if _, dup := seen[p]; dup {
			t.Fatalf("SĐT thứ %d trùng", s.k)
		}
		if !phoneRe.MatchString(p) {
			t.Fatalf("SĐT thứ %d sai dạng: %s", s.k, p)
		}
		seen[p] = struct{}{}
		tails[p[len(p)-4:]]++
		n++
		return true
	})
	if n != 1_300_000 {
		t.Fatalf("%d SĐT, muốn 1.300.000", n)
	}
	for tail, c := range tails {
		if c > 3*n/10_000 {
			t.Errorf("đuôi %s có %d SĐT, trung bình %d", tail, c, n/10_000)
		}
	}
}

// TestUUIDV7At: mã mang đúng mốc mili giây, phiên bản 7, biến thể RFC 9562; mã sau mốc sau lớn hơn.
func TestUUIDV7At(t *testing.T) {
	t.Parallel()
	r := randFor(1, 1)
	at := time.Date(2026, 1, 2, 3, 4, 5, 6_000_000, time.UTC)
	u := uuidV7At(at, r)
	if u.Version() != 7 || u.Variant().String() != "RFC4122" {
		t.Fatalf("phiên bản %d, biến thể %s", u.Version(), u.Variant())
	}
	sec, nsec := u.Time().UnixTime()
	if got := time.Unix(sec, nsec).UTC(); !got.Equal(at.Truncate(time.Millisecond)) {
		t.Errorf("mốc %s, muốn %s", got, at)
	}
	if v := uuidV7At(at.Add(time.Millisecond), r); v.String() <= u.String() {
		t.Errorf("mã sau %s không lớn hơn %s", v, u)
	}
}

// TestOrderGen_SameSeedSameOrders: hai bộ sinh cùng hạt giống ra cùng dãy đơn (copyOrders và copyItems chạy song song
// dựa vào điều này); 8 dòng mỗi 5 đơn, hai dòng của một đơn khác biến thể; khách đã tạo trước đơn.
func TestOrderGen_SameSeedSameOrders(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	tl := timeline{to: now, from: now.AddDate(0, -24, 0), lead: 30 * 24 * time.Hour, n: 1000}
	vs := []variantRef{{SKU: "A", Price: 1}, {SKU: "B", Price: 2}, {SKU: "C", Price: 3}}
	a, b := newOrderGen(9, 500, tl, vs), newOrderGen(9, 500, tl, vs)
	items := 0
	for {
		x, okA := a.next()
		y, okB := b.next()
		if okA != okB {
			t.Fatal("hai bộ sinh dừng khác nhau")
		}
		if !okA {
			break
		}
		if x.ID != y.ID || x.Customer != y.Customer || len(x.Items) != len(y.Items) || x.Items[0].ID != y.Items[0].ID {
			t.Fatalf("đơn %s khác %s", x.Code, y.Code)
		}
		if len(x.Items) == 2 && x.Items[0].Variant.SKU == x.Items[1].Variant.SKU {
			t.Errorf("đơn %s hai dòng cùng biến thể", x.Code)
		}
		if tl.customerCreated(x.Customer).After(x.CreatedAt) {
			t.Errorf("đơn %s lúc %s của khách tạo sau đó", x.Code, x.CreatedAt)
		}
		items += len(x.Items)
	}
	if items != 800 {
		t.Errorf("%d dòng cho 500 đơn, muốn 800", items)
	}
}
