package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"elimaz.com.vn/crm/internal/adapters/facebook"
	"elimaz.com.vn/crm/internal/modules/channel"
	"elimaz.com.vn/crm/internal/platform/config"
	"elimaz.com.vn/crm/internal/platform/datadeletion"
	"elimaz.com.vn/crm/internal/platform/db"
)

// Callback xóa dữ liệu của Meta (kế hoạch M4, Task 179): ingest phục vụ POST /hooks/facebook/data-deletion và trang
// trạng thái khi bật kênh Facebook (có META_APP_SECRET, HOOKS_PUBLIC_BASE_URL), nạp lại tệp tạm mỗi 5 s; worker chạy
// lịch platform.data_deletion xóa phiên đăng nhập Facebook (channel.DeleteOAuthDataFor) ở mọi tenant.

// dataDeletionSpool trả thư mục tệp tạm của yêu cầu xóa (<SPOOL_DIR>/data-deletion, tạo nếu chưa có, chỉ ingest đọc).
func dataDeletionSpool(cfg config.Config) (*datadeletion.Spool, error) {
	dir := filepath.Join(cfg.SpoolDir, datadeletion.SpoolSubdir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("tạo thư mục tệp tạm yêu cầu xóa: %w", err)
	}
	return &datadeletion.Spool{Dir: dir}, nil
}

// registerDataDeletion đăng ký hai đường của callback xóa dữ liệu vào mux của ingest (chỉ khi bật kênh Facebook).
func registerDataDeletion(mux *http.ServeMux, cfg config.Config, d *db.DB, logger *slog.Logger) error {
	if !cfg.Meta.Enabled {
		return nil
	}
	spool, err := dataDeletionSpool(cfg)
	if err != nil {
		return err
	}
	secret := cfg.Meta.AppSecret
	ep, err := datadeletion.NewEndpoint(datadeletion.EndpointOptions{
		DB: d, Spool: spool, Source: facebookSource, BaseURL: cfg.Meta.HooksPublicBaseURL, Logger: logger,
		Parse: func(sr string) (string, error) { return facebook.ParseSignedRequest(secret, sr) },
	})
	if err != nil {
		return fmt.Errorf("dựng callback xóa dữ liệu: %w", err)
	}
	ep.Register(mux)
	return nil
}

// startDataDeletionDrainer chạy vòng nạp lại tệp tạm của yêu cầu xóa trong wg tới khi ctx bị hủy (ingest, khi bật
// kênh Facebook).
func startDataDeletionDrainer(ctx context.Context, wg *sync.WaitGroup, cfg config.Config, d *db.DB, logger *slog.Logger) error {
	if !cfg.Meta.Enabled {
		return nil
	}
	spool, err := dataDeletionSpool(cfg)
	if err != nil {
		return err
	}
	dr := &datadeletion.Drainer{Spool: spool, DB: d, Logger: logger}
	wg.Go(func() { dr.Run(ctx) })
	return nil
}

// dataDeletionProcessor là handler của lịch platform.data_deletion (worker): xóa phiên đăng nhập Facebook.
func dataDeletionProcessor() *datadeletion.Processor {
	return datadeletion.NewProcessor(channel.DeleteOAuthDataFor)
}

// facebookSource là khóa nguồn của callback (đường /hooks/facebook/data-deletion, cột source).
const facebookSource = "facebook"
