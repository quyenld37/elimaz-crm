package main

import (
	"context"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"elimaz.com.vn/crm/internal/platform/metrics"
)

// Chấm sáu bài thử tải M5 theo ngưỡng của kế hoạch M5 Task 219 (M5-36): loadkit m5report đọc tóm tắt k6
// (handleSummary của loadtest/k6/m5/common.js), hai lần m5stats (trước, sau bài) và mã thoát của crm verify-flash.

const (
	m5Spike       = "flash_spike"
	m5PerCustomer = "flash_per_customer"
	m5Sustained   = "flash_sustained"
	m5Abandon     = "flash_abandon"
	m5Close       = "flash_close_with_holds"
	m5WebRead     = "web_read"

	// Bài hỏng giữa tải (Task 220, M5-37): ba nhóm bước của chaos-m5.sh.
	m5ChaosValkey = "chaos_valkey" // (a) dừng valkey-gate, (b) dừng valkey-cache, (c) FLUSHALL cả hai
	m5ChaosAPI    = "chaos_api"    // (d) giết api
	m5ChaosPG     = "chaos_pg"     // (e) dừng PostgreSQL

	critM5ChaosErrRate = "Lỗi không phải 503/429 dưới 0,1% yêu cầu mua (không 500 hàng loạt)"
	critM5NoDupRef     = "Không đơn trùng external_ref"
	critM5ChaosSaw503  = "Trong lúc ngừng lượt mua nhận 503 retry_later"

	critM5Bought         = "Đúng số suất bought (không thiếu, không vượt)"
	critM5OneBought      = "Một SĐT, 100 yêu cầu song song, giới hạn 1: đúng 1 bought"
	critM5BuyP95         = "Lượt mua p95 phía máy chủ (mọi kết quả) ≤ 300 ms"
	critM5NoOtherErr     = "Ngoài 503 retry_later và 429 không mã lỗi nào khác"
	critM5Verify         = "crm verify-flash đạt"
	critM5Deadlocks      = "pg_stat_database.deadlocks không tăng"
	critM5RetryExhausted = "tx_retry_exhausted < 1% lượt mua"
	critM5Orders         = "Mọi suất thành đơn hoàn tất"
	critM5PickupP95      = "Nhận việc sự kiện (event.*) p95 ≤ 1 s"
	critM5Reopened       = "Suất hết hạn giữ mở lại và bán hết ở đợt hai"
	critM5NoFalseSoldOut = "Không suất nào hết oan (đợt hai không nhận sold_out khi còn suất)"
	critM5Closed         = "Đóng đợt khi còn suất đang giữ thành công"
	critM5HalfDone       = "Một nửa suất đang giữ hoàn tất được sau khi đóng"
	critM5ReadP95        = "Lượt đọc của website p95 < 200 ms"
	critM5ReadP99        = "Lượt đọc của website p99 < 500 ms"
	critM5ReadErr        = "Lượt đọc không lỗi"
	critM5CacheHit       = "Tỷ lệ trúng bộ đệm > 95%"

	m5BuyP95LimitMs    = 300
	m5PickupP95LimitMs = 1000
	m5ReadP95LimitMs   = 200
	m5ReadP99LimitMs   = 500
	m5ExhaustedMax     = 0.01
	m5ChaosErrMax      = 0.001
	m5CacheHitMin      = 0.95
	m5PercentileP95    = 95
	m5MaxPercent       = 100
)

// m5Input là mọi số liệu của một bài.
type m5Input struct {
	Mode       string
	Slots      int
	K6         k6Metrics
	Before     m5Stats
	After      m5Stats
	Dups       int // số external_ref có từ hai đơn (bài hỏng giữa tải)
	VerifyExit int // mã thoát của crm verify-flash (bài web_read không chạy: bỏ qua)
}

func (in m5Input) count(metric string) (float64, bool) {
	return k6Of(in.K6, metric, "count")
}

// k6Count: bộ đếm k6 không có dòng nào nghĩa là 0 (k6 bỏ bộ đếm chưa tăng lần nào khỏi tóm tắt).
func (in m5Input) k6Count(metric string) float64 {
	v, _ := in.count(metric)
	return v
}

// histDelta trả after − before theo từng ngăn (chỉ số cộng dồn theo phút).
func histDelta(after, before []int) []int {
	out := make([]int, len(metrics.HistBoundsMs)+1)
	for i := range out {
		if i < len(after) {
			out[i] = after[i]
		}
		if i < len(before) {
			out[i] -= before[i]
		}
	}
	return out
}

