package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"elimaz.com.vn/crm/internal/platform/config"
)

// `crm seed-staging …` dựng dữ liệu giả cho bản thử và kịch bản T1 qua public API của module (kế hoạch M7, Task
// M7-30 = 275, để sau). Khung của Task 260 (M7-15): lệnh đã nối trong run; Task 275 điền runSeedStaging (cờ, dữ liệu),
// không sửa main.go.

const seedStagingCommand = "seed-staging"

var (
	// errSeedStagingProduction: lệnh không bao giờ chạy trên production (Q21), kiểm trước mọi việc.
	errSeedStagingProduction = errors.New("lệnh seed-staging chỉ chạy trên dev, test, staging")
	// errSeedStagingNotImplemented: khung, Task 275 chưa làm.
	errSeedStagingNotImplemented = errors.New("lệnh seed-staging chưa làm (Task 275)")
)

func isSeedStagingCommand(args []string) bool { return len(args) >= 1 && args[0] == seedStagingCommand }

// runSeedStaging chặn production rồi (khung) trả errSeedStagingNotImplemented, không ghi gì.
func runSeedStaging(_ context.Context, cfg config.Config, _ []string) error {
	if cfg.Env == "production" {
		return errSeedStagingProduction
	}
	return errSeedStagingNotImplemented
}

// seedStagingExit in lỗi ra stderr và trả mã thoát: production → exitConfig (2, Q21); lỗi khác → exitFail.
func seedStagingExit(err error, stderr io.Writer) int {
	if err == nil {
		return exitOK
	}
	_, _ = fmt.Fprintln(stderr, seedStagingCommand+":", err)
	if errors.Is(err, errSeedStagingProduction) {
		return exitConfig
	}
	return exitFail
}
