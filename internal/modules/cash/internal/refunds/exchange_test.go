package refunds

import (
	"testing"

	"pgregory.net/rapid"
)

// Quy luật chia tiền đổi hàng (vùng rủi ro cao, ADR-25): với tổng đơn thay thế G, đã thu P ≤ G, tiền hoàn R ≥ 0 và tiền
// hoàn cuối R' ≥ 0: cấn trừ C = min(R, G − P) trong [0, R]; P + C ≤ G nên COD còn thu G − P − C ≥ 0; C + dư = max(R', C)
// (khách nhận lại đủ R' khi R' ≥ C, không bao giờ âm).
func TestExchangeSplitProperty(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		g := rapid.Int64Range(0, 1_000_000_000).Draw(t, "grand")
		p := rapid.Int64Range(0, g).Draw(t, "paid")
		r := rapid.Int64Range(0, 1_000_000_000).Draw(t, "refund")
		final := rapid.Int64Range(0, 1_000_000_000).Draw(t, "final")
		c := creditAmount(r, g, p)
		if c < 0 || c > r || p+c > g || c != min(r, g-p) {
			t.Fatalf("cấn trừ %d (R %d, G %d, P %d)", c, r, g, p)
		}
		s := surplus(final, c)
		if s < 0 || c+s != max(final, c) {
			t.Fatalf("dư %d (R' %d, C %d)", s, final, c)
		}
	})
}

// Ca biên: đã thu vượt tổng đơn (dữ liệu cũ) → cấn trừ 0, không âm; dư khi tiền hoàn cuối nhỏ hơn cấn trừ → 0.
func TestExchangeSplitEdges(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ r, g, p, want int64 }{
		{177049, 180000, 0, 177049}, {177049, 150000, 0, 150000}, {177049, 100, 200, 0}, {0, 180000, 0, 0},
	} {
		if got := creditAmount(c.r, c.g, c.p); got != c.want {
			t.Errorf("creditAmount(%d, %d, %d) = %d, muốn %d", c.r, c.g, c.p, got, c.want)
		}
	}
	if got := surplus(100000, 150000); got != 0 {
		t.Errorf("surplus(100.000, 150.000) = %d", got)
	}
	if got := surplus(177049, 150000); got != 27049 {
		t.Errorf("surplus(177.049, 150.000) = %d", got)
	}
}
