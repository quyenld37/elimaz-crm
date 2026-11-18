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

(không có) — lượt 2: `TestErrorRulesNewError` (Task 20) không xóa, đổi kỳ vọng theo luật mới (Quyết định lượt 2 mục 4).

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
  `WithAllowOutofOrder` (Q-X3). (lượt 2: Task 318 đã bật trên main, database dev cũ không cần dựng lại; ghi chú
  "phải dựng lại" trong đầu `0010_ops.sql` đã cũ nhưng migration đã commit không sửa.)
- `platform.alert_events` chưa có lịch dọn (vài dòng mỗi ngày; thêm vào `schedules.Cleanup` khi cần).
- `jobs_expired` chưa có luật cảnh báo; module đầu tiên có lệnh thật (gửi tin) nên xét luật `expired:<kind>` cho lệnh
  nghiệp vụ (loại trừ việc định kỳ).
- Luật của Task 20 (`new-error`, `error-spike`) thêm vào `rules.go` theo cùng mẫu `tableRules`.
- (lượt 2) main tự nó đỏ ở `make jscpd`: `db/migrate_test.go` (Task 318) và `queue/errors_test.go`~`worker_test.go`
  (Task 20) là đoạn trùng mới ngoài mốc; task chung cần quyết gộp hay cập nhật mốc.
