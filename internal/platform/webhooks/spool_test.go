package webhooks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/webhooks"
)

func entry(body string, at time.Time) webhooks.Entry {
	return webhooks.Entry{
		Source: "selftest", Headers: json.RawMessage(`{"X-A":["1"]}`), Body: []byte(body), ReceivedAt: at,
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestSpoolWriteThenDrainKeepsOrderAndReceivedAt(t *testing.T) {
	d := openDB(t)
	s := &webhooks.Spool{Dir: t.TempDir()}
	base := time.Date(2026, 11, 18, 1, 0, 0, 123_456_000, time.UTC)
	bodies := []string{`{"n":1}`, `{"n":2}`, `{"n":3}`}
	writeOwnerOnly(t, s, bodies, base)
	names, err := s.List()
	if err != nil || len(names) != 3 {
		t.Fatalf("List = %v, %v", names, err)
	}
	files, age, err := s.Stats()
	if err != nil || files != 3 || age < time.Since(base)-time.Second {
		t.Fatalf("Stats = %d, %v, %v", files, age, err)
	}

	n, err := s.Drain(t.Context(), d)
	if err != nil || n != 3 {
		t.Fatalf("Drain = %d, %v; muốn 3", n, err)
	}
	if left := dirEntries(t, s.Dir); len(left) != 0 {
		t.Fatalf("thư mục còn %v", left)
	}
	rows, err := d.Pool.Query(t.Context(), "SELECT body, received_at FROM platform.webhook_deliveries ORDER BY received_at")
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Body []byte
		At   time.Time
	}
	got, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d dòng, muốn 3", len(got))
	}
	for i, r := range got {
		if string(r.Body) != bodies[i] || !r.At.Equal(base.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("dòng %d = %s %v; muốn %s %v", i, r.Body, r.At, bodies[i], base.Add(time.Duration(i)*time.Second))
		}
	}
	if _, jobs := counts(t, d); jobs != 3 {
		t.Fatalf("%d việc tách gói, muốn 3", jobs)
	}
}

// writeOwnerOnly ghi mỗi thân một tệp, cách nhau 1 s từ base, và đòi tệp có quyền 0600.
func writeOwnerOnly(t *testing.T, s *webhooks.Spool, bodies []string, base time.Time) {
	t.Helper()
	for i, b := range bodies {
		name, err := s.Write(entry(b, base.Add(time.Duration(i)*time.Second)))
		if err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(filepath.Join(s.Dir, name))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("tệp %s: %v; muốn quyền 0600", name, err)
		}
	}
}

