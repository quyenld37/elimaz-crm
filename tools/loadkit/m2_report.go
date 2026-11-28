package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Chấm bài thử tải M2 (kế hoạch M2 Task 88, M2-31): ngưỡng đúng như kế hoạch, không nới. Số đo trên bể GitHub là số đo
// tạm (QĐ-88-1): đo lại trên server thử trước chạy thật.

const (
	critM2SearchP95   = "Tìm khách p95 < 300 ms (DH02)"
	critM2SearchErr   = "Tìm khách lỗi < 0,1%"
	critM2SearchFound = "Tìm SĐT đủ, mã khách: luôn thấy khách"
	critM2CreateP95   = "createOrder p95 < 400 ms"
	critM2ListP95     = "listOrders p95 < 200 ms"
	critM2CreateErr   = "createOrder: có đơn tạo được, lỗi ngoài nghiệp vụ < 0,1%"
	critM2ListErr     = "listOrders lỗi < 0,1%"
	critM2Coupons     = "used_count mỗi mã bằng số lượt hiệu lực"
	critM2Exhausted   = "tx_retry_exhausted < 1% số transaction ghi"

	m2SearchP95Ms  = 300
	m2CreateP95Ms  = 400
	m2ListP95Ms    = 200
	m2SearchErrPct = 0.1
	m2ExhaustedPct = 1
)

// m2Sample là phần của m2-sample.json (tools/seedperf) báo cáo dùng.
type m2Sample struct {
	Customers   int     `json:"customers"`
	Phones      int     `json:"phones"`
	Orders      int     `json:"orders"`
	Items       int     `json:"items"`
	SeedSeconds float64 `json:"seedSeconds"`
}

type m2ReportInput struct {
	Search, Orders k6Metrics
	Series         []m2Stats // đầu: trước bài lên đơn; cuối: sau khi hàng đợi cạn
	Sample         m2Sample
	Slow           string // markdown của loadkit m2slow
}

func m2Count(m k6Metrics, name string) (float64, bool) { return k6Of(m, name, "count") }

func m2Val(m k6Metrics, name string) float64 {
	v, _ := m2Count(m, name)
	return v
}

func evaluateM2(in m2ReportInput) []verdict {
	p95 := func(name string, m k6Metrics, metric string, limit float64) verdict {
		got, ok := k6Of(m, metric, "p(95)")
		return below(name, got, ok, limit, "ms")
	}
	vs := []verdict{
		p95(critM2SearchP95, in.Search, "search_ms", m2SearchP95Ms),
		m2SearchErrors(in.Search),
		m2SearchMissing(in.Search),
		p95(critM2CreateP95, in.Orders, "create_ms", m2CreateP95Ms),
		p95(critM2ListP95, in.Orders, "list_ms", m2ListP95Ms),
		m2CreateErrors(in.Orders),
		m2ErrorRate(critM2ListErr, in.Orders, "list_req", "list_bad"),
	}
	if len(in.Series) == 0 {
		return append(vs, na(critFBHolds, "mỗi dòng held ≤ on_hand; sổ giữ = tổng held"), na(critM2Coupons, "used = lượt active ≤ maxUses"),
			na(critFBDeadlocks, "cuối − đầu = 0"), na(critM2Exhausted, "< 1%"), na(critFBNoDead, "0"))
	}
	first, last := in.Series[0], in.Series[len(in.Series)-1]
	return append(vs, m2Holds(last), m2CouponsOK(last),
		fbDeadlocks(fbStatsLine{DB: first.DB}, fbStatsLine{DB: last.DB}, true),
		m2ExhaustedOK(first, last, m2Val(in.Orders, "create_req")),
		fbNoDead(fbStatsLine{DB: last.DB}, true))
}

func m2SearchErrors(m k6Metrics) verdict {
	return m2ErrorRate(critM2SearchErr, m, "search_req", "search_bad")
}

// m2ErrorRate: số lượt lỗi (bad) trên số lượt (req) dưới m2SearchErrPct phần trăm (cùng ngưỡng 0,1% cho mọi API).
func m2ErrorRate(name string, m k6Metrics, req, bad string) verdict {
	v := na(name, fmt.Sprintf("< %g%% số lượt", m2SearchErrPct))
	n, ok := m2Count(m, req)
	if !ok || n == 0 {
		return v
	}
	b := m2Val(m, bad)
	pct := b / n * 100
	v.Got = fmt.Sprintf("%.0f / %.0f lượt (%.3f%%)", b, n, pct)
	v.Status = passIf(pct < m2SearchErrPct)
	return v
}