- (lượt 2) Hook Stop và `make ci` cùng chạy golangci-lint thì test lint của `tests/arch` đỏ ("parallel golangci-lint
  is running"): không chạy `make ci` nền trong lúc hook chạy.
- Chọn và cài dịch vụ chạy `scripts/external-probe` ngoài Cloudflare, ngoài VPS: cần chủ shop OK (spec 04 mục 8).

## Lượt 2 (gộp main: Task 20, 317, 318, 48; lần gộp thứ hai: Task 54, 34, 43)

### Ảnh hưởng (lượt 2)

- Gộp `main` (merge, không rebase). Xung đột: `cmd/crm/main.go` (giữ `Role` của Task 20 và lệnh `ops...` của Task 19),
  `internal/httpapi/handlers_health.go` (giữ `HealthDeep` + `healthToken` và `GetPlatformHealth`),
  `internal/httpapi/handlers_health_test.go` (cùng tên tệp ở hai bên: giữ đủ test của hai bên trong một tệp),
  `docs/danh-muc-dung-chung.md` (sinh lại bằng `make catalog`).
- **Vòng nhập gói sau gộp**: Task 20 cho `queue` nhập `observability` (ghi lỗi của handler); `observability/schedule.go`
  của Task 19 nhập `commands` → `queue`. Sửa (commit gộp, chỉ cấu trúc): dời `schedule.go` sang gói con mới
  `internal/platform/observability/alertjobs` (`NewAlertsCommand`, `NewDigestCommand`, `AlertsSchedule`,
  `AlertsEvery`, `DigestSchedule`, `DigestEvery`; chỗ dùng duy nhất `cmd/crm/main.go`, 6 tham chiếu);
  `digestDue` → `DigestDue` (xuất ra cho `alertjobs`; 1 chỗ định nghĩa + 3 chỗ trong `rules_eval_test.go`).
  `pgwatch_dynsql_test.go` chuyển sang gói `observability_test` (gói trong không được nhập `testkit`: testkit → files →
  queue → observability), đọc qua `export_test.go` (`ReadPGStats`, `PGStatsView`, chỉ khi test).
- **Nối Task 20 với Task 19** (commit riêng, hành vi; Task 20 ghi "người gộp làm"):
  - `CheckRules` (`tableRules`) gọi `ErrorRules` của Task 20: luật `new-error:<8 ký tự>` và `error-spike` (normal)
    đi qua `Alerter` như mọi luật khác. `RuleHit`, `ErrorRules` không đổi chữ ký.
  - `Summary` (tóm tắt sức khỏe, `GET /api/v1/platform/health`) đọc `backup_runs` (`lastBaseAgeH` của `base` thành công
    gần nhất; `lastDrillAgeH`, `lastDrillOk` của `restore_drill` gần nhất) và `alert_events` 24 giờ (trừ `digest:`,
    mới nhất trước, tối đa 200 dòng). Hợp đồng không đổi (các trường đã có, `backup` tùy chọn); oasdiff không đổi.
  - Câu `ListNewErrorFingerprints` (`queries/platform/errors.sql`, Task 20) đổi: thêm điều kiện chưa có `alert_events`
    của key và tham số `prefix_len`; chỗ gọi duy nhất `ErrorRules`. Hằng `newErrorWindow` (60 s) → `newErrorLookback`
    (1 giờ).
  - Câu sqlc mới trong `queries/platform/ops.sql`: `LastBackupRun`, `ListAlertsSince`; `make gen` (mã sinh
    `internal/platform/internal/store`). Không đổi bảng, không migration mới.
- Gói phụ thuộc ngược: `observability` ← `cmd/crm`, `cmd/imager`, `internal/httpapi`, `queue` (mới từ Task 20) và qua
  `queue` mọi gói dùng hàng đợi; `alertjobs` ← `cmd/crm`.
- Màn hình: tóm tắt sức khỏe (Task 48 chưa có màn hình sức khỏe) — không đổi hợp đồng.
- Test sẽ chạy: `make ci` (toàn bộ, vì gộp main), riêng: `./internal/platform/observability/...`, `./internal/httpapi/...`,
  `./internal/platform/queue/...`, `./internal/platform/commands/...`, `./cmd/...`, `./tests/...`.

### Tái dùng (lượt 2)

- Đã kiểm danh mục: `observability.ErrorRules`/`RuleHit` (Task 20) dùng lại nguyên, chỉ đổi `RuleHit` sang `alert`
  nội bộ; `LastBackupAt` (Task 19) chỉ trả `finished_at` của lần thành công: tóm tắt cần cả lần diễn tập hỏng (`ok`)
  nên thêm `LastBackupRun` (không lọc `ok`) thay vì đổi câu cũ (luật `backup-age` cần lần thành công).
  `AlertCountsSince` đếm theo key, không có `severity`, `sent_at`: thêm `ListAlertsSince`.
- `alertjobs` là gói con mới chứ không vào `cmd/crm` vì có chính sách của lệnh (Kind, chạy lại, 429) cần test riêng.

- jscpd (`make jscpd`, mốc nền từ Task 3): 4 đoạn trùng mới. Hai của Task 19, giữ kèm lý do: `newEnv`
  (`observability/helpers_test.go`) giống `openDB` của `metrics/flusher_test.go` nhưng cần cả `adminURL`, `appURL`
  của cùng database nên không dùng được `testkit.OpenAppDB`; `stripURL` của `scripts/external-probe/probe.go` giống
  `alerter.go` vì bộ kiểm từ ngoài cố ý chỉ dùng thư viện chuẩn, không nhập gói nội bộ. Hai của main (Task 318
  `db/migrate_test.go`, Task 20 `queue/errors_test.go`~`worker_test.go`): main tự nó cũng đỏ ở bước này (đã chạy jscpd
  trên `git archive main`); ngoài phạm vi, ghi ở Việc phát hiện thêm. Không cập nhật `jscpd-baseline.json` (nới cổng).

### Soát thiết kế (lượt 2)

- 5.1–5.3: chỉ đọc `platform.backup_runs`, `platform.alert_events`, `platform.error_events` (bảng nền tảng, cùng gói).
- 5.12: SQL tĩnh qua sqlc; lỗi bọc một lần; không log trong `Summary` (handler là mép).
- 6.1: luật lỗi chạy trong cùng lượt `platform.alerts`; chống lặp của Alerter như luật khác (new-error theo key riêng
  từng dấu vân tay, cửa sổ 10 phút; dấu vân tay chỉ "mới" trong 60 s nên không lặp).
- 6.9: câu tóm tắt có giới hạn dòng (200) và dùng index `(key, sent_at DESC)` không được: lọc theo `sent_at`; bảng nhỏ
  (vài trăm dòng mỗi ngày), chấp nhận quét.
- 6.12: văn bản cảnh báo lỗi chỉ có 8 ký tự đầu dấu vân tay và số đếm (Task 20), không có nội dung lỗi; tóm tắt chỉ
  trả key, mức, giờ gửi của cảnh báo (không trả `text`).
- 6.13: quyền `platform.health.read` không đổi.
- Còn lại không áp dụng: không đường ghi, không tiền, không tenant, không gọi ngoài mới.

### Quyết định thay chủ shop (lượt 2)

1. **Gỡ vòng nhập gói bằng gói con `observability/alertjobs`** (chứ không đưa `queue` thôi nhập `observability` hay
   dời lệnh vào `cmd/crm`). Vì sao: không đụng mã của Task 20; chính sách của lệnh lịch vẫn có test riêng. Phương án
   khác: `cmd/crm/alerts.go` (không test được), hay để `queue` nhận một interface ghi lỗi (đổi mã Task 20).
2. **Tự nối Task 20 với Task 19** (`CheckRules` gọi `ErrorRules`; tóm tắt đọc `backup_runs`, `alert_events`) trong
   lượt này, commit riêng. Vì sao: Task 20 ghi "người gộp làm"; không nối thì luật `new-error`, `error-spike` không bao
   giờ gửi và tóm tắt sức khỏe luôn rỗng phần sao lưu, cảnh báo. Dễ đảo ngược (một commit).
3. **Cảnh báo trong tóm tắt: 24 giờ, trừ `digest:`, mới nhất trước, tối đa 200 dòng** (hợp đồng không có
   `maxItems`). Vì sao: tin tổng kết không phải cảnh báo; giới hạn để câu trả lời không phình khi bão cảnh báo.
4. **Luật `new-error` báo theo "chưa từng báo", nhìn lùi 1 giờ** thay cho chữ "first_seen_at trong 60 s qua" của kế
   hoạch (Task 20, Interfaces) (reviewer lượt 2). Vì sao: lịch `platform.alerts` cũng 60 s; Telegram lỗi, 429 hay
   lịch chạy trễ làm loại lỗi mới mất cảnh báo hẳn (cùng loại lỗi đã sửa cho `dead:` ở lượt 1). Cách mới:
   `ListNewErrorFingerprints` bỏ dấu vân tay đã có `alert_events` key `new-error:<8>`; test cũ của Task 20
   `TestErrorRulesNewError` đổi kỳ vọng "2 phút sau không còn mới" thành "chưa báo thì còn mới; đã báo thì thôi;
   quá 1 giờ thì thôi". Phương án khác: giữ 60 s (đúng chữ, mất cảnh báo); cửa sổ 10 phút dựa vào chống lặp (vẫn
   mất khi Telegram lỗi quá 10 phút).
