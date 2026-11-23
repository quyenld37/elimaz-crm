package promotion_test

import (
	"math"
	"testing"

	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/promotion"
)

// TestCampaignPrice_Invariants (rapid; quy trình mục 6 câu 11): mọi giá niêm yết, mọi phần trăm 1–99 → hoặc không áp,
// hoặc giá trong (0, giá niêm yết); khi tích list × (100 − %) nằm trong vùng số nguyên an toàn của JavaScript, giá
// bằng Math.round(list × (100 − %) / 100) tính bằng số thực như website (math.Round làm tròn nửa xa 0, cùng kết quả
// với Math.round cho số dương); ngoài vùng đó không áp. Phần trăm lớn hơn không bao giờ cho giá cao hơn.
func TestCampaignPrice_Invariants(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		list := rapid.OneOf(rapid.Int64Range(-10, 1000), rapid.Int64Range(1, 1<<40), rapid.Int64Range(1, math.MaxInt64/100)).
			Draw(t, "list")
		pct := rapid.IntRange(1, 99).Draw(t, "percent")
		price, ok := promotion.CampaignPrice(list, pct)
		if ok && (price <= 0 || price >= list) {
			t.Fatalf("giá %d giảm %d%% ra %d ngoài (0, list)", list, pct, price)
		}
		safe := list > 0 && list <= (1<<53-1)/int64(100-pct) // tích ≤ 2^53 − 1: số thực tính đúng
		if !safe {
			if ok {
				t.Fatalf("giá %d giảm %d%% ngoài vùng an toàn mà vẫn áp", list, pct)
			}
			return
		}
		want := int64(math.Round(float64(list) * float64(100-pct) / 100))
		if wantOK := want > 0 && want < list; ok != wantOK || (ok && price != want) {
			t.Fatalf("giá %d giảm %d%%: (%d, %v), website (%d, %v)", list, pct, price, ok, want, wantOK)
		}
		checkMonotone(t, list, pct, price, ok)
	})
}

// checkMonotone: giảm pct+1 % không bao giờ cho giá cao hơn giảm pct % (khi cả hai áp được).
func checkMonotone(t *rapid.T, list int64, pct int, price int64, ok bool) {
	if pct == 99 || !ok {
		return
	}
	if more, ok2 := promotion.CampaignPrice(list, pct+1); ok2 && more > price {
		t.Fatalf("giá %d: giảm %d%% ra %d cao hơn giảm %d%% ra %d", list, pct+1, more, pct, price)
	}
}
