package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"elimaz.com.vn/crm/internal/modules/customer"
	"elimaz.com.vn/crm/internal/platform/db"
	"elimaz.com.vn/crm/internal/testkit/loadstats"
)

// Bộ thử tải M2 (kế hoạch M2 Task 88, M2-31; loadtest/m2_run.sh): tìm khách trên 1 triệu khách (m2-search.js) và lên
// đơn 10× (m2-orders.js). Dữ liệu lớn (khách, SĐT, đơn cũ) do tools/seedperf nạp bằng COPY; phần dưới đây dựng qua API
// thật những gì bài lên đơn cần: biến thể có tồn, 5 biến thể ít hàng để tranh, 10 mã giảm giá.

// Cỡ của dữ liệu thử M2 (QĐ-88-4 của tệp task-88).
const (
	m2Variants      = 20      // biến thể nhiều hàng
	m2StockEach     = 100_000 // tồn mỗi biến thể nhiều hàng
	m2LowVariants   = 5       // biến thể ít hàng (10% đơn tranh)
	m2LowStock      = 10      // tồn mỗi biến thể ít hàng
	m2Coupons       = 10      // mã giảm giá
	m2LimitedCoupon = 2       // số mã có giới hạn lượt (2 mã cuối)
	m2CouponMaxUses = 100     // giới hạn lượt của mã có giới hạn
	m2CouponValue   = 10_000  // giảm cố định (đồng)
)

// Đơn vị hành chính thử của bài lên đơn (đơn confirmed cần tỉnh, xã có trong danh mục; database đo không có danh mục
// thật). Mã có chữ T như testkit.SampleProvinceCode nên không trùng danh mục thật; tools/seedperf ghi cùng mã vào địa chỉ
// đơn cũ.
const (
	m2ProvinceCode = "T01"
	m2WardCode     = "T01001"
)

// m2Fixture là kết quả của loadkit m2seed (m2.json): k6 và loadkit m2stats đọc lại.
type m2Fixture struct {
	TenantID     uuid.UUID  `json:"tenantId"`
	WarehouseID  uuid.UUID  `json:"warehouseId"`
	ProvinceCode string     `json:"provinceCode"`
	WardCode     string     `json:"wardCode"`
	Variants     []string   `json:"variants"`
	LowVariants  []string   `json:"lowVariants"`
	Coupons      []m2Coupon `json:"coupons"`
}

type m2Coupon struct {
	Code    string `json:"code"`
	MaxUses int32  `json:"maxUses,omitempty"` // 0: không giới hạn
}

// m2CouponCode là mã giảm giá thứ i của bài (cố định; mỗi database đo dựng một lần).
func m2CouponCode(i int) string { return fmt.Sprintf("M2LT%02d", i+1) }

// seedM2 dựng qua API: mẫu m2Variants biến thể (tồn m2StockEach), mẫu m2LowVariants biến thể (tồn m2LowStock), m2Coupons
// mã giảm m2CouponValue đồng có hạn 30 ngày (m2LimitedCoupon mã cuối tối đa m2CouponMaxUses lượt).
func seedM2(ctx context.Context, c apiClient, wh uuid.UUID, now time.Time) (m2Fixture, error) {
	f := m2Fixture{WarehouseID: wh}
	var err error
	if f.Variants, err = seedProduct(ctx, c, m2Variants); err != nil {
		return f, err
	}
	if err := seedStock(ctx, c, wh, f.Variants, m2StockEach); err != nil {
		return f, err
	}
	if f.LowVariants, err = seedProduct(ctx, c, m2LowVariants); err != nil {
		return f, err
	}
	if err := seedStock(ctx, c, wh, f.LowVariants, m2LowStock); err != nil {
		return f, err
	}
	for i := range m2Coupons {
		cp := m2Coupon{Code: m2CouponCode(i)}
		body := map[string]any{
			"code": cp.Code, "name": "Mã thử tải " + cp.Code, "discountKind": "fixed", "value": m2CouponValue,
			"validFrom": now.Add(-time.Hour).Format(time.RFC3339), "validTo": now.Add(30 * 24 * time.Hour).Format(time.RFC3339),
		}
		if i >= m2Coupons-m2LimitedCoupon {
			cp.MaxUses = m2CouponMaxUses
			body["maxUses"] = cp.MaxUses
		}
		if err := c.call(ctx, http.MethodPost, "/api/v1/coupons", body, nil, http.StatusCreated); err != nil {
			return f, err
		}
		f.Coupons = append(f.Coupons, cp)
	}
	return f, nil
}