func TestSpoolDrainDatabaseDownKeepsFiles(t *testing.T) {
	s := &webhooks.Spool{Dir: t.TempDir()}
	for i := range 2 {
		if _, err := s.Write(entry(`{"i":`+string(rune('0'+i))+`}`, time.Now())); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.Drain(t.Context(), openDeadDB(t))
	if err == nil || n != 0 {
		t.Fatalf("Drain khi database chết = %d, %v; muốn 0 và lỗi", n, err)
	}
	if names, _ := s.List(); len(names) != 2 {
		t.Fatalf("còn %d tệp, muốn 2", len(names))
	}
}

func TestSpoolDrainDuplicateOfExistingRow(t *testing.T) {
	d := openDB(t)
	e := entry(`{"dup":true}`, time.Now())
	if created, err := webhooks.Save(t.Context(), d, e); err != nil || !created {
		t.Fatalf("Save = %v, %v", created, err)
	}
	s := &webhooks.Spool{Dir: t.TempDir()}
	if _, err := s.Write(e); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Drain(t.Context(), d); err != nil || n != 1 {
		t.Fatalf("Drain = %d, %v", n, err)
	}
	if rows, jobs := counts(t, d); rows != 1 || jobs != 1 {
		t.Fatalf("%d dòng, %d việc; muốn 1, 1", rows, jobs)
	}
	if left := dirEntries(t, s.Dir); len(left) != 0 {
		t.Fatalf("thư mục còn %v", left)
	}
}

func TestSpoolWriteUnwritableDir(t *testing.T) {
	dir := readOnlyDir(t)
	s := &webhooks.Spool{Dir: dir}
	if _, err := s.Write(entry("{}", time.Now())); err == nil {
		t.Fatal("Write vào thư mục chỉ đọc mà không lỗi")
	}
	if _, err := (&webhooks.Spool{Dir: filepath.Join(dir, "khong-co")}).Write(entry("{}", time.Now())); err == nil {
		t.Fatal("Write vào thư mục không có mà không lỗi")
	}
}

func TestSpoolIgnoresForeignAndCorruptFiles(t *testing.T) {
	d := openDB(t)
	s := &webhooks.Spool{Dir: t.TempDir()}
	for name, content := range map[string]string{
		"ghi-chu.txt":   "x",
		".tmp-123.json": "{",
		"1700000000000-018f2b6e-0000-7000-8000-000000000000.json": "không phải json",
	} {
		if err := os.WriteFile(filepath.Join(s.Dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Write(entry(`{"ok":1}`, time.Now())); err != nil {
		t.Fatal(err)
	}
	if names, _ := s.List(); len(names) != 2 {
		t.Fatalf("List = %v; muốn 2 tệp đúng mẫu tên", names)
	}
	n, err := s.Drain(t.Context(), d)
	if n != 1 || err == nil {
		t.Fatalf("Drain = %d, %v; muốn 1 và lỗi báo tệp hỏng", n, err)
	}
	if names, _ := s.List(); len(names) != 0 {
		t.Fatalf("tệp hỏng vẫn nằm trong hàng chờ nạp: %v", names)
	}
	left := dirEntries(t, s.Dir)
	var kept bool
	for _, name := range left {
		if bytes.HasSuffix([]byte(name), []byte(".corrupt")) {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("tệp hỏng phải được giữ lại dưới đuôi .corrupt: %v", left)
	}
}

func TestDrainerLoadsSpoolAndReportsGauge(t *testing.T) {
	d := openDB(t)
	s := &webhooks.Spool{Dir: t.TempDir()}
	if _, err := s.Write(entry(`{"drainer":1}`, time.Now())); err != nil {
		t.Fatal(err)
	}
	reg := metrics.NewRegistry()
	dr := &webhooks.Drainer{
		Spool: s, DB: d, Interval: 20 * time.Millisecond, Metrics: reg,
		Logger: slog.New(slog.DiscardHandler),
	}
	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Go(func() { dr.Run(ctx) })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if rows, _ := counts(t, d); rows == 1 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			wg.Wait()
			t.Fatal("Drainer chưa nạp tệp trong 5 s")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	var seen bool
	for _, r := range reg.Drain() {
		if r.Name == "spool_files" && r.Kind == metrics.KindGauge {
			seen = true
		}
	}
	if !seen {
		t.Fatal("Drainer không đặt gauge spool_files")
	}
}

func TestCleanupDeliveriesOlderThan30Days(t *testing.T) {
	d := openDB(t)
	now := time.Now().UTC()
	for i, age := range []time.Duration{31 * 24 * time.Hour, 29 * 24 * time.Hour} {
		e := entry(`{"age":`+string(rune('0'+i))+`}`, now.Add(-age))
		if _, err := webhooks.Save(t.Context(), d, e); err != nil {
			t.Fatal(err)
		}
	}
	var deleted int64
	err := d.RetryTx(t.Context(), dbTxOptions(), func(tx pgx.Tx) error {
		var err error
		deleted, err = webhooks.CleanupDeliveries(context.WithoutCancel(t.Context()), tx, now)
		if err != nil {
			return fmt.Errorf("dọn: %w", err)
		}
		return nil
	})
	if err != nil || deleted != 1 {
		t.Fatalf("CleanupDeliveries = %d, %v; muốn 1", deleted, err)
	}
	if rows, _ := counts(t, d); rows != 1 {
		t.Fatalf("còn %d dòng, muốn 1 (bản 29 ngày)", rows)
	}
}
