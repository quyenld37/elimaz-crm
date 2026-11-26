package live_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/conversation/internal/live"
)

// Q3, LV10: với hằng số thật, luồng của worker chết mở lại trong 15 s; gia hạn đủ dày để một lần gia hạn lỡ vẫn còn
// khóa; hỏi bù phủ 5 s. Hằng số thu nhỏ của test cho 1,5 s.
func TestLeaseConstants(t *testing.T) {
	t.Parallel()
	if d := live.MaxReopenDelay(live.LeaseTTL, live.ScanEvery); d != 15*time.Second {
		t.Errorf("mở lại tối đa %v, muốn 15 s", d)
	}
	if 3*live.RenewEvery > live.LeaseTTL {
		t.Errorf("gia hạn %v quá thưa so với khóa %v", live.RenewEvery, live.LeaseTTL)
	}
	if live.Overlap != 5*time.Second {
		t.Errorf("hỏi bù %v, muốn 5 s", live.Overlap)
	}
	o := scaled("x")
	if d := live.MaxReopenDelay(o.LeaseTTL, o.ScanEvery); d != 1500*time.Millisecond {
		t.Errorf("hằng số test: %v, muốn 1,5 s", d)
	}
}
