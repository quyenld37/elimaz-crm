# Task 19 — Cảnh báo, kiểm sâu, theo dõi PostgreSQL, kiểm từ ngoài

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Không ký hiệu có sẵn nào đổi chữ ký. **Thêm** vào `internal/platform/observability`: `Alerter`,
    `NewTelegramAlerter`, `Severity` (`SeverityUrgent`, `SeverityNormal`), `CheckRules`, `RuleOptions`,
    `SendDailyDigest`, `DeepHealth`, `DeepHealthReport`, `NewAlertsCommand`, `NewDigestCommand` và hằng tên lịch.
  - `metrics.Flusher`: **thêm** trường `DiskPaths map[string]string` (nhãn → đường dẫn) để ghi gauge
    `disk_used_pct` mỗi phút (luật `disk:<process>` cần; chưa ai ghi gauge này). 3 chỗ dựng `Flusher{…}` (cmd/crm,
    flusher_test) không phải sửa (trường mới để trống = không đo).
  - `queue.Worker` (hàm không xuất `finish`, `runCommand`): thêm đếm `jobs_expired` theo `kind` (việc treo của
    Task 14). Không đổi trạng thái hay câu SQL.
  - `config.Config`: thêm `BackupExpected` (`BACKUP_EXPECTED`), `DBDiskBudgetBytes` (`DB_DISK_BUDGET_BYTES`),
    `DBConnBudget` (`DB_CONN_BUDGET`, mặc định 36 = 5 + 20 + 10 + 1 LISTEN theo ADR-42) cho vai trò worker.
  - `internal/httpapi`: handler `HealthDeep` (operation `healthDeep` đã có trong hợp đồng, đang trả 501) và
    middleware kiểm `Authorization: Bearer <HEALTH_TOKEN>` chỉ cho route này.
  - `cmd/crm`: `runWorker` đăng ký hai lịch `platform.alerts` (60 s), `platform.digest` (15 phút, chỉ gửi từ 08:00 giờ
    Việt Nam = 01:00 UTC, một tin mỗi ngày), Flusher đo đĩa `/` (+ `SPOOL_DIR` của ingest, `LOG_DIR` nếu có).
  - Mã sinh `internal/platform/internal/store`: **thêm** câu của `queries/platform/ops.sql`; câu cũ không đổi.
    `models.go` của các khối sqlc khác (`identity`, `catalog`) thêm struct của hai bảng mới vì dùng chung
    `migrations`; không ai dùng. `docs/danh-muc-dung-chung.md` sinh lại (`make catalog`), ghi chú gói ở
    `docs/danh-muc-ghi-chu.yaml`.
- Gói phụ thuộc ngược (go list): `observability` ← `cmd/crm`, `cmd/imager`, `internal/httpapi`; `metrics` ←
  `cmd/crm`, `queue`, `realtime`, `webhooks`; `queue` ← `cmd/crm`, `httpapi`, `commands`, `events`, `schedules`,
  `webhooks`; `config` ← `cmd/crm`, `httpapi`, `logging`, `metrics`, `telemetry`; `httpapi` ← `cmd/crm`.
  `observability` mới nhập `commands`, `queue`, `config`, `db`, `internal/store` (không vòng: không gói nào trong
  số đó nhập `observability`).
- Operation OpenAPI bị chạm, oasdiff: `healthDeep` — **không đổi hợp đồng** (schema `DeepHealth` và mã 200/503 có
  sẵn; 401 đi qua `default: Error`). Không `make gen` cho openapi; oasdiff không đổi.
- Chỗ dùng trên màn hình: không (`/health/deep` cho bộ kiểm từ ngoài, không phải màn hình).
- Câu sqlc, bảng, pha migration: **mở rộng**. `migrations/0010_ops.sql` đúng Interfaces: `platform.alert_events`
  (+ index `(key, sent_at DESC)`), `platform.backup_runs`, hàm `platform.wal_bytes()` `SECURITY DEFINER`,
  `GRANT pg_monitor TO crm_app`; `crm_app` được `SELECT, INSERT` hai bảng (chỉ chèn: ADR-46 nhóm (a) đã liệt kê hai
  bảng). Bảng nền tảng không RLS, không có `tenant_id`. `GRANT pg_monitor` cần `crm_owner` có `ADMIN OPTION` trên
  `pg_monitor`: thêm vào `docker/postgres/init/01-roles.sql`, `init-drill/01-roles.sql` và `testkit.CreateDatabase`
  (cụm dev cũ vẫn chạy test được). Câu mới `queries/platform/ops.sql`. Số 0010 nhỏ hơn 0012/0112 đã có trên main:
  database mới chạy đúng thứ tự số; database dev cũ đã áp 0012/0112 cần dựng lại (goose mặc định không chạy số nhỏ
  đến sau).