func m5Passed(vs []verdict) bool {
	return !slices.ContainsFunc(vs, func(v verdict) bool { return v.Status != statusPass })
}

// evaluateM5 chấm bài in.Mode.
func evaluateM5(in m5Input) []verdict {
	switch in.Mode {
	case m5Spike:
		return []verdict{
			equalCount(critM5Bought, in.k6Count("buy_bought"), float64(in.Slots)),
			pickup(critM5BuyP95, histDelta(in.After.FlashBuyHist, in.Before.FlashBuyHist), m5PercentileP95, m5BuyP95LimitMs),
			countZero(critM5NoOtherErr, in.k6Count("buy_err"), true),
			m5Verify(in), m5DeadlocksVerdict(in), m5Exhausted(in),
		}
	case m5PerCustomer:
		return []verdict{equalCount(critM5OneBought, in.k6Count("buy_bought"), 1), m5Verify(in)}
	case m5Sustained:
		return []verdict{
			equalCount(critM5Bought, in.k6Count("buy_bought"), float64(in.Slots)),
			equalCount(critM5Orders, in.k6Count("complete_ok"), float64(in.Slots)),
			pickup(critM5BuyP95, histDelta(in.After.FlashBuyHist, in.Before.FlashBuyHist), m5PercentileP95, m5BuyP95LimitMs),
			pickup(critM5PickupP95, histDelta(in.After.PickupEventHist, in.Before.PickupEventHist), m5PercentileP95,
				m5PickupP95LimitMs),
			countZero(critM5NoOtherErr, in.k6Count("buy_err")+in.k6Count("complete_err"), true),
			m5Verify(in), m5DeadlocksVerdict(in),
		}
	case m5Abandon:
		// buy_bought đếm cả hai đợt; đợt một = tổng − đợt hai.
		wave1 := in.k6Count("buy_bought") - in.k6Count("wave2_bought")
		left := wave1 - in.k6Count("complete_ok")
		return []verdict{
			equalCount(critM5Bought, wave1, float64(in.Slots)),
			equalCount(critM5Reopened, in.k6Count("wave2_bought"), left),
			countZero(critM5NoFalseSoldOut, in.k6Count("wave2_sold_out_early"), true),
			m5Verify(in),
		}
	case m5Close:
		// Slots ở bài này là số người giữ suất lúc đóng; người số chẵn (một nửa, làm tròn lên) hoàn tất sau khi đóng.
		return []verdict{
			equalCount(critM5Bought, in.k6Count("buy_bought"), float64(in.Slots)),
			equalCount(critM5Closed, in.k6Count("close_ok"), 1),
			equalCount(critM5HalfDone, in.k6Count("complete_ok"), float64((in.Slots+1)/2)), //nolint:mnd // một nửa
			m5Verify(in),
		}
	case m5WebRead:
		p95, ok95 := k6Of(in.K6, "read_ms", "p(95)")
		p99, ok99 := k6Of(in.K6, "read_ms", "p(99)")
		return []verdict{
			below(critM5ReadP95, p95, ok95, m5ReadP95LimitMs, "ms"),
			below(critM5ReadP99, p99, ok99, m5ReadP99LimitMs, "ms"),
			countZero(critM5ReadErr, in.k6Count("read_err"), true),
			m5CacheVerdict(in),
		}
	case m5ChaosValkey:
		return []verdict{
			equalCount(critM5Bought, in.k6Count("buy_bought"), float64(in.Slots)),
			m5ChaosErrRate(in), m5Verify(in), m5Dups(in),
		}
	case m5ChaosAPI:
		// Lượt đang bay lúc giết api mất kết nối (k6 đếm lỗi): không chấm tỷ lệ, chỉ chấm nhất quán.
		return []verdict{m5Verify(in), m5Dups(in)}
	case m5ChaosPG:
		saw := in.k6Count("buy_503")
		return []verdict{
			{Name: critM5ChaosSaw503, Want: "> 0", Got: fmt.Sprintf("%.0f", saw), Status: passIf(saw > 0)},
			equalCount(critM5Bought, in.k6Count("buy_bought"), float64(in.Slots)),
			m5ChaosErrRate(in), m5Verify(in), m5Dups(in),
		}
	}
	return []verdict{na("Bài "+in.Mode, "một trong các bài M5")}
}

func m5Dups(in m5Input) verdict {
	return verdict{Name: critM5NoDupRef, Want: "0", Got: fmt.Sprintf("%d", in.Dups), Status: passIf(in.Dups == 0)}
}

