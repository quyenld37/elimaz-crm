# Task 445 — DeepHealth tính tuổi sao lưu và WAL bằng một đồng hồ (PostgreSQL)

Kế hoạch: task chung của điều phối viên (21/11), không có trong kế hoạch M1; số toàn cục `x-clock` (đã thêm dòng
`| chung (21/11) | x-clock | 445 | DeepHealth một đồng hồ |` vào `../../docs/superpowers/plans/danh-so-task-toan-cuc.md`,
tệp ngoài repo crm). Loại việc (quy trình mục 3): A

Gốc (Task 443, Dừng để hỏi): `TestDeepHealth` đỏ trên main vì mốc sao lưu do `scripts/backup/runs-lib.sh` ghi bằng
`now()` của PostgreSQL còn `DeepHealth` (`deephealth.go:78`) tính tuổi bằng `time.Now()` của Go. Đồng hồ PostgreSQL trong
VM colima nhanh hơn Mac ~0,13 s → tuổi 2,99996 giờ → cắt thành 2. Tuổi WAL (`deephealth.go:100`,
`pg_stat_archiver.last_archived_time` theo đồng hồ PostgreSQL) cùng kiểu. Cách điều phối viên chọn: tính hai tuổi này
ngay trong SQL bằng `now()` của PostgreSQL; test giữ mốc `'3 hours'` và điều kiện `== 3`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi chữ ký nào. `DeepHealth(ctx, d)` giữ nguyên (1 chỗ gọi:
  `internal/httpapi/handlers_health.go:51`), thân gọi hàm không xuất mới `deepHealth(ctx, d, now)` (now chỉ còn dùng cho
  nhịp sống và lần chép tệp); `DeepHealthReport` giữ nguyên trường. `export_test.go` (chỉ test) thêm `DeepHealthAt`. Mã
  sinh sqlc thêm `LastBackupAgeSeconds` (gói `internal/platform/internal/store`, chỉ observability gọi: `deephealth.go`,
  `summary.go`, `digest.go`) và trường `AgeS` của `LastBackupRunRow` (`LastBackupRun` có 2 chỗ gọi: `rules.go:306`
  `verifyRule` chỉ đọc `Ok`, `summary.go` dùng `AgeS` thay `FinishedAt`). Rà cùng gói (sau khi điền lần đầu): `Summary`
  (`summary.go` `backupAndAlerts`) và `SendDailyDigest` (`digest.go`) giữ chữ ký, chỉ đổi nguồn tuổi sao lưu; chỗ gọi
  `/health/summary` (`internal/httpapi/handlers_health.go:116-120`), job tổng kết (`observability/alertjobs`) không đổi.
  `LastBackupAt` còn 1 chỗ gọi (`rules.go:290` `ageRule`, xem Quyết định).
- Gói phụ thuộc ngược (go list): nhập `internal/platform/observability`: `cmd/crm`, `cmd/imager`, `internal/httpapi`,
  `internal/modules/{cash,fulfillment,inventory}`, `internal/platform/queue`, `internal/platform/webhooks`,
  `observability/alertjobs`, `observability/liveprobe`. API không đổi nên các gói này chỉ cần build; test chạy thêm
  `internal/httpapi -run TestHealth` (đường `/health/deep`).
- Operation OpenAPI bị chạm, oasdiff: không (`GET /health/deep` giữ schema `DeepHealth`).
- Chỗ dùng trên màn hình: không (bộ kiểm từ ngoài đọc `/health/deep`).
- Câu sqlc, bảng, pha migration: thêm câu `LastBackupAgeSeconds` trong `queries/platform/ops.sql` (đọc
  `platform.backup_runs`, không đổi bảng); `archiverSQL` (`pgwatch_dynsql.go`) thêm một cột tuổi
  `extract(epoch FROM now() - last_archived_time)`. Không migration.