// m2CreateErrors: bài lên đơn chỉ có nghĩa khi đơn tạo được (chạy khói 27/11: mọi lượt 400 mà các tiêu chí tồn, mã
// vẫn ĐẠT vì không đơn nào chạm tới). Từ chối nghiệp vụ (hết hàng, mã hết lượt) không tính là lỗi.
func m2CreateErrors(m k6Metrics) verdict {
	v := m2ErrorRate(critM2CreateErr, m, "create_req", "create_bad")
	if v.Status == statusNA {
		return v
	}
	ok := m2Val(m, "create_ok")
	v.Got = fmt.Sprintf("tạo %.0f; %s; hết hàng %.0f, mã bị từ chối %.0f", ok, v.Got, m2Val(m, "create_oos"),
		m2Val(m, "create_coupon_rejected"))
	if ok == 0 {
		v.Status = statusFail
	}
	return v
}

func m2SearchMissing(m k6Metrics) verdict {
	v := na(critM2SearchFound, "không thấy = 0")
	exact, ok := m2Count(m, "search_exact")
	if !ok || exact == 0 {
		return v
	}
	miss := m2Val(m, "search_miss")
	v.Got = fmt.Sprintf("%.0f lượt tìm SĐT đủ hay mã, không thấy %.0f", exact, miss)
	v.Status = passIf(miss == 0)
	return v
}

func m2Holds(s m2Stats) verdict {
	v := na(critFBHolds, "mỗi dòng held ≤ on_hand; sổ giữ = tổng held")
	d := s.DB
	v.Got = fmt.Sprintf("tồn %d, giữ %d (sổ giữ %d), dòng vượt %d", d.OnHand, d.Held, d.Reserved, d.OverHeld)
	v.Status = passIf(d.OverHeld == 0 && d.Held <= d.OnHand && d.Reserved == d.Held)
	return v
}

func m2CouponsOK(s m2Stats) verdict {
	v := na(critM2Coupons, "mỗi mã: used_count = lượt active, ≤ maxUses")
	if len(s.Coupons) == 0 {
		return v
	}
	ok := true
	var parts []string
	for _, c := range s.Coupons {
		good := int64(c.Used) == c.Active && (c.MaxUses == 0 || c.Used <= c.MaxUses)
		ok = ok && good
		limit := "∞"
		if c.MaxUses > 0 {
			limit = strconv.Itoa(int(c.MaxUses))
		}
		mark := ""
		if !good {
			mark = " ✗"
		}
		parts = append(parts, fmt.Sprintf("%s %d/%d/%s%s", c.Code, c.Used, c.Active, limit, mark))
	}
	v.Got = "used/active/max: " + strings.Join(parts, ", ")
	v.Status = passIf(ok)
	return v
}

// m2ExhaustedOK: số lần hết lượt thử lại (chỉ số máy chủ, cộng mọi transaction từ lúc bắt đầu bài lên đơn) trên số lượt
// createOrder k6 gửi (mỗi lượt một transaction ghi).
func m2ExhaustedOK(first, last m2Stats, creates float64) verdict {
	v := na(critM2Exhausted, fmt.Sprintf("< %d%% số lượt createOrder", m2ExhaustedPct))
	if creates == 0 {
		return v
	}
	ex := last.TxExhausted - first.TxExhausted
	pct := float64(ex) / creates * 100
	v.Got = fmt.Sprintf("%d / %.0f (%.2f%%); tx_retry %d", ex, creates, pct, last.TxRetry-first.TxRetry)
	v.Status = passIf(pct < m2ExhaustedPct)
	return v
}

