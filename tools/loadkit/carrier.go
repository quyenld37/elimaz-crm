package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Dữ liệu của bài thử tải webhook hãng (kế hoạch M3, Task M3-48; ADR-42): tài khoản hãng giả fake-1 có tuyến webhook,
// địa chỉ giao đã khớp mã của hãng giả, n đơn 1 cái, mỗi đơn một vận đơn tạo qua API (lệnh tạo của worker gửi hãng giả
// thật) tới khi đủ n vận đơn chờ thu gom có mã hãng. k6 carrier_webhooks.js đọc carrier.json.

// carrierFixture là carrier.json: tenant, khóa tuyến /hooks/fake/<routeKey>, mã hãng của các vận đơn.
type carrierFixture struct {
	TenantID  uuid.UUID `json:"tenantId"`
	RouteKey  string    `json:"routeKey"`
	Trackings []string  `json:"trackings"`
}

// Mã tỉnh, phường của địa chỉ giao thử: hãng giả (tools/fakecarrier /v1/locations) có cùng mã.
const (
	carrierProvince = "79"
	carrierWard     = "26734"
	// carrierWorkers: số yêu cầu tạo đơn, vận đơn cùng lúc (chia đều cho các nhân viên thử, dưới giới hạn 300/60 s).
	carrierWorkers = 16
)

var errCarrierSeed = errors.New("dựng dữ liệu thử hãng")

// cmdCarrierSeed: loadkit carrierseed -db <chủ bảng> -api <gốc> -tokens F -n 3000 -out carrier.json. Chạy sau loadkit
// seed (tenant, nhân viên) khi ba vai trò và hãng giả đang chạy.
func cmdCarrierSeed(ctx context.Context, fs *flag.FlagSet, args []string, logger *slog.Logger) error {
	dbURL := fs.String("db", "", "chuỗi kết nối chủ bảng (crm_owner)")
	api := fs.String("api", "http://127.0.0.1:4290", "gốc api")
	tokens := fs.String("tokens", "", "tokens.json của loadkit access")
	out := fs.String("out", "", "tệp carrier.json")
	n := fs.Int("n", 3000, "số vận đơn")
	wait := fs.Duration("wait", 10*time.Minute, "hạn chờ đủ vận đơn chờ thu gom")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	if *out == "" || *tokens == "" || *n <= 0 {
		return fmt.Errorf("%w: thiếu -out, -tokens hoặc -n ≤ 0", errUsage)
	}
	owner, tenantID, wh, err := openLoadShop(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer owner.Pool.Close()
	account, route, err := seedCarrierAccount(ctx, owner, tenantID, wh)
	if err != nil {
		return err
	}
	toks, err := allTokens(*tokens)
	if err != nil {
		return err
	}
	hc := &http.Client{Timeout: 30 * time.Second}
	clients := make([]apiClient, len(toks))
	for i, tok := range toks {
		clients[i] = apiClient{base: *api, token: tok, hc: hc}
	}
	_, vars, err := seedProduct(ctx, clients[0], 1)
	if err != nil {
		return err
	}
	if err := seedStock(ctx, clients[0], wh, vars, *n); err != nil {
		return err
	}
	ids, err := shipOrders(ctx, clients, vars[0], account, *n)
	if err != nil {
		return err
	}
	trackings, err := awaitTrackings(ctx, owner, tenantID, ids, *wait)
	if err != nil {
		return err
	}
	logger.Info("carrierseed xong", slog.Int("shipments", len(trackings)))
	return writeJSON(*out, carrierFixture{TenantID: tenantID, RouteKey: route, Trackings: trackings})
}

// shipOrders lên n đơn 1 cái của biến thể variant (ship 30.000) và gửi hãng bằng tài khoản account, carrierWorkers
// yêu cầu cùng lúc, nhân viên thứ i % len(clients); trả mã vận đơn thứ i ở vị trí i.
func shipOrders(ctx context.Context, clients []apiClient, variant string, account uuid.UUID, n int) ([]uuid.UUID, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	next := make(chan int)
	ids := make([]uuid.UUID, n)
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	for range carrierWorkers {
		wg.Go(func() {
			for i := range next {
				id, err := shipOne(ctx, clients[i%len(clients)], variant, account, i)
				ids[i] = id // mỗi i chỉ một goroutine ghi
				if err != nil {
					mu.Lock()
					if first == nil {
						first = err
						cancel()
					}
					mu.Unlock()
				}
			}
		})
	}
feed:
	for i := range n {
		select {
		case next <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(next)
	wg.Wait()
	return ids, first
}

func shipOne(ctx context.Context, c apiClient, variant string, account uuid.UUID, i int) (uuid.UUID, error) {
	var o struct {
		ID          string `json:"id"`
		WarehouseID string `json:"warehouseId"`
		Status      string `json:"status"`
	}
	if err := c.call(ctx, http.MethodPost, "/api/v1/orders", map[string]any{
		"customer": map[string]any{"fullName": fmt.Sprintf("Khách thử %d", i)},
		"items":    []map[string]any{{"variantId": variant, "qty": 1}},
		"shipAddress": map[string]any{
			"name": "Khách thử", "phone": "+84900000000", "line": "Địa chỉ thử", "provinceCode": carrierProvince,
			"wardCode": carrierWard,
		},
		"shippingFee": 30000,
	}, &o, http.StatusCreated); err != nil {
		return uuid.Nil, err
	}
	var sh struct {
		ID uuid.UUID `json:"id"`
	}
	err := c.call(ctx, http.MethodPost, "/api/v1/orders/"+o.ID+"/shipments",
		map[string]any{"carrierAccountId": account}, &sh, http.StatusAccepted)
	if err == nil {
		return sh.ID, nil
	}
	// Chẩn đoán: tài khoản hãng (trạng thái, chế độ, kho) và đơn (trạng thái, kho) lúc bị chặn.
	var page struct {
		Items []struct {
			ID, Status, Mode, WarehouseID, CarrierKey string
		} `json:"items"`
	}
	_ = c.call(ctx, http.MethodGet, "/api/v1/carrier-accounts", nil, &page, http.StatusOK)
	return uuid.Nil, fmt.Errorf("gửi hãng đơn %s (đơn %s, kho %s; tài khoản %+v): %w", o.ID, o.Status, o.WarehouseID, page.Items, err)
}

// allTokens đọc mọi thẻ của tokens.json.
func allTokens(path string) ([]string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // công cụ đo chạy tay; đường dẫn do người chạy (carrier_run.sh) đưa vào
	if err != nil {
		return nil, fmt.Errorf("đọc %s: %w", path, err)
	}
	var toks []staffToken
	if err := json.Unmarshal(b, &toks); err != nil || len(toks) == 0 {
		return nil, fmt.Errorf("%w: tokens.json rỗng hay hỏng", errUsage)
	}
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = t.Token
	}
	return out, nil
}
