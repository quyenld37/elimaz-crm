package reporting_test

import (
	"errors"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/reporting"
)

func mustUTC(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Khoảng ngày giờ Việt Nam đổi ra UTC: Start là 00:00 giờ VN của from, End (loại trừ) là 00:00 giờ VN của ngày sau to.
func TestParsePeriod(t *testing.T) {
	t.Parallel()
	p, err := reporting.ParsePeriod("2026-11-01", "2026-11-30", 366)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Start.Equal(mustUTC(t, "2026-10-31T17:00:00Z")) || !p.End.Equal(mustUTC(t, "2026-11-30T17:00:00Z")) {
		t.Fatalf("Start, End = %s, %s", p.Start, p.End)
	}
	if p.Start.Location() != time.UTC || p.End.Location() != time.UTC {
		t.Fatalf("Start, End phải là UTC: %s, %s", p.Start.Location(), p.End.Location())
	}
	if p.FromDay != "2026-11-01" || p.ToDay != "2026-11-30" || p.Days() != 30 {
		t.Fatalf("FromDay, ToDay, Days = %s, %s, %d", p.FromDay, p.ToDay, p.Days())
	}
	one, err := reporting.ParsePeriod("2026-11-17", "2026-11-17", 366)
	if err != nil || one.Days() != 1 || one.End.Sub(one.Start) != 24*time.Hour {
		t.Fatalf("một ngày: %+v, %v", one, err)
	}
}

func TestParsePeriodErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, from, to string
		want           error
	}{
		{"from sau to", "2026-11-30", "2026-11-01", reporting.ErrFromAfterTo},
		{"367 ngày", "2025-01-01", "2026-01-02", reporting.ErrRangeTooLong},
		{"ngày không có", "2026-02-30", "2026-03-01", reporting.ErrBadDate},
		{"to không có", "2026-02-01", "2026-02-29", reporting.ErrBadDate},
		{"sai dạng", "2026-11-1", "2026-11-02", reporting.ErrBadDate},
		{"có giờ", "2026-11-01T00:00:00Z", "2026-11-02", reporting.ErrBadDate},
		{"rỗng", "", "2026-11-02", reporting.ErrBadDate},
		{"khoảng trắng", " 2026-11-01", "2026-11-02", reporting.ErrBadDate},
	}
	for _, c := range cases {
		if _, err := reporting.ParsePeriod(c.from, c.to, 366); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, mong %v", c.name, err, c.want)
		}
	}
	// Đúng 366 ngày (năm nhuận 2024) thì được.
	p, err := reporting.ParsePeriod("2024-01-01", "2024-12-31", 366)
	if err != nil || p.Days() != 366 {
		t.Fatalf("366 ngày: %d, %v", p.Days(), err)
	}
	if _, err := reporting.ParsePeriod("2025-01-01", "2026-01-01", 366); err != nil {
		t.Fatalf("366 ngày (2025-01-01..2026-01-01): %v", err)
	}
}

func TestTodayAndDayCutoff(t *testing.T) {
	t.Parallel()
	if got := reporting.Today(mustUTC(t, "2026-11-01T16:59:59Z")); got != "2026-11-01" {
		t.Fatalf("Today 23:59:59 giờ VN = %s", got)
	}
	if got := reporting.Today(mustUTC(t, "2026-11-01T17:00:00Z")); got != "2026-11-02" {
		t.Fatalf("Today 00:00 giờ VN = %s", got)
	}
	c, err := reporting.DayCutoff("2026-11-17")
	if err != nil || !c.Equal(mustUTC(t, "2026-11-17T17:00:00Z")) || c.Location() != time.UTC {
		t.Fatalf("DayCutoff = %s, %v", c, err)
	}
	if _, err := reporting.DayCutoff("2026-13-01"); !errors.Is(err, reporting.ErrBadDate) {
		t.Fatalf("DayCutoff ngày sai: %v", err)
	}
	if reporting.VN.String() != "ICT" {
		t.Fatalf("VN = %s", reporting.VN)
	}
	if _, off := time.Date(2026, 6, 1, 0, 0, 0, 0, reporting.VN).Zone(); off != 7*3600 {
		t.Fatalf("độ lệch VN = %d", off)
	}
}
