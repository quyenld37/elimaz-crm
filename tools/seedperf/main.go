// Lệnh seedperf nạp dữ liệu đo của bài thử tải M2 (kế hoạch M2 Task 88, M2-31) vào một database đo: tenant "Thử tải"
// (của loadkit seed) 1.000.000 khách, 1.300.000 danh tính SĐT giả +8490xxxxxxx không trùng, 5.000.000 đơn đã kết thúc
// với 8.000.000 dòng rải trong 24 tháng; tenant "Thử tải B" 100.000 khách cùng các SĐT đó (RLS thật); rồi VACUUM
// ANALYZE. Nạp bằng COPY của người quản trị; chạy sau loadkit m2seed (người bán, kho, biến thể qua API).
//
//	seedperf -admin <chuỗi kết nối superuser của database đo> -out m2-sample.json [-customers N -orders N …]
//
// Chỉ cho database đo mới tạo: từ chối khi có khách của tenant khác (ErrNotPerfDB) hay đã nạp (ErrAlreadySeeded).
// Không bao giờ chạy với database production. Phần nạp ở internal/testkit/perfseed.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"elimaz.com.vn/crm/internal/platform/logging"
	"elimaz.com.vn/crm/internal/testkit/perfseed"
)

var errUsage = errors.New("cách dùng: seedperf -admin <url> -out <tệp> [cờ]")

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := runSignals(os.Args[1:], logger); err != nil {
		logger.Error("seedperf", logging.Err(err))
		os.Exit(1)
	}
}

// runSignals chạy run tới khi xong hay nhận SIGTERM, Ctrl-C.
func runSignals(args []string, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	return run(ctx, args, logger)
}

func run(ctx context.Context, args []string, logger *slog.Logger) error {
	fs := flag.NewFlagSet("seedperf", flag.ContinueOnError)
	admin := fs.String("admin", "", "chuỗi kết nối người quản trị (superuser) của database đo")
	out := fs.String("out", "", "tệp mẫu cho k6 (JSON)")
	o := perfseed.Options{Now: time.Now().UTC()}
	fs.IntVar(&o.Customers, "customers", 1_000_000, "số khách của tenant đo")
	fs.IntVar(&o.Orders, "orders", 5_000_000, "số đơn (dòng đơn = 1,6 × đơn)")
	fs.IntVar(&o.Tenant2, "tenant2", 100_000, "số khách của tenant thứ hai")
	fs.IntVar(&o.ExtraPhonePermille, "extra-phone-permille", 300, "phần nghìn số khách có SĐT thứ hai")
	fs.IntVar(&o.Months, "months", 24, "đơn rải trong ngần ấy tháng")
	fs.Uint64Var(&o.Seed, "seed", 88, "hạt giống")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("đọc cờ: %w", err)
	}
	if *admin == "" || *out == "" || o.Customers < 1 || o.Orders < 0 || o.Tenant2 < 0 || o.Months < 1 ||
		o.ExtraPhonePermille < 0 || o.ExtraPhonePermille > 1000 || o.Customers+o.Customers*o.ExtraPhonePermille/1000 > perfseed.PhoneSpace {
		return errUsage
	}
	s, err := perfseed.Seed(ctx, *admin, o, logger)
	if err != nil {
		return fmt.Errorf("nạp database đo: %w", err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("mã hóa mẫu: %w", err)
	}
	if err := os.WriteFile(*out, b, 0o600); err != nil {
		return fmt.Errorf("ghi %s: %w", *out, err)
	}
	logger.Info("seedperf xong", slog.Int("khách", o.Customers), slog.Int("SĐT", s.Phones), slog.Int("đơn", s.Orders),
		slog.Int("dòng đơn", s.Items), slog.Float64("giây", s.SeedSeconds))
	return nil
}