5. **Không nới mốc jscpd** dù `make jscpd` đỏ (2 đoạn có sẵn trên main, 2 của Task 19 giữ kèm lý do).

### Bằng chứng (lượt 2)

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Gộp main hết vòng nhập gói, build và vet sạch | — | `go build ./... && go vet ./...` | ok (trước khi dời: `import cycle not allowed` queue → observability → commands → queue) |
| Lệnh lịch của `alertjobs`: Kind, tự chạy lại not_sent, lỗi → not_sent, 429 → RetryAfter, đạt → done | TestScheduledKind, TestScheduledExecute | `go test -race ./internal/platform/observability/alertjobs/` | đỏ trước khi dời (setup failed: import cycle), sau: ok |
| Câu pg_stat_* chạy bằng crm_app (test chuyển sang observability_test) | TestReadPGStats | `go test -race ./internal/platform/observability/...` | ok |
| Test của hai bên trong `handlers_health_test.go` (TestHealthDeep, TestPanicRecordsScrubbedError, TestHandlerErrorRecordsOnlyInternal, TestPlatformHealth) | như tên | `go test -race ./internal/httpapi/...` | ok (35.8 s) |
| Toàn bộ test sau gộp | ./... | `make ci` (bước test: `go test -race ./...`) | mọi gói ok trừ `tests/arch` đỏ vì hook chạy golangci-lint cùng lúc ("parallel golangci-lint is running"); chạy lại riêng `go test -race -count=1 ./tests/arch/...`: ok |
| build, vet, lint, gen-check, sqlc vet | — | `make ci` | qua; `golangci-lint run ./...`: 0 issues |
| vuln, oasdiff, catalog-check | — | `make vuln oasdiff catalog-check` | qua |
| jscpd | — | `make jscpd` | đỏ: 4 đoạn trùng (xem Tái dùng lượt 2; 2 có sẵn trên main) |
| Lần gộp thứ hai (main 8f437db: Task 54, 34, 43): một xung đột ở `cmd/crm/main.go` (main dời dựng `hub` lên trước `newCommands`; giữ chỗ của main, thêm `alertSchedules`), mã sinh và danh mục khớp | tests/arch, tests/e2e, httpapi, config | `make gen` (không chênh), `make catalog`, `golangci-lint run ./...`, `go test -race -count=1 ./tests/... ./cmd/... ./internal/platform/config/... ./internal/httpapi/...` | 0 issues; mọi gói ok |
| Nối Task 20: CheckRules gửi new-error:<8 ký tự> và error-spike (normal), văn bản không có nội dung lỗi | TestRuleErrorsWired | `go test -race -count=1 ./internal/platform/observability/...` | đỏ trước khi nối (không key nào), sau: ok |
| Tóm tắt: base thành công gần nhất (bỏ lần hỏng sau đó), diễn tập gần nhất kèm ok=false, cảnh báo 24 giờ mới nhất trước, không có digest và dòng 25 giờ; database mới: backup rỗng, alerts mảng rỗng | TestSummaryBackupAndAlerts | `go test -race -count=1 ./internal/platform/observability/...` | đỏ trước khi nối (backup nil), sau: ok |
| Lệnh lịch nhận 429 đã bọc bằng %w (ghi chú reviewer) | TestScheduledExecute | `go test -race -count=1 ./internal/platform/observability/alertjobs/` | ok |
| Gói chạm và phụ thuộc sau khi nối | observability, httpapi (TestPlatformHealth), tests/arch | `go test -race -count=1 ./internal/platform/observability/... ./internal/httpapi/... ./tests/arch/...`; `golangci-lint run ./...`; `make gen`, `make catalog` | ok; 0 issues; không chênh. `tests/arch` lần đầu đỏ vì golangci-lint của phiên khác trên máy giữ khóa, chạy lại riêng: ok |
| new-error không mất: gặp lần đầu 5 phút trước chưa báo → báo; đã có alert_events → thôi; quá 1 giờ → thôi (reviewer lượt 2) | TestRuleNewErrorNotLost, TestErrorRulesNewError (kỳ vọng đổi, xem Quyết định lượt 2 mục 4) | `go test -race -count=1 ./internal/platform/observability/...` | đỏ trước khi sửa (không key; "phải còn là lỗi mới sau 2 phút"), sau: ok |

