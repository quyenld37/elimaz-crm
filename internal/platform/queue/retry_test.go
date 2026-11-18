package queue_test

import (
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/platform/queue"
)

func half() float64 { return 0.5 }

// Lịch thử lại của spec 04 mục 5.1: 5 s, 30 s, 2 phút, 10 phút, 1 giờ, 6 giờ, các lần sau giữ 6 giờ; jitter ±20%.
func TestNextDelaySchedule(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 5 * time.Second},
		{2, 30 * time.Second},
		{3, 2 * time.Minute},
		{4, 10 * time.Minute},
		{5, time.Hour},
		{6, 6 * time.Hour},
		{7, 6 * time.Hour},
		{9, 6 * time.Hour},
		{100, 6 * time.Hour},
	}
	for _, c := range cases {
		if got := queue.NextDelay(c.attempt, half); got != c.want {
			t.Errorf("NextDelay(%d, 0.5) = %v, muốn %v", c.attempt, got, c.want)
		}
	}
}

func TestNextDelayJitterBounds(t *testing.T) {
	for attempt := 1; attempt <= 8; attempt++ {
		base := queue.RetrySchedule[min(attempt-1, len(queue.RetrySchedule)-1)]
		lo := queue.NextDelay(attempt, func() float64 { return 0 })
		hi := queue.NextDelay(attempt, func() float64 { return 1 })
		if lo != base*8/10 {
			t.Errorf("attempt %d, rnd 0: %v, muốn %v (−20%%)", attempt, lo, base*8/10)
		}
		if hi != base*12/10 {
			t.Errorf("attempt %d, rnd 1: %v, muốn %v (+20%%)", attempt, hi, base*12/10)
		}
	}
}

// attempt < 1 không có nghĩa; coi như lần đầu thay vì panic vì chỉ số âm.
func TestNextDelayAttemptZero(t *testing.T) {
	if got := queue.NextDelay(0, half); got != 5*time.Second {
		t.Fatalf("NextDelay(0, 0.5) = %v", got)
	}
}
