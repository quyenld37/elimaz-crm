package lists_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"elimaz.com.vn/crm/internal/modules/fulfillment/internal/lists"
)

func ictZone() *time.Location { return time.FixedZone("ICT", 7*60*60) }

// Cảnh báo ngày: lượt đầu tiên có giờ Việt Nam ≥ 08:00 (ngày theo giờ Việt Nam); trước 08:00 không gửi.
func TestAlertDay(t *testing.T) {
	ict := ictZone()
	for _, c := range []struct {
		name string
		at   time.Time
		day  string
		ok   bool
	}{
		{"07:59 giờ VN", time.Date(2026, 11, 21, 7, 59, 0, 0, ict), "", false},
		{"08:00 giờ VN", time.Date(2026, 11, 21, 8, 0, 0, 0, ict), "20261121", true},
		{"23:30 giờ VN", time.Date(2026, 11, 21, 23, 30, 0, 0, ict), "20261121", true},
		// 01:00 UTC là 08:00 VN cùng ngày; 17:30 UTC ngày 21 là 00:30 VN ngày 22 (chưa tới 08:00).
		{"01:00 UTC", time.Date(2026, 11, 21, 1, 0, 0, 0, time.UTC), "20261121", true},
		{"17:30 UTC", time.Date(2026, 11, 21, 17, 30, 0, 0, time.UTC), "", false},
	} {
		day, ok := lists.AlertDay(c.at)
		if day != c.day || ok != c.ok {
			t.Errorf("%s: AlertDay = %q %v, muốn %q %v", c.name, day, ok, c.day, c.ok)
		}
	}
}

// Văn bản cảnh báo chỉ có số lượng theo trạng thái (thứ tự cố định), không gửi khi tổng 0.
func TestAlertText(t *testing.T) {
	got := lists.AlertText(map[string]int64{"shipping": 1, "awaiting_pickup": 1, "returning": 0})
	if !strings.Contains(got, "awaiting_pickup: 1, shipping: 1") || strings.Contains(got, "returning") {
		t.Fatalf("văn bản = %q", got)
	}
	if !strings.Contains(got, "2 vận đơn") {
		t.Fatalf("văn bản thiếu tổng: %q", got)
	}
	if lists.AlertText(map[string]int64{"shipping": 0}) != "" || lists.AlertText(nil) != "" {
		t.Fatal("tổng 0 vẫn có văn bản")
	}
	// Trạng thái lạ (không trong danh sách) vẫn được đếm, sau các trạng thái biết.
	if got := lists.AlertText(map[string]int64{"zzz": 2, "creating": 1}); !strings.Contains(got, "creating: 1, zzz: 2") {
		t.Fatalf("văn bản = %q", got)
	}
}

func TestHoursSince(t *testing.T) {
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		at   time.Time
		want int32
	}{
		{now.Add(-73 * time.Hour), 73}, {now.Add(-90 * time.Minute), 1}, {now.Add(time.Hour), 0}, {now, 0},
	} {
		if got := lists.HoursSince(now, c.at); got != c.want {
			t.Errorf("HoursSince(%v) = %d, muốn %d", c.at, got, c.want)
		}
	}
}

// Con trỏ: qua chuỗi rồi đọc lại ra đúng giá trị (micro giây, UTC); chuỗi hỏng → ErrBadCursor.
func TestCursor_RoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		at := time.UnixMicro(rapid.Int64Range(0, 1<<52).Draw(t, "micros")).UTC()
		var id uuid.UUID
		copy(id[:], rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(t, "id"))
		c, err := lists.ParseCursor(lists.Cursor{At: at, ID: id}.String())
		if err != nil || !c.At.Equal(at) || c.ID != id || c.At.Location() != time.UTC {
			t.Fatalf("đọc lại = %+v %v, muốn %v %v", c, err, at, id)
		}
	})
	for _, bad := range []string{"", "x", "AAAA", strings.Repeat("A", 40), "!!!!"} {
		if _, err := lists.ParseCursor(bad); !errors.Is(err, lists.ErrBadCursor) {
			t.Errorf("ParseCursor(%q) = %v, muốn ErrBadCursor", bad, err)
		}
	}
}