- Test sẽ chạy: `go test -race ./internal/platform/observability/...`, `go test -race ./internal/httpapi/ -run TestHealth`,
  rồi ci-task một lần.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có hàm "tuổi theo
  đồng hồ database"; grep `extract(epoch FROM now()` trong `queries/platform`: đã có `HeartbeatAgeSeconds`
  (metrics.sql), `OldestPendingAgeSeconds` (jobs.sql), `ListHeartbeats` (health.sql) — cùng cách tính tuổi trong SQL.
- Dùng lại / viết mới vì: theo đúng cách của các câu trên (tuổi `float8` giây tính bằng `now()` trong SQL); dùng lại
  `readArchiver` (thêm một cột thay vì câu thứ hai đọc `pg_stat_archiver`). Viết câu mới `LastBackupAgeSeconds` vì
  `LastBackupAt` trả mốc và rules, digest, summary vẫn dùng mốc đó (giữ nguyên).

## Soát thiết kế

- 5.1–5.3: chỉ đọc bảng của chính platform (`platform.backup_runs`) và view thống kê `pg_stat_archiver`. 5.4–5.6, 5.9:
  không áp dụng: chỉ đọc, không ghi, không gọi ra ngoài. 5.7: không áp dụng: bảng không có `tenant_id`. 5.8: gói không
  nhập HTTP. 5.10–5.11: không Valkey, không thư viện mới. 5.12: SQL tĩnh ở `queries/` qua sqlc; câu đọc view thống kê ở
  `pgwatch_dynsql.go` như cũ (chạy trên PostgreSQL thật trong `TestReadPGStats`, `TestDeepHealth*`).
