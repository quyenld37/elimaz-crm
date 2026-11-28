//go:build perf

package reporting_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/testkit"
	"elimaz.com.vn/crm/internal/testkit/shoptest"
)

// Ngưỡng thời gian của kế hoạch M7 Task 269 (chạy bằng make perf-test trên máy rảnh, Task 400): 3.000 biến thể,
// 200 mẫu, 50 nhân viên → một lần đánh giá dưới 500 ms.
func TestLaunchReadinessPerf_3000Variants(t *testing.T) {
	e := newReadyEnv(t, testkit.OpenStaff(t))
	for i := range 200 {
		e.draft("N"+string(rune('A'+i/26))+string(rune('A'+i%26)), shoptest.Lack{})
	}
	e.exec(`INSERT INTO catalog.variants (tenant_id, seller_id, product_id, sku, barcode, color, size, price)
		SELECT p.tenant_id, p.seller_id, p.id, p.code || '-V' || k, 'B' || p.code || k, 'Mặc định', 'V' || k, 100000
		FROM catalog.products AS p CROSS JOIN generate_series(1, 14) AS k`)
	for range 50 {
		e.seed.Staff("Nhân viên")
	}
	if n := e.count(`SELECT count(*) FROM catalog.variants`); n != 3000 {
		t.Fatalf("biến thể: %d, mong 3000", n)
	}
	e.eval() // làm nóng
	start := time.Now()
	r := e.eval()
	took := time.Since(start)
	t.Logf("báo cáo sẵn sàng 3.000 biến thể: %v", took)
	if took > 500*time.Millisecond || len(r.Items) != 15 {
		t.Fatalf("%d mục trong %v, mong 15 dưới 500 ms", len(r.Items), took)
	}
}
