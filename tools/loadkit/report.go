package main

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

// Chấm kết quả một bài thử theo ngưỡng đạt của kế hoạch M0 Task 22 (spec 04 mục 1, ADR-42).

const (
	modeNormal = "normal"
	modeHeavy  = "heavy"
	modeChaos  = "chaos"
	modeSoak   = "soak"

	statusPass = "ĐẠT"
	statusFail = "KHÔNG ĐẠT"
	statusNA   = "CHƯA ĐO"

	critWebhookP95     = "Nhận webhook p95 < 300 ms"
	critPipeP95        = "Webhook tới báo hiệu p95 < 2 s"
	critPipeP99        = "Webhook tới báo hiệu p99 < 5 s"
	critPickupP95      = "Việc nền ghi tới nhận p95 < 1 s"
	critPickupP99      = "Việc nền ghi tới nhận p99 < 3 s"
	critReadP95        = "API đọc p95 < 200 ms"
	critReadP99        = "API đọc p99 < 500 ms"
	critWebhookNon200  = "Không webhook nào nhận mã khác 200"
	critSSEExactlyOnce = "Mỗi eventId tới mỗi client đúng một lần"
	critSSENoDup       = "Không client nào nhận trùng eventId"
	critNoLoss         = "Không mất sự kiện (mọi webhook 200 có việc inbound done)"
	critCommandsOnce   = "Không lệnh nào gửi hai lần"
	critRecovery       = "Tự trở lại trong 2 phút"
	critSoakMemory     = "Bộ nhớ không tăng quá 20% sau giờ đầu"
	critSoakDeadTuples = "Dòng chết của platform.jobs < 50.000"
	critSoakJobsSize   = "Bảng jobs không lớn thêm sau giờ thứ hai"

	recoveryLimitSec = 120
	maxMemGrowth     = 0.20
	maxDeadTuples    = 50_000
)

// k6Metrics là data.metrics của handleSummary (common.js ghi nguyên dạng).
type k6Metrics map[string]struct {
	Values map[string]float64 `json:"values"`
}

// statSample là một dòng docker stats.
type statSample struct {
	At       time.Time
	Service  string
	CPUPct   float64
	MemBytes int64
}

// chaosEvent là một lần hỏng của chaos.sh: thời gian từ lúc gây hỏng tới lúc hệ thống trở lại.
type chaosEvent struct {
	Fault       string
	RecoverySec float64
	Recovered   bool
}

type reportInput struct {
	Mode     string
	Start    time.Time // lúc bắt đầu tải
	K6       k6Metrics
	SSE      *sseResult
	DB       dbStats   // số đo cuối (sau khi hàng đợi đã cạn)
	DBSeries []dbStats // mẫu theo thời gian (bài ngâm)
	Stats    []statSample
	Chaos    []chaosEvent
}

type verdict struct {
	Name, Want, Got, Status string
}

func (in reportInput) k6Value(metric, stat string) (float64, bool) {
	m, ok := in.K6[metric]
	if !ok {
		return 0, false
	}
	v, ok := m.Values[stat]
	return v, ok
}

// below: đạt khi got < limit.
func below(name string, got float64, ok bool, limit float64, unit string) verdict {
	v := verdict{Name: name, Want: fmt.Sprintf("< %g %s", limit, unit), Status: statusNA, Got: "không có số đo"}
	if !ok {
		return v
	}
	v.Got = fmt.Sprintf("%.1f %s", got, unit)
	v.Status = passIf(got < limit)
	return v
}

func passIf(ok bool) string {
	if ok {
		return statusPass
	}
	return statusFail
}

// pickup chấm phân vị pct của histogram nhận việc: cận trên của ngăn chứa phân vị phải ≤ limit (ms). Histogram chỉ
// biết cận ngăn (50, 100, …, 1000, 2000, 5000), nên "≤ cận" là cách chấm chặt nhất có được.
func pickup(name string, hist []int, pct int, limit float64) verdict {
	v := verdict{Name: name, Want: fmt.Sprintf("≤ %g ms (cận ngăn histogram)", limit), Status: statusNA, Got: "không có số đo"}
	total := 0
	for _, n := range hist {
		total += n
	}
	if total == 0 {
		return v
	}
	q := metrics.QuantileFromHist(hist, pct)
	v.Got = fmt.Sprintf("≤ %g ms (%d lần nhận việc)", q, total)
	v.Status = passIf(q <= limit)
	return v
}

