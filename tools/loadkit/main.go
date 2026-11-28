// Lệnh loadkit là phần Go của bộ thử tải M0 (kế hoạch M0 Task 22, ADR-42), chạy cùng k6 qua loadtest/run.sh:
//
//	loadkit seed   -db <chủ bảng> -n 50 -tenant-key <khóa>     dựng tenant thử, 50 nhân viên giả, định tuyến selftest
//	loadkit access -listen 127.0.0.1:P -team host:P -out DIR     máy chủ khóa Access giả; ghi DIR/ca.pem, DIR/tokens.json
//	loadkit sse    -base <api> -tokens F -run R -out F           50 client SSE, đếm pong của lượt R, độ trễ, trùng
//	loadkit stats  -db <chủ bảng> -run R -from T [-to T]         một dòng JSON số đo database
//	loadkit report -mode M -start T -k6 F -sse F -db F …         chấm ngưỡng, in bảng markdown; có mục KHÔNG ĐẠT thì thoát 1
//
// Bộ thử tải M4 (kế hoạch M4 Task 180, loadtest/fb_run.sh):
//
//	loadkit fbseed  -db <chủ bảng> -api <gốc> -graph <máy giả> -tokens F -out fb.json   sản phẩm, tồn, Page, phiên live
//	loadkit sse     -fixture fb.json -db <chủ bảng> …          client SSE đo hội thoại tới màn hình (thay pong selftest)
//	loadkit fbend   -api <gốc> -graph <máy giả> -tokens F -fixture fb.json             kết thúc phiên live (đối chiếu)
//	loadkit fbstats -db <chủ bảng> -fixture fb.json -run R -from T [-graph <máy giả>]   một dòng JSON số đo
//	loadkit fbreport -mode fb|fbchaos -live F -staff F -sse F -series F …   chấm ngưỡng M4; KHÔNG ĐẠT thì thoát 1
//
// Bộ thử tải webhook hãng M3 (kế hoạch M3 Task M3-48, loadtest/carrier_run.sh):
//
//	loadkit carrierseed -db <chủ bảng> -api <gốc> -tokens F -n 3000 -out carrier.json   tài khoản hãng giả, n vận đơn
//
// Chỉ cho máy dev và máy thử: không bao giờ chạy với database production.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/testkit/loadstats"
)

var errUsage = errors.New("cách dùng: loadkit seed|access|sse|stats|report|fbseed|fbend|fbstats|fbreport|carrierseed [cờ]")

