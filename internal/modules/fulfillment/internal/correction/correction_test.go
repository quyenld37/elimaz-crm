package correction_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/correction"
)

// d1 là vận đơn V1 của đơn D1 (TI08): vA ×1, đã giao, hãng thu 630.000, chưa có bảng kê.
func d1() correction.Facts {
	return correction.Facts{
		ShipmentID: uuid.MustParse("0193a0b0-0000-7000-8000-000000000001"), Version: 4,
		From: "delivered", To: "returning", Lines: []correction.Line{{SKU: "VA-DO-S", Qty: 1}}, Collected: 630000,
	}
}

// TI08: ba câu tác động (đơn, tồn, tiền), không câu chặn, token 64 ký tự hex.
func TestDecide_TI08(t *testing.T) {
	p := correction.Decide(d1())
	want := []string{
		"Vận đơn từ Thành công sang Đang chuyển hoàn; đơn bỏ Thành công, mở phiếu Hãng hoàn chờ kho nhận",
		"Đảo phiếu xuất VA-DO-S ×1: hàng về Đang chuyển, kho nhận lại khi hàng hoàn về",
		"Đảo bút toán tiền hãng đã thu 630.000 đ",
	}
	if !slices.Equal(p.Effects, want) {
		t.Fatalf("Effects = %q, muốn %q", p.Effects, want)
	}
	if len(p.Blocking) != 0 {
		t.Fatalf("Blocking = %q, muốn rỗng", p.Blocking)
	}
	if len(p.Token) != 64 {
		t.Fatalf("Token = %q, muốn 64 ký tự", p.Token)
	}
}

// Không tiền hãng thu (COD 0) → không câu tiền; nhiều dòng → mỗi dòng một câu theo thứ tự.
func TestDecide_NoMoneyManyLines(t *testing.T) {
	f := d1()
	f.Collected = 0
	f.Lines = []correction.Line{{SKU: "A", Qty: 1}, {SKU: "B", Qty: 2}}
	p := correction.Decide(f)
	if len(p.Effects) != 3 || p.Effects[1] != "Đảo phiếu xuất A ×1: hàng về Đang chuyển, kho nhận lại khi hàng hoàn về" ||
		p.Effects[2] != "Đảo phiếu xuất B ×2: hàng về Đang chuyển, kho nhận lại khi hàng hoàn về" {
		t.Fatalf("Effects = %q", p.Effects)
	}
}

// Luật chặn khớp cash (postings/rules.go): shop_received ≠ 0 (cả âm) hoặc còn bút toán remitted chưa đảo.
func TestBlocking(t *testing.T) {
	cases := []struct {
		name     string
		received int64
		remitted bool
		want     []string
	}{
		{"chưa có tiền về", 0, false, nil},
		{"bảng kê đã chốt 598.000", 598000, true, []string{
			"Vận đơn đã có tiền về shop 598.000 đ trong bảng kê đã chốt — kế toán điều chỉnh sổ COD trước",
		}},
		{"shop_received âm", -1000, false, []string{
			"Vận đơn đã có tiền về shop -1.000 đ trong bảng kê đã chốt — kế toán điều chỉnh sổ COD trước",
		}},
		{"remitted còn sống, số dư 0", 0, true, []string{
			"Vận đơn đã có bút toán tiền về shop trong bảng kê đã chốt — kế toán điều chỉnh sổ COD trước",
		}},
	}
	for _, c := range cases {
		if got := correction.Blocking(c.received, c.remitted); !slices.Equal(got, c.want) {
			t.Errorf("%s: Blocking = %q, muốn %q", c.name, got, c.want)
		}
		f := d1()
		f.ShopReceived, f.Remitted = c.received, c.remitted
		if got := correction.Decide(f).Blocking; !slices.Equal(got, c.want) {
			t.Errorf("%s: Decide.Blocking = %q, muốn %q", c.name, got, c.want)
		}
	}
}

func TestVND(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 đ", 5: "5 đ", 999: "999 đ", 1000: "1.000 đ", 630000: "630.000 đ", 1234567: "1.234.567 đ",
		-598000: "-598.000 đ", 9007199254740991: "9.007.199.254.740.991 đ",
	} {
		if got := correction.VND(n); got != want {
			t.Errorf("VND(%d) = %q, muốn %q", n, got, want)
		}
	}
}

// Quy luật token: cùng dữ kiện → cùng token; đổi version, trạng thái, dòng, tiền thu → token khác; tiền về shop (chỉ
// đổi câu chặn) không đổi token (chặn kiểm riêng, 409 correction_blocked).
func TestToken_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		f := correction.Facts{
			ShipmentID: uuid.New(), Version: rapid.Int64Range(1, 1<<40).Draw(t, "version"),
			From: "delivered", To: "returning", Collected: rapid.Int64Range(0, 1<<40).Draw(t, "collected"),
		}
		for range rapid.IntRange(1, 4).Draw(t, "lines") {
			f.Lines = append(f.Lines, correction.Line{
				SKU: rapid.StringMatching(`[A-Z0-9-]{1,12}`).Draw(t, "sku"), Qty: rapid.Int32Range(1, 99).Draw(t, "qty"),
			})
		}
		base := correction.Decide(f).Token
		same := f
		same.Lines = slices.Clone(f.Lines)
		same.ShopReceived, same.Remitted = 598000, true
		if correction.Decide(same).Token != base {
			t.Fatal("token đổi khi chỉ tiền về shop đổi")
		}
		changed := []correction.Facts{f, f, f, f}
		changed[0].Version++
		changed[1].Collected++
		changed[2].Lines = append(slices.Clone(f.Lines), correction.Line{SKU: "X", Qty: 1})
		changed[3].ShipmentID = uuid.New()
		for i, c := range changed {
			if correction.Decide(c).Token == base {
				t.Fatalf("thay đổi %d không đổi token", i)
			}
		}
	})
}