func evaluate(in reportInput) []verdict {
	var out []verdict
	ok, haveOK := in.k6Value("webhook_ok", "count")
	if in.Mode != modeChaos {
		p95, has := in.k6Value("webhook_ms", "p(95)")
		out = append(out, below(critWebhookP95, p95, has, 300, "ms"))
		if in.SSE != nil {
			out = append(out, below(critPipeP95, in.SSE.Latency.P95, in.SSE.Latency.Count > 0, 2000, "ms"),
				below(critPipeP99, in.SSE.Latency.P99, in.SSE.Latency.Count > 0, 5000, "ms"))
		} else {
			out = append(out, below(critPipeP95, 0, false, 2000, "ms"), below(critPipeP99, 0, false, 5000, "ms"))
		}
		out = append(out, pickup(critPickupP95, in.DB.PickupHist, 95, 1000), pickup(critPickupP99, in.DB.PickupHist, 99, 3000))
		r95, has95 := in.k6Value("read_ms", "p(95)")
		r99, has99 := in.k6Value("read_ms", "p(99)")
		out = append(out, below(critReadP95, r95, has95, 200, "ms"), below(critReadP99, r99, has99, 500, "ms"))
	}
	bad, haveBad := in.k6Value("webhook_bad", "count")
	out = append(out, countZero(critWebhookNon200, bad, haveBad))
	if in.Mode == modeChaos {
		out = append(out, sseNoDup(in.SSE))
	} else {
		out = append(out, sseExactlyOnce(in.SSE, ok, haveOK))
	}
	out = append(out, noLoss(in.DB, ok, haveOK))
	if in.Mode == modeChaos {
		out = append(out, commandsOnce(in.DB))
		out = append(out, recovery(in.Chaos)...)
	}
	if in.Mode == modeSoak {
		out = append(out, soakMemory(in)...)
		out = append(out, soakDeadTuples(in), soakJobsSize(in))
	}
	return out
}

func countZero(name string, got float64, ok bool) verdict {
	v := verdict{Name: name, Want: "0", Status: statusNA, Got: "không có số đo"}
	if ok {
		v.Got = strconv.FormatFloat(got, 'f', 0, 64)
		v.Status = passIf(got == 0)
	}
	return v
}

func sseExactlyOnce(s *sseResult, sent float64, haveSent bool) verdict {
	v := verdict{
		Name: critSSEExactlyOnce, Want: "mỗi client: duy nhất = số webhook 200, trùng = 0", Status: statusNA,
		Got: "không có số đo",
	}
	if s == nil || len(s.Clients) == 0 || !haveSent {
		return v
	}
	worstMiss, dups := int64(0), int64(0)
	for _, c := range s.Clients {
		worstMiss = max(worstMiss, int64(sent)-c.Unique)
		dups += c.Duplicates
	}
	v.Got = fmt.Sprintf("%d client; thiếu nhiều nhất %d; trùng %d", len(s.Clients), worstMiss, dups)
	ok := dups == 0
	for _, c := range s.Clients {
		ok = ok && c.Unique == int64(sent)
	}
	v.Status = passIf(ok)
	return v
}

func sseNoDup(s *sseResult) verdict {
	v := verdict{Name: critSSENoDup, Want: "0", Status: statusNA, Got: "không có số đo"}
	if s == nil || len(s.Clients) == 0 {
		return v
	}
	dups := int64(0)
	for _, c := range s.Clients {
		dups += c.Duplicates
	}
	v.Got = strconv.FormatInt(dups, 10)
	v.Status = passIf(dups == 0)
	return v
}

func noLoss(d dbStats, sent float64, haveSent bool) verdict {
	v := verdict{Name: critNoLoss, Want: "done ≥ số webhook 200, chưa done = 0", Status: statusNA, Got: "không có số đo"}
	if !haveSent {
		return v
	}
	v.Got = fmt.Sprintf("webhook 200: %.0f; done: %d; chưa done: %d", sent, d.InboundDone, d.InboundOther)
	v.Status = passIf(d.InboundOther == 0 && float64(d.InboundDone) >= sent)
	return v
}

func commandsOnce(d dbStats) verdict {
	v := verdict{Name: critCommandsOnce, Want: "lệnh có attempts > 1 = 0", Status: statusNA, Got: "không có lệnh nào"}
	if d.CommandsTotal == 0 {
		return v
	}
	v.Got = fmt.Sprintf("%d lệnh; gửi hơn một lần: %d; done %d; in_doubt %d", d.CommandsTotal, d.CommandsResent,
		d.CommandsDone, d.CommandsInDoubt)
	v.Status = passIf(d.CommandsResent == 0)
	return v
}

func recovery(events []chaosEvent) []verdict {
	if len(events) == 0 {
		return []verdict{{Name: critRecovery, Want: "≤ 120 s", Status: statusNA, Got: "không có lần hỏng nào"}}
	}
	out := make([]verdict, 0, len(events))
	for _, e := range events {
		v := verdict{Name: critRecovery + ": " + e.Fault, Want: "≤ 120 s", Status: statusFail, Got: "không trở lại"}
		if e.Recovered {
			v.Got = fmt.Sprintf("%.1f s", e.RecoverySec)
			v.Status = passIf(e.RecoverySec <= recoveryLimitSec)
		}
		out = append(out, v)
	}
	return out
}

