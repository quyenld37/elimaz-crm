package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"elimaz.com.vn/crm/internal/platform/metrics"
	"elimaz.com.vn/crm/internal/testkit/loadstats"
)

// Chấm bài thử tải M4 (kế hoạch M4 Task 180: HT17 lặp lại cho ca live, CH01, LV02, LV10, HT01–HT03 trên đường
// Facebook). Ngưỡng đúng như kế hoạch; không nới.

const (
	modeFB      = "fb"      // fb_live.js + fb_staff.js
	modeFBChaos = "fbchaos" // như trên + fb_chaos.sh

	critFBInboxP95    = "Tin khách tới báo hiệu p95 < 2 s"
	critFBInboxP99    = "Tin khách tới báo hiệu p99 < 5 s"
	critFBCommentP95  = "Comment live tới báo hiệu p95 < 3 s"
	critFBSendP95     = "Bấm gửi tới lúc gọi Send API p95 < 1 s"
	critFBInboxOnce   = "Mỗi tin khách tới database đúng một lần"
	critFBCommentsAll = "Mỗi comment live tới database đúng một lần, đủ comment"
	critFBRefOnce     = "Máy Meta giả nhận mỗi client_ref tối đa một lần"
	critFBHolds       = "Tổng giữ hàng không vượt tồn"
	critFBDeadlocks   = "deadlocks không tăng"
	critFBNoDead      = "Không việc dead"
	critFBSendsDone   = "Mọi tin gửi ra đã gửi (không in_doubt, failed, expired)"
	critFBLiveRegain  = "Luồng live nhận lại trong 15 s"

	liveRegainLimitSec = 15
)

// liveRegain là một lần đo nối lại luồng live của fb_chaos.sh: sau lần hỏng Fault, video có luồng mở lại sau Sec giây.
type liveRegain struct {
	Fault, Video string
	Sec          float64
	Regained     bool
}

type fbReportInput struct {
	Mode   string
	Start  time.Time
	Live   k6Metrics     // fb_live.js
	Staff  k6Metrics     // fb_staff.js
	SSE    *fbSSEResult  // loadkit sse -fixture
	Series []fbStatsLine // loadkit fbstats: dòng đầu là trước tải, dòng cuối sau khi hàng đợi cạn
	Stats  []statSample
	Chaos  []chaosEvent
	Live2  []liveRegain
}

func k6Of(m k6Metrics, metric, stat string) (float64, bool) {
	return reportInput{K6: m}.k6Value(metric, stat)
}

func evaluateFB(in fbReportInput) []verdict {
	var first, last fbStatsLine
	haveDB := len(in.Series) > 0
	if haveDB {
		first, last = in.Series[0], in.Series[len(in.Series)-1]
	}
	var out []verdict
	if in.Mode == modeFB {
		p95, has := k6Of(in.Live, "webhook_ms", "p(95)")
		out = append(out, below(critWebhookP95, p95, has, 300, "ms"))
		var inbox, comment fbLatency
		if in.SSE != nil {
			inbox, comment = in.SSE.Inbox, in.SSE.Comment
		}
		out = append(out,
			below(critFBInboxP95, inbox.P95, inbox.Pairs > 0, 2000, "ms"),
			below(critFBInboxP99, inbox.P99, inbox.Pairs > 0, 5000, "ms"),
			below(critFBCommentP95, comment.P95, comment.Pairs > 0, 3000, "ms"))
		var click latencyResult
		if last.Graph != nil {
			click = last.Graph.ClickToAPI
		}
		out = append(out, below(critFBSendP95, click.P95, click.Count > 0, 1000, "ms"))
		r95, h95 := k6Of(in.Staff, "read_ms", "p(95)")
		r99, h99 := k6Of(in.Staff, "read_ms", "p(99)")
		out = append(out, below(critReadP95, r95, h95, 200, "ms"), below(critReadP99, r99, h99, 500, "ms"))
	}
	bad, haveBad := k6Of(in.Live, "webhook_bad", "count")
	out = append(out, countZero(critWebhookNon200, bad, haveBad))
	out = append(out, fbInboxOnce(in, last, haveDB), fbCommentsAll(in, last, haveDB), fbRefOnce(last),
		fbHolds(last, haveDB), fbDeadlocks(first, last, len(in.Series) >= 2), fbNoDead(last, haveDB))
	if in.Mode == modeFB {
		out = append(out, fbSendsDone(last, haveDB))
	} else {
		out = append(out, recovery(in.Chaos)...)
		out = append(out, liveRegains(in.Live2)...)
	}
	return out
}

