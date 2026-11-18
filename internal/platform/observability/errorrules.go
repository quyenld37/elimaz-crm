package observability

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
)

const (
	// newErrorLookback: loại lỗi gặp lần đầu trong chừng này mà chưa từng được báo là lỗi mới (luật new-error). Dài
	// hơn chu kỳ 60 s của platform.alerts để Telegram lỗi hay lịch chạy trễ không làm mất cảnh báo.
	newErrorLookback = time.Hour
	// spikeWindow, spikeThreshold: quá spikeThreshold lỗi trong spikeWindow là tăng vọt (luật error-spike).
	spikeWindow    = 5 * time.Minute
	spikeThreshold = 50
	// keyPrefixLen: số ký tự đầu của dấu vân tay trong key new-error.
	keyPrefixLen   = 8
	severityNormal = "normal"
)

// RuleHit là một luật cảnh báo đang chạm ngưỡng: key chống lặp, mức (normal/urgent), văn bản gửi đi. Văn bản chỉ có
// tên luật, số lượng, dấu vân tay rút gọn; không có nội dung lỗi.
type RuleHit struct {
	Key, Severity, Text string
}

// ErrorRules trả hai luật về lỗi ứng dụng tại now (spec 04 mục "Gom lỗi ứng dụng"):
//   - new-error:<8 ký tự đầu dấu vân tay> (normal): loại lỗi có first_seen_at trong 1 giờ qua và chưa có
//     alert_events của key đó (báo một lần, lần gửi hỏng thì lượt sau báo lại);
//   - error-spike (normal): tổng counter errors của mọi tiến trình trong 5 phút gần nhất vượt 50.
//
// Bộ chạy luật (CheckRules, Task 19) gửi từng RuleHit qua Alerter.
func ErrorRules(ctx context.Context, d *db.DB, now time.Time) ([]RuleHit, error) {
	q := store.New(d.Pool)
	fps, err := q.ListNewErrorFingerprints(ctx, store.ListNewErrorFingerprintsParams{
		Since: now.Add(-newErrorLookback), PrefixLen: keyPrefixLen,
	})
	if err != nil {
		return nil, fmt.Errorf("đọc lỗi mới: %w", err)
	}
	hits := make([]RuleHit, 0, len(fps)+1)
	for _, fp := range fps {
		short := fp[:min(len(fp), keyPrefixLen)]
		hits = append(hits, RuleHit{Key: "new-error:" + short, Severity: severityNormal, Text: "Loại lỗi mới " + short})
	}
	total, err := q.SumErrorsSince(ctx, now.Add(-spikeWindow))
	if err != nil {
		return nil, fmt.Errorf("đếm lỗi 5 phút: %w", err)
	}
	if total > spikeThreshold {
		hits = append(hits, RuleHit{
			Key: "error-spike", Severity: severityNormal,
			Text: "Lỗi tăng vọt: " + strconv.FormatInt(total, 10) + " lỗi trong 5 phút",
		})
	}
	return hits, nil
}
