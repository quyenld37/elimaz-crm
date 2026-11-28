package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// Bộ thử tải M5 (kế hoạch M5 Task 219 / M5-36, loadtest/m5_run.sh): dựng khóa dịch vụ của website và đợt flash sale
// qua API nhân viên (thẻ Access của nhân viên 1, owner), đóng đợt giữa bài.
//
//	loadkit m5key   -db <chủ bảng> -api <gốc> -tokens F -out key.json       người bán, kho, khóa dịch vụ (web.*)
//	loadkit m5sale  -api <gốc> -tokens F -slots N -limit L -hold M -start-in D [-catalog N] -out sale.json
//	loadkit m5close -api <gốc> -tokens F -sale <mã đợt>

// m5KeyPerms là quyền của khóa dịch vụ thử (đọc danh mục, mua flash sale).
func m5KeyPerms() []string { return []string{"web.catalog.read", "web.flash.buy"} }

// m5Key là tệp key.json: khóa đầy đủ (chỉ sống trong thư mục kết quả của lượt, m5_run.sh xóa khi xong).
type m5Key struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
}

// m5Sale là tệp sale.json của một đợt thử.
type m5Sale struct {
	SaleID    string    `json:"saleId"`
	ItemID    string    `json:"itemId"`
	VariantID string    `json:"variantId"`
	ProductID string    `json:"productId"`
	Slots     int       `json:"slots"`
	Limit     int       `json:"limit"`
	Hold      int       `json:"hold"`
	StartsAt  time.Time `json:"startsAt"`
	// Danh mục thử của bài web_read (cờ -catalog): mẫu và biến thể đọc qua catalog/products?ids và availability.
	CatalogProducts []string `json:"catalogProducts"`
	CatalogVariants []string `json:"catalogVariants"`
}

// m5Client: nhân viên 1 (owner) gọi API nhân viên ở gốc api.
func m5Client(api, token string) apiClient {
	return apiClient{base: api, token: token, hc: &http.Client{Timeout: 60 * time.Second}}
}

// cmdM5Key: người bán, kho, cài đặt đơn mặc định (seedShop, chủ bảng) rồi khóa dịch vụ qua API nhân viên.
func cmdM5Key(ctx context.Context, fs *flag.FlagSet, args []string, logger *slog.Logger) error {
	sf := addSeedFlags(fs, "key.json")
	sh, err := sf.prepare(ctx, fs, args, seedAdminUnits)
	if err != nil {
		return err
	}
	var k m5Key
	if err := m5Client(*sf.api, sh.token).call(ctx, http.MethodPost, "/api/v1/identity/service-keys",
		map[string]any{"name": "Thử tải M5", "permissions": m5KeyPerms()}, &k, http.StatusCreated); err != nil {
		return err
	}
	logger.Info("m5key xong", slog.String("key", k.ID))
	return writeJSON(*sf.out, k)
}

// cmdM5Sale: mẫu một biến thể, nhập tồn slots + 100, đợt [now+start-in, +2h) một món, lên lịch.
func cmdM5Sale(ctx context.Context, fs *flag.FlagSet, args []string, logger *slog.Logger) error {
	sf := addSeedFlags(fs, "sale.json")
	slots := fs.Int("slots", 500, "số suất")
	limit := fs.Int("limit", 1, "giới hạn mỗi khách")
	hold := fs.Int("hold", 10, "phút giữ đơn nháp (5–30)")
	startIn := fs.Duration("start-in", 20*time.Second, "đợt mở sau bấy lâu")
	catalog := fs.Int("catalog", 0, "số mẫu thêm (3 biến thể, có tồn) cho bài web_read")
	sh, err := sf.prepare(ctx, fs, args, nil)
	if err != nil {
		return err
	}
	c := m5Client(*sf.api, sh.token)
	s, err := seedM5Sale(ctx, c, sh.warehouse, *slots, *limit, *hold, time.Now().UTC().Add(*startIn))
	if err != nil {
		return err
	}
	if err := seedM5Catalog(ctx, c, sh.warehouse, *catalog, &s); err != nil {
		return err
	}
	logger.Info("m5sale xong", slog.String("sale", s.SaleID), slog.Int("slots", s.Slots))
	return writeJSON(*sf.out, s)
}

