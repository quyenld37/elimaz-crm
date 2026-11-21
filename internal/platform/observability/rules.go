package observability

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/internal/store"
	"elimaz.com.vn/crm/internal/platform/metrics"
)

// Ngưỡng của luật (kế hoạch M0 bản 5, Task 19; spec 04 mục 8, ADR-42).
const (
	queueLagAfter      = 60 * time.Second
	inDoubtAfter       = 120 * time.Second
	spoolFor           = 5 * time.Minute
	listenDownFor      = 2 * time.Minute
	diskFor            = 2 * time.Minute
	diskMaxPct         = 80
	heartbeatPingFor   = 5 * time.Minute
	backupMaxAge       = 26 * time.Hour
	fileSyncMaxAge     = 90 * time.Minute
	seriesLookback     = 8 * time.Minute
	minuteGapTolerated = 2 * time.Minute
)

// RuleOptions là đầu vào của CheckRules. Now là đồng hồ của lượt (rỗng: đồng hồ máy; test đặt đồng hồ giả): mốc do
// tiến trình Go ghi (chỉ số theo phút, nhịp sống, lần chép tệp) so với Now; mốc do PostgreSQL ghi bằng now() (hàng đợi,
// sao lưu, loại lỗi mới, alert_events, thống kê PostgreSQL) so với now() của PostgreSQL dời cùng độ lệch của Now so với
// đồng hồ máy — mỗi mốc so với đồng hồ của bên ghi, máy app và máy database lệch giờ không đổi kết quả (Task 446).
// BackupExpected bật luật backup-age và backup-verify; FileSyncExpected bật file-sync-age, file-sync-failed,
// file-sync-mismatch (FILESYNC_EXPECTED, Task M1-45); DBDiskBudgetBytes > 0 bật db-size; ConnBudget > 0 bật pg-connections.
type RuleOptions struct {
	Now               time.Time
	BackupExpected    bool
	FileSyncExpected  bool
	DBDiskBudgetBytes int64
	ConnBudget        int
	// dbNow: đồng hồ database của lượt (CheckRules đặt, dbClock).
	dbNow time.Time
}

// alert là một luật đã chạm ngưỡng.
type alert struct{ key, severity, text string }

// CheckRules đọc trạng thái hàng đợi, chỉ số theo phút, nhịp sống, sao lưu và PostgreSQL, rồi Notify mỗi luật chạm
// ngưỡng (Alerter tự chống lặp; Alerter của gói ghi mốc alert_events là lúc đọc theo đồng hồ database). Trả key của
// mọi luật chạm ngưỡng (kể cả khi Alerter không gửi lại). Một lần đọc hay Notify lỗi không chặn các luật khác: lỗi được
// gộp và trả cùng các key đã có; không đọc được đồng hồ database thì không chạy luật nào.
func CheckRules(ctx context.Context, d *db.DB, a Alerter, o RuleOptions) ([]string, error) {
	return checkRules(ctx, d, a, o, time.Now())
}

// checkRules là CheckRules với đồng hồ máy goNow (test giả lệch đồng hồ giữa Go và PostgreSQL).
func checkRules(ctx context.Context, d *db.DB, a Alerter, o RuleOptions, goNow time.Time) ([]string, error) {
	if o.Now.IsZero() {
		o.Now = goNow
	}
	dbNow, err := dbClock(ctx, store.New(d.Pool), o.Now, goNow)
	if err != nil {
		return nil, err
	}
	o.dbNow = dbNow
	var errs []error
	alerts, err := tableRules(ctx, d, o)
	errs = append(errs, err)
	stats, err := readPGStats(ctx, d, o)
	errs = append(errs, err)
	alerts = append(alerts, pgRules(stats, o)...)
	keys := make([]string, 0, len(alerts))
	for _, al := range alerts {
		keys = append(keys, al.key)
		notify := a.Notify
		if na, ok := a.(asOfNotifier); ok {
			notify = func(ctx context.Context, key, severity, text string) error {
				return na.notifyAsOf(ctx, key, severity, text, o.dbNow)
			}
		}
		if err := notify(ctx, al.key, al.severity, al.text); err != nil {
			errs = append(errs, err)
		}
	}
	return keys, errors.Join(errs...)
}

