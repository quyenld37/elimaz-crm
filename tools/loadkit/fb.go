package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/catalog"
	"elimaz.com.vn/crm/internal/modules/inventory"
	"elimaz.com.vn/crm/internal/modules/order"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/platform/tenancy"
	"elimaz.com.vn/crm/internal/testkit/loadstats"
)

// Bộ thử tải M4 (kế hoạch M4 Task 180): dữ liệu thử dựng qua API thật của CRM (sản phẩm, phiếu nhập, kết nối Page
// bằng mã system user của máy Meta giả, bật gửi, theo dõi live) để token Page được mã hóa và định tuyến Page →
// tenant đúng như máy thật; chỉ phần không có API (người bán, kho, cài đặt đơn mặc định) dựng bằng chủ bảng như
// `crm seed-dev`.

// fbFixture là kết quả của loadkit fbseed (fb.json): k6 và loadkit đọc lại.
type fbFixture struct {
	TenantID    uuid.UUID `json:"tenantId"`
	WarehouseID uuid.UUID `json:"warehouseId"`
	Variants    []string  `json:"variants"`
	StockEach   int       `json:"stockEach"`
	Pages       []fbPage  `json:"pages"`
}

type fbPage struct {
	PageID    string `json:"pageId"`
	AccountID string `json:"accountId"`
	VideoID   string `json:"videoId"`
	SessionID string `json:"sessionId"`
}

// fbEventKind là loại sự kiện k6 tạo, đọc từ tiền tố mã (fb_common.js).
type fbEventKind string

const (
	fbInbox   fbEventKind = "m"  // tin hộp thư: mid m_<run>_<ms>_<vu>_<lượt>
	fbComment fbEventKind = "lc" // comment live: comment_id lc_<run>_<ms>_<vu>_<lượt>
)

// fbEventID dựng mã sự kiện như fb_common.js eventID.
func fbEventID(kind fbEventKind, run string, sent time.Time, vu, iter int) string {
	return fmt.Sprintf("%s_%s_%d_%d_%d", kind, run, sent.UnixMilli(), vu, iter)
}

// fbPrefixes là tiền tố mã của tin, comment của lượt run (để đọc từ database).
func fbPrefixes(run string) (inbox, comment string) {
	return string(fbInbox) + "_" + run + "_", string(fbComment) + "_" + run + "_"
}

// fbDBStats là số đo database của bài M4 (internal/testkit/loadstats).
type fbDBStats = loadstats.Stats

// parseFBEventID tách loại và lúc gửi của mã sự kiện của lượt run; ok false khi không đúng dạng hay lượt khác. Phần
// sau loại cùng dạng eventId của bộ M0 (run, ms, VU, lượt), nên đọc bằng parseEventID.
func parseFBEventID(id, run string) (fbEventKind, time.Time, bool) {
	kind, rest, ok := strings.Cut(id, "_")
	if !ok || (kind != string(fbInbox) && kind != string(fbComment)) || strings.Contains(rest, ".") {
		return "", time.Time{}, false
	}
	r, sent, ok := parseEventID("lt." + strings.ReplaceAll(rest, "_", "."))
	if !ok || r != run {
		return "", time.Time{}, false
	}
	return fbEventKind(kind), sent, true
}