func na(name, want string) verdict {
	return verdict{Name: name, Want: want, Status: statusNA, Got: "không có số đo"}
}

func fbInboxOnce(in fbReportInput, d fbStatsLine, have bool) verdict {
	v := na(critFBInboxOnce, "dòng = mã khác nhau = tin ingest nhận (200)")
	sent, ok := k6Of(in.Live, "inbox_sent", "count")
	if !have || !ok {
		return v
	}
	v.Got = fmt.Sprintf("gửi %.0f (trùng cùng lúc %.0f); dòng %d; mã khác nhau %d", sent,
		val(k6Of(in.Live, "inbox_dup", "count")), d.DB.InboxRows, d.DB.InboxDistinct)
	v.Status = passIf(d.DB.InboxRows == d.DB.InboxDistinct && float64(d.DB.InboxDistinct) == sent)
	return v
}

// val bỏ cờ có số đo (số đo vắng in 0).
func val(v float64, _ bool) float64 { return v }

func fbCommentsAll(in fbReportInput, d fbStatsLine, have bool) verdict {
	v := na(critFBCommentsAll, "dòng = mã khác nhau = comment đã giao, đều thuộc phiên live; đẩy vào máy giả lỗi 0")
	sent, ok := k6Of(in.Live, "comment_sent", "count")
	pushBad, _ := k6Of(in.Live, "graph_push_bad", "count")
	if !have || !ok {
		return v
	}
	db := d.DB
	v.Got = fmt.Sprintf("đã giao %.0f (qua cả hai đường %.0f); dòng %d; mã khác nhau %d; thuộc phiên live %d; đẩy lỗi %.0f",
		sent, val(k6Of(in.Live, "comment_both", "count")), db.CommentRows, db.CommentDistinct, db.CommentLive, pushBad)
	v.Status = passIf(db.CommentRows == db.CommentDistinct && float64(db.CommentDistinct) == sent &&
		db.CommentLive == db.CommentDistinct && pushBad == 0)
	return v
}

func fbRefOnce(d fbStatsLine) verdict {
	v := na(critFBRefOnce, "client_ref trùng = 0")
	if d.Graph == nil {
		return v
	}
	g := d.Graph
	v.Got = fmt.Sprintf("%d lời gọi Send API, %d có client_ref, trùng %d", g.Calls, g.WithRef, g.DuplicateRef)
	v.Status = passIf(g.DuplicateRef == 0)
	return v
}

func fbHolds(d fbStatsLine, have bool) verdict {
	v := na(critFBHolds, "mỗi dòng held ≤ on_hand; sổ giữ = tổng held")
	if !have {
		return v
	}
	db := d.DB
	v.Got = fmt.Sprintf("tồn %d, giữ %d (sổ giữ %d), dòng vượt %d; chốt %d", db.OnHand, db.Held, db.Reserved, db.OverHeld, db.Claims)
	v.Status = passIf(db.OverHeld == 0 && db.Held <= db.OnHand && db.Reserved == db.Held)
	return v
}

func fbDeadlocks(a, b fbStatsLine, have bool) verdict {
	v := na(critFBDeadlocks, "cuối − đầu = 0")
	if !have {
		return v
	}
	v.Got = fmt.Sprintf("%d → %d", a.DB.Deadlocks, b.DB.Deadlocks)
	v.Status = passIf(b.DB.Deadlocks == a.DB.Deadlocks)
	return v
}

func fbNoDead(d fbStatsLine, have bool) verdict {
	v := na(critFBNoDead, "0 (bài không có lỗi cố ý)")
	if !have {
		return v
	}
	v.Got = strconv.FormatInt(d.DB.Dead, 10)
	if len(d.DB.DeadKinds) > 0 {
		var parts []string
		for _, k := range slices.Sorted(maps.Keys(d.DB.DeadKinds)) {
			parts = append(parts, fmt.Sprintf("%s %d", k, d.DB.DeadKinds[k]))
		}
		v.Got += " (" + strings.Join(parts, ", ") + ")"
	}
	v.Status = passIf(d.DB.Dead == 0)
	return v
}

func fbSendsDone(d fbStatsLine, have bool) verdict {
	v := na(critFBSendsDone, "0")
	if !have {
		return v
	}
	db := d.DB
	v.Got = fmt.Sprintf("%d tin; đã gửi %d, đang chờ %d, hỏng/không rõ %d", db.Sends, db.SendsSent, db.SendsPending, db.SendsBad)
	v.Status = passIf(db.SendsBad == 0 && db.SendsPending == 0)
	return v
}

