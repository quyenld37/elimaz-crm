//go:build perf

package catalog_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/catalog"
)

// Ngưỡng thời gian của kế hoạch M7 Task 261 (chạy bằng make perf-test trên máy rảnh, Task 400): 200 mẫu đủ điều kiện,
// một lần Publish(allReady) xong dưới 2 s.
func TestLaunchPerf_Publish200(t *testing.T) {
	e := newLaunchEnv(t)
	e.seed200()
	start := time.Now()
	out := e.mustPublish(catalog.PublishInput{AllReady: true})
	took := time.Since(start)
	t.Logf("Publish 200 mẫu: %v", took)
	if len(out.Published) != 200 || took > 2*time.Second {
		t.Fatalf("200 mẫu: %d hiện trong %v, mong 200 dưới 2 s", len(out.Published), took)
	}
}
