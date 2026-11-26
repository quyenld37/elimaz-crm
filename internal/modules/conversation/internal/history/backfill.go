package history

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/deps"
	"elimaz.com.vn/crm/internal/modules/conversation/internal/store"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Đọc comment định kỳ (kế hoạch M4, Task 150, Q4): webhook feed có thể không về (ứng dụng ở chế độ phát triển, Meta
// lỗi), nên mỗi 30 giây lịch này ghi lệnh đọc comment (command.fb.comment_poll của channel) cho Page active theo chế độ
// page_settings.comment_poll_mode: on → luôn; auto → khi feed cuối cũ hơn 10 phút (hay chưa từng có) trong giờ bán;
// off → không. Lệnh hỏi bù, sức khỏe, im lặng thuộc channel (health.go); phần này chỉ quyết định Page nào cần đọc,
// vì chế độ là cài đặt hộp chat của conversation.

// CommentPollSchedule là tên lịch đọc comment định kỳ.
const CommentPollSchedule = "channel.comment_poll"

// Chế độ đọc comment (CHECK của page_settings.comment_poll_mode; mặc định của cột là auto).
const (
	PollAuto = "auto"
	PollOn   = "on"
	PollOff  = "off"
)

const (
	commentPollEvery = 30 * time.Second
	// feedQuiet: chế độ auto bật đọc khi webhook feed im lâu hơn chừng này.
	feedQuiet = 10 * time.Minute
	// pollOverlap: mỗi lần đọc lùi chừng này (gối bốn kỳ: một kỳ lỡ vẫn không mất comment; trùng thì khóa chống trùng
	// của webhook loại).
	pollOverlap = 2 * time.Minute
)

// Due báo Page có chế độ mode, mốc feed cuối lastFeed cần đọc comment lúc now.
func Due(mode string, lastFeed *time.Time, now time.Time) bool {
	switch mode {
	case PollOn:
		return true
	case PollAuto:
		return channel.InSalesHours(now) && (lastFeed == nil || now.Sub(*lastFeed) > feedQuiet)
	}
	return false
}

// pollJob là việc của lịch CommentPollSchedule (theo tenant).
type pollJob struct{ d deps.Deps }

// Kind: lỗi thì không chạy lại (kỳ sau có việc mới), quá một kỳ chưa chạy thì expired.
func (pollJob) Kind() queue.Kind {
	return queue.Kind{Name: "task." + CommentPollSchedule, Policy: queue.Policy{MaxAttempts: 1, ExpiresAfter: commentPollEvery}}
}

// Handle ghi lệnh đọc comment cho Page cần đọc; kết quả {"enqueued": n}.
func (h pollJob) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	if j.TenantID == nil {
		return nil, fmt.Errorf("đọc comment định kỳ: việc của lịch thiếu tenant: %w", queue.ErrPermanent)
	}
	now := time.Now().UTC()
	if h.d.Now != nil {
		now = h.d.Now().UTC()
	}
	n, err := enqueuePolls(ctx, h.d, tx, now)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(map[string]int{"enqueued": n})
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả đọc comment định kỳ: %w", err)
	}
	return out, nil
}

// enqueuePolls ghi lệnh đọc comment từ now − 2 phút cho mỗi Page active (kênh có adapter) mà Due.
func enqueuePolls(ctx context.Context, d deps.Deps, tx pgx.Tx, now time.Time) (int, error) {
	as, err := channel.ListAccounts(ctx, tx, nil)
	if err != nil {
		return 0, err //nolint:wrapcheck // channel đã bọc việc đang làm
	}
	var live []channel.ChannelAccount
	ids := []uuid.UUID{}
	for _, a := range as {
		if a.Status != channel.StatusActive {
			continue
		}
		if _, ok := d.Registry.Lookup(a.Channel); ok {
			live = append(live, a)
			ids = append(ids, a.ID)
		}
	}
	if len(live) == 0 {
		return 0, nil
	}
	rows, err := store.New(tx).CommentPollModes(ctx, ids)
	if err != nil {
		return 0, fmt.Errorf("đọc chế độ đọc comment: %w", err)
	}
	modes := make(map[uuid.UUID]string, len(rows))
	for _, r := range rows {
		modes[r.ChannelAccountID] = r.CommentPollMode
	}
	n := 0
	for _, a := range live {
		mode, ok := modes[a.ID]
		if !ok {
			mode = PollAuto
		}
		if !Due(mode, a.LastFeedAt, now) {
			continue
		}
		if err := channel.EnqueueCommentPoll(ctx, tx, a, now.Add(-pollOverlap), now); err != nil {
			return n, err //nolint:wrapcheck // channel đã bọc việc đang làm
		}
		n++
	}
	return n, nil
}

// pollParts: lịch đọc comment định kỳ và việc của nó.
func pollParts(d deps.Deps) deps.Parts {
	h := pollJob{d: d}
	return deps.Parts{
		Jobs:      []queue.Handler{h},
		Schedules: []schedules.Schedule{{Name: CommentPollSchedule, Every: commentPollEvery, PerTenant: true, Kind: h.Kind()}},
	}
}
