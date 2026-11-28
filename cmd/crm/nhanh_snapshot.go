package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"elimaz.com.vn/crm/internal/adapters/nhanh"
)

// nhanhSnapshotCommand là lệnh vận hành `crm nhanh-snapshot` (ADR-48, kế hoạch M7 Task 540): chụp dữ liệu CHỈ ĐỌC từ
// API Nhanh (khách, tối đa 50 mẫu sản phẩm mới kèm biến thể, chi tiết, tồn) ra một tệp JSON riêng tư trên server, để
// các lệnh nhập (Task 541, 542) dùng. Không ghi gì lên Nhanh, không cần database, không gọi từ api/worker. Token đọc
// từ biến môi trường của server, không bao giờ in.
const nhanhSnapshotCommand = "nhanh-snapshot"

// Biến môi trường của lệnh (đặt trong .env trên server; chủ shop đặt token, không gửi qua chat).
const (
	envNhanhAppID      = "CRM_NHANH_APP_ID"
	envNhanhBusinessID = "CRM_NHANH_BUSINESS_ID"
	envNhanhToken      = "CRM_NHANH_TOKEN"    //nolint:gosec // G101: tên biến môi trường, không phải mật khẩu
	envNhanhBaseURL    = "CRM_NHANH_BASE_URL" // chỉ test và thử; rỗng là địa chỉ thật của Nhanh
	envNhanhDir        = "CRM_NHANH_SNAPSHOT_DIR"
)

const defaultNhanhDir = "/var/lib/crm/nhanh-snapshot"

var errNhanhUsage = errors.New("cách gọi: `crm nhanh-snapshot [--limit N (≤50)] [--product-ids 1,2,3 (≤50)] " +
	"[--since <giây Unix hay RFC3339>] [--no-customers] [--out-dir <thư mục>]`; cần " +
	envNhanhAppID + ", " + envNhanhBusinessID + ", " + envNhanhToken)

func isNhanhSnapshotCommand(args []string) bool {
	return len(args) > 0 && args[0] == nhanhSnapshotCommand
}

// runNhanhSnapshot chạy lệnh; trả mã thoát. In ra stdout chỉ số đếm, không giá trị của khách hay sản phẩm.
func runNhanhSnapshot(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, os.Interrupt)
	defer stop()
	if err := nhanhSnapshot(ctx, args, getenv, stdout, time.Now); err != nil {
		_, _ = fmt.Fprintln(stderr, nhanhSnapshotCommand+":", err)
		if errors.Is(err, errNhanhUsage) || errors.Is(err, nhanh.ErrBadOptions) {
			return exitConfig
		}
		return exitFail
	}
	return exitOK
}

func nhanhSnapshot(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer, now func() time.Time) error {
	fs := flag.NewFlagSet(nhanhSnapshotCommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 0, "")
	idsArg := fs.String("product-ids", "", "")
	sinceArg := fs.String("since", "", "")
	noCustomers := fs.Bool("no-customers", false, "")
	dir := fs.String("out-dir", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errNhanhUsage
	}
	ids, err := parseIDs(*idsArg)
	if err != nil {
		return fmt.Errorf("--product-ids: %w", errNhanhUsage)
	}
	since, err := parseSince(*sinceArg)
	if err != nil {
		return fmt.Errorf("--since: %w", errNhanhUsage)
	}
	outDir := *dir
	if outDir == "" {
		outDir = getenv(envNhanhDir)
	}
	if outDir == "" {
		outDir = defaultNhanhDir
	}
	c, err := nhanh.New(nhanh.Config{
		BaseURL: getenv(envNhanhBaseURL), AppID: getenv(envNhanhAppID), BusinessID: getenv(envNhanhBusinessID), Token: getenv(envNhanhToken),
	})
	if err != nil {
		return fmt.Errorf("%w: %w", errNhanhUsage, err)
	}
	c.Pace = 250 * time.Millisecond // dưới 150 lời gọi / 30 s mỗi đường
	snap, err := nhanh.Collect(ctx, c, nhanh.Options{Limit: *limit, ProductIDs: ids, Since: since, SkipCustomers: *noCustomers, Now: now})
	if err != nil {
		return fmt.Errorf("chụp: %w", err)
	}
	path, err := nhanh.WriteSnapshot(outDir, snap)
	if err != nil {
		return fmt.Errorf("ghi bản chụp: %w", err)
	}
	pruned, err := nhanh.PruneSnapshots(outDir, now())
	if err != nil {
		return fmt.Errorf("dọn bản chụp cũ: %w", err)
	}
	variants, inv := 0, 0
	for _, p := range snap.Products {
		variants += len(p.Variants)
		inv += len(p.Inventory)
	}
	if _, err = fmt.Fprintf(stdout, "bản chụp: %s\nmẫu: %d, biến thể: %d, dòng tồn: %d, khách: %d, trường thiếu: %d, bản chụp cũ đã xóa: %d\n",
		path, len(snap.Products), variants, inv, len(snap.Customers), len(snap.Gaps), pruned); err != nil {
		return fmt.Errorf("in kết quả: %w", err)
	}
	return nil
}

func parseIDs(s string) ([]int64, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []int64
	for p := range strings.SplitSeq(s, ",") {
		n, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || n <= 0 {
			return nil, errNhanhUsage
		}
		out = append(out, n)
	}
	return out, nil
}

// parseSince nhận giây Unix hay RFC3339; rỗng là không lọc.
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		return time.Unix(n, 0).UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("giờ sai: %w", err)
	}
	return t.UTC(), nil
}
