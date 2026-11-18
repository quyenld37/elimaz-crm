# ADR-19 — Kiểm từ ngoài máy chủ; chỉ số theo phút; sao lưu mã hóa

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026); bổ sung bởi ADR-37, ADR-38
Ngày: 12/11/2026
Kiểm bằng: scripts/external-probe/probe_test.go; internal/platform/observability (rules_test.go, rules_eval_test.go); internal/platform/metrics/flusher_test.go; `scripts/backup/recreate-check.sh`; `TestProdCompose_ImagesPinnedNotBuilt` (tests/deploy/compose_test.go)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-19. Tệp này không chép lại quyết định.

Dịch vụ ngoài gọi `/health` và `/health/deep` mỗi phút, báo Telegram khi hai lần liền không đạt; cảnh báo WAL, sao lưu,
tệp tạm, `LISTEN`; WAL-G mã hóa, khóa giữ hai nơi ngoài máy; image PostgreSQL ghim digest; chỉ số mỗi phút vào
`metrics_minute`; cảnh báo và log không mang dữ liệu cá nhân.

## Kiểm ở đâu

- Điểm 1: `TestEvaluate`, `TestRunAlertsOnceAndBeats`, `TestRunFlakyNoAlert` (scripts/external-probe/probe_test.go); `TestHealthDeep` (internal/httpapi/handlers_health_test.go).
  Cài ở hai nơi trên máy thử: chưa có, Task 23 bước 7 (cần OK chủ shop).
- Điểm 2: `TestPGRuleThresholds` (`wal-archive`, `wal-size`; internal/platform/observability/rules_eval_test.go); `TestRuleBackupAge`,
  `TestRuleSpool`, `TestRuleListenDown` (internal/platform/observability/rules_test.go).
- Điểm 3: `scripts/backup/restore-drill.sh`, `scripts/backup/verify-drill.sh` (sao lưu có mã hóa); giữ khóa hai nơi: bước của chủ shop, Task 23 bước 7.
- Điểm 4: `TestProdCompose_ImagesPinnedNotBuilt`, `TestProdCompose_PostgresVolume` (tests/deploy/compose_test.go); `scripts/backup/recreate-check.sh`.
- Điểm 5: `TestFlushAccumulatesWithinMinute`, `TestFlushHistogram` (internal/platform/metrics/flusher_test.go).
- Điểm 6: `TestRuleDeadNoPayload` (rules_test.go); `TestScrubTextRemovesPersonalData` (internal/platform/logging/scrub_test.go).