## Lượt 3 (gộp main: Task 28, 34, 398, 399)

### Ảnh hưởng (lượt 3)

- Không đổi mã viết tay nào của Task 19. Chỉ gộp main (`git merge main`, không rebase).
- Xung đột duy nhất: mã sinh sqlc `internal/{modules/catalog,modules/identity,platform}/internal/store/models.go`
  (mỗi bên thêm kiểu cho bảng mới của mình: Task 19 `PlatformAlertEvent`, `PlatformBackupRun`; Task 28
  `Inventory*`). `internal/modules/inventory/internal/store/models.go` (mới từ main) cũng được sinh lại để có kiểu
  của bảng Task 19.
- Ký hiệu Go xuất ra, operation OpenAPI, câu sqlc, migration: không đổi so với hai bên.
- Test chạy: `make ci` một lần trên bản đã gộp.

### Tái dùng (lượt 3)

- Không viết mã mới; không có gì để tái dùng.

### Soát thiết kế (lượt 3)

- Migration: hai bên thêm migration riêng (Task 19 `0010`, Task 28 `0115`), không chạm cùng bảng; chạy không theo
  thứ tự đã được main cho phép (`WithAllowOutofOrder`, xem lượt 2).

### Quyết định thay chủ shop (lượt 3)

- Giải xung đột mã sinh: lấy bản của main (`git checkout --theirs`) rồi `make gen` để sqlc sinh lại từ nguồn đã gộp
  (migration và `queries/**` của cả hai bên). Không sửa tay. Phương án khác: lấy bản của task-19 rồi `make gen` —
  cho cùng kết quả vì sqlc sinh từ nguồn.