// tableRules: luật đọc bảng của platform (jobs, metrics_minute, process_heartbeats, backup_runs, file_sync_runs,
// error_events). Luật đọc thống kê
// của PostgreSQL ở pgwatch.go.
func tableRules(ctx context.Context, d *db.DB, o RuleOptions) ([]alert, error) {
	q := store.New(d.Pool)
	var out []alert
	var errs []error
	add := func(a []alert, err error) {
		out = append(out, a...)
		errs = append(errs, err)
	}
	add(queueLagRule(ctx, q, o))
	add(terminalRule(ctx, q, o, "dead", "dead:", 0))
	add(terminalRule(ctx, q, o, "in_doubt", "in-doubt:", inDoubtAfter))
	add(metricRules(ctx, q, o))
	add(heartbeatRules(ctx, q, o))
	add(backupRule(ctx, q, o))
	add(fileSyncRules(ctx, q, o))
	add(errorRules(ctx, d, o))
	return out, errors.Join(errs...)
}

// dbClock trả now() của PostgreSQL dời cùng độ lệch của now (đồng hồ của lượt) so với goNow (đồng hồ máy lúc gọi):
// lịch chạy thật now = goNow → đúng now() của PostgreSQL; test đặt now giả lệch d thì đồng hồ database cũng lệch d.
func dbClock(ctx context.Context, q *store.Queries, now, goNow time.Time) (time.Time, error) {
	at, err := q.DBNow(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("đọc đồng hồ database: %w", err)
	}
	return at.Add(now.Sub(goNow)), nil
}

func queueLagRule(ctx context.Context, q *store.Queries, o RuleOptions) ([]alert, error) {
	r, err := q.QueueLag(ctx, store.QueueLagParams{Now: o.dbNow, LagS: queueLagAfter.Seconds()})
	if err != nil {
		return nil, fmt.Errorf("đọc việc chờ: %w", err)
	}
	if r.N == 0 {
		return nil, nil
	}
	return []alert{{
		"queue-lag", SeverityUrgent,
		fmt.Sprintf("queue-lag: %d việc tới hạn chờ quá %d s, lâu nhất %d s", r.N, int(queueLagAfter.Seconds()), int64(r.OldestS)),
	}}, nil
}

// terminalRule: việc status mới (sau lần cảnh báo cuối của cùng key) theo kind; văn bản chỉ có số và mã việc.
func terminalRule(ctx context.Context, q *store.Queries, o RuleOptions, status, prefix string, minAge time.Duration) ([]alert, error) {
	rows, err := q.NewTerminalJobs(ctx, store.NewTerminalJobsParams{
		Status: status, Now: o.dbNow, MinAgeS: minAge.Seconds(), KeyPrefix: prefix,
	})
	if err != nil {
		return nil, fmt.Errorf("đọc việc %s: %w", status, err)
	}
	out := make([]alert, 0, len(rows))
	for _, r := range rows {
		out = append(out, alert{
			prefix + r.Kind, SeverityNormal,
			fmt.Sprintf("%s%s: %d việc %s mới; mã: %s", prefix, r.Kind, r.N, status, joinIDs(r.SampleIds)),
		})
	}
	return out, nil
}

func joinIDs(ids []uuid.UUID) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = id.String()
	}
	return strings.Join(s, ", ")
}

// point là giá trị của một phút trong một chuỗi chỉ số.
type point struct {
	bucket time.Time
	v      float64
}

// streak trả độ dài (tính cả phút cuối) của chuỗi phút liền ở cuối pts (đã xếp theo phút tăng dần) mà ok(v) đúng;
// hở tối đa một phút (lần ghi chỉ số trễ) vẫn tính là liền. Phút cuối cũ hơn 2 phút so với now (tiến trình đã ngừng
// ghi) thì 0.
func streak(pts []point, now time.Time, ok func(float64) bool) time.Duration {
	if len(pts) == 0 {
		return 0
	}
	last := pts[len(pts)-1]
	if last.bucket.Before(now.UTC().Truncate(time.Minute).Add(-minuteGapTolerated)) || !ok(last.v) {
		return 0
	}
	first := last.bucket
	for i := len(pts) - 2; i >= 0; i-- {
		if !ok(pts[i].v) || first.Sub(pts[i].bucket) > minuteGapTolerated {
			break
		}
		first = pts[i].bucket
	}
	return last.bucket.Sub(first) + time.Minute
}

type seriesKey struct{ process, name, label string }

