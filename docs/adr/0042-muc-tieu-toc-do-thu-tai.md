# ADR-42 — Mục tiêu tốc độ có đo; thử tải sớm; theo dõi PostgreSQL; hàng đợi và realtime đứng sau interface

Trạng thái: đã duyệt (spec 07-adr, 15/11/2026); bổ sung ADR-15, 17, 19; phép đo flash sale bằng Go đầu M0 thêm bởi ADR-44
Ngày: 15/11/2026
Kiểm bằng: bộ thử tải `loadtest/` (k6: normal.js, heavy.js, soak.js, chaos.sh) với tiêu chí trong tools/loadkit (`TestEvaluateNormalAllPass`, `TestEvaluateChaos`, `TestEvaluateSoak`), kết quả `docs/do-luong/thu-tai-m0.md`; phép thử ngắn `docs/do-luong/phep-thu-ngan.md`; `TestPGRuleThresholds` (internal/platform/observability/rules_eval_test.go)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-42. Tệp này không chép lại quyết định.

Mục tiêu bằng số ở 04 mục 1; thử tải có tiêu chí đạt từ M0 và lặp lại mỗi mốc; năm phép thử ngắn đầu M0; theo dõi
riêng PostgreSQL; ngân sách kết nối theo tiến trình; ngữ cảnh tenant chỉ đặt trong transaction; `JobQueue` và
`RealtimeBus` là interface; nhịp giữ kết nối và tải lại 20 giây.

## Kiểm ở đâu

- Điểm 2: tải thường, nặng, ngâm, hỏng giữa tải: `loadtest/` + tools/loadkit/report_test.go; lần 1 trên laptop ở `docs/do-luong/thu-tai-m0.md`;
  lần chính thức trên máy thử: Task 23 bước 7.9. Đột biến và ghi bền flash sale: chưa có, làm ở mốc M5.
- Điểm 3: `docs/do-luong/phep-thu-ngan.md` (`search-plan-probe.sh`, `restore-drill.sh`, `recreate-check.sh`); SSE qua Cloudflare: Task 23 bước 7.8.
- Điểm 4: `TestPGRuleThresholds`, `TestPGRuleSeverityAndText` (rules_eval_test.go); `TestPGWatchLive` (internal/platform/observability/pgwatch_test.go).
- Điểm 5: `TestLoadDefaultsPerRole` (internal/platform/config/config_test.go); `TestRetryTxLimitsDoNotLeakToConnection` (internal/platform/db/retry_test.go);
  `TestWithTenantSetsContextOnEveryAttempt` (internal/platform/tenancy/tenancy_test.go).
- Điểm 6: nghiệp vụ không viết SQL lên `jobs`: `TestSQLUsageProductCode` (tests/arch/sqlusage_test.go). Nghiệp vụ không gọi `NOTIFY` trực tiếp: chưa có test kiến trúc (phát hiện Task 23).
- Điểm 7: nhịp 20 s phía máy chủ `defaultHeartbeat` (internal/platform/realtime/sse.go), phía màn hình console/src/realtime/client.ts; test riêng cho hai con số 20 s: chưa có (phát hiện Task 23).