### Bằng chứng (lượt 3)

| Tiêu chí | Kiểm bằng | Lệnh | Kết quả |
|---|---|---|---|
| Mã sinh khớp nguồn đã gộp, không còn dấu xung đột | gen-check trong `make ci`; `grep -c '<<<<<<<'` | `make gen`, `make ci` | build, vet, lint, gen-check, sqlc-vet, `go test -race ./...` (34 gói ok, 0 FAIL), vuln, oasdiff, catalog-check: qua |
| jscpd | — | `make jscpd` | đỏ: 2 đoạn trùng của Task 19 đã giữ kèm lý do ở lượt 2 (`metrics/flusher_test.go`~`observability/helpers_test.go`, `alerter.go`~`external-probe/probe.go`); 2 đoạn của main ở lượt 2 nay đã hết. Không nới mốc |
| test-guard, console-check | — | `make test-guard console-check` | qua |
| gitleaks | — | `make gitleaks` | đỏ 6 dòng, tất cả ở commit `aedb415` chỉ có trên nhánh `task-319` (gitleaks quét mọi nhánh của repo chung); `gitleaks git --log-opts=HEAD .` và `gitleaks dir .`: no leaks found |
| Kiểu của cả hai bên có mặt | `git diff --cached main/HEAD -- internal/platform/internal/store/models.go` | xem bên | +`PlatformAlertEvent`, `PlatformBackupRun` so với main; +`Inventory*` so với task-19 |

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

### Reviewer — lượt 2, commit gộp main

Kết luận: ĐẠT, không phát hiện phải sửa. Đã so phần stage với cả HEAD (Task 19) và MERGE_HEAD (main): không mất phần
nào của hai bên ở `main.go`, `handlers_health.go`, `handlers_health_test.go`, `worker.go`, danh mục; gói
`alertjobs` giữ nguyên hành vi lệnh lịch (Kind, AutoRetryNotSent, not_sent, RetryAfter qua `%w`). Ghi chú không bắt
buộc: (1) thêm ca 429 đã bọc vào `TestScheduledExecute`; (2) doc gói `observability` còn nói "hai lệnh định kỳ" nằm
trong gói — làm ở commit nối sau.

### Reviewer — lượt 2, lần gộp main thứ hai (8f437db)

Kết luận: ĐẠT, không phát hiện phải sửa. `main.go`: `hub` dựng một lần (chỗ của main), `alertSchedules` sau lịch tra
lại lệnh, `resolver` chỉ nhận `cmds`; `config.go` auto-merge sạch (ba biến của Task 19 cạnh biến M1 của main).

### Reviewer — lượt 2, commit nối Task 20 với Task 19

- Lần 1: CẦN SỬA — `new-error` chỉ xét `first_seen_at` trong 60 s, bằng chu kỳ lịch: Telegram lỗi/429 hay lịch trễ
  làm mất cảnh báo hẳn. Đã sửa: "chưa từng báo" (NOT EXISTS `alert_events` của key), nhìn lùi 1 giờ; test
  `TestRuleNewErrorNotLost` (Quyết định lượt 2 mục 4).
- Lần 2: ĐẠT, không phát hiện phải sửa. Ghi chú: hai dấu vân tay trùng 8 ký tự đầu thì cái sau không được báo
  (xác suất ~1/4 tỷ, cách cũ cũng gộp).

### Reviewer — lượt 3, commit gộp main (Task 28, 34, 398, 399)

- Kết luận: **ĐẠT**. Không có phát hiện phải sửa.
- Đã kiểm: phần stage so với `MERGE_HEAD` (f591177) đúng bằng 51 tệp Task 19 + `inventory/.../models.go` (+2 kiểu của Task 19);
  so với `HEAD` đúng bằng 49 tệp của main + tệp task. Tệp tự gộp (`danh-muc-dung-chung.md`, `queue/worker_test.go`,
  `helpers_test.go`, `sqlc.yaml`) giữ đủ hai bên. Migration `0010` và `0115` không chạm cùng đối tượng. Không còn dấu xung đột.
- Ghi chú: main đã tiến thêm `e1f780b` (chỉ đổi git hook pre-commit), không nằm trong lần gộp này.