// appServices là các tiến trình ứng dụng có trong mẫu (crm-*), theo thứ tự tên.
func appServices(samples []statSample) []string {
	var out []string
	for _, s := range samples {
		if strings.HasPrefix(s.Service, "crm-") && !slices.Contains(out, s.Service) {
			out = append(out, s.Service)
		}
	}
	slices.Sort(out)
	return out
}

// soakMemory: mỗi tiến trình, bộ nhớ ở mẫu cuối so với mẫu đầu tiên từ giờ thứ nhất trở đi.
func soakMemory(in reportInput) []verdict {
	services := appServices(in.Stats)
	if len(services) == 0 {
		return []verdict{{Name: critSoakMemory, Want: "≤ +20%", Status: statusNA, Got: "không có mẫu docker stats"}}
	}
	out := make([]verdict, 0, len(services))
	for _, svc := range services {
		var base, last *statSample
		for i := range in.Stats {
			s := &in.Stats[i]
			if s.Service != svc {
				continue
			}
			if base == nil && !s.At.Before(in.Start.Add(time.Hour)) {
				base = s
			}
			if last == nil || s.At.After(last.At) {
				last = s
			}
		}
		v := verdict{Name: critSoakMemory + ": " + svc, Want: "≤ +20%", Status: statusNA, Got: "chạy chưa quá giờ đầu"}
		if base != nil && last.At.After(base.At) && base.MemBytes > 0 {
			growth := float64(last.MemBytes)/float64(base.MemBytes) - 1
			v.Got = fmt.Sprintf("%s → %s (%+.1f%%)", mib(base.MemBytes), mib(last.MemBytes), growth*100)
			v.Status = passIf(growth <= maxMemGrowth)
		}
		out = append(out, v)
	}
	return out
}

func soakDeadTuples(in reportInput) verdict {
	worst := in.DB.JobsDeadTuples
	for _, s := range in.DBSeries {
		worst = max(worst, s.JobsDeadTuples)
	}
	return verdict{
		Name: critSoakDeadTuples, Want: "< 50000", Got: strconv.FormatInt(worst, 10) + " (lớn nhất)",
		Status: passIf(worst < maxDeadTuples),
	}
}

func soakJobsSize(in reportInput) verdict {
	v := verdict{Name: critSoakJobsSize, Want: "cuối ≤ lúc giờ thứ hai", Status: statusNA, Got: "chạy chưa quá giờ thứ hai"}
	series := slices.Clone(in.DBSeries)
	slices.SortFunc(series, func(a, b dbStats) int { return a.At.Compare(b.At) })
	for _, s := range series {
		if s.At.Before(in.Start.Add(2 * time.Hour)) {
			continue
		}
		last := series[len(series)-1]
		if !last.At.After(s.At) {
			return v
		}
		v.Got = fmt.Sprintf("%d B → %d B", s.JobsBytes, last.JobsBytes)
		v.Status = passIf(last.JobsBytes <= s.JobsBytes)
		return v
	}
	return v
}

func mib(b int64) string { return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20)) }

var errStatsLine = errors.New("dòng docker stats sai dạng")

// parseDockerStats đọc các dòng "<epoch>,<tên container>,<CPU%>,<dùng> / <giới hạn>" (run.sh ghi). Tên container
// bỏ tiền tố "<project>-" và hậu tố "-<số>". Dòng sai dạng bị bỏ qua.
func parseDockerStats(r io.Reader, project string) ([]statSample, error) {
	var out []statSample
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		s, err := parseStatsLine(sc.Text(), project)
		if err != nil {
			continue
		}
		out = append(out, s)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("đọc docker stats: %w", err)
	}
	return out, nil
}

func parseStatsLine(line, project string) (statSample, error) {
	parts := strings.SplitN(line, ",", 4)
	if len(parts) != 4 {
		return statSample{}, errStatsLine
	}
	sec, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return statSample{}, errStatsLine
	}
	name := strings.TrimPrefix(parts[1], project+"-")
	if i := strings.LastIndex(name, "-"); i > 0 {
		if _, err := strconv.Atoi(name[i+1:]); err == nil {
			name = name[:i]
		}
	}
	s := statSample{At: time.Unix(0, int64(sec*float64(time.Second))), Service: name}
	if cpu, err := strconv.ParseFloat(strings.TrimSuffix(parts[2], "%"), 64); err == nil {
		s.CPUPct = cpu
	}
	used, _, _ := strings.Cut(parts[3], "/")
	s.MemBytes = parseSize(strings.TrimSpace(used))
	return s, nil
}

