# ADR-30 — Bảy bổ sung vận hành và phòng vệ

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026); mục 6 thay bởi ADR-35; mục 2, 3 bổ sung bởi ADR-38; mục 5 (đồng bộ tệp) đổi thành mỗi giờ bởi ADR-36
Ngày: 12/11/2026
Kiểm bằng: internal/platform/webhooks/lockout_test.go; internal/httpapi/ratelimit_test.go; internal/platform/logging/dailyfile_test.go; internal/platform/observability (errors_test.go, alerter_test.go, summary_test.go); diễn tập dựng lại máy: Task 23 (`REBUILD.md`)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-30. Tệp này không chép lại quyết định.

Bảy mục: chặn gọi dồn dập; nơi lưu log; gom lỗi `error_events`; xem sức khỏe; sao lưu cấu hình và tệp; đăng nhập khẩn
cấp (đã thay bởi ADR-35); phân loại cảnh báo hai mức, hai kênh, nhắc lại 30 phút, tin tổng kết mỗi sáng.

## Kiểm ở đâu

- Mục 1: `TestLockoutCountsFailuresInWindow`, `TestLockoutOverHTTP` (internal/platform/webhooks/lockout_test.go); `TestInFlightLimit`
  (internal/platform/webhooks/ingest_test.go); `TestUserRateLimitHTTP`, `TestRateLimitDefaults` (internal/httpapi/ratelimit_test.go).
  `TestLockoutOverHTTP` cũng kiểm 100 tin đúng chữ ký không bị giới hạn. Luật Cloudflare: Task 23 bước 7.
- Mục 2: `TestDailyFileKeeps30`, `TestDailyFileDropsOverMaxBytes` (internal/platform/logging/dailyfile_test.go).
- Mục 3: `TestRecordErrorGroupsBySite` (internal/platform/observability/errors_test.go); `TestErrorRulesNewError`, `TestErrorRulesSpike` (summary_test.go).
- Mục 4: `TestPlatformHealth` (internal/httpapi/handlers_health_test.go); `TestSummary` (internal/platform/observability/summary_test.go).
- Mục 5: xuất cấu hình `scripts/ops/export-config.sh` (`TestExportConfig_NamesOnlyAndDigests`, `TestExportConfig_StopsOnSecret`, `TestExportConfig_KeepsEightAndUploads` trong tests/deploy/exportconfig_test.go); dựng lại máy theo `docs/REBUILD.md`, diễn tập trên máy thật: Task 23 bước 7 (cần OK chủ shop); tệp: `TestFilesyncIdentity` (tests/probe/seaweedfs_test.go).
- Mục 7: `TestUrgentRepeatsEvery30Minutes`, `TestChannelsPerSeverity` (internal/platform/observability/alerter_test.go); `TestDailyDigestOncePerDay` (digest_test.go).
