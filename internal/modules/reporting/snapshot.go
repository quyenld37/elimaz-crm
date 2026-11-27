package reporting

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/reporting/internal/store"
	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/platform/queue"
	"elimaz.com.vn/crm/internal/platform/schedules"
)

// Chụp tồn hằng ngày (BC03, kế hoạch M6 Task 233, Q7). Mốc chụp là 24:00 giờ Việt Nam; giá trị của một ngày = số dư
// hiện tại − tổng sổ tồn ghi từ mốc trở đi, trong một câu SQL (queries/reporting/snapshot.sql). Lịch chạy mỗi 15 phút
// theo tenant; chỉ chụp ngày có mốc đã qua SnapshotGrace; mỗi lượt bù tối đa SnapshotMaxDays ngày thiếu. Bản chụp chỉ
// ghi thêm: chụp xong một ngày thì không đổi, kể cả khi sổ ghi muộn có mốc trước đó.

// SnapshotKindName là tên việc giao dịch chụp tồn (theo tenant).
const SnapshotKindName = "reporting.stock_snapshot"

// Lịch và giới hạn của chụp tồn (Q7).
const (
	// SnapshotEvery là chu kỳ của lịch reporting.stock_snapshot.
	SnapshotEvery = 15 * time.Minute
	// SnapshotGrace: chỉ chụp ngày có mốc 24:00 đã qua ít nhất chừng này (transaction ghi sổ bắt đầu trước mốc kịp
	// commit; hạn câu lệnh của ứng dụng là 30 s).
	SnapshotGrace = 10 * time.Minute
	// SnapshotMaxDays là số ngày tối đa một lượt chụp (bù ngày thiếu dần qua các lượt, giữ lượt dưới hạn 30 s).
	SnapshotMaxDays = 7
)

// SnapshotKind là loại việc chụp tồn: chính sách mặc định, không hết hạn, để việc lỗi mãi đi đủ 8 lần tới dead và bắn
// cảnh báo dead:reporting.stock_snapshot của M0 (hết hạn ngắn sẽ biến nó thành expired, không có cảnh báo).
func SnapshotKind() queue.Kind {
	return queue.Kind{Name: SnapshotKindName, Policy: queue.DefaultPolicy}
}

// SnapshotHandler chụp tồn những ngày chưa chụp (việc reporting.stock_snapshot).
type SnapshotHandler struct {
	// Now là đồng hồ (test cố định giờ).
	Now func() time.Time
	// Grace: xem SnapshotGrace.
	Grace time.Duration
	// MaxDays: xem SnapshotMaxDays.
	MaxDays int
	// Metrics nhận gauge stock_snapshot_lag_days (nil: không ghi).
	Metrics *metrics.Registry
}

// NewSnapshotHandler dựng việc chụp tồn với giới hạn của lịch thật; đồng hồ là d.Now (nil: time.Now).
func NewSnapshotHandler(d Deps) *SnapshotHandler {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &SnapshotHandler{Now: now, Grace: SnapshotGrace, MaxDays: SnapshotMaxDays, Metrics: d.Metrics}
}

// Kind là SnapshotKind.
func (*SnapshotHandler) Kind() queue.Kind { return SnapshotKind() }

// snapshotOutput là kết quả của việc: các ngày lượt này chụp và tổng số dòng chụp (không có dữ liệu cá nhân).
type snapshotOutput struct {
	Days []string `json:"days"`
	Rows int64    `json:"rows"`
}