func m5ChaosErrRate(in m5Input) verdict {
	v := verdict{Name: critM5ChaosErrRate, Want: "< 0,1%", Status: statusNA, Got: "không có yêu cầu mua"}
	reqs := in.k6Count("buy_ms")
	if reqs <= 0 {
		return v
	}
	r := in.k6Count("buy_err") / reqs
	v.Got = fmt.Sprintf("%.0f / %.0f (%.3f%%)", in.k6Count("buy_err"), reqs, r*m5MaxPercent)
	v.Status = passIf(r < m5ChaosErrMax)
	return v
}

func equalCount(name string, got, want float64) verdict {
	return verdict{Name: name, Want: fmt.Sprintf("= %.0f", want), Got: fmt.Sprintf("%.0f", got), Status: passIf(got == want)}
}

func m5Verify(in m5Input) verdict {
	return verdict{
		Name: critM5Verify, Want: "mã thoát 0", Got: fmt.Sprintf("mã thoát %d", in.VerifyExit),
		Status: passIf(in.VerifyExit == 0),
	}
}

func m5DeadlocksVerdict(in m5Input) verdict {
	d := in.After.Deadlocks - in.Before.Deadlocks
	return verdict{Name: critM5Deadlocks, Want: "+0", Got: fmt.Sprintf("+%d", d), Status: passIf(d == 0)}
}

func m5Exhausted(in m5Input) verdict {
	v := verdict{Name: critM5RetryExhausted, Want: "< 1%", Status: statusNA, Got: "không có lượt mua"}
	buys := in.After.FlashBuyCount - in.Before.FlashBuyCount
	if buys <= 0 {
		return v
	}
	ex := in.After.TxRetryExhausted - in.Before.TxRetryExhausted
	r := float64(ex) / float64(buys)
	v.Got = fmt.Sprintf("%d / %d lượt (%.2f%%)", ex, buys, r*m5MaxPercent)
	v.Status = passIf(r < m5ExhaustedMax)
	return v
}

func m5CacheVerdict(in m5Input) verdict {
	v := verdict{Name: critM5CacheHit, Want: "> 95%", Status: statusNA, Got: "không có lần đọc bộ đệm"}
	hit := in.After.CacheHit - in.Before.CacheHit
	miss := in.After.CacheMiss - in.Before.CacheMiss
	if hit+miss <= 0 {
		return v
	}
	r := float64(hit) / float64(hit+miss)
	v.Got = fmt.Sprintf("%.2f%% (%d trúng, %d trượt)", r*m5MaxPercent, hit, miss)
	v.Status = passIf(r > m5CacheHitMin)
	return v
}

// renderM5 in bảng chấm và số đo khác (k6, máy chủ, tx_retry theo SQLSTATE, kết nối, FS14).
func renderM5(in m5Input, vs []verdict) string {
	var b strings.Builder
	renderVerdicts(&b, in.Mode, vs)
	k6 := func(metric, stat string) string {
		if v, ok := k6Of(in.K6, metric, stat); ok {
			return fmt.Sprintf("%.1f", v)
		}
		return "-"
	}
	fmt.Fprintf(&b, "- k6 lượt mua: bought %s, sold_out %s, over_limit %s, not_open %s; lần nhận 429 %s, 503 %s; "+
		"lượt bấm hết 10 lần gửi vẫn 503 %s; lỗi khác %s; thời gian phía k6 p50 %s / p95 %s / p99 %s ms; "+
		"%s yêu cầu mua, %s lượt k6 (%s/s)\n",
		k6("buy_bought", "count"), k6("buy_sold_out", "count"), k6("buy_over_limit", "count"),
		k6("buy_not_open", "count"), k6("buy_429", "count"), k6("buy_503", "count"), k6("buy_gave_up", "count"),
		k6("buy_err", "count"), k6("buy_ms", "med"), k6("buy_ms", "p(95)"), k6("buy_ms", "p(99)"),
		k6("buy_ms", "count"), k6("iterations", "count"), k6("iterations", "rate"))
	fmt.Fprintf(&b, "- k6 hoàn tất đơn: %s đạt, %s hết hạn giữ, %s lỗi; p95 %s ms\n", k6("complete_ok", "count"),
		k6("complete_expired", "count"), k6("complete_err", "count"), k6("complete_ms", "p(95)"))
	fmt.Fprintf(&b, "- k6 lượt đọc: p50 %s / p95 %s / p99 %s ms, %s lượt, %s lỗi\n", k6("read_ms", "med"),
		k6("read_ms", "p(95)"), k6("read_ms", "p(99)"), k6("read_ms", "count"), k6("read_err", "count"))
	fmt.Fprintf(&b, "- FS14 (giây từ lúc mở tới lượt bấm đầu của người mua được): p50 %s, p95 %s, lớn nhất %s; "+
		"của mọi người bấm: p50 %s, lớn nhất %s\n", k6("bought_arrival_s", "med"), k6("bought_arrival_s", "p(95)"),
		k6("bought_arrival_s", "max"), k6("arrival_s", "med"), k6("arrival_s", "max"))
	buy := histDelta(in.After.FlashBuyHist, in.Before.FlashBuyHist)
	fmt.Fprintf(&b, "- Máy chủ flash_buy_ms: %d lượt, histogram %s\n", in.After.FlashBuyCount-in.Before.FlashBuyCount,
		histLine(buy))
	fmt.Fprintf(&b, "- Máy chủ nhận việc sự kiện: histogram %s\n",
		histLine(histDelta(in.After.PickupEventHist, in.Before.PickupEventHist)))
	fmt.Fprintf(&b, "- tx_retry theo SQLSTATE: %s; hết lượt %d\n", retryLine(in.Before.TxRetry, in.After.TxRetry),
		in.After.TxRetryExhausted-in.Before.TxRetryExhausted)
	fmt.Fprintf(&b, "- flash_result: %s; bộ đệm %d trúng, %d trượt; kết nối PostgreSQL lúc cuối %d; deadlock +%d\n",
		retryLine(in.Before.FlashResults, in.After.FlashResults), in.After.CacheHit-in.Before.CacheHit,
		in.After.CacheMiss-in.Before.CacheMiss, in.After.Connections, in.After.Deadlocks-in.Before.Deadlocks)
	return b.String()
}

