package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

func writeTemp(t *testing.T, dir, name string, v any) string {
	t.Helper()
	p := filepath.Join(dir, name)
	var b []byte
	switch x := v.(type) {
	case string:
		b = []byte(x)
	default:
		var err error
		if b, err = json.Marshal(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// loadkit report đọc đủ các tệp run.sh ghi (k6, sse, db, series, docker stats, chaos) và chấm: mọi tiêu chí đạt thì
// không lỗi; một tiêu chí không đạt thì errFailed; tệp không có thì lỗi khác errFailed.
func TestReportCommand(t *testing.T) {
	dir := t.TempDir()
	in := goodInput()
	hist := make([]int, len(metrics.HistBoundsMs)+1)
	hist[0] = 10
	in.DB.PickupHist = hist
	in.DB.CommandsTotal = 4
	start := time.Now().UTC().Add(-20 * time.Minute).Truncate(time.Second)
	args := []string{
		"report", "-mode", modeChaos, "-start", start.Format(time.RFC3339),
		"-k6", writeTemp(t, dir, "k6.json", in.K6),
		"-sse", writeTemp(t, dir, "sse.json", in.SSE),
		"-db", writeTemp(t, dir, "db.json", in.DB),
		"-series", writeTemp(t, dir, "series.jsonl", "{\"jobs_bytes\":10}\nhỏng\n"),
		"-stats", writeTemp(t, dir, "stats.csv", "1700000000,crm-t22-crm-api-1,1.0%,10MiB / 512MiB\n"),
		"-chaos", writeTemp(t, dir, "chaos.csv", "fault,start,recovered\ngiết crm-api,100,130\n"),
		"-project", "crm-t22",
	}
	logger := slog.New(slog.DiscardHandler)
	if err := run(t.Context(), args, logger); err != nil {
		t.Fatalf("mọi tiêu chí đạt: %v", err)
	}
	args[len(args)-3] = writeTemp(t, dir, "chaos2.csv", "fault,start,recovered\ngiết crm-api,100,\n")
	if err := run(t.Context(), args, logger); !errors.Is(err, errFailed) {
		t.Fatalf("không trở lại: muốn errFailed, nhận %v", err)
	}
	args[6] = filepath.Join(dir, "khong-co.json") // giá trị của -k6
	if err := run(t.Context(), args, logger); err == nil || errors.Is(err, errFailed) {
		t.Fatalf("thiếu tệp: muốn lỗi đọc, nhận %v", err)
	}
	if err := run(t.Context(), []string{"report", "-mode", "la"}, logger); !errors.Is(err, errUsage) {
		t.Fatalf("bài lạ: muốn errUsage, nhận %v", err)
	}
}