func renderM2(in m2ReportInput, vs []verdict) string {
	var b strings.Builder
	renderVerdicts(&b, "M2 (tìm khách + lên đơn 10×)", vs)
	line := func(label string, m k6Metrics, name, stat string) {
		if x, ok := k6Of(m, name, stat); ok {
			fmt.Fprintf(&b, "- %s: %.1f\n", label, x)
		}
	}
	for _, k := range []string{"search", "search_tail", "search_phone", "search_name", "search_code"} {
		line(k+" p50 (ms)", in.Search, k+"_ms", "med")
		line(k+" p95 (ms)", in.Search, k+"_ms", "p(95)")
		line(k+" p99 (ms)", in.Search, k+"_ms", "p(99)")
	}
	line("lượt tìm", in.Search, "search_req", "count")
	for _, k := range []string{"create", "list"} {
		line(k+" p50 (ms)", in.Orders, k+"_ms", "med")
		line(k+" p99 (ms)", in.Orders, k+"_ms", "p(99)")
		line(k+" lớn nhất (ms)", in.Orders, k+"_ms", "max")
	}
	for _, k := range []string{"create_req", "create_ok", "create_oos", "create_coupon_rejected", "create_bad", "list_req", "list_bad"} {
		line(k, in.Orders, k, "count")
	}
	if n := len(in.Series); n > 0 {
		last := in.Series[n-1]
		fmt.Fprintf(&b, "- đơn tạo trong bài: %d (có mã %d)\n", last.Orders, last.CouponOrders)
		fmt.Fprintf(&b, "- dung lượng database: %s\n", mib(last.DatabaseBytes))
		for _, t := range last.Tables {
			fmt.Fprintf(&b, "- %s: %s, ~%d dòng\n", t.Name, mib(t.Bytes), t.Rows)
		}
	}
	s := in.Sample
	fmt.Fprintf(&b, "- dữ liệu nạp: %d khách, %d SĐT, %d đơn, %d dòng đơn, nạp trong %.0f s\n", s.Customers, s.Phones, s.Orders,
		s.Items, s.SeedSeconds)
	if in.Slow != "" {
		b.WriteString("\n" + in.Slow)
	}
	return b.String()
}

type m2ReportFiles struct{ search, orders, series, sample, slow string }

// cmdM2Report: loadkit m2report -search F -orders F -series F -sample F [-slow F].
func cmdM2Report(fs *flag.FlagSet, args []string) error {
	var f m2ReportFiles
	fs.StringVar(&f.search, "search", "", "summary k6 của m2-search.js")
	fs.StringVar(&f.orders, "orders", "", "summary k6 của m2-orders.js")
	fs.StringVar(&f.series, "series", "", "các dòng loadkit m2stats")
	fs.StringVar(&f.sample, "sample", "", "m2-sample.json của seedperf")
	fs.StringVar(&f.slow, "slow", "", "kết quả loadkit m2slow (markdown)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	var in m2ReportInput
	for _, j := range []struct {
		path string
		dst  any
	}{{f.search, &in.Search}, {f.orders, &in.Orders}, {f.sample, &in.Sample}} {
		if err := readJSONFile(j.path, j.dst); err != nil {
			return err
		}
	}
	if err := withFile(f.series, func(r io.Reader) error {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		for sc.Scan() {
			var s m2Stats
			if json.Unmarshal(sc.Bytes(), &s) == nil {
				in.Series = append(in.Series, s)
			}
		}
		return sc.Err()
	}); err != nil {
		return err
	}
	if f.slow != "" {
		b, err := os.ReadFile(f.slow)
		if err != nil {
			return fmt.Errorf("đọc %s: %w", f.slow, err)
		}
		in.Slow = string(b)
	}
	vs := evaluateM2(in)
	fmt.Print(renderM2(in, vs)) //nolint:forbidigo // đầu ra của lệnh là bảng markdown
	for _, v := range vs {
		if v.Status == statusFail {
			return errFailed
		}
	}
	return nil
}

// ---- Câu chậm: kế hoạch auto_explain trong log PostgreSQL.

// slowPlan là một câu chậm auto_explain ghi: thời gian lớn nhất, số lần ghi, kế hoạch của lần lớn nhất.
type slowPlan struct {
	Query string
	MaxMs float64
	Count int
	Plan  []string
}

var (
	// slowHead: dòng đầu một mục auto_explain ("LOG:  duration: 12.345 ms  plan:"), sau tiền tố log tùy ý.
	slowHead = regexp.MustCompile(`LOG:\s+duration: ([0-9.]+) ms\s+plan:`)
	// composePrefix: tiền tố "<dịch vụ>-1  | " của docker compose logs.
	composePrefix = regexp.MustCompile(`^\S+\s+\|\s?`)
)

// parseSlowLog đọc các mục auto_explain (định dạng text) của log PostgreSQL, gom theo câu (khoảng trắng gộp lại), giữ
// kế hoạch của lần chậm nhất mỗi câu; bỏ dòng "Query Parameters" (giá trị tham số có thể là SĐT).
func parseSlowLog(r io.Reader) ([]slowPlan, error) {
	byQuery := map[string]*slowPlan{}
	var cur *slowPlan
	var curMs float64
	inQuery := false
	flush := func() {
		if cur == nil {
			return
		}
		cur.Query = strings.Join(strings.Fields(cur.Query), " ")
		p, ok := byQuery[cur.Query]
		if !ok {
			p = &slowPlan{Query: cur.Query}
			byQuery[cur.Query] = p
		}
		p.Count++
		if curMs > p.MaxMs {
			p.MaxMs, p.Plan = curMs, cur.Plan
		}
		cur = nil
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := composePrefix.ReplaceAllString(sc.Text(), "")
		if m := slowHead.FindStringSubmatch(line); m != nil {
			flush()
			curMs, _ = strconv.ParseFloat(m[1], 64)
			cur, inQuery = &slowPlan{}, false
			continue
		}
		if cur == nil {
			continue
		}
		if line == "" || (line[0] != '\t' && line[0] != ' ') {
			flush()
			continue
		}
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "Query Text:"):
			cur.Query, inQuery = strings.TrimSpace(strings.TrimPrefix(t, "Query Text:")), true
		case strings.HasPrefix(t, "Query Parameters:"):
			inQuery = false
		case inQuery && !looksLikePlanNode(t):
			cur.Query += " " + t
		default:
			inQuery = false
			cur.Plan = append(cur.Plan, strings.TrimPrefix(strings.TrimPrefix(line, "\t"), " "))
		}
	}
	flush()
	out := make([]slowPlan, 0, len(byQuery))
	for _, p := range byQuery {
		out = append(out, *p)
	}
	slices.SortFunc(out, func(a, b slowPlan) int {
		return cmp.Or(cmp.Compare(b.MaxMs, a.MaxMs), strings.Compare(a.Query, b.Query))
	})
	return out, sc.Err() //nolint:wrapcheck // bên gọi bọc
}