- Test sẽ chạy: `./internal/platform/observability/...` (mới), `./internal/platform/metrics/...`,
  `./internal/platform/queue/...` (vùng rủi ro cao: chạy cả bộ), `./internal/platform/commands/...`,
  `./internal/platform/config/...`, `./internal/httpapi/...`, `./internal/platform/{realtime,webhooks,schedules,events}/...`
  (nhập metrics/queue), `./scripts/external-probe/...`, `./cmd/...`, `./tests/arch/...`, `./tests/e2e/...`.
  Dự án compose `crm-t19`, cổng 5519.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` có `db.RetryTx`,
  `queue.Kind`/`Policy`, `commands.Handler`/`Outcome`/`NewInDoubtResolver` (mẫu lệnh chạy theo lịch ngoài
  transaction), `schedules.Registry`/`Schedule`, `metrics.Registry`/`Flusher`/`Pinger`/`HeartbeatMaxAge`/
  `ProcessName`, `observability.Health`, `config.Config` (Telegram, `HealthToken` đã có), `testkit.OpenAppDB`,
  `testkit.Do`. Tìm `telegram`, `sendMessage`, `alert`, `statfs`, `pg_stat_activity`, `pg_stat_archiver`, `Bearer`,
  `subtle.ConstantTimeCompare` trong `internal/`, `cmd/`: chưa có alerter, chưa có đo đĩa; so thẻ hằng thời gian đã
  có ở webhooks (HMAC) nhưng là kiểm chữ ký, không phải thẻ Bearer.
- Dùng lại / viết mới vì:
  - Lịch: `schedules.Registry.Define`; việc gửi Telegram chạy như **lệnh** (`commands.Handler`, giống
    `NewInDoubtResolver`) vì handler transactional của hàng đợi không được gọi mạng.
  - Đọc tuổi nhịp sống, ngưỡng 90 s: dùng `metrics.HeartbeatMaxAge`. Gửi nhịp sống của bộ kiểm từ ngoài: không dùng
    `metrics.Pinger` (bộ kiểm là tệp chạy riêng chỉ dùng thư viện chuẩn, cài ở dịch vụ ngoài; không kéo gói nội bộ).
  - `alerter.go`, `rules.go`, `pgwatch.go`, `deephealth.go`, `digest.go` viết mới (chưa có gì tương tự).
  - Không thêm thư viện (Telegram qua `net/http`, đo đĩa qua `syscall.Statfs`).

## Soát thiết kế

Mục 5:
1. Chủ dữ liệu: `alert_events`, `backup_runs` thuộc `platform` (spec 04 mục 3, kế hoạch Global Constraints liệt kê
   là bảng nền tảng không RLS). Chỉ `internal/platform/observability` đọc/ghi.
2. Không ghi bảng module khác: chỉ đọc `platform.jobs`, `metrics_minute`, `process_heartbeats` (cùng `platform`, qua
   `internal/store`) và view hệ thống của PostgreSQL. Không đọc `payload`.
3. Không đọc bảng nghiệp vụ của module nào.
4. Không có việc cần nguyên tử giữa module.
5, 6. Gửi Telegram là gọi ra ngoài: chạy trong việc mode command (Runner gọi ngoài transaction). Không dùng
   `in_doubt` cho cảnh báo: lỗi gửi → `not_sent` có tự chạy lại trong hạn 60 s (cảnh báo gửi lại hai lần ít hại hơn
   mất cảnh báo); chống lặp theo `alert_events` (ghi sau khi gửi được).
7. Không bảng có `tenant_id`; câu đọc `jobs` là toàn hệ thống (không `WithTenant`, giống quét của worker).
8. Nghiệp vụ không biết HTTP: `observability` là gói nền tảng có `net/http` sẵn (health); không có module nghiệp vụ.
9. Không đường ghi HTTP mới. `GET /health/deep` công khai trong hợp đồng, tự kiểm thẻ `HEALTH_TOKEN`.
10. Không dùng Valkey.
11. Không thư viện mới.
12. Theo mẫu: SQL trong `queries/platform/ops.sql` qua sqlc; lỗi bọc một lần; log ở mép (Execute của lệnh định kỳ là
    mép của worker).

Mục 6:
1. Đồng thời: hai lượt `platform.alerts` chạy chồng (hai worker) có thể gửi trùng một cảnh báo; chặn trong một
   tiến trình bằng mutex của Alerter, giữa tiến trình chấp nhận trùng hiếm (không khóa khi đang gọi mạng).
2. Gửi lại: digest theo khóa `digest:<ngày>` (đã có dòng thì không gửi); cảnh báo theo cửa sổ 10/30 phút.
3. Khóa: chỉ chèn `alert_events` (ADR-46 nhóm (a)), không khóa dòng nào.
4. Lỗi tạm của database: `RetryTx`; hàm trong `RetryTx` không gọi mạng.
5. Mức cách ly mặc định.
6. Việc nền lỗi: lệnh `not_sent` tự chạy lại trong hạn sống (60 s / 15 phút), quá hạn `expired`.
7. 429 của Telegram: `not_sent`, `RetryAfter` theo `retry_after` của Telegram.
8. Tiến trình chết giữa chừng: giữ chỗ hết hạn → chạy lại (OnLeaseExpired retry; trùng cảnh báo chấp nhận được).
9. Treo lâu: gọi Telegram hạn 10 s; lệnh hạn 30 s; câu đọc chạy với giới hạn của pool worker.
10. Không tiền.
11. Không vùng quy luật tiền/tồn; test chống lặp và luật theo ngưỡng.
12. Dữ liệu cá nhân: văn bản cảnh báo chỉ tên luật, số lượng, `kind`, mã việc; không đọc `payload`, `last_error`
    (có thể mang dữ liệu bên ngoài); không đưa câu SQL của `pg_stat_statements` vào văn bản (chỉ `queryid`). Test
    `payload {"phone":"0911"}`.
13. Quyền: `/health/deep` sai/thiếu thẻ → 401 (so hằng thời gian); không có dữ liệu khách trong câu trả lời.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| normal cùng key 10 phút gửi 1 lần, quá 10 phút gửi lại; mỗi lần gửi 1 dòng alert_events | TestNormalDedupTenMinutes | `go test -race -count=1 ./internal/platform/observability/...` | ok |
| urgent: 29 phút không gửi, 31 phút gửi lại | TestUrgentRepeatsEvery30Minutes | `go test -race -count=1 ./internal/platform/observability/...` | ok |
| urgent → chat gấp, normal → chat thường, thiếu chat gấp → chat thường | TestChannelsPerSeverity | `go test -race -count=1 ./internal/platform/observability/...` | ok |
| Thiếu biến Telegram: không gọi mạng, không lỗi, có alert_events | TestNoTelegramOnlyRecords | `go test -race -count=1 ./internal/platform/observability/...` | ok |
| Telegram lỗi: không ghi, lỗi không lộ token; gửi lại được | TestTelegramFailureNotRecorded | `go test -race -count=1 ./internal/platform/observability/...` | ok |
| Digest hai lần một ngày một tin, đủ ba con số; ngày mới tin mới | TestDailyDigestOncePerDay, TestDailyDigestNoBackup | `go test -race -count=1 ./internal/platform/observability/...` | ok |
| Mỗi luật: ca chạm ngưỡng có key, ca dưới ngưỡng không (bảng platform) | TestRuleQueueLag, TestRuleInDoubt, TestRuleSpool, TestRuleListenDown, TestRuleHeartbeats, TestRuleBackupAge, TestRuleDBSize, TestRuleDisk, TestRuleHooksAttack, TestRuleHeartbeatPing, TestRulesQuiet | `go test -race -count=1 ./internal/platform/observability/...` | ok |
| Mỗi luật PostgreSQL: chạm/dưới ngưỡng (wal-archive, wal-size, jobs-dead-tuples, pg-connections, long-tx, lock-wait, slow-query) | TestPGRuleThresholds, TestPGRuleSeverityAndText, TestStreak | `go test -race -count=1 ./internal/platform/observability/...` | ok |
| pgwatch thật: transaction mở (đồng hồ +6 phút), phiên chờ khóa, 15 kết nối/ConnBudget 20 → đúng ba key; yên → không | TestPGWatchLive | `go test -race -count=1 ./internal/platform/observability/...` | ok |
| Câu pg_stat_* chạy bằng crm_app | TestReadPGStats (pgwatch_dynsql_test.go) | `go test -race -count=1 ./internal/platform/observability/...` | ok |
| Việc dead payload {"phone":"0911"} (và last_error có 0911) → văn bản không chứa 0911, có mã việc | TestRuleDeadNoPayload | `go test -race -count=1 ./internal/platform/observability/...` | ok |
| /health/deep: không/sai thẻ 401, worker 200 s → 503 ok:false, tốt → 200 | TestHealthDeep, TestServerRouting | `go test -race -count=1 ./internal/httpapi/...` | ok (110 s) |
| Báo cáo kiểm sâu: số đếm, tuổi; chờ 125 s hoặc ingest 91 s → OK false | TestDeepHealth, TestDeepHealthStaleIngest | `go test -race -count=1 ./internal/platform/observability/...` | ok |
| Evaluate: một lượt không báo, hai lượt báo và nêu tên; Run nhắn một lần trong 10 phút, gửi nhịp sống | TestEvaluate, TestRunAlertsOnceAndBeats, TestRunFlakyNoAlert, TestConfigFromEnv | `go test -race -count=1 ./scripts/external-probe/` | ok |
| disk_used_pct ghi mỗi Flush | TestDiskUsedPct, TestFlushWritesDiskGauge | `go test -race -count=1 ./internal/platform/metrics/...` | ok |
| jobs_expired đếm theo kind (quét pending và lệnh) | TestExpiredCountsMetric, TestCommandExpiredCountsMetric | `go test -race -count=1 ./internal/platform/queue/... ./internal/platform/commands/...` | ok |
| Biến BACKUP_EXPECTED, DB_DISK_BUDGET_BYTES, DB_CONN_BUDGET | TestLoadAlertOptions | `go test -race -count=1 ./internal/platform/config/` | ok |
| Kiến trúc, SQL, dynsql, lint, ranh giới; e2e (main.go đổi) | tests/arch, tests/e2e | `go test -race -count=1 ./tests/arch/... ./tests/e2e/...` | ok (17 s, 71 s) |
| Sau khi gộp main: mọi gói chạm và phụ thuộc | ./internal/... ./scripts/... ./cmd/... | `go test -race -count=1 ./internal/... ./scripts/... ./cmd/... ./tests/...` | ok (lượt cuối sau sửa reviewer: mọi gói ok; `internal/platform/files` chạy với SeaweedFS của dự án crm-t19 ở cổng 8419: ok). Lượt đầu `internal/imager` đỏ dưới -race lúc máy tải nặng, chạy lại riêng: ok (gói không bị chạm) |
| Việc in_doubt kết thúc trước lần báo trước, đủ 120 s sau đó vẫn được báo; việc dead kết thúc trong lúc gửi được lượt sau báo | TestRuleInDoubtAfterEarlierAlert, TestRuleDeadDuringSendNotLost | `go test -race -count=1 ./internal/platform/observability/` | ok (đỏ trước khi sửa) |
| golangci-lint | — | `golangci-lint run ./...` | 0 issues |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `NewTelegramAlerter(d, cfg, client, logger)`: thêm tham số `logger` (kế hoạch: ba tham số). "Thiếu biến Telegram →
  chỉ log" cần logger của tiến trình; dự án không đặt `slog.SetDefault`.
- `migrations/0010_ops.sql` thêm index `platform.jobs_attention (kind, finished_at) WHERE status IN ('dead','in_doubt')`
  (kế hoạch không ghi): luật `dead:`, `in-doubt:`, kiểm sâu và tổng kết đếm việc mỗi phút, không được quét cả bảng
  `jobs` (việc done giữ 30 ngày). Không `CONCURRENTLY`: M0 chưa chạy production.
- Lịch `platform.digest`: chu kỳ 15 phút, chỉ gửi từ 08:00 giờ Việt Nam (01:00 UTC), một tin mỗi ngày theo key
  `digest:<ngày>`, thay cho "chạy lúc 01:00 UTC" (`schedules` chỉ có chu kỳ, không có giờ cố định). Worker ngừng lúc
  01:00 thì tin đi ở lượt đầu tiên sau đó.
- Hai lịch chạy như **lệnh** (`commands.Handler`, như `NewInDoubtResolver`), không phải handler transactional: gửi
  Telegram là gọi mạng.
- Thêm gauge `disk_used_pct` (Flusher `DiskPaths`): luật `disk:<process>` của kế hoạch cần nó, chưa task nào ghi.
- Thêm biến worker `BACKUP_EXPECTED`, `DB_DISK_BUDGET_BYTES`, `DB_CONN_BUDGET` cho `RuleOptions`.
- `/health/deep`: thẻ kiểm ở một middleware `healthToken` trong `internal/httpapi` (route công khai `healthDeep`), vì
  handler ogen không thấy header; hợp đồng không đổi (401 đi qua `default: Error`).
- Câu đọc view thống kê PostgreSQL nằm ở `pgwatch_dynsql.go` (+ `pgwatch_dynsql_test.go`) vì sqlc không có danh mục
  `pg_stat_*`; câu còn lại ở `queries/platform/ops.sql`.
- Thêm `schedule.go` (hai lệnh định kỳ), `rules_eval_test.go`, `helpers_test.go`, `internal/httpapi/handlers_health_test.go`,
  `internal/platform/config/ops_test.go`, `internal/platform/metrics/disk.go`, `disk_test.go`.
- Test có sẵn đổi theo hành vi mới: `TestServerRouting` mong `/health/deep` 401 `unauthenticated` (trước: 501).

## Quyết định thay chủ shop

1. **Luật `dead:<kind>`, `in-doubt:<kind>` báo theo việc mới**: chỉ đếm việc **đủ tuổi** (`finished_at` + 0 s / 120 s)
   sau mốc của lần cảnh báo cuối của cùng key; mốc (`sent_at`) là lúc đọc của lượt luật, không phải lúc ghi, để việc
   kết thúc trong lúc đang gửi không bị bỏ sót (reviewer lượt 1). Vì sao: spec 04 mục 8 "Thường … gửi một lần"; báo mỗi 10 phút khi việc dead còn mở sẽ thành ồn. Phương án
   khác: báo mỗi 10 phút khi còn việc dead mở (đúng chữ "10 phút một lần" nhưng lặp vô hạn).
2. **Chưa có nhịp sống nào của worker/ingest → vẫn báo** (`worker-heartbeat`, `ingest-heartbeat`) và `/health/deep`
   `ok:false`. Vì sao: an toàn hơn (máy chưa từng chạy ingest là lỗi cấu hình). Phương án khác: bỏ qua khi chưa có dòng.
3. **"N phút liền" cho phép hở một phút** (một lần ghi chỉ số trễ), và phút cuối phải trong 2 phút qua (tiến trình đã
   tắt thì không báo). Phương án khác: đòi đủ N dòng liên tiếp tuyệt đối (lỡ một nhịp ghi là đếm lại).
4. **Gửi Telegram lỗi thì không ghi `alert_events`** và lệnh `not_sent` chạy lại trong hạn (60 s). Gửi được mới ghi.
   Vì sao: lần gửi hỏng phải được thử lại; trùng tin hiếm (hết giờ chờ khi Telegram đã nhận) ít hại hơn mất cảnh báo.
   Hai worker cùng chạy có thể gửi trùng (không khóa database khi gọi mạng). Phương án khác: ghi trước rồi gửi (mất
   cảnh báo gấp tới 30 phút khi Telegram lỗi).
5. **`wal-archive` chỉ xét khi `archive_mode` bật**; chưa đẩy lần nào thì tính từ `stats_reset`. Cụm dev không bật
   archive nên không báo. Phương án khác: báo cả khi archive tắt (dev luôn đỏ).
6. **`long-tx`, `lock-wait`, `pg-connections` chỉ xét phiên của database hiện tại**; `pg-connections` đếm phiên của
   role đang chạy (`current_user` = `crm_app`); ngân sách mặc định 36 (ADR-42). Phương án khác: cả cụm.
7. **`slow-query` không lấy câu SQL**, chỉ `queryid` và số đo (câu có thể mang hằng số là dữ liệu khách).
   `pg_stat_statements` chưa nạp hay chưa cài → bỏ qua, không lỗi.
8. **Chỉ số `jobs_expired`** (việc treo của Task 14) chỉ đếm, chưa có luật cảnh báo riêng: việc định kỳ (resolver,
   alerts, digest) hết hạn là bình thường, báo sẽ ồn. Ghi ở Việc phát hiện thêm.
9. **`GRANT pg_monitor TO crm_owner WITH ADMIN OPTION`** thêm vào `docker/postgres/init*/01-roles.sql` và
   `testkit.CreateDatabase` (để cụm dev cũ vẫn chạy test). Máy chủ: bước dựng role phải có dòng này (ghi ở Việc phát
   hiện thêm, RUNBOOK Task 22).
10. **Bộ kiểm từ ngoài là tệp chạy một lần** (dịch vụ theo lịch gọi mỗi phút), nhớ lần nhắn cuối qua tệp
    `PROBE_STATE_FILE`; nhắn kênh gấp. Chưa cài (cần chủ shop chọn dịch vụ).

## Việc phát hiện thêm

- Máy chủ: bước dựng role (RUNBOOK, Task 22) cần `GRANT pg_monitor TO crm_owner WITH ADMIN OPTION` trước khi migrate
  0010; database dev cũ đã áp 0012/0112 cần dựng lại (`make db-reset`).
- RUNBOOK: thiếu `pg_monitor` (hay lỗi đọc kéo dài) làm mọi lượt `platform.alerts` thành `not_sent` rồi `expired`;
  cảnh báo khác vẫn gửi, nhưng log có dòng "lịch platform.alerts lỗi" mỗi phút: kiểm quyền trước.
- Migration 0010 đứng trước 0012/0112 đã có: chỉ chạy trên database mới cho tới khi task chung bật
  `WithAllowOutofOrder` (Q-X3).
- `platform.alert_events` chưa có lịch dọn (vài dòng mỗi ngày; thêm vào `schedules.Cleanup` khi cần).
- `jobs_expired` chưa có luật cảnh báo; module đầu tiên có lệnh thật (gửi tin) nên xét luật `expired:<kind>` cho lệnh
  nghiệp vụ (loại trừ việc định kỳ).
- Luật của Task 20 (`new-error`, `error-spike`) thêm vào `rules.go` theo cùng mẫu `tableRules`.
- Chọn và cài dịch vụ chạy `scripts/external-probe` ngoài Cloudflare, ngoài VPS: cần chủ shop OK (spec 04 mục 8).

## Reviewer

Kết luận: ĐẠT (lượt 2).

- Lượt 1: CẦN SỬA, hai phát hiện về tính đúng, đã sửa:
  1. Việc `in_doubt` kết thúc trước lần báo trước nhưng đủ 120 s sau đó bị loại mãi mãi (`finished_at > sent_at`).
     Sửa: so `finished_at + min_age` với mốc; test `TestRuleInDoubtAfterEarlierAlert`.
  2. Việc `dead` kết thúc trong lúc đang gửi Telegram bị bỏ sót (mốc `sent_at = now()` lúc ghi). Sửa: `sent_at` là
     lúc đọc của lượt luật (`notifyAsOf`, không xuất); test `TestRuleDeadDuringSendNotLost`.
  - Ghi chú không bắt buộc đã xử lý: thêm `status IN ('dead','in_doubt')` để dùng index partial với generic plan;
    README nói `PROBE_STATE_FILE` phải bền; RUNBOOK `pg_monitor` và 0010 trước 0012/0112 ghi ở Việc phát hiện thêm.
- Lượt 2: ĐẠT, không phát hiện phải sửa. Ghi chú: `sent_at` theo đồng hồ tiến trình, `finished_at` theo đồng hồ
  database; cùng máy có NTP thì lệch không đáng kể.