// metricRules: luật đọc chỉ số theo phút (spool_files, rt_listening, disk_used_pct, hooks_blocked,
// heartbeat_ping_failed).
func metricRules(ctx context.Context, q *store.Queries, o RuleOptions) ([]alert, error) {
	rows, err := q.MetricSeries(ctx, store.MetricSeriesParams{
		Names: []string{"spool_files", "rt_listening", "disk_used_pct", "hooks_blocked", "heartbeat_ping_failed"},
		Since: o.Now.UTC().Truncate(time.Minute).Add(-seriesLookback), Now: o.Now,
	})
	if err != nil {
		return nil, fmt.Errorf("đọc chỉ số theo phút: %w", err)
	}
	series := map[seriesKey][]point{}
	var order []seriesKey
	for _, r := range rows {
		k := seriesKey{r.Process, r.Name, r.Label}
		if _, ok := series[k]; !ok {
			order = append(order, k)
		}
		series[k] = append(series[k], point{r.Bucket, r.Sum})
	}
	fired := map[string]bool{}
	var out []alert
	for _, k := range order {
		if a, ok := seriesRule(k, series[k], o.Now); ok && !fired[a.key] {
			fired[a.key] = true
			out = append(out, a)
		}
	}
	return out, nil
}

// seriesRule xét một chuỗi chỉ số (pts khác rỗng, xếp theo phút tăng dần) theo luật của tên chỉ số.
func seriesRule(k seriesKey, pts []point, now time.Time) (alert, bool) {
	positive := func(v float64) bool { return v > 0 }
	last := pts[len(pts)-1].v
	switch k.name {
	case "spool_files":
		if streak(pts, now, positive) >= spoolFor {
			return alert{"spool", SeverityUrgent, fmt.Sprintf("spool: %s còn %d tệp tạm webhook quá 5 phút", k.process, int64(last))}, true
		}
	case "rt_listening":
		if streak(pts, now, func(v float64) bool { return v == 0 }) >= listenDownFor {
			return alert{"listen-down", SeverityUrgent, "listen-down: " + k.process + " mất kết nối LISTEN quá 2 phút"}, true
		}
	case "disk_used_pct":
		if streak(pts, now, func(v float64) bool { return v > diskMaxPct }) >= diskFor {
			return alert{
				"disk:" + k.process, SeverityUrgent,
				fmt.Sprintf("disk:%s: đĩa %s dùng %d%% quá 2 phút", k.process, k.label, int(last)),
			}, true
		}
	case "hooks_blocked":
		if n := sumSince(pts, now.UTC().Truncate(time.Minute).Add(-time.Minute)); n > 0 {
			return alert{
				"hooks-attack", SeverityNormal,
				fmt.Sprintf("hooks-attack: %s chặn %d tin webhook sai chữ ký trong phút qua", k.process, int64(n)),
			}, true
		}
	case "heartbeat_ping_failed":
		if streak(pts, now, positive) >= heartbeatPingFor {
			return alert{"heartbeat-ping", SeverityNormal, "heartbeat-ping: " + k.process + " gửi nhịp sống ra ngoài lỗi 5 phút liền"}, true
		}
	}
	return alert{}, false
}

// sumSince cộng giá trị các phút từ since trở đi.
func sumSince(pts []point, since time.Time) float64 {
	var n float64
	for _, p := range pts {
		if !p.bucket.Before(since) {
			n += p.v
		}
	}
	return n
}

// heartbeatRules: nhịp sống mới nhất của worker, ingest (mọi tiến trình cùng vai trò) cũ hơn 90 s hay chưa có.
func heartbeatRules(ctx context.Context, q *store.Queries, o RuleOptions) ([]alert, error) {
	rows, err := q.HeartbeatAgesByRole(ctx, o.Now)
	if err != nil {
		return nil, fmt.Errorf("đọc nhịp sống: %w", err)
	}
	ages := map[string]float64{}
	for _, r := range rows {
		ages[r.Role] = r.AgeS
	}
	var out []alert
	for _, role := range []string{"worker", "ingest"} {
		age, ok := ages[role]
		switch {
		case !ok:
			out = append(out, alert{role + "-heartbeat", SeverityUrgent, role + "-heartbeat: chưa có nhịp sống"})
		case age > metrics.HeartbeatMaxAge.Seconds():
			out = append(out, alert{
				role + "-heartbeat", SeverityUrgent,
				fmt.Sprintf("%s-heartbeat: nhịp sống cũ %d s (quá %d s)", role, int64(age), int(metrics.HeartbeatMaxAge.Seconds())),
			})
		}
	}
	return out, nil
}

// errorRules: hai luật lỗi ứng dụng của Task 20 (new-error:<dấu vân tay rút gọn>, error-spike) qua ErrorRules.
func errorRules(ctx context.Context, d *db.DB, o RuleOptions) ([]alert, error) {
	hits, err := errorRulesAt(ctx, store.New(d.Pool), o.Now, o.dbNow)
	out := make([]alert, 0, len(hits))
	for _, h := range hits {
		out = append(out, alert{h.Key, h.Severity, h.Text})
	}
	return out, err
}

