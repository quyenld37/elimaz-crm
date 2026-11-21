package customer

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// Biên của mức hay hoàn (kế hoạch M3-21): high khi Returned ≥ HighMinReturned và tỷ lệ ≥ HighRateBP; watch khi
// Returned ≥ WatchMinReturned; ngược lại none. Hint theo mức.
func TestClassifyReturnRisk(t *testing.T) {
	rr := ReturnRisk{WatchMinReturned: 2, HighMinReturned: 3, HighRateBP: 3000}
	for _, c := range []struct {
		name                string
		delivered, returned int32
		want                string
	}{
		{"chưa có đơn kết thúc", 0, 0, RiskNone},
		{"dưới ngưỡng theo dõi", 10, 1, RiskNone},
		{"đúng ngưỡng theo dõi", 10, 2, RiskWatch},
		{"đủ số hoàn cao, tỷ lệ dưới 30%", 8, 3, RiskWatch},     // 2727
		{"đủ số hoàn cao, tỷ lệ đúng 30%", 7, 3, RiskHigh},      // 3000
		{"tỷ lệ 100% nhưng thiếu số hoàn cao", 0, 2, RiskWatch}, // 10000
		{"chỉ hoàn, đủ số cao", 0, 3, RiskHigh},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := classify(statsOf(0, c.delivered, c.returned, 0, pgtype.Timestamptz{}), rr)
			hint := map[string]string{RiskNone: "", RiskWatch: HintWatch, RiskHigh: HintHigh}[c.want]
			if got.Risk != c.want || got.Hint != hint || got.Delivered != int(c.delivered) || got.Returned != int(c.returned) {
				t.Errorf("%+v; muốn %s %q", got, c.want, hint)
			}
		})
	}
	// Ngưỡng theo dõi cao hơn ngưỡng cao (cài đặt lạ nhưng hợp lệ): high vẫn thắng.
	if got := classify(statsOf(0, 0, 2, 0, pgtype.Timestamptz{}), ReturnRisk{
		WatchMinReturned: 5, HighMinReturned: 2,
		HighRateBP: 5000,
	}); got.Risk != RiskHigh {
		t.Errorf("high trước watch: %+v", got)
	}
	// Ngưỡng rỗng (bên gọi quên truyền): khách chưa hoàn đơn nào vẫn none.
	if got := classify(statsOf(0, 5, 0, 0, pgtype.Timestamptz{}), ReturnRisk{}); got.Risk != RiskNone || got.Hint != "" {
		t.Errorf("ngưỡng rỗng, 0 đơn hoàn: %+v", got)
	}
}