- 6.1–6.11: không áp dụng: đường chỉ đọc, không khóa, không transaction ghi, không tiền. Đúng về thời gian (bẫy "một
  đồng hồ"): mỗi tuổi so với đồng hồ của bên ghi mốc — sao lưu (`runs-lib.sh`, `now()`) và WAL (PostgreSQL) tính bằng
  `now()` trong SQL; nhịp sống (`metrics.flusher` ghi `time.Now()` của Go) và lần chép tệp (`scripts/filesync`,
  `date +%s` của máy chạy container) giữ so với `time.Now()` của Go như Task 440 đã chốt; việc chờ
  (`OldestPendingAgeSeconds`) vốn đã tính bằng `now()`.
- 6.12: báo cáo chỉ có tuổi, số đếm. 6.13: không đổi quyền (crm_app đọc `backup_runs`, `pg_stat_archiver` như cũ).

## Đã làm

- `deephealth.go`: `DeepHealth` gọi `deepHealth(ctx, d, time.Now())`; tuổi sao lưu đọc bằng câu mới
  `LastBackupAgeSeconds` (`extract(epoch FROM now() - finished_at)`), giờ = `int64(age_s / 3600)` (cắt như cũ); tuổi
  WAL lấy cột mới của `archiverSQL` (`extract(epoch FROM now() - last_archived_time)`, NULL → nil). `now` của Go chỉ còn
  cho nhịp sống và lần chép tệp (bên ghi dùng đồng hồ máy).
- Rà cùng gói, cùng lỗi (mốc ghi bằng `now()` của PostgreSQL, tuổi tính bằng đồng hồ Go): `summary.go`
  (`backupAndAlerts`: tuổi bản base và lần diễn tập, cắt giờ — cùng kiểu đỏ như DeepHealth, test cũ né bằng 10 phút dư)
  và `digest.go` (tuổi bản base, làm tròn) → cùng cách: `LastBackupAgeSeconds`, `LastBackupRun` thêm cột `age_s`.
- `TestDeepHealth` giữ nguyên (mốc `'3 hours'`, `== 3`).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tái hiện lỗi trước khi sửa | `TestDeepHealth`, `TestDeepHealthClockSkew` (mới), `TestSummaryBackupClockSkew` (mới), `TestDailyDigestBackupClockSkew` (mới) | `go test -race ./internal/platform/observability/ -run 'TestDeepHealth'` và `-run ClockSkew` (trước khi sửa mã) | `deephealth_test.go:36: tuổi: {…}` (TestDeepHealth đỏ như trên main); `-5m0s_3_hours_3_minutes`, `5m0s_3_hours_57_minutes` FAIL; summary `base 29, drill 4` / `base 31, drill 6; muốn 30, 5`; digest `sao lưu: 4 giờ` / `sao lưu: 6 giờ` |
| Tuổi sao lưu `/health/deep` đúng với mốc `now() - '3 hours'` | `TestDeepHealth` | `go test -race ./internal/platform/observability/...` | `ok … observability 7.253s` |
| Đồng hồ Go lệch ±5 phút → tuổi sao lưu vẫn 3 giờ, nhịp sống ghi bằng đồng hồ Go vẫn tươi (OK true), tuổi WAL bằng tuổi đọc riêng theo đồng hồ PostgreSQL | `TestDeepHealthClockSkew` (4 ca: −5/+5 phút × 3 giờ 3 phút / 3 giờ 57 phút) | như trên | `ok` |
| Tóm tắt sức khỏe, tin tổng kết: tuổi sao lưu, diễn tập theo đồng hồ PostgreSQL khi Go lệch ±5 phút | `TestSummaryBackupClockSkew` (4 ca), `TestDailyDigestBackupClockSkew` (2 ca); test cũ `TestSummaryBackupAndAlerts`, `TestDailyDigestOncePerDay` vẫn qua | như trên | `ok` |
| Không chập chờn | ba lần liền | `go test -race -count=1 ./internal/platform/observability/ -run 'TestDeepHealth\|TestSummary\|TestDailyDigest'` ×3 | `ok 2.490s`, `ok 2.611s`, `ok 2.464s` |
| Câu `archiverSQL` mới chạy được bằng crm_app trên PostgreSQL 18 thật | `TestReadPGStats`, `TestDeepHealth*` | như trên | `ok` (cụm dev archive tắt → nil; xem Việc phát hiện thêm) |
| `/health/deep`, `/health/summary` qua HTTP không đổi | `internal/httpapi` `TestHealth*` | `go test -race ./internal/httpapi/ -run TestHealth` | `ok … httpapi 2.022s` |
| Cổng của task: build, vet, lint, gen-check, sqlc-vet, danh mục, jscpd, gitleaks, oasdiff, test-guard | — | ci-task (lần chạy sau khi stage) | mọi cổng tĩnh qua; `CI-TASK ĐỎ: test (gói đổi, phụ thuộc, vùng cao)` chỉ vì các gói không dùng phần đã sửa: order, fulfillment `panic: test timed out after 10m0s` (máy quá tải: identity 412 s, inventory 499 s), fulfillment `TestStuck_PlanUsesIndexes` `57014` khi ghi dữ liệu thử, queue `TestBestEffortRetryingHeadDoesNotBlock`; trong cùng lần chạy `ok observability 39.092s`, `ok httpapi 242.127s` |
| Các gói đỏ ở ci-task là chập chờn do tải, không do task | chạy lại riêng | `go test -race -count=1 -timeout 30m ./internal/modules/order/ ./internal/modules/fulfillment/`; `go test -race -count=1 ./internal/platform/queue/ -run TestBestEffortRetryingHeadDoesNotBlock` ×3 | `ok order 351.895s`, `ok fulfillment 154.732s`; queue `ok` ×3. Lần ci-task đầu (trước khi stage) order `TestDuplicates_ConsumerReplay` đỏ một lần, chạy lại riêng `ok` |

## Quyết định thay chủ shop

- **Sửa cả `summary.go` và `digest.go`** (ngoài hai chỗ brief nêu): cùng gói, cùng lỗi trộn đồng hồ (brief: "sửa nếu
  cùng gói"); `Summary` cắt giờ nên đỏ đúng như `DeepHealth` khi đồng hồ lệch, test cũ chỉ né nhờ 10 phút dư. Phương án
  khác: ghi vào Việc phát hiện thêm. Dễ đảo: hai hàm chỉ đổi nguồn tuổi.
- **Không đổi luật cảnh báo (`rules.go` `ageRule` 26 giờ, `pgRules` wal-archive 5 phút, `readPGStats` long-tx,
  lock-wait)**: luật so mốc với `RuleOptions.Now`, và đó là điểm test dùng để giả thời gian (vd. `Now: time.Now().Add(2h)`
  để bản sao lưu 25 giờ thành quá 26 giờ). Chuyển sang `now()` của PostgreSQL phải đổi cách luật nhận đồng hồ (đổi thiết
  kế `RuleOptions`), còn sai số chỉ cỡ độ lệch đồng hồ (~0,13 s dev, ~0 trên máy chủ một nhân Linux) so với ngưỡng
  phút, giờ; không có test nào đặt mốc sát ngưỡng. Ghi vào Việc phát hiện thêm.
- **Nhịp sống và lần chép tệp giữ so với đồng hồ Go**: bên ghi dùng đồng hồ máy (Task 440 đã chốt), đổi sang `now()` là
  trộn đồng hồ theo chiều ngược lại.
- **Tuổi WAL không có test với archive bật**: crm-shared để `archive_mode = off` và không được đổi hạ tầng; test so tuổi
  với một câu đọc riêng theo đồng hồ PostgreSQL (khớp cả khi bật), câu thật chạy trên PostgreSQL thật với crm_app.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

- Luật cảnh báo trong `rules.go` còn so mốc PostgreSQL với `RuleOptions.Now` (đồng hồ Go): `ageRule` (`backup_runs`),
  `pgRules` wal-archive (`pg_stat_archiver`), `readPGStats` (`xact_start`, `query_start` của `pg_stat_activity` so với
  `o.Now - 5 phút`). Ngưỡng phút, giờ nên chỉ lệch ở sát ngưỡng; sửa triệt để cần đổi cách luật nhận đồng hồ.
- `LatestGaugeSum` (ops.sql) so `metrics_minute.bucket` (Go ghi) với `now() - 5 phút` của PostgreSQL; `ListLatestSpoolFiles`
  ngược lại. Cửa sổ 5 phút nên chỉ lệch ở mép.
- Test chập chờn khi máy tải cao (gặp trong ci-task của task này, chạy lại riêng đều qua): fulfillment
  `TestStuck_PlanUsesIndexes` (`57014` statement timeout lúc testkit ghi dữ liệu thử), queue
  `TestBestEffortRetryingHeadDoesNotBlock` (`besteffort_test.go:49` tin 1 chưa ở trạng thái chờ thử lại), order
  `TestDuplicates_ConsumerReplay` (`duplicates_test.go:428`), cùng hạn 10 phút mặc định của `go test` với gói order,
  fulfillment.
- Đường WAL của `/health/deep` chỉ kiểm được khi archive bật (cụm crm-shared tắt): nên có một lần chạy trên cụm có
  `archive_mode = on` (bộ diễn tập sao lưu) để thấy `lastWalArchivedAgeS` nhỏ, không âm.

## Reviewer

Kết luận: ĐẠT (lượt 1, agent reviewer, trên phần đã stage sau khi gộp main — main chưa đổi, `Already up to date`).

- Phát hiện phải sửa: không có.
- Ảnh hưởng khớp thực tế: `DeepHealth` giữ chữ ký; `walArchivedAge` bỏ `now` (1 chỗ gọi); `LastBackupRun` 2 chỗ gọi
  (`rules.go:306` chỉ đọc `Ok`, `summary.go:136` dùng `AgeS`); `LastBackupAt` chỉ còn `rules.go:290` (đã ghi Quyết định).
- Bằng chứng: các test lệch đồng hồ bắt được cả hai chiều lệch với cách tính cũ; tuổi WAL chỉ kiểm được một phần vì
  crm-shared tắt archive (đã ghi). Không test nào bị xóa, tắt, nới.
- Ghi chú: tái dùng đúng cách `HeartbeatAgeSeconds`, `OldestPendingAgeSeconds`; luật dự án (lỗi `%w`, sqlc,
  `*_dynsql.go`, mã sinh khớp nguồn) đạt.