func backupRule(ctx context.Context, q *store.Queries, o RuleOptions) ([]alert, error) {
	if !o.BackupExpected {
		return nil, nil
	}
	age, err := ageRule(ctx, q, o)
	verify, verr := verifyRule(ctx, q)
	return append(age, verify...), errors.Join(err, verr)
}

// ageRule: không có bản sao lưu đầy đủ thành công trong 26 giờ.
func ageRule(ctx context.Context, q *store.Queries, o RuleOptions) ([]alert, error) {
	at, err := q.LastBackupAt(ctx, "base")
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return []alert{{"backup-age", SeverityUrgent, "backup-age: chưa có bản sao lưu đầy đủ thành công"}}, nil
	case err != nil:
		return nil, fmt.Errorf("đọc lần sao lưu: %w", err)
	}
	if age := o.dbNow.Sub(at); age > backupMaxAge { // finished_at ghi bằng now() (scripts/backup)
		return []alert{{"backup-age", SeverityUrgent, fmt.Sprintf("backup-age: bản sao lưu đầy đủ cuối cũ %d giờ (quá 26 giờ)", int(age.Hours()))}}, nil
	}
	return nil, nil
}

// verifyRule: lần kiểm bản sao lưu hằng tuần gần nhất (scripts/backup/weekly-verify.sh, ADR-37 điều 3) hỏng nghĩa là
// bản sao lưu có thể không khôi phục được. Chưa có lần kiểm nào thì không báo.
func verifyRule(ctx context.Context, q *store.Queries) ([]alert, error) {
	run, err := q.LastBackupRun(ctx, "restore_verify")
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("đọc lần kiểm bản sao lưu: %w", err)
	}
	if run.Ok {
		return nil, nil
	}
	return []alert{{"backup-verify", SeverityUrgent, "backup-verify: lần kiểm bản sao lưu hằng tuần gần nhất hỏng"}}, nil
}

// syncedBuckets là ba bucket logic container filesync chép sang kho ngoài (scripts/filesync/lib.sh; tmp không chép).
// file_sync_runs.bucket ghi tên logic, không có tiền tố S3_BUCKET_PREFIX (Task 410, review độc lập M1 C-06).
// Luật và /health/deep xét từng bucket: một bucket ngừng chép là đủ báo.
func syncedBuckets() []string { return []string{"public", "originals", "private"} }

// fileSyncRules (ADR-36, TP10): file-sync-age gấp khi lần copy ok gần nhất của một bucket cũ hơn 90 phút hay chưa có;
// file-sync-failed khi lần copy gần nhất của một bucket lỗi; file-sync-mismatch khi lần đối chiếu gần nhất của một
// bucket lệch. Văn bản chỉ có tên bucket và số phút.
func fileSyncRules(ctx context.Context, q *store.Queries, o RuleOptions) ([]alert, error) {
	if !o.FileSyncExpected {
		return nil, nil
	}
	rows, err := q.FileSyncStatus(ctx, syncedBuckets())
	if err != nil {
		return nil, fmt.Errorf("đọc lần chép sang kho ngoài: %w", err)
	}
	var stale, failed, mismatch []string
	for _, r := range rows {
		switch {
		case !r.HasOk:
			stale = append(stale, r.Bucket+" chưa có lần chép tốt")
		case o.Now.Sub(r.LastOkAt) > fileSyncMaxAge:
			stale = append(stale, fmt.Sprintf("%s %d phút", r.Bucket, int(o.Now.Sub(r.LastOkAt).Minutes())))
		}
		if r.CopyFailed {
			failed = append(failed, r.Bucket)
		}
		if r.ReconcileFailed {
			mismatch = append(mismatch, r.Bucket)
		}
	}
	var out []alert
	if len(stale) > 0 {
		out = append(out, alert{
			"file-sync-age", SeverityUrgent,
			"file-sync-age: lần chép sang kho ngoài thành công gần nhất quá 90 phút: " + strings.Join(stale, ", "),
		})
	}
	if len(failed) > 0 {
		out = append(out, alert{
			"file-sync-failed", SeverityNormal,
			"file-sync-failed: lần chép sang kho ngoài gần nhất lỗi: " + strings.Join(failed, ", "),
		})
	}
	if len(mismatch) > 0 {
		out = append(out, alert{
			"file-sync-mismatch", SeverityNormal,
			"file-sync-mismatch: đối chiếu gần nhất thấy kho ngoài thiếu tệp: " + strings.Join(mismatch, ", "),
		})
	}
	return out, nil
}