// Handle chụp các ngày DaysToSnapshot trả về, tăng dần, trong tx của việc (WithTenant). Ngày mà lượt khác đã chụp
// (chạy trùng) không có trong kết quả. Chạy lại được: ngày đã chụp bị bỏ qua nhờ khóa (tenant_id, day).
func (h *SnapshotHandler) Handle(ctx context.Context, j queue.Job, tx pgx.Tx) (json.RawMessage, error) {
	if j.TenantID == nil {
		return nil, fmt.Errorf("việc %s: thiếu tenant: %w", j.ID, queue.ErrPermanent)
	}
	q := store.New(tx)
	b, err := q.SnapshotBounds(ctx)
	if err != nil {
		return nil, fmt.Errorf("đọc mốc chụp tồn: %w", err)
	}
	last, first := optional(b.LastDay != "", b.LastDay), optional(b.HasLedger, b.FirstLedgerAt)
	now := h.Now()
	days := DaysToSnapshot(last, first, now, h.Grace, h.MaxDays)
	out := snapshotOutput{Days: []string{}}
	for _, day := range days {
		cutoff, err := DayCutoff(day)
		if err != nil {
			return nil, fmt.Errorf("mốc chụp tồn %s: %w", day, err)
		}
		r, err := q.TakeSnapshot(ctx, store.TakeSnapshotParams{Day: day, Cutoff: cutoff})
		if err != nil {
			return nil, fmt.Errorf("chụp tồn ngày %s: %w", day, err)
		}
		if r.Taken > 0 {
			out.Days = append(out.Days, day)
			out.Rows += int64(r.RowCount)
		}
	}
	if len(days) > 0 {
		last = &days[len(days)-1]
	}
	if h.Metrics != nil {
		h.Metrics.Gauge("stock_snapshot_lag_days", float64(missingDays(last, first, now, h.Grace)), j.TenantID.String())
	}
	res, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("mã hóa kết quả chụp tồn: %w", err)
	}
	return res, nil
}

// optional trả &v khi ok, nil khi không.
func optional[T any](ok bool, v T) *T {
	if !ok {
		return nil
	}
	return &v
}

// DaysToSnapshot là các ngày (YYYY-MM-DD giờ VN) cần chụp, tăng dần, tối đa maxDays: từ max(last + 1, ngày VN của dòng sổ
// đầu tiên) tới ngày cuối có DayCutoff(day) ≤ now − grace. Chưa có sổ (firstLedger nil) hay last sai dạng → không ngày
// nào.
func DaysToSnapshot(last *string, firstLedger *time.Time, now time.Time, grace time.Duration, maxDays int) []string {
	start, end, ok := snapshotRange(last, firstLedger, now, grace)
	if !ok || maxDays <= 0 {
		return nil
	}
	var out []string
	for d := start; !d.After(end) && len(out) < maxDays; d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format(dayLayout))
	}
	return out
}

// missingDays là số ngày còn phải chụp sau ngày last (gauge stock_snapshot_lag_days).
func missingDays(last *string, firstLedger *time.Time, now time.Time, grace time.Duration) int {
	start, end, ok := snapshotRange(last, firstLedger, now, grace)
	if !ok || start.After(end) {
		return 0
	}
	return int(end.Sub(start).Hours()/24) + 1
}

// snapshotRange là ngày đầu và ngày cuối (00:00 UTC của ngày lịch, để cộng ngày không lệch) cần chụp; ok = false khi
// chưa có sổ hay last sai dạng.
func snapshotRange(last *string, firstLedger *time.Time, now time.Time, grace time.Duration) (start, end time.Time, ok bool) {
	if firstLedger == nil {
		return time.Time{}, time.Time{}, false
	}
	start = calendarDay(Today(*firstLedger))
	if last != nil {
		l, err := time.Parse(dayLayout, *last)
		if err != nil {
			return time.Time{}, time.Time{}, false
		}
		if next := l.AddDate(0, 0, 1); next.After(start) {
			start = next
		}
	}
	// DayCutoff(d) = 00:00 giờ VN của d + 1 ≤ now − grace ⇔ d ≤ (ngày VN của now − grace) − 1.
	end = calendarDay(Today(now.Add(-grace))).AddDate(0, 0, -1)
	return start, end, true
}

// calendarDay đọc ngày YYYY-MM-DD do Today trả (luôn đúng dạng) thành 00:00 UTC của ngày đó.
func calendarDay(day string) time.Time {
	t, _ := time.Parse(dayLayout, day)
	return t
}

// snapshotSchedules là lịch reporting.stock_snapshot (mỗi 15 phút, theo tenant).
func snapshotSchedules() []schedules.Schedule {
	return []schedules.Schedule{{Name: SnapshotKindName, Every: SnapshotEvery, PerTenant: true, Kind: SnapshotKind()}}
}