func seedM5Sale(ctx context.Context, c apiClient, wh uuid.UUID, slots, limit, hold int, start time.Time) (m5Sale, error) {
	s := m5Sale{Slots: slots, Limit: limit, Hold: hold, StartsAt: start.Truncate(time.Second)}
	product, vars, err := seedProduct(ctx, c, 1)
	if err != nil {
		return s, err
	}
	s.ProductID, s.VariantID = product, vars[0]
	if err := seedStock(ctx, c, wh, vars, slots+100); err != nil {
		return s, err
	}
	// FlashSale của API nhân viên: {sale: {id, version, …}, items}.
	var sale struct {
		Sale struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
		} `json:"sale"`
	}
	if err := c.call(ctx, http.MethodPost, "/api/v1/flash-sales", map[string]any{
		"name": "Thử tải " + s.StartsAt.Format("15:04:05"), "startsAt": s.StartsAt,
		"endsAt": s.StartsAt.Add(2 * time.Hour),
	}, &sale, http.StatusCreated); err != nil {
		return s, err
	}
	var item struct {
		ID string `json:"id"`
	}
	if err := c.call(ctx, http.MethodPost, "/api/v1/flash-sales/"+sale.Sale.ID+"/items", map[string]any{
		"variantId": s.VariantID, "salePrice": 99_000, "quantity": slots, "perCustomerLimit": limit, "holdMinutes": hold,
	}, &item, http.StatusCreated); err != nil {
		return s, err
	}
	if err := c.call(ctx, http.MethodGet, "/api/v1/flash-sales/"+sale.Sale.ID, nil, &sale, http.StatusOK); err != nil {
		return s, err
	}
	if err := c.call(ctx, http.MethodPost, "/api/v1/flash-sales/"+sale.Sale.ID+"/schedule",
		map[string]int{"version": sale.Sale.Version}, nil, http.StatusOK); err != nil {
		return s, err
	}
	s.SaleID, s.ItemID = sale.Sale.ID, item.ID
	return s, nil
}

// seedM5Catalog thêm n mẫu (mỗi mẫu 3 biến thể, tồn 50) vào s.CatalogProducts, s.CatalogVariants; mẫu của món flash
// sale cũng vào danh mục đọc.
func seedM5Catalog(ctx context.Context, c apiClient, wh uuid.UUID, n int, s *m5Sale) error {
	if s.ProductID != "" {
		s.CatalogProducts = append(s.CatalogProducts, s.ProductID)
		s.CatalogVariants = append(s.CatalogVariants, s.VariantID)
	}
	for range n {
		product, vars, err := seedProduct(ctx, c, 3) //nolint:mnd // ba biến thể mỗi mẫu
		if err != nil {
			return err
		}
		if err := seedStock(ctx, c, wh, vars, 50); err != nil { //nolint:mnd // tồn đủ để availability có số
			return err
		}
		s.CatalogProducts = append(s.CatalogProducts, product)
		s.CatalogVariants = append(s.CatalogVariants, vars...)
	}
	return nil
}

// cmdM5Close: đóng đợt (suất còn trống trả về tồn).
func cmdM5Close(ctx context.Context, fs *flag.FlagSet, args []string) error {
	api := fs.String("api", "http://127.0.0.1:4290", "gốc api")
	tokens := fs.String("tokens", "", "tokens.json của loadkit access")
	sale := fs.String("sale", "", "mã đợt")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	if *sale == "" || *tokens == "" {
		return fmt.Errorf("%w: thiếu -sale hoặc -tokens", errUsage)
	}
	tok, err := firstToken(*tokens)
	if err != nil {
		return err
	}
	return m5Client(*api, tok).call(ctx, http.MethodPost, "/api/v1/flash-sales/"+*sale+"/close", nil, nil, http.StatusOK)
}