func liveRegains(rs []liveRegain) []verdict {
	if len(rs) == 0 {
		return []verdict{{Name: critFBLiveRegain, Want: "≤ 15 s", Status: statusNA, Got: "không có lần đo nào"}}
	}
	out := make([]verdict, 0, len(rs))
	for _, r := range rs {
		v := verdict{
			Name: fmt.Sprintf("%s: %s, video %s", critFBLiveRegain, r.Fault, r.Video), Want: "≤ 15 s",
			Status: statusFail, Got: "không nối lại",
		}
		if r.Regained {
			v.Got = fmt.Sprintf("%.1f s", r.Sec)
			v.Status = passIf(r.Sec <= liveRegainLimitSec)
		}
		out = append(out, v)
	}
	return out
}

// parseLiveRegain đọc CSV của fb_chaos.sh: fault,video,start,regained (epoch giây; regained rỗng = không nối lại).
// Dòng tiêu đề và dòng sai số cột bị bỏ qua.
func parseLiveRegain(r io.Reader) ([]liveRegain, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	var out []liveRegain
	for header := true; ; header = false {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("đọc live csv: %w", err)
		}
		if header || len(rec) != 4 {
			continue
		}
		l := liveRegain{Fault: rec[0], Video: rec[1]}
		if start, err := strconv.ParseFloat(rec[2], 64); err == nil {
			if end, err := strconv.ParseFloat(rec[3], 64); err == nil {
				l.Regained, l.Sec = true, math.Round((end-start)*10)/10
			}
		}
		out = append(out, l)
	}
}

// histLine tóm tắt một histogram của metrics_minute: số lần và cận ngăn của p50, p95, p99.
func histLine(h []int) string {
	total := 0
	for _, n := range h {
		total += n
	}
	if total == 0 {
		return "không có"
	}
	return fmt.Sprintf("%d lần; p50 ≤ %g, p95 ≤ %g, p99 ≤ %g ms", total, metrics.QuantileFromHist(h, 50),
		metrics.QuantileFromHist(h, 95), metrics.QuantileFromHist(h, 99))
}

func fbLatLine(b *strings.Builder, label string, l fbLatency) {
	fmt.Fprintf(b, "- %s (ms, mọi cặp sự kiện × client): p50 %.0f, p95 %.0f, p99 %.0f, lớn nhất %.0f; %d sự kiện, %d cặp, thiếu khung %d\n",
		label, l.P50, l.P95, l.P99, l.Max, l.Events, l.Pairs, l.Missing)
}

// renderFB in bảng chấm và số đo khác của bài M4.
func renderFB(in fbReportInput, vs []verdict) string {
	var b strings.Builder
	renderVerdicts(&b, in.Mode, vs)
	for _, m := range []struct {
		label        string
		src          k6Metrics
		metric, stat string
	}{
		{"webhook 200", in.Live, "webhook_ok", "count"},
		{"webhook p99 (ms)", in.Live, "webhook_ms", "p(99)"},
		{"lượt đọc API", in.Staff, "read_ms", "count"},
		{"lượt đọc khác 200", in.Staff, "read_bad", "count"},
		{"tin nhân viên gửi được (201)", in.Staff, "send_ok", "count"},
		{"tin nhân viên lỗi", in.Staff, "send_bad", "count"},
		{"gửi tin: thời gian trả lời p95 (ms)", in.Staff, "send_api_ms", "p(95)"},
		{"chốt được (201)", in.Staff, "claim_ok", "count"},
		{"chốt bị từ chối nghiệp vụ (409)", in.Staff, "claim_conflict", "count"},
		{"chốt lỗi khác", in.Staff, "claim_bad", "count"},
		{"chốt: thời gian trả lời p95 (ms)", in.Staff, "claim_ms", "p(95)"},
	} {
		if v, ok := k6Of(m.src, m.metric, m.stat); ok {
			fmt.Fprintf(&b, "- %s: %.1f\n", m.label, v)
		}
	}
	if in.SSE != nil {
		fbLatLine(&b, "tin khách → báo hiệu", in.SSE.Inbox)
		fbLatLine(&b, "comment live → báo hiệu", in.SSE.Comment)
		var rec, errs int64
		for _, c := range in.SSE.Clients {
			rec += c.Reconnects
			errs += c.Errors
		}
		fmt.Fprintf(&b, "- client SSE: %d; nối lại %d lần, nối hỏng %d lần\n", len(in.SSE.Clients), rec, errs)
	}
	if len(in.Series) > 0 {
		last := in.Series[len(in.Series)-1]
		for _, name := range loadstats.HistNames() {
			fmt.Fprintf(&b, "- %s (máy chủ, metrics_minute): %s\n", name, histLine(last.DB.Hist[name]))
		}
		if g := last.Graph; g != nil {
			c := g.ClickToAPI
			fmt.Fprintf(&b, "- bấm gửi → Send API (ms): p50 %.0f, p95 %.0f, p99 %.0f, lớn nhất %.0f; %d tin; client_ref trùng %d\n",
				c.P50, c.P95, c.P99, c.Max, c.Count, g.DuplicateRef)
		}
		d := last.DB
		fmt.Fprintf(&b, "- việc in_doubt %d, còn chờ %d; deadlock (cả database) %d\n", d.InDoubt, d.Backlog, d.Deadlocks)
	}
	renderResources(&b, in.Stats)
	return b.String()
}