// parseSize đọc kích thước kiểu docker ("12.5MiB", "900KiB", "1.2GiB", "3MB", "0B"); không đọc được thì 0.
func parseSize(s string) int64 {
	units := []struct {
		suffix string
		mult   float64
	}{{"KiB", 1 << 10}, {"MiB", 1 << 20}, {"GiB", 1 << 30}, {"kB", 1e3}, {"MB", 1e6}, {"GB", 1e9}, {"B", 1}}
	for _, u := range units {
		if num, ok := strings.CutSuffix(s, u.suffix); ok {
			f, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0
			}
			return int64(f * u.mult)
		}
	}
	return 0
}

// parseChaos đọc CSV của chaos.sh: fault,start,recovered (epoch giây; recovered rỗng = không trở lại).
func parseChaos(r io.Reader) ([]chaosEvent, error) {
	recs, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("đọc chaos csv: %w", err)
	}
	var out []chaosEvent
	for i, rec := range recs {
		if i == 0 || len(rec) != 3 {
			continue
		}
		e := chaosEvent{Fault: rec[0]}
		start, err1 := strconv.ParseFloat(rec[1], 64)
		end, err2 := strconv.ParseFloat(rec[2], 64)
		if err1 == nil && err2 == nil {
			e.Recovered, e.RecoverySec = true, math.Round((end-start)*10)/10
		}
		out = append(out, e)
	}
	return out, nil
}

// renderMarkdown in bảng chấm, số đo khác và CPU, bộ nhớ theo dịch vụ.
func renderMarkdown(in reportInput, vs []verdict) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### Bài %s\n\n| Tiêu chí | Ngưỡng | Đo được | Kết quả |\n|---|---|---|---|\n", in.Mode)
	for _, v := range vs {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", v.Name, v.Want, v.Got, v.Status)
	}
	b.WriteString("\nSố đo khác:\n\n")
	for _, m := range []struct{ label, metric, stat string }{
		{"webhook gửi được (200)", "webhook_ok", "count"},
		{"webhook p99 (ms)", "webhook_ms", "p(99)"},
		{"webhook lớn nhất (ms)", "webhook_ms", "max"},
		{"lượt đọc API", "read_ms", "count"},
		{"API đọc lớn nhất (ms)", "read_ms", "max"},
		{"lượt đọc khác 200", "read_bad", "count"},
	} {
		if v, ok := in.k6Value(m.metric, m.stat); ok {
			fmt.Fprintf(&b, "- %s: %.1f\n", m.label, v)
		}
	}
	if in.SSE != nil {
		l := in.SSE.Latency
		var rec, errs int64
		for _, c := range in.SSE.Clients {
			rec += c.Reconnects
			errs += c.Errors
		}
		fmt.Fprintf(&b, "- webhook → báo hiệu (ms): p50 %.0f, p95 %.0f, p99 %.0f, lớn nhất %.0f; %d lần nhận; %d lần nối lại, %d lần nối hỏng\n",
			l.P50, l.P95, l.P99, l.Max, l.Count, rec, errs)
	}
	d := in.DB
	fmt.Fprintf(&b, "- việc dead trong khung: %d; lệnh: %d (done %d, in_doubt %d, gửi hơn một lần %d)\n",
		d.Dead, d.CommandsTotal, d.CommandsDone, d.CommandsInDoubt, d.CommandsResent)
	fmt.Fprintf(&b, "- platform.jobs: %d dòng chết, %s\n", d.JobsDeadTuples, mib(d.JobsBytes))
	renderResources(&b, in.Stats)
	return b.String()
}

func renderResources(b *strings.Builder, samples []statSample) {
	if len(samples) == 0 {
		return
	}
	var services []string
	for _, s := range samples {
		if !slices.Contains(services, s.Service) {
			services = append(services, s.Service)
		}
	}
	slices.Sort(services)
	b.WriteString("\n| Dịch vụ | CPU trung bình (%) | CPU lớn nhất (%) | Bộ nhớ lớn nhất (MiB) | Bộ nhớ cuối (MiB) |\n|---|---|---|---|---|\n")
	for _, svc := range services {
		var sum, peak float64
		var n int
		var memPeak int64
		var last statSample
		for _, s := range samples {
			if s.Service != svc {
				continue
			}
			sum += s.CPUPct
			n++
			peak = max(peak, s.CPUPct)
			memPeak = max(memPeak, s.MemBytes)
			if !s.At.Before(last.At) {
				last = s
			}
		}
		fmt.Fprintf(b, "| %s | %.1f | %.1f | %.1f | %.1f |\n", svc, sum/float64(n), peak,
			float64(memPeak)/(1<<20), float64(last.MemBytes)/(1<<20))
	}
}