// planNode: dòng đầu của kế hoạch ("Limit  (cost=…", "Function Scan on f s  (actual …") — hết phần câu SQL.
var planNode = regexp.MustCompile(`^[A-Z].*\S  \((cost|actual)[= ]`)

func looksLikePlanNode(t string) bool { return planNode.MatchString(t) }

func renderSlow(ps []slowPlan, top int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#### Câu chậm nhất (auto_explain, EXPLAIN ANALYZE, BUFFERS)\n\n")
	if len(ps) == 0 {
		b.WriteString("Không câu nào vượt ngưỡng ghi của auto_explain.\n")
		return b.String()
	}
	for i, p := range ps[:min(top, len(ps))] {
		q := p.Query
		if len(q) > 400 {
			q = q[:400] + " …"
		}
		fmt.Fprintf(&b, "%d. Lớn nhất %.1f ms, %d lần vượt ngưỡng ghi.\n\n```sql\n%s\n```\n\n```\n%s\n```\n\n", i+1, p.MaxMs, p.Count, q,
			strings.Join(p.Plan, "\n"))
	}
	return b.String()
}

// cmdM2Slow: loadkit m2slow -log <log PostgreSQL> [-top 3] — in markdown kế hoạch của top câu chậm nhất.
func cmdM2Slow(fs *flag.FlagSet, args []string) error {
	path := fs.String("log", "", "log PostgreSQL (docker compose logs postgres)")
	top := fs.Int("top", 3, "số câu")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	if *path == "" || *top < 1 {
		return fmt.Errorf("%w: thiếu -log", errUsage)
	}
	var ps []slowPlan
	if err := withFile(*path, func(r io.Reader) (err error) {
		ps, err = parseSlowLog(r)
		return err
	}); err != nil {
		return err
	}
	fmt.Print(renderSlow(ps, *top)) //nolint:forbidigo // đầu ra của lệnh là markdown
	return nil
}