// parseSendMarker tách lúc bấm gửi của tin nhân viên "lt <run> <ms> <vu> <lượt>" (fb_common.js sendOne).
func parseSendMarker(text, run string) (time.Time, bool) {
	f := strings.Fields(text)
	if len(f) != 5 || f[0] != "lt" || f[1] != run {
		return time.Time{}, false
	}
	ms, err := strconv.ParseInt(f[2], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(ms), true
}

// errAPI: API của CRM trả mã không mong đợi.
var errAPI = errors.New("API trả lỗi")

// devUserToken là token người dùng dev của máy giả tools/fakegraph (quản trị mọi Page của máy giả).
const devUserToken = "EAAB-dev-user-token" //nolint:gosec // G101: token giả của máy giả, không phải bí mật

// consoleOrigin là CONSOLE_ORIGINS của api trong bài thử (fb_lib.sh): yêu cầu ghi phải mang Origin của màn hình.
const consoleOrigin = "http://localhost:4192"

// apiClient gọi API của CRM bằng thẻ Access của một nhân viên, như màn hình (Origin của màn hình).
type apiClient struct {
	base, token string
	hc          *http.Client
}

// call gửi method path với body (nil: không thân) và giải JSON trả về vào out (nil: bỏ); yêu cầu ghi có
// Idempotency-Key mới. Mã khác want → errAPI kèm mã lỗi của thân.
func (c apiClient) call(ctx context.Context, method, path string, body, out any, want int) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("mã hóa thân %s: %w", path, err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd) //nolint:gosec // G704: gốc api do người chạy đưa vào cờ -api
	if err != nil {
		return fmt.Errorf("dựng %s %s: %w", method, path, err)
	}
	req.Header.Set("Cf-Access-Jwt-Assertion", c.token)
	req.Header.Set("Origin", consoleOrigin)
	req.Header.Set("Content-Type", "application/json") // route ghi đòi cả khi không có thân (chống CSRF)
	if method != http.MethodGet {
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}
	resp, err := c.hc.Do(req) //nolint:gosec // G704: gốc api do người chạy (fb_run.sh, m5_run.sh) đưa vào cờ -api
	if err != nil {
		return fmt.Errorf("gọi %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != want {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &e)
		return fmt.Errorf("%w: %s %s → %d %s", errAPI, method, path, resp.StatusCode, e.Error)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("đọc kết quả %s: %w", path, err)
	}
	return nil
}

// openLoadShop mở database bằng chủ bảng, đọc tenant thử (loadkit seed đã dựng) và dựng cửa hàng của nó (seedShop);
// trả pool (bên gọi đóng), tenant, kho. Lỗi sau khi mở: pool đã đóng.
func openLoadShop(ctx context.Context, dbURL string) (*db.DB, uuid.UUID, uuid.UUID, error) {
	owner, err := openOwner(ctx, dbURL)
	if err != nil {
		return nil, uuid.Nil, uuid.Nil, err
	}
	tenantID, err := tenancy.SeedTenant(ctx, owner, loadTenantName)
	if err != nil {
		owner.Pool.Close()
		return nil, uuid.Nil, uuid.Nil, fmt.Errorf("đọc tenant thử: %w", err)
	}
	wh, err := seedShop(ctx, owner, tenantID)
	if err != nil {
		owner.Pool.Close()
		return nil, uuid.Nil, uuid.Nil, err
	}
	return owner, tenantID, wh, nil
}

// seedShop (chủ bảng) dựng người bán ELIMAZ, kho MAIN và cài đặt đơn mặc định của tenant (như `crm seed-dev`); trả kho.
func seedShop(ctx context.Context, owner *db.DB, tenantID uuid.UUID) (uuid.UUID, error) {
	var wh uuid.UUID
	err := tenancy.WithTenant(ctx, owner, tenantID, func(tx pgx.Tx) error {
		seller, err := catalog.EnsureDefaultSeller(ctx, tx, tenantID)
		if err != nil {
			return err //nolint:wrapcheck // đã bọc trong catalog
		}
		if wh, err = inventory.EnsureDefaultWarehouse(ctx, tx, tenantID, seller); err != nil {
			return err //nolint:wrapcheck // đã bọc trong inventory
		}
		return order.EnsureDefaults(ctx, tx, tenantID)
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("seed người bán, kho, cài đặt đơn: %w", err)
	}
	return wh, nil
}

type fbSeedOptions struct {
	Pages, Variants, StockEach int
	UserToken                  string // token người dùng dev của máy giả (tools/fakegraph devUserToken), quản trị mọi Page
	PageBase                   int64  // mã Page đầu tiên của máy giả (FAKEGRAPH_PAGE_ID)
	VideoBase                  int64  // mã video live thứ i = VideoBase + i
	Graph                      string // gốc máy Meta giả (đường /_fake/*)
}

// seedFB dựng phần API của dữ liệu thử: một mẫu có o.Variants biến thể, phiếu nhập o.StockEach mỗi biến thể ở kho
// wh rồi ghi sổ; kết nối o.Pages Page của máy giả, bật gửi; mỗi Page một video live ở máy giả và một phiên theo dõi
// tay.
func seedFB(ctx context.Context, c apiClient, hc *http.Client, wh uuid.UUID, o fbSeedOptions) (fbFixture, error) {
	f := fbFixture{WarehouseID: wh, StockEach: o.StockEach}
	_, vars, err := seedProduct(ctx, c, o.Variants)
	if err != nil {
		return f, err
	}
	f.Variants = vars
	if err := seedStock(ctx, c, wh, vars, o.StockEach); err != nil {
		return f, err
	}
	pages, err := connectPages(ctx, c, o.UserToken, o.PageBase, o.Pages)
	if err != nil {
		return f, err
	}
	for i, p := range pages {
		video := strconv.FormatInt(o.VideoBase+int64(i), 10)
		if err := postFake(ctx, hc, o.Graph+"/_fake/live-video",
			map[string]string{"videoId": video, "pageId": p.PageID, "status": "LIVE"}); err != nil {
			return f, err
		}
		var s struct {
			ID string `json:"id"`
		}
		if err := c.call(ctx, http.MethodPost, "/api/v1/channels/"+p.AccountID+"/live-sessions",
			map[string]string{"videoIdOrUrl": video}, &s, http.StatusCreated); err != nil {
			return f, err
		}
		p.VideoID, p.SessionID = video, s.ID
		f.Pages = append(f.Pages, p)
	}
	return f, nil
}

// seedProduct tạo một mẫu đang bán có n biến thể; trả mã mẫu và mã biến thể.
func seedProduct(ctx context.Context, c apiClient, n int) (string, []string, error) {
	type variant struct {
		SKU   string `json:"sku"`
		Color string `json:"color"`
		Size  string `json:"size"`
		Price int64  `json:"price"`
	}
	in := struct {
		Code     string    `json:"code"`
		Name     string    `json:"name"`
		Status   string    `json:"status"`
		Variants []variant `json:"variants"`
	}{Code: "LT" + strings.ToUpper(rand.Text()[:6]), Name: "Áo thử tải", Status: "active"}
	for i := 1; i <= n; i++ {
		in.Variants = append(in.Variants, variant{SKU: fmt.Sprintf("%s-%02d", in.Code, i), Color: "Đỏ", Size: fmt.Sprintf("S%02d", i), Price: 199_000})
	}
	var out struct {
		ID       string `json:"id"`
		Variants []struct {
			ID string `json:"id"`
		} `json:"variants"`
	}
	if err := c.call(ctx, http.MethodPost, "/api/v1/catalog/products", in, &out, http.StatusCreated); err != nil {
		return "", nil, err
	}
	ids := make([]string, 0, len(out.Variants))
	for _, v := range out.Variants {
		ids = append(ids, v.ID)
	}
	if len(ids) != n {
		return "", nil, fmt.Errorf("%w: mẫu thử có %d biến thể, cần %d", errAPI, len(ids), n)
	}
	return out.ID, ids, nil
}

func seedStock(ctx context.Context, c apiClient, wh uuid.UUID, variants []string, each int) error {
	var doc struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	if err := c.call(ctx, http.MethodPost, "/api/v1/inventory/documents",
		map[string]string{"warehouseId": wh.String(), "kind": "import"}, &doc, http.StatusCreated); err != nil {
		return err
	}
	type line struct {
		VariantID string `json:"variantId"`
		Quantity  int    `json:"quantity"`
	}
	lines := make([]line, 0, len(variants))
	for _, v := range variants {
		lines = append(lines, line{VariantID: v, Quantity: each})
	}
	if err := c.call(ctx, http.MethodPut, "/api/v1/inventory/documents/"+doc.ID+"/lines",
		map[string]any{"version": doc.Version, "lines": lines}, &doc, http.StatusOK); err != nil {
		return err
	}
	return c.call(ctx, http.MethodPost, "/api/v1/inventory/documents/"+doc.ID+"/post",
		map[string]int64{"version": doc.Version}, nil, http.StatusOK)
}

// connectPages nối n Page pageBase, pageBase+1… bằng đường "mã system user" của màn Kênh (token chỉ sống trong yêu
// cầu; CRM lấy token Page từ máy giả, mã hóa, ghi định tuyến), rồi bật gửi (Page mới luôn chỉ xem). Đường đăng nhập
// Facebook (OAuth) không dùng được với máy giả: máy giả chưa đổi được token dài hạn (fb_exchange_token; tệp task-180,
// Việc phát hiện thêm).
func connectPages(ctx context.Context, c apiClient, token string, pageBase int64, n int) ([]fbPage, error) {
	out := make([]fbPage, 0, n)
	for i := range n {
		page := strconv.FormatInt(pageBase+int64(i), 10)
		var acc struct {
			ID      string `json:"id"`
			Version int32  `json:"version"`
		}
		if err := c.call(ctx, http.MethodPost, "/api/v1/channels/facebook/pages",
			map[string]string{"pageId": page, "systemUserToken": token}, &acc, http.StatusOK); err != nil {
			return nil, err
		}
		if err := c.call(ctx, http.MethodPut, "/api/v1/channels/"+acc.ID+"/send-mode",
			map[string]any{"mode": "enabled", "expectedVersion": acc.Version}, nil, http.StatusOK); err != nil {
			return nil, err
		}
		out = append(out, fbPage{PageID: page, AccountID: acc.ID})
	}
	return out, nil
}

// postFake gửi một lệnh điều khiển /_fake/* của máy giả.
func postFake(ctx context.Context, hc *http.Client, u string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("mã hóa lệnh máy giả: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b)) //nolint:gosec // G704: địa chỉ máy giả do người chạy đưa vào cờ -graph
	if err != nil {
		return fmt.Errorf("dựng lệnh máy giả: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req) //nolint:gosec // G704: địa chỉ máy giả do người chạy (fb_run.sh) đưa vào cờ -graph
	if err != nil {
		return fmt.Errorf("gọi máy giả %s: %w", u, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: máy giả %s → %d", errAPI, u, resp.StatusCode)
	}
	return nil
}

// endSessions kết thúc tay mọi phiên live của f (CRM ghi lệnh đối chiếu cuối phiên) và đặt video ở máy giả là đã dừng.
func endSessions(ctx context.Context, c apiClient, hc *http.Client, graph string, f fbFixture) error {
	for _, p := range f.Pages {
		if err := postFake(ctx, hc, graph+"/_fake/live-video",
			map[string]string{"videoId": p.VideoID, "pageId": p.PageID, "status": "LIVE_STOPPED"}); err != nil {
			return err
		}
		if err := c.call(ctx, http.MethodPost, "/api/v1/live-sessions/"+p.SessionID+"/end", nil, nil, http.StatusOK); err != nil {
			return err
		}
	}
	return nil
}

func readFixture(path string) (fbFixture, error) {
	var f fbFixture
	if err := readJSONFile(path, &f); err != nil {
		return f, err
	}
	if len(f.Pages) == 0 {
		return f, fmt.Errorf("%w: %s không có Page", errUsage, path)
	}
	return f, nil
}

func firstToken(path string) (string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // công cụ đo chạy tay; đường dẫn do người chạy (fb_run.sh) đưa vào
	if err != nil {
		return "", fmt.Errorf("đọc %s: %w", path, err)
	}
	var toks []staffToken
	if err := json.Unmarshal(b, &toks); err != nil || len(toks) == 0 {
		return "", fmt.Errorf("%w: tokens.json rỗng hay hỏng", errUsage)
	}
	return toks[0].Token, nil
}

// cmdFBSeed: loadkit fbseed -db <chủ bảng> -api <gốc> -graph <gốc máy giả> -tokens F -out fb.json. Chạy sau
// loadkit seed (tenant, nhân viên); nhân viên 1 (owner) gọi API.
func cmdFBSeed(ctx context.Context, fs *flag.FlagSet, args []string, logger *slog.Logger) error {
	sf := addSeedFlags(fs, "fb.json")
	graph := fs.String("graph", "http://127.0.0.1:4293", "gốc máy Meta giả")
	o := fbSeedOptions{UserToken: devUserToken, PageBase: 100_000_000_000_001, VideoBase: 500_000_000_000_001}
	fs.IntVar(&o.Pages, "pages", 2, "số Page")
	fs.IntVar(&o.Variants, "variants", 20, "số biến thể")
	fs.IntVar(&o.StockEach, "stock", 500, "tồn mỗi biến thể")
	sh, err := sf.prepare(ctx, fs, args, nil)
	if err != nil {
		return err
	}
	o.Graph = *graph
	hc := &http.Client{Timeout: 30 * time.Second}
	f, err := seedFB(ctx, apiClient{base: *sf.api, token: sh.token, hc: hc}, hc, sh.warehouse, o)
	if err != nil {
		return err
	}
	f.TenantID = sh.tenantID
	logger.Info("fbseed xong", slog.Int("pages", len(f.Pages)), slog.Int("variants", len(f.Variants)))
	return writeJSON(*sf.out, f)
}

// seedFlags là các cờ chung của fbseed, m2seed, m5key, m5sale.
type seedFlags struct{ db, api, tokens, out *string }

func addSeedFlags(fs *flag.FlagSet, outFile string) seedFlags {
	return seedFlags{
		db:     fs.String("db", "", "chuỗi kết nối chủ bảng (crm_owner)"),
		api:    fs.String("api", "http://127.0.0.1:4290", "gốc api"),
		tokens: fs.String("tokens", "", "tokens.json của loadkit access"),
		out:    fs.String("out", "", "tệp "+outFile),
	}
}

// prepare đọc cờ args vào fs, đòi đủ -out, -tokens rồi prepareShop.
func (f seedFlags) prepare(
	ctx context.Context, fs *flag.FlagSet, args []string, extra func(context.Context, *db.DB) error,
) (shopSetup, error) {
	if err := fs.Parse(args); err != nil {
		return shopSetup{}, fmt.Errorf("đọc cờ: %w", err)
	}
	if *f.out == "" || *f.tokens == "" {
		return shopSetup{}, fmt.Errorf("%w: thiếu -out hoặc -tokens", errUsage)
	}
	return prepareShop(ctx, *f.db, *f.tokens, extra)
}

// shopSetup là phần chung của fbseed, m2seed, m5key, m5sale: tenant thử, kho của người bán thử, token của nhân viên 1 (owner).
type shopSetup struct {
	tenantID, warehouse uuid.UUID
	token               string
}

// prepareShop mở chủ bảng dbURL, lấy tenant thử (của loadkit seed), dựng người bán, kho (seedShop), chạy extra trên chủ
// bảng (nil: bỏ qua) rồi đọc token đầu của tokens; đóng chủ bảng trước khi trả.
func prepareShop(ctx context.Context, dbURL, tokens string, extra func(context.Context, *db.DB) error) (shopSetup, error) {
	var sh shopSetup
	owner, tenantID, wh, err := openLoadShop(ctx, dbURL)
	if err != nil {
		return sh, err
	}
	defer owner.Pool.Close()
	sh.tenantID, sh.warehouse = tenantID, wh
	if extra != nil {
		if err := extra(ctx, owner); err != nil {
			return sh, err
		}
	}
	sh.token, err = firstToken(tokens)
	return sh, err
}

// cmdFBEnd: loadkit fbend -api <gốc> -graph <gốc máy giả> -tokens F -fixture fb.json — kết thúc các phiên live.
func cmdFBEnd(ctx context.Context, fs *flag.FlagSet, args []string) error {
	api := fs.String("api", "http://127.0.0.1:4290", "gốc api")
	graph := fs.String("graph", "http://127.0.0.1:4293", "gốc máy Meta giả")
	tokens := fs.String("tokens", "", "tokens.json của loadkit access")
	fixture := fs.String("fixture", "", "fb.json của loadkit fbseed")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	f, err := readFixture(*fixture)
	if err != nil {
		return err
	}
	tok, err := firstToken(*tokens)
	if err != nil {
		return err
	}
	hc := &http.Client{Timeout: 30 * time.Second}
	return endSessions(ctx, apiClient{base: *api, token: tok, hc: hc}, hc, *graph, f)
}