// cmdM2Seed: loadkit m2seed -db <chủ bảng> -api <gốc> -tokens F -out m2.json. Chạy sau loadkit seed (tenant, nhân
// viên); nhân viên 1 (owner) gọi API.
func cmdM2Seed(ctx context.Context, fs *flag.FlagSet, args []string, logger *slog.Logger) error {
	sf := addSeedFlags(fs, "m2.json")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	if err := sf.check(); err != nil {
		return err
	}
	sh, err := prepareShop(ctx, *sf.db, *sf.tokens, func(owner *db.DB) error { return seedAdminUnits(ctx, owner) })
	if err != nil {
		return err
	}
	f, err := seedM2(ctx, apiClient{base: *sf.api, token: sh.token, hc: &http.Client{Timeout: 30 * time.Second}}, sh.warehouse, time.Now())
	if err != nil {
		return err
	}
	f.TenantID, f.ProvinceCode, f.WardCode = sh.tenantID, m2ProvinceCode, m2WardCode
	logger.Info("m2seed xong", slog.Int("variants", len(f.Variants)), slog.Int("coupons", len(f.Coupons)))
	return writeJSON(*sf.out, f)
}

// seedAdminUnits (chủ bảng; crm_app chỉ đọc danh mục) chèn tỉnh m2ProvinceCode, xã m2WardCode khi chưa có, như bộ e2e
// website (customer.EnsureFixtureAdminUnits: không đè danh mục thật).
func seedAdminUnits(ctx context.Context, owner *db.DB) error {
	units := []customer.AdminUnit{
		{Code: m2ProvinceCode, Level: customer.AdminProvince, Name: "Thành phố Thử Một"},
		{Code: m2WardCode, Level: customer.AdminWard, ParentCode: m2ProvinceCode, Name: "Phường Bến Thử"},
	}
	return owner.RetryTx(ctx, db.TxOptions{}, func(tx pgx.Tx) error { //nolint:wrapcheck // customer đã bọc
		_, conflicts, err := customer.EnsureFixtureAdminUnits(ctx, tx, units)
		if err == nil && len(conflicts) > 0 {
			err = fmt.Errorf("%w: mã hành chính thử %v đã có với nội dung khác", errAPI, conflicts)
		}
		return err
	})
}

// cmdM2Stats: loadkit m2stats -db <chủ bảng> -fixture m2.json -from T — một dòng JSON số đo database (m2Stats).
func cmdM2Stats(ctx context.Context, fs *flag.FlagSet, args []string) error {
	dbURL := fs.String("db", "", "chuỗi kết nối chủ bảng")
	fixture := fs.String("fixture", "", "m2.json của loadkit m2seed")
	fromS := fs.String("from", "", "lúc bắt đầu tải (RFC3339)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	var f m2Fixture
	if err := readJSONFile(*fixture, &f); err != nil {
		return err
	}
	if f.TenantID == uuid.Nil {
		return fmt.Errorf("%w: thiếu -fixture", errUsage)
	}
	from, err := parseTime(*fromS)
	if err != nil {
		return err
	}
	owner, err := openOwner(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer owner.Pool.Close()
	s, err := readM2Stats(ctx, owner, f, from)
	if err != nil {
		return err
	}
	return printJSONLine(s)
}

// m2NoEvents là tiền tố mã tin, comment không sự kiện nào có (mã của M4 bắt đầu bằng m_, lc_).
const m2NoEvents = "m2-khong-co-"

// m2Stats là một dòng của loadkit m2stats: tồn, việc nền, deadlock (loadstats.Read) cộng số đo riêng của M2
// (loadstats.ReadM2: mã giảm giá, đơn, thử lại transaction, dung lượng).
type m2Stats struct {
	DB loadstats.Stats `json:"db"`
	loadstats.M2
}

// readM2Stats đọc số đo của tenant f.TenantID từ lúc from.
func readM2Stats(ctx context.Context, owner *db.DB, f m2Fixture, from time.Time) (m2Stats, error) {
	var s m2Stats
	var err error
	// Bài M2 không có tin, comment (m2NoEvents không khớp mã nào); chỉ lấy phần tồn, việc nền, deadlock.
	if s.DB, err = loadstats.Read(ctx, owner, f.TenantID, m2NoEvents, m2NoEvents, from); err != nil {
		return s, fmt.Errorf("đọc tồn, việc nền: %w", err)
	}
	codes := make([]string, 0, len(f.Coupons))
	for _, c := range f.Coupons {
		codes = append(codes, c.Code)
	}
	if s.M2, err = loadstats.ReadM2(ctx, owner, f.TenantID, codes, from); err != nil {
		return s, fmt.Errorf("đọc số đo M2: %w", err)
	}
	return s, nil
}

// printJSONLine in v thành một dòng JSON ở stdout (một dòng của tệp series).
func printJSONLine(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("mã hóa số đo: %w", err)
	}
	_, err = fmt.Fprintln(os.Stdout, string(b))
	return err //nolint:wrapcheck // ghi stdout
}
