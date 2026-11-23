package customer_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/testkit"
)

func TestPersonalDataLogsRedacted(t *testing.T) {
	t.Parallel()
	p, err := customer.NormalizePhone("0912 345 678")
	if err != nil {
		t.Fatal(err)
	}
	l, buf := testkit.CaptureLog(t)
	l.Info("sđt", slog.Any("p", p))
	l.Info("tên", slog.Any("n", customer.Name("Nguyễn Thị Hồng Nhung")))
	l.Info("sinh", slog.Any("b", customer.Birthday(time.Date(1990, 5, 17, 0, 0, 0, 0, time.UTC))))
	l.Info("lồng", slog.Any("m", map[string]any{
		"a": map[string]any{
			"full_name":      "Trần Văn Bình",
			"birthday":       "1991-02-03",
			"recipient_name": "Lê Thị Cúc",
			"email":          "khach@example.test",
			"note":           "giao giờ hành chính",
			"city":           "giữ-lại",
		},
	}))
	out := buf.String()
	for _, leak := range []string{
		"912345", "Hồng Nhung", "1990", "Trần Văn Bình", "1991-02-03", "Lê Thị Cúc", "khach@example.test", "giờ hành chính",
	} {
		if strings.Contains(out, leak) {
			t.Errorf("log còn %q:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "giữ-lại") || strings.Count(out, "[Redacted]") < 8 {
		t.Errorf("phải giữ khóa không nhạy cảm và che 8 giá trị:\n%s", out)
	}
}

func TestPersonalDataLogValue(t *testing.T) {
	t.Parallel()
	for _, v := range []slog.LogValuer{customer.Name("Lan"), customer.Birthday(time.Now())} {
		if got := v.LogValue().String(); got != "[Redacted]" {
			t.Errorf("LogValue() = %q, mong [Redacted]", got)
		}
	}
}
