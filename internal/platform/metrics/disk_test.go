package metrics_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

func TestDiskUsedPct(t *testing.T) {
	pct, err := metrics.DiskUsedPct(t.TempDir())
	if err != nil {
		t.Fatalf("DiskUsedPct: %v", err)
	}
	if pct <= 0 || pct > 100 {
		t.Fatalf("phần trăm đĩa = %v, muốn trong (0, 100]", pct)
	}
	if _, err := metrics.DiskUsedPct("/khong-co/thu-muc-nay"); err == nil {
		t.Fatal("đường dẫn không có phải lỗi")
	}
}

// Flush ghi gauge disk_used_pct cho từng nhãn của DiskPaths; đường dẫn hỏng chỉ bỏ qua nhãn đó.
func TestFlushWritesDiskGauge(t *testing.T) {
	d, _, f := newFlusher(t)
	f.DiskPaths = map[string]string{"root": t.TempDir(), "hong": "/khong-co/thu-muc-nay"}
	if err := f.Flush(t.Context(), time.Now()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	r := readRow(t, d, "disk_used_pct", "root")
	if r.sum <= 0 || r.sum > 100 || r.hist != nil {
		t.Fatalf("gauge disk_used_pct = %+v", r)
	}
	var n int
	if err := d.Pool.QueryRow(t.Context(),
		"SELECT count(*) FROM platform.metrics_minute WHERE name = 'disk_used_pct' AND label = 'hong'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("nhãn hỏng không được ghi: %d %v", n, err)
	}
}