type fbReportFiles struct {
	live, staff, sse, series, stats, chaos, regain string
}

// cmdFBReport: loadkit fbreport -mode fb|fbchaos -live F -staff F -sse F -series F [-stats F -chaos F -regain F].
func cmdFBReport(fs *flag.FlagSet, args []string) error {
	in := fbReportInput{}
	var f fbReportFiles
	var startS string
	fs.StringVar(&in.Mode, "mode", modeFB, "fb|fbchaos")
	fs.StringVar(&startS, "start", "", "lúc bắt đầu tải (RFC3339)")
	fs.StringVar(&f.live, "live", "", "summary k6 của fb_live.js")
	fs.StringVar(&f.staff, "staff", "", "summary k6 của fb_staff.js")
	fs.StringVar(&f.sse, "sse", "", "kết quả loadkit sse -fixture")
	fs.StringVar(&f.series, "series", "", "các dòng loadkit fbstats (đầu: trước tải; cuối: sau khi hàng đợi cạn)")
	fs.StringVar(&f.stats, "stats", "", "CSV CPU, bộ nhớ của fb_run.sh")
	fs.StringVar(&f.chaos, "chaos", "", "chaos.csv của fb_chaos.sh")
	fs.StringVar(&f.regain, "regain", "", "live.csv của fb_chaos.sh")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	if in.Mode != modeFB && in.Mode != modeFBChaos {
		return fmt.Errorf("%w: -mode %q", errUsage, in.Mode)
	}
	start, err := parseTime(startS)
	if err != nil {
		return err
	}
	in.Start = start
	if err := loadFBReportFiles(&in, f); err != nil {
		return err
	}
	vs := evaluateFB(in)
	fmt.Print(renderFB(in, vs)) //nolint:forbidigo // đầu ra của lệnh là bảng markdown
	for _, v := range vs {
		if v.Status == statusFail {
			return errFailed
		}
	}
	return nil
}

func loadFBReportFiles(in *fbReportInput, f fbReportFiles) error {
	for _, j := range []struct {
		path string
		dst  any
	}{{f.live, &in.Live}, {f.staff, &in.Staff}} {
		if err := readJSONFile(j.path, j.dst); err != nil {
			return err
		}
	}
	if f.sse != "" {
		in.SSE = &fbSSEResult{}
		if err := readJSONFile(f.sse, in.SSE); err != nil {
			return err
		}
	}
	if err := readJSONLines(f.series, &in.Series); err != nil {
		return err
	}
	if err := withFile(f.stats, func(r io.Reader) (err error) {
		in.Stats, err = parseDockerStats(r, "")
		return err
	}); err != nil {
		return err
	}
	if err := withFile(f.chaos, func(r io.Reader) (err error) {
		in.Chaos, err = parseChaos(r)
		return err
	}); err != nil {
		return err
	}
	return withFile(f.regain, func(r io.Reader) (err error) {
		in.Live2, err = parseLiveRegain(r)
		return err
	})
}

// readJSONLines đọc các dòng JSON của path vào dst (dòng hỏng bị bỏ qua); path rỗng: không có.
func readJSONLines(path string, dst *[]fbStatsLine) error {
	return withFile(path, func(r io.Reader) error {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		for sc.Scan() {
			var s fbStatsLine
			if json.Unmarshal(sc.Bytes(), &s) == nil {
				*dst = append(*dst, s)
			}
		}
		return sc.Err()
	})
}