// errFailed: report có tiêu chí KHÔNG ĐẠT (thoát 1, bảng vẫn in ra).
var errFailed = errors.New("có tiêu chí không đạt")

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], logger)
	stop()
	if err != nil {
		if !errors.Is(err, errFailed) {
			logger.Error("loadkit", logging.Err(err))
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, logger *slog.Logger) error {
	if len(args) == 0 {
		return errUsage
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "seed":
		return cmdSeed(ctx, fs, args[1:], logger)
	case "access":
		return cmdAccess(ctx, fs, args[1:], logger)
	case "sse":
		return cmdSSE(ctx, fs, args[1:], logger)
	case "stats":
		return cmdStats(ctx, fs, args[1:])
	case "report":
		return cmdReport(fs, args[1:])
	case "fbseed":
		return cmdFBSeed(ctx, fs, args[1:], logger)
	case "fbend":
		return cmdFBEnd(ctx, fs, args[1:])
	case "fbstats":
		return cmdFBStats(ctx, fs, args[1:])
	case "fbreport":
		return cmdFBReport(fs, args[1:])
	case "carrierseed":
		return cmdCarrierSeed(ctx, fs, args[1:], logger)
	}
	return errUsage
}

func openOwner(ctx context.Context, url string) (*db.DB, error) {
	if url == "" {
		return nil, fmt.Errorf("%w: thiếu -db", errUsage)
	}
	d, err := db.Open(ctx, url, db.PoolOptions{MaxConns: 2})
	if err != nil {
		return nil, fmt.Errorf("mở database: %w", err)
	}
	return d, nil
}

func cmdSeed(ctx context.Context, fs *flag.FlagSet, args []string, logger *slog.Logger) error {
	dbURL := fs.String("db", "", "chuỗi kết nối chủ bảng (crm_owner)")
	n := fs.Int("n", 50, "số nhân viên giả")
	key := fs.String("tenant-key", "loadtest", "khóa định tuyến selftest (tenantKey của ping)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	owner, err := openOwner(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer owner.Pool.Close()
	id, err := seedStaff(ctx, owner, *n, *key)
	if err != nil {
		return err
	}
	logger.Info("seed xong", slog.String("tenant_id", id.String()), slog.Int("staff", *n))
	return nil
}

func cmdAccess(ctx context.Context, fs *flag.FlagSet, args []string, logger *slog.Logger) error {
	listen := fs.String("listen", "127.0.0.1:24222", "địa chỉ nghe của máy chủ khóa")
	team := fs.String("team", "host.docker.internal:24222", "ACCESS_TEAM_DOMAIN mà api dùng (host:port)")
	out := fs.String("out", "", "thư mục ghi ca.pem, tokens.json")
	n := fs.Int("n", 50, "số thẻ (nhân viên giả)")
	aud := fs.String("aud", "loadtest-aud", "ACCESS_AUD")
	claim := fs.String("mfa-claim", "amr", "ACCESS_MFA_CLAIM")
	ttl := fs.Duration("ttl", 12*time.Hour, "hạn của thẻ")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	if *out == "" {
		return fmt.Errorf("%w: thiếu -out", errUsage)
	}
	host, _, err := net.SplitHostPort(*team)
	if err != nil {
		return fmt.Errorf("đọc -team: %w", err)
	}
	kit, err := newAccessKit(*team, []string{host, "localhost", "127.0.0.1"})
	if err != nil {
		return err
	}
	if err := writeAccessFiles(*out, kit, *n, *aud, *claim, *ttl); err != nil {
		return err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", *listen)
	if err != nil {
		return fmt.Errorf("nghe %s: %w", *listen, err)
	}
	srv := &http.Server{Handler: kit.handler(), TLSConfig: kit.tlsConfig(), ReadHeaderTimeout: 5 * time.Second}
	logger.Info("máy chủ khóa Access giả đang chạy", slog.String("listen", *listen), slog.String("team", *team))
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("máy chủ khóa: %w", err)
	}
	return nil
}

func cmdSSE(ctx context.Context, fs *flag.FlagSet, args []string, logger *slog.Logger) error {
	base := fs.String("base", "http://127.0.0.1:4190", "gốc api")
	tokensPath := fs.String("tokens", "", "tokens.json của loadkit access")
	clients := fs.Int("clients", 50, "số client SSE (mỗi client một nhân viên)")
	runID := fs.String("run", "", "mã lượt chạy (phần run của eventId)")
	out := fs.String("out", "", "tệp kết quả JSON (ghi lại mỗi 10 s và khi dừng)")
	fixture := fs.String("fixture", "", "bài M4: fb.json của loadkit fbseed (đo conversation.changed thay pong selftest)")
	dbURL := fs.String("db", "", "bài M4: chuỗi chủ bảng, đọc hội thoại của sự kiện khi dừng")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	if *runID == "" || *out == "" || *tokensPath == "" {
		return fmt.Errorf("%w: thiếu -run, -out hoặc -tokens", errUsage)
	}
	b, err := os.ReadFile(*tokensPath) //nolint:gosec // công cụ đo chạy tay; đường dẫn do người chạy (run.sh) đưa vào
	if err != nil {
		return fmt.Errorf("đọc %s: %w", *tokensPath, err)
	}
	var toks []staffToken
	if err := json.Unmarshal(b, &toks); err != nil {
		return fmt.Errorf("đọc tokens.json: %w", err)
	}
	if len(toks) < *clients {
		return fmt.Errorf("%w: %d thẻ, cần %d", errUsage, len(toks), *clients)
	}
	if *fixture != "" {
		return runFBSSE(ctx, fbSSEOptions{base: *base, run: *runID, out: *out, fixture: *fixture, db: *dbURL}, toks[:*clients], logger)
	}
	col := newCollector(*clients, *runID)
	var wg sync.WaitGroup
	for i := range *clients {
		wg.Go(func() { runClient(ctx, sseClientOptions{Base: *base, Token: toks[i].Token, Index: i}, col) })
	}
	logger.Info("client SSE đang chạy", slog.Int("clients", *clients), slog.String("run", *runID))
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return writeJSON(*out, col.result())
		case <-tick.C:
			if err := writeJSON(*out, col.result()); err != nil {
				return err
			}
		}
	}
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("mã hóa %s: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil { //nolint:gosec // tệp -out do người chạy (run.sh) đưa vào
		return fmt.Errorf("ghi %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil { //nolint:gosec // như trên
		return fmt.Errorf("đổi tên %s: %w", tmp, err)
	}
	return nil
}

func parseTime(s string) (time.Time, error) {
	if s == "" || s == "now" {
		return time.Now().UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("đọc thời điểm %q: %w", s, err)
	}
	return t, nil
}

func cmdStats(ctx context.Context, fs *flag.FlagSet, args []string) error {
	dbURL := fs.String("db", "", "chuỗi kết nối chủ bảng")
	runID := fs.String("run", "", "mã lượt chạy")
	fromS := fs.String("from", "", "đầu khung (RFC3339)")
	toS := fs.String("to", "now", "cuối khung (RFC3339 hoặc now)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	from, err := parseTime(*fromS)
	if err != nil {
		return err
	}
	to, err := parseTime(*toS)
	if err != nil {
		return err
	}
	if *runID == "" || *fromS == "" {
		return fmt.Errorf("%w: thiếu -run hoặc -from", errUsage)
	}
	owner, err := openOwner(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer owner.Pool.Close()
	s, err := readStats(ctx, owner, *runID, from, to)
	if err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("mã hóa số đo: %w", err)
	}
	_, err = fmt.Fprintln(os.Stdout, string(b))
	return err //nolint:wrapcheck // ghi stdout
}

type reportFiles struct {
	k6, sse, db, series, stats, chaos string
}

func cmdReport(fs *flag.FlagSet, args []string) error {
	in := reportInput{}
	var f reportFiles
	var startS, project string
	fs.StringVar(&in.Mode, "mode", modeNormal, "normal|heavy|chaos|soak")
	fs.StringVar(&startS, "start", "", "lúc bắt đầu tải (RFC3339)")
	fs.StringVar(&f.k6, "k6", "", "summary JSON của k6 (common.js handleSummary)")
	fs.StringVar(&f.sse, "sse", "", "kết quả loadkit sse")
	fs.StringVar(&f.db, "db", "", "số đo database cuối (một dòng loadkit stats)")
	fs.StringVar(&f.series, "series", "", "số đo database theo thời gian (các dòng loadkit stats)")
	fs.StringVar(&f.stats, "stats", "", "CSV docker stats của run.sh")
	fs.StringVar(&f.chaos, "chaos", "", "CSV của chaos.sh")
	fs.StringVar(&project, "project", "crm", "tên dự án compose (bỏ khỏi tên container)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	if !slices.Contains([]string{modeNormal, modeHeavy, modeChaos, modeSoak}, in.Mode) {
		return fmt.Errorf("%w: -mode %q", errUsage, in.Mode)
	}
	start, err := parseTime(startS)
	if err != nil {
		return err
	}
	in.Start = start
	if err := loadReportFiles(&in, f, project); err != nil {
		return err
	}
	vs := evaluate(in)
	fmt.Print(renderMarkdown(in, vs)) //nolint:forbidigo // đầu ra của lệnh là bảng markdown
	for _, v := range vs {
		if v.Status == statusFail {
			return errFailed
		}
	}
	return nil
}

func loadReportFiles(in *reportInput, f reportFiles, project string) error {
	for _, j := range []struct {
		path string
		dst  any
	}{{f.k6, &in.K6}, {f.db, &in.DB}} {
		if err := readJSONFile(j.path, j.dst); err != nil {
			return err
		}
	}
	if f.sse != "" {
		in.SSE = &sseResult{}
		if err := readJSONFile(f.sse, in.SSE); err != nil {
			return err
		}
	}
	var err error
	if in.DBSeries, err = readSeries(f.series); err != nil {
		return err
	}
	if err := withFile(f.stats, func(r io.Reader) (err error) {
		in.Stats, err = parseDockerStats(r, project)
		return err
	}); err != nil {
		return err
	}
	return withFile(f.chaos, func(r io.Reader) (err error) {
		in.Chaos, err = parseChaos(r)
		return err
	})
}

// readSeries đọc các dòng JSON của loadkit stats; dòng hỏng bị bỏ qua. path rỗng: không có.
func readSeries(path string) ([]dbStats, error) {
	var out []dbStats
	err := withFile(path, func(r io.Reader) error {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			var s dbStats
			if json.Unmarshal(sc.Bytes(), &s) == nil {
				out = append(out, s)
			}
		}
		return sc.Err()
	})
	return out, err
}

// withFile mở path và gọi fn; path rỗng thì bỏ qua (tệp không bắt buộc).
func withFile(path string, fn func(io.Reader) error) error {
	if path == "" {
		return nil
	}
	r, err := os.Open(path) //nolint:gosec // công cụ đo chạy tay; đường dẫn do người chạy (run.sh) đưa vào
	if err != nil {
		return fmt.Errorf("mở %s: %w", path, err)
	}
	defer func() { _ = r.Close() }()
	if err := fn(r); err != nil {
		return fmt.Errorf("đọc %s: %w", path, err)
	}
	return nil
}

// readJSONFile đọc path vào v; path rỗng thì bỏ qua.
func readJSONFile(path string, v any) error {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path) //nolint:gosec // công cụ đo chạy tay; đường dẫn do người chạy (run.sh) đưa vào
	if err != nil {
		return fmt.Errorf("đọc %s: %w", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("đọc JSON %s: %w", path, err)
	}
	return nil
}

type fbSSEOptions struct {
	base, run, out, fixture, db string
}

// runFBSSE giữ một client SSE cho mỗi thẻ tới khi ctx hủy (SIGINT của fb_run.sh), rồi đọc hội thoại của các sự kiện
// của lượt từ database và ghi kết quả độ trễ (fbSSEResult) ra o.out.
func runFBSSE(ctx context.Context, o fbSSEOptions, toks []staffToken, logger *slog.Logger) error {
	f, err := readFixture(o.fixture)
	if err != nil {
		return err
	}
	owner, err := openOwner(context.WithoutCancel(ctx), o.db)
	if err != nil {
		return err
	}
	defer owner.Pool.Close()
	sink := newFBSink(len(toks))
	var wg sync.WaitGroup
	for i, t := range toks {
		wg.Go(func() { runClient(ctx, sseClientOptions{Base: o.base, Token: t.Token, Index: i}, sink) })
	}
	logger.Info("client SSE (M4) đang chạy", slog.Int("clients", len(toks)), slog.String("run", o.run))
	<-ctx.Done()
	wg.Wait()
	inbox, comment := fbPrefixes(o.run)
	events, err := loadstats.Events(context.WithoutCancel(ctx), owner, f.TenantID, inbox, comment)
	if err != nil {
		return fmt.Errorf("ghép khung với sự kiện của lượt: %w", err)
	}
	return writeJSON(o.out, sink.result(o.run, events))
}

// fbStatsLine là một dòng của loadkit fbstats.
type fbStatsLine struct {
	DB    fbDBStats     `json:"db"`
	Graph *fbGraphStats `json:"graph,omitempty"`
}

func cmdFBStats(ctx context.Context, fs *flag.FlagSet, args []string) error {
	dbURL := fs.String("db", "", "chuỗi kết nối chủ bảng")
	fixture := fs.String("fixture", "", "fb.json của loadkit fbseed")
	runID := fs.String("run", "", "mã lượt chạy")
	fromS := fs.String("from", "", "lúc bắt đầu tải (RFC3339)")
	graph := fs.String("graph", "", "gốc máy Meta giả (có: đọc thêm lời gọi Send API)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	if *runID == "" || *fromS == "" {
		return fmt.Errorf("%w: thiếu -run hoặc -from", errUsage)
	}
	from, err := parseTime(*fromS)
	if err != nil {
		return err
	}
	f, err := readFixture(*fixture)
	if err != nil {
		return err
	}
	owner, err := openOwner(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer owner.Pool.Close()
	var line fbStatsLine
	inbox, comment := fbPrefixes(*runID)
	if line.DB, err = loadstats.Read(ctx, owner, f.TenantID, inbox, comment, from); err != nil {
		return fmt.Errorf("số đo database: %w", err)
	}
	if *graph != "" {
		g, err := readGraphStats(ctx, &http.Client{Timeout: 60 * time.Second}, *graph, f, *runID)
		if err != nil {
			return err
		}
		line.Graph = &g
	}
	b, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("mã hóa số đo: %w", err)
	}
	_, err = fmt.Fprintln(os.Stdout, string(b))
	return err //nolint:wrapcheck // ghi stdout
}