// retryLine in after − before theo nhãn, nhãn tăng dần.
func retryLine(before, after map[string]int64) string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(after)) {
		if b.Len() > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s %d", k, after[k]-before[k])
	}
	if b.Len() == 0 {
		return "không có"
	}
	return b.String()
}

// cmdM5Stats: loadkit m5stats -db <chủ bảng> -from T -out F → tệp JSON m5Stats.
func cmdM5Stats(ctx context.Context, fs *flag.FlagSet, args []string) error {
	dbURL := fs.String("db", "", "chuỗi kết nối chủ bảng (crm_owner)")
	fromS := fs.String("from", "", "mốc bắt đầu (RFC 3339)")
	out := fs.String("out", "", "tệp kết quả")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	if *out == "" {
		return fmt.Errorf("%w: thiếu -out", errUsage)
	}
	owner, from, err := openOwnerFrom(ctx, *dbURL, *fromS)
	if err != nil {
		return err
	}
	defer owner.Pool.Close()
	s, err := readM5Stats(ctx, owner, from)
	if err != nil {
		return err
	}
	return writeJSON(*out, s)
}

// cmdM5Report: loadkit m5report -mode <bài> -slots N -k6 F -before F -after F -verify-exit N; in bảng, KHÔNG ĐẠT
// (hay CHƯA ĐO) → errFailed (thoát 1).
func cmdM5Report(fs *flag.FlagSet, args []string) error {
	in := m5Input{}
	fs.StringVar(&in.Mode, "mode", "", "bài: "+strings.Join([]string{
		m5Spike, m5PerCustomer, m5Sustained, m5Abandon,
		m5Close, m5WebRead, m5ChaosValkey, m5ChaosAPI, m5ChaosPG,
	}, ", "))
	fs.IntVar(&in.Slots, "slots", 0, "số suất của đợt")
	fs.IntVar(&in.Dups, "dups", 0, "số external_ref có từ hai đơn (bài chaos_*)")
	fs.IntVar(&in.VerifyExit, "verify-exit", 0, "mã thoát của crm verify-flash")
	k6File := fs.String("k6", "", "tóm tắt k6 (SUMMARY_OUT)")
	before := fs.String("before", "", "m5stats trước bài")
	after := fs.String("after", "", "m5stats sau bài")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	if *k6File == "" || *before == "" || *after == "" {
		return fmt.Errorf("%w: thiếu -k6, -before hoặc -after", errUsage)
	}
	if err := readJSONFile(*k6File, &in.K6); err != nil {
		return err
	}
	if err := readJSONFile(*before, &in.Before); err != nil {
		return err
	}
	if err := readJSONFile(*after, &in.After); err != nil {
		return err
	}
	vs := evaluateM5(in)
	if _, err := fmt.Fprint(os.Stdout, renderM5(in, vs)); err != nil {
		return fmt.Errorf("in bảng: %w", err)
	}
	if !m5Passed(vs) {
		return errFailed
	}
	return nil
}
