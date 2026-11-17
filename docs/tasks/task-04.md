# Task 04 — Pool pgx, transaction, migration bằng goose, sqlc, RetryTx (ADR-46)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md` (Task M0-04, bản sửa 18/11 theo ADR-46).
Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu xuất ra ngoài **có sẵn** nào đổi. Gói mới
  `internal/platform/db` (PoolOptions, DB, Open, Setting, TxOptions, TxFlashBuy/TxAPI/TxIngest/TxWorker, ErrRetryLater,
  RetryTx, Backoff, TransientCode, Migrate, LintContract), gói mới `migrations` (FS), `internal/testkit` (StartPostgres),
  mã sqlc sinh ở `internal/platform/internal/store`. Trong `cmd/crm/main.go` chỉ đổi ký hiệu không xuất ra ngoài:
  xóa `openPool`, `errMigrateNotYet`; `serveHTTP`, `runWorker` nhận `*db.DB` thay `*pgxpool.Pool`.
  `observability.NewHealth(p Pinger)` giữ nguyên chữ ký, nhận `d.Pool` (đã là `*pgxpool.Pool`, thỏa `Pinger`).
- Gói phụ thuộc ngược (go list): `internal/platform/db` mới nên chưa ai nhập; sau task: `cmd/crm`, `internal/testkit`,
  test của `internal/platform/db`, `tests/arch`, `tests/e2e`. `cmd/crm` hiện nhập config, logging, observability,
  telemetry (không gói nào nhập `cmd/crm`; `tests/e2e` dựng nó thành tệp chạy).
- Operation OpenAPI bị chạm, oasdiff: không (chưa có `openapi.yaml`, Task 6).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: pha **mở rộng** (migration đầu tiên `0001_platform.sql`, dải M0 0001–0099): schema
  `platform`, `identity`; bảng `platform.tenants`, `platform.tenant_routes`, `platform.migration_checksums`; ba
  extension. Bảng phiên bản của goose đặt ở `platform.goose_db_version` (không bảng nào trong `public`). Câu sqlc mới
  trong `queries/platform/`: `tenants.sql` (theo kế hoạch), `tx.sql` (đặt giới hạn theo transaction),
  `migrate.sql` (advisory lock, checksum). Bảng mới xếp vào `docs/adr/0046-thu-tu-khoa.md` cùng commit.
- Test sẽ chạy: `go test -race ./internal/platform/db/... ./internal/testkit/... ./tests/arch/... ./tests/e2e/... ./cmd/...`
  trên PostgreSQL dev riêng của phiên (dự án compose `crm-t04`, cổng 5504); rồi `make ci` phần đã có.
  Không chạm vùng rủi ro cao (tenancy, queue, commands, idempotency, identity, guard.go chưa có).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` có config, logging,
  observability, telemetry; không có gì về pool, transaction, migration, thử lại. Tìm `Begin`, `retry`, `backoff`,
  `pgxpool`, `migrat` trong `internal/`, `cmd/`: chỉ có `openPool` tạm của Task 3 (`cmd/crm/main.go`) → xóa, thay bằng
  `db.Open`. `spikes/flashsale/buy.go` có bản sao tối giản vòng thử lại (module riêng, không nhập `internal/`, kế hoạch
  Task 1 nói rõ) → không gộp. `tests/arch/lint_test.go` có `repoRoot` → dùng lại cho các test kiến trúc mới.
- Dùng lại / viết mới vì:
  - pgx `pgxpool` (ADR-44) cho pool; `pgconn.PgError` đọc SQLSTATE; goose v3.30.1 (ADR-44) chạy migration;
    otelpgx v0.13.0 (Tech Stack của kế hoạch, ADR-44 dòng "Dấu vết") cho dấu vết SQL; sqlc v1.31.1 (ADR-44) sinh mã.
  - Vòng thử lại tự viết trong `RetryTx`: thư viện chuẩn không có; `cenkalti/backoff`, `sethvargo/go-retry` có trong
    go.sum (gián tiếp) nhưng không thuộc ADR-44, và luật thử lại của ADR-46 (đúng ba mã SQLSTATE, 4 lần gọi, công thức
    chờ cố định, nhả transaction trước khi chờ) chỉ vài chục dòng.
  - `LintContract` tự viết: không công cụ nào trong ADR-44 kiểm luật `-- contract:`.
  - `testkit.StartPostgres` mới (Task 3 ghi "testkit có ở Task 4"); `tests/e2e/boot_test.go` giữ `databaseURL` riêng
    vì nó chỉ cần chuỗi `crm_app` có sẵn.
  - Không có cảnh báo dupl/jscpd nào để trả lời lúc viết mục này; nếu có sẽ ghi ở "Bằng chứng".

## Soát thiết kế

Mục 5 (khớp kiến trúc):

1. Chủ dữ liệu: `tenants`, `tenant_routes`, `migration_checksums` thuộc `platform` (04 mục 3, Global Constraints:
   bảng nền tảng không RLS); schema `identity` chỉ được tạo (bảng của nó ở Task 7).
2. Không ghi bảng module khác: chỉ mã `internal/platform/**` nhắc bảng `platform.*`; `sqlusage_test` kiểm.
3. Không đọc bảng module khác: không áp dụng: chưa có module nghiệp vụ.
4. Việc nguyên tử trong một transaction: `RetryTx` là cách duy nhất mở transaction (`txusage_test`).
5. Việc không nguyên tử sau commit: không áp dụng: task không phát sự kiện hay lệnh.
6. Gọi ra ngoài: không áp dụng: không có lệnh ra ngoài. Tài liệu của `RetryTx` ghi luật "hàm không gọi mạng".
7. Ngữ cảnh shop: `tenants`, `tenant_routes` là bảng nền tảng không RLS theo Global Constraints. `RetryTx` nhận
   `Settings` để Task 5 thêm `app.tenant_id` trong cùng câu `set_config`.
8. Nghiệp vụ không biết HTTP: gói `db` không nhập `net/http`.
9. Đường ghi có khóa chống trùng: không áp dụng: không có route mới.
10. PostgreSQL là nguồn sự thật: không dùng Valkey.
11. Thư viện: goose, otelpgx, sqlc thuộc ADR-44/Tech Stack; không thêm gì ngoài danh sách (`pgx/v5/stdlib` thuộc pgx).
12. Module mẫu: không áp dụng: chưa có (Task 7).

Mục 6 (đúng khi chạy đồng thời):

1. Hai yêu cầu cùng sửa: không áp dụng cho dữ liệu nghiệp vụ; hai tiến trình `migrate` cùng lúc → `pg_advisory_lock(727001)` trên một kết nối riêng giữ suốt lần chạy.
2. Gửi lại: `Migrate` chạy lại được (lần hai trả `[]`); checksum ghi `ON CONFLICT DO NOTHING`.
3. Thứ tự khóa: task tạo `docs/adr/0046-thu-tu-khoa.md` (13 nhóm của spec + mọi bảng M0).
4. Lỗi tạm thời: `RetryTx` đúng ADR-46 điểm 1 (40P01, 55P03, 40001; 4 lần gọi; 20/40/80 ms × [0,5; 1,5]; hết lượt bọc `ErrRetryLater`; lỗi commit không SQLSTATE không thử lại; ctx hủy dừng ngay). Test deadlock thật trên PostgreSQL.
5. Mức cô lập: READ COMMITTED khi `IsoLevel` rỗng (ghi tường minh trong `BEGIN`).
6. Việc nền lỗi: không áp dụng: chưa có hàng đợi.
7. Bên ngoài 429: không áp dụng.
8. Tiến trình chết giữa chừng: transaction tự lùi; migrate: mỗi tệp goose chạy trong transaction riêng, tệp lỗi không để lại bảng; khóa advisory gắn với phiên nên tự nhả khi kết nối chết; checksum thiếu của tệp đã áp dụng được ghi bù ở lần chạy sau.
9. Treo lâu: pool đặt `statement_timeout` 30 s, `idle_in_transaction_session_timeout` 60 s, `lock_timeout` 5 s qua `RuntimeParams`; mỗi transaction đặt hai giới hạn riêng bằng `set_config(…, true)` (không rò sang kết nối, có test `MaxConns: 1`).
10. Tiền: `SELECT 150000::int8` đọc thành `int64` (test); chưa có cột tiền.
11. Quy luật luôn đúng (rapid): không áp dụng: gói `db` không thuộc vùng rủi ro cao; số lần gọi được kiểm bằng test cụ thể (a)–(d).
12. Dữ liệu cá nhân: otelpgx không bật `WithIncludeQueryParameters`; lỗi của `Migrate` chỉ nêu tên tệp, không in chuỗi kết nối.
13. Quyền: `crm_app` không tạo được bảng (test); chỉ được `SELECT` trên `platform.tenants`, `platform.tenant_routes`.

## Quyết định thay chủ shop

Chủ shop vắng, giao toàn quyền trong phạm vi kế hoạch và spec. Mỗi mục: chọn gì, vì sao, phương án khác.

1. **`testkit` dùng container PostgreSQL dev của phiên, không dùng testcontainers-go.** `StartPostgres`,
   `CreateDatabase` tạo database `crm_test_<ngẫu nhiên>` (chủ `crm_owner`) trên máy chủ ở `CRM_TEST_ADMIN_URL`, hoặc
   máy và cổng của `CRM_TEST_DATABASE_URL` với role dev; xóa bằng `DROP DATABASE … WITH (FORCE)` khi test xong.
   Vì sao: cùng cách Task 3 đã chọn (quyết định 10 của task-03), đúng luật mỗi phiên một dự án compose và cổng riêng;
   testcontainers kéo thêm vài chục module và phải dựng image Task 2 (tải WAL-G từ GitHub) ở mỗi máy chạy test. Đảo
   ngược dễ: chỉ đổi phần trong `internal/testkit`, chữ ký giữ nguyên. Phương án khác: testcontainers-go v0.45.0
   (ADR-44) chạy image `docker/postgres` của Task 2.
2. **Bảng phiên bản của goose ở `platform.goose_db_version`; `Migrate` tạo trước `CREATE SCHEMA IF NOT EXISTS platform`
   (`migrate_dynsql.go`), nên `0001` ghi `CREATE SCHEMA IF NOT EXISTS platform` thay cho `CREATE SCHEMA platform`.**
   Vì sao: Global Constraints cấm bảng trong `public`, và `crm_owner` không có quyền CREATE trên `public` của database
   `crm` dev (PostgreSQL 15+). Phương án khác: để bảng goose ở `public` (trái luật), hoặc một schema riêng cho goose.
3. **Extension `pg_stat_statements` do người quản trị tạo trước** (dev: `docker/postgres/init/01-roles.sql` tạo cả ba
   extension trong database `crm`; test: `testkit.CreateDatabase`). Vì sao: extension này không "trusted", `crm_owner`
   không tự tạo được; `CREATE EXTENSION IF NOT EXISTS` trong `0001` khi đó không làm gì. Phương án khác: bỏ dòng này
   khỏi `0001` (trái kế hoạch) hoặc chạy migrate bằng superuser (trái ADR-26/Global Constraints).
4. **Câu `set_config` đầu transaction là một câu tĩnh qua sqlc** (`queries/platform/tx.sql`: mảng tên và giá trị,
   `generate_subscripts`), thay cho câu dựng lúc chạy liệt kê từng `set_config`. Vẫn một lượt đi về, bỏ phần bằng 0,
   số Settings tùy ý. Vì sao: câu tĩnh phải qua sqlc (ADR-44 chốt 3), không cần tệp `*_dynsql.go`. Phương án khác:
   dựng chuỗi trong `tx_dynsql.go` kèm test.
5. **`PoolOptions` thêm trường `Tracing bool`** để "gắn otelpgx khi dấu vết bật" (kế hoạch không khai trường này);
   `cmd/crm` đặt bằng `OTEL_ENABLED`. Phương án khác: dò TracerProvider toàn cục (ngầm, khó test).
6. **`TxFlashBuy`, `TxAPI`, `TxIngest`, `TxWorker` giữ là `var`** đúng Interfaces của kế hoạch, kèm
   `//nolint:gochecknoglobals` có lý do. Luật Go số 4 (không biến toàn cục thay đổi được) đứng sau kế hoạch theo thứ bậc
   nguồn. Phương án khác: hàm `TxAPI()` (an toàn hơn, nhưng lệch Interfaces mà Task 5, 9, 12 dựa vào).
7. **Mã sqlc của `queries/platform` sinh vào `internal/platform/internal/store`** để mọi gói trong `internal/platform`
   (db; sau này tenancy, idempotency, queue…) dùng chung, mã ngoài `internal/platform` không nhập được.
8. **Kết nối của migrate:** kết nối giữ khóa advisory không đặt giới hạn ADR-26 (để chờ khóa của lần migrate khác
   quá 5 s mà không lỗi; ctx giới hạn); kết nối goose có đủ ba giới hạn ADR-26 như ADR-46 điểm 6 nói, `SetMaxOpenConns(1)` nên tổng 2 kết nối đúng ngân sách `migrate` của ADR-42. Cả hai dùng
   `QueryExecModeExec` (không đệm câu đã chuẩn bị cho DDL; không phải simple protocol). goose cần `*sql.DB`: lấy từ
   `pgx/v5/stdlib.OpenDB`, gói `db` không nhập `database/sql` (depguard vẫn giữ).
9. **`Migrate` chỉ chạy `LintContract` trên tệp sắp chạy**, checksum theo tên tệp, ghi sau khi goose chạy (ghi bù cho
   tệp đã áp dụng mà chưa có dòng, ví dụ khi tiến trình chết giữa chừng). Phương án khác: lint cả tệp đã áp dụng
   (một luật mới có thể chặn mọi lần deploy sau).
10. **Test kiến trúc `txusage`, `dynsql` không quét tệp `_test.go`; `dynsql` không quét `internal/testkit`.** Luật nói
    về mã sản phẩm; test cần SQL thô để dựng bảng thử và giữ khóa. `sqlmode` quét cả test (Global Constraints: "mọi nơi").
11. **`make sqlc-vet` chạy test `TestSqlcVet`** (dựng database thử bằng testkit, chạy `sqlc vet` với
    `CRM_SQLC_DATABASE_URL`): một cách cho cả "xanh trên queries/" và "đỏ với câu mẫu sai". `sqlc.yaml` đặt
    `analyzer.database: false` để `make gen` không cần database.
12. **`crm healthcheck` với `CRM_ROLE=migrate` thoát khác 0** kèm lời "chạy một lần rồi thoát" (Task 3 trả "chưa có").

## Bằng chứng

Lệnh chung: `go test -race -count=1 -v ./internal/... ./tests/... ./tools/...` trên PostgreSQL 18 của dự án compose
`crm-t04` (cổng 5504) → 8 gói `ok`, 122 dòng PASS, 0 FAIL. Trước khi viết mã: `go test -count=1
./internal/platform/db/... ./tests/...` → `FAIL … [setup failed]` (`no required module provides package
…/internal/testkit`, chưa có gói `db`) — đỏ đúng chỗ. Lần chạy đầu sau khi có mã: 1 test đỏ
(`TestMigrateRejectsContractWithoutMarker`: test dựng sai — database rỗng nên 0001 cũng chưa chạy; sửa test cho chạy
migrate thật trước, ý kiểm giữ nguyên).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (a) Deadlock thật: hai transaction khóa chéo đều xong, tổng 3 lần gọi, OnRetry 1 lần 40P01, v = [2 2] | `TestRetryTxRealDeadlockBothSucceed` | lệnh chung | `--- PASS (1.14s)` |
| (b) Lỗi tạm thời kéo dài → 4 lần gọi, `errors.Is(err, ErrRetryLater)`, `TransientCode` = 40001, OnExhausted 1 lần | `TestRetryTxExhaustedReturnsRetryLater` | lệnh chung | PASS |
| (b) Khóa giữ 2 s, `LockTimeout` 50 ms → `ErrRetryLater` mã 55P03, 4 lần gọi; giữ 100 ms → nil, 2 hoặc 3 lần | `TestRetryTxLockTimeoutRetriesThenGivesUp`, `TestRetryTxLockTimeoutRecoversWhenLockReleased` | lệnh chung | PASS, PASS |
| (c) Lỗi không tạm thời không thử lại: 23505 (1 lần, không ErrRetryLater, OnRetry 0), 57014 (1 lần); lỗi của hàm (1 lần, dòng lùi); panic (lùi, ném lại, 1 lần) | `TestRetryTxNonTransientNotRetried`, `TestRetryTxPlainErrorRollsBackOnce`, `TestRetryTxPanicRollsBackAndRepanics` | lệnh chung | PASS ×3 |
| (d) Số lần gọi = số lỗi tạm thời + 1 (40P01, 55P03, rồi nil → 3), lần trước không để lại dòng (chỉ id 3) | `TestRetryTxCallsEqualTransientErrorsPlusOne` | lệnh chung | PASS |
| `lock_timeout`, `statement_timeout` theo transaction, không rò sang kết nối (MaxConns 1: 200ms trong, 5s sau); DefaultTx của pool, Settings, READ COMMITTED mặc định, IsoLevel đổi được | `TestRetryTxLimitsDoNotLeakToConnection`, `TestRetryTxUsesPoolDefaultsSettingsAndReadCommitted` | lệnh chung | PASS, PASS |
| Backoff 20/40/80 ms, rnd 0 → nửa, 1 → gấp rưỡi; TransientCode | `TestBackoff`, `TestTransientCode` | lệnh chung | PASS |
| ctx hủy lúc chờ → `context.Canceled` dưới 50 ms, không gọi thêm | `TestRetryTxStopsWhenContextCanceledDuringWait` | lệnh chung | PASS |
| Không mã sản phẩm nào mở transaction ngoài RetryTx; tệp mẫu `pool.Begin` bị báo | `TestTxUsageProductCode`, `TestTxUsageReportsSample` | lệnh chung | PASS, PASS |
| Pool: int8 → int64 (150000; +30000 = 180000); SHOW 30s / 1min / 5s; StatementTimeout 100ms + pg_sleep(1) → "canceling statement due to statement timeout" | `TestOpenReadsInt8AsInt64`, `TestOpenSetsConnectionLimits` | lệnh chung | PASS, PASS |
| `crm_app` không tạo được bảng (42501); chỉ đọc `platform.tenants` | `TestAppRoleCannotCreateTable`, `TestAppRoleReadsButCannotWriteTenants` | lệnh chung | PASS, PASS |
| Migrate: lần đầu mọi tệp theo thứ tự, lần hai `[]`, bảng goose không ở public; hai lần song song áp dụng mỗi tệp đúng một lần; sửa tệp đã áp dụng → lỗi nêu tên tệp và "checksum"; tệp lỗi SQL → không bảng nào của tệp còn lại; thiếu `-- contract:` → từ chối, nêu tên tệp | `TestMigrate*` (6 test) | lệnh chung | PASS ×6 |
| LintContract: DROP COLUMN → 1; có `-- contract: v0.3.0` → 0; ADD COLUMN, CREATE INDEX → 0 (17 ca) | `TestLintContract` | lệnh chung | PASS |
| sqlmode: tệp mẫu bị báo, mã thật 0 | `TestSQLModeReportsSample`, `TestSQLModeProductCode` | lệnh chung | PASS, PASS |
| `make sqlc-vet`: câu mẫu sai tên cột → đỏ; queries/ → xanh | `TestSqlcVet` | `make sqlc-vet` | `ok elimaz.com.vn/crm/tests/arch` |
| sqlusage: identity nhắc `platform.jobs` → báo; câu thiếu schema → báo; dynsql của identity nhắc platform → báo; câu đúng (CTE, hàm trong FROM, EXTRACT, DISTINCT FROM) không báo; mã thật 0 | `TestSQLUsageReportsSample`, `TestSQLUsageProductCode` | lệnh chung | PASS, PASS |
| dynsql: `QueryRow("SELECT …")` ngoài `*_dynsql.go` → báo; `*_dynsql.go` thiếu test → báo; có test → không báo; mã thật 0 | `TestDynSQLReportsSample`, `TestDynSQLProductCode` | lệnh chung | PASS, PASS |
| Vai trò migrate thật (thay chỗ "chưa có" của Task 3): tệp chạy áp dụng 0001 rồi thoát 0; lần hai không áp dụng lại; healthcheck của migrate khác 0 | `TestMigrateRoleAppliesThenExits`, `TestMigrateRoleHasNoHealthcheck` | lệnh chung | PASS, PASS |
| Ba vai trò còn lại vẫn khởi động và tắt êm với `db.Open` | `TestBootEachRoleThenGracefulStop`, `TestHealthcheckCommand` | lệnh chung | PASS, PASS |
| Đường dev thật: `crm` (CRM_ROLE=migrate) trên database `crm` của compose crm-t04 | — | chạy tệp chạy hai lần | `"applied":["0001_platform.sql"],"count":1` exit 0; lần hai `"applied":[],"count":0` exit 0 |
| `docs/adr/0046-thu-tu-khoa.md` đủ 13 nhóm theo thứ tự spec; mọi bảng M0 Task 4–20 có chỗ | đối chiếu bằng tay | `grep -oE "(platform|identity)\.[a-z_]+"` trên kế hoạch | Bảng M0 trong kế hoạch: platform.{tenants, tenant_routes, idempotency_keys, jobs} (nhóm 1, 13); identity.{users, roles, role_permissions, user_role_grants} (nhóm 2); identity.audit_logs, platform.{domain_events, webhook_deliveries, alert_events, backup_runs} (mục a); platform.{schedules, metrics_minute, process_heartbeats, error_events, migration_checksums} (mục b); jobs/idempotency_keys (mục c). Thêm `platform.goose_db_version` (Task 4). Các tên khác grep ra (`platform.health`, `platform.digest`, `platform.cleanup`, `platform.alerts`, `platform.wal_bytes`, `platform.resolve_in_doubt`, `platform.put_tenant_route`, `platform.enable_tenant_rls`) là quyền, loại việc hay hàm, không phải bảng |
| Cổng máy | — | `make build vet lint sqlc-vet test vuln jscpd test-guard gitleaks` | tất cả QUA; `catalog-check` và `gen-check` so với git nên chạy lại sau khi stage (ghi ở dưới) |

Đoạn trùng (jscpd) lần đầu báo 3 đoạn mới, đã trả lời: (1) `lintPending`/`recordChecksums` cùng đọc trạng thái goose
và lọc theo trạng thái → gộp thành `sourcesIn` (cùng mục đích, cùng lý do đổi); (2) mở kết nối chủ bảng trong ba tệp
test → gộp thành `ownerConn`; (3) hai test lỗi/panic dựng cùng một hàm → `insertThen`. Sau khi gộp: 0 đoạn mới.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `0001_platform.sql`: `CREATE SCHEMA IF NOT EXISTS platform` (quyết định 2). Bảng goose ở `platform.goose_db_version`.
- `PoolOptions.Tracing` thêm (quyết định 5). `DB` có thêm trường không xuất ra ngoài `defaultTx`.
- Câu `set_config` là câu tĩnh qua sqlc dùng mảng (quyết định 4), không phải câu liệt kê từng `set_config`.
- `testkit` dùng container dev của phiên, không testcontainers (quyết định 1); thêm `testkit.CreateDatabase` (database
  chưa migrate, cho test của Migrate và vai trò migrate).
- Thêm `db.ErrMigration` (sentinel cho lỗi từ chối migration) và `TransientCode` trả cả mã không tạm thời (`ok=false`).
- Tệp thêm ngoài danh sách Files: `queries/platform/{tx.sql,migrate.sql}`, `internal/platform/db/{migrate_dynsql.go,
  migrate_dynsql_test.go,export_test.go}`, `tests/arch/{scan_test.go,sqlcvet_test.go}`, `tests/e2e/migrate_test.go`;
  sửa `cmd/crm/main.go` (thay pool tạm và vai trò migrate "chưa có" của Task 3, xóa `openPool`, `errMigrateNotYet`),
  `Makefile` (`gen`, `gen-check`, `sqlc-vet` vào `ci`), `docker/postgres/init/01-roles.sql` (extension, quyết định 3),
  `docs/danh-muc-ghi-chu.yaml` và `docs/danh-muc-dung-chung.md` (gói `db`).
- Thư viện mới vào `go.mod`: goose v3.30.1, otelpgx v0.13.0 (đều ADR-44/Tech Stack, không cần dòng trong
  `docs/phu-thuoc.md`); `google/uuid` từ gián tiếp thành trực tiếp (đã có dòng ở Task 3). `go mod tidy` nâng
  `golang.org/x/{net,sync,sys,text}`, `grpc`, `genproto` theo yêu cầu của goose. Công cụ: cài `sqlc` v1.31.1 đúng
  `tools/versions.mk` (`go install`).

## Việc phát hiện thêm

- Cụm PostgreSQL ở máy chủ (image Task 2, `docker/postgres/init-drill/01-roles.sql`) chưa tạo `pg_stat_statements`,
  và chưa rõ `crm_owner` có quyền CREATE trên database của ứng dụng: lần migrate đầu ở máy chủ có thể lỗi. Cần làm ở task
  dựng máy chủ/diễn tập (Task 21 hoặc trước lần deploy đầu). Không tự sửa vì ngoài phạm vi và là việc ngoài máy.
- `docker/compose.dev.yml` dùng `postgres:18` gốc, không phải image Task 2; test của Task 4 chạy trên image gốc
  (cùng PostgreSQL 18.7, thiếu WAL-G không ảnh hưởng test).
- Volume dev đã tạo trước task này không chạy lại `docker/postgres/init`: phiên khác cần tạo ba extension trong
  database `crm` bằng `postgres` (hoặc `make db-reset`) trước khi chạy `CRM_ROLE=migrate` trên database dev.
- **Hook Stop chặn sai ở bước lint:** `scripts/agent/affected.py` in import path (`elimaz.com.vn/crm/...`) và
  `stop-check.sh` đưa thẳng cho `golangci-lint run`, vốn cần đường dẫn thư mục → "typechecking error: stat
  …/elimaz.com.vn/crm/cmd/crm: directory not found", mã thoát 7, dù "0 issues". Kiểm tay: `golangci-lint run
  elimaz.com.vn/crm/internal/platform/db` → rc 7; `golangci-lint run ./cmd/... ./internal/... ./tests/... ./tools/...`
  → 0 issues, rc 0. Lộ ra ở task này vì `go.mod` đổi (hook lint mọi gói). Hook và `scripts/agent/` chỉ chủ shop sửa;
  không vượt hook. Đề xuất: `affected.py` in đường dẫn `./<thư mục>` cho golangci-lint (go vet, go test nhận cả hai).
- `go.sum` có thêm module chỉ goose dùng cho test của nó (modernc sqlite…); không vào tệp chạy (`govulncheck` QUA).

## Reviewer

**Vòng 1: CẦN SỬA** (agent `reviewer`, trên `git diff --cached`). Mười hai quyết định thay chủ shop: không mục nào trái
spec hay thiếu an toàn. Phát hiện và cách xử lý:

1. *Phải sửa:* `sqlusage` chỉ bắt bảng ngay sau `FROM|JOIN|INTO|UPDATE|TABLE`, sót bảng sau dấu phẩy
   (`FROM a x, other.b y`) và sau `USING` (`DELETE … USING`). → Viết lại phần dò bảng theo token: đọc cả danh sách
   sau `FROM`/`USING` ở độ sâu ngoặc 0, bỏ `FROM` trong lời gọi hàm (EXTRACT, trim, substring), `IS DISTINCT FROM`,
   `JOIN … USING (cột)`. Thêm testdata `commalist.sql`, `using.sql` (phải báo) và ca đúng trong `ok.sql` (không báo).
   Test đỏ trước khi sửa: `TestSQLUsageReportsSample` "không báo tệp mẫu commalist.sql…, using.sql…".
2. *Ghi chú, đã sửa vì ảnh hưởng tính đúng:* `sleep` trả `context.Cause(ctx)` → đổi sang `ctx.Err()` (ADR-46 "trả lỗi
   của ctx"; với `WithCancelCause` thì `errors.Is(err, context.Canceled)` sai). Test đổi sang `WithCancelCause` có lý do
   riêng.
3. *Ghi chú, đã sửa:* lùi transaction trên ctx tách khỏi hủy nhưng không hạn → thêm hạn 5 s (`rollbackTimeout`).
4. *Ghi chú, đã sửa:* `LintContract` xét cả phần `-- +goose Down` (không chạy khi migrate) → dừng ở `-- +goose Down`;
   `ALTER TABLE t DROP c, DROP CONSTRAINT k` lọt vì chỉ xét `DROP` cuối → xét mọi `DROP` trong câu `ALTER TABLE`.
   Hai ca mới trong `TestLintContract` đỏ trước khi sửa.
5. *Ghi chú, giữ nguyên kèm lý do:* checksum ghi bù có thể nhận một tệp bị sửa đúng lúc tiến trình chết giữa goose và
   bước ghi checksum (quyết định 9, chấp nhận); `Exec(ctx, "BEGIN")` trong một tệp `*_dynsql.go` lọt cả hai test kiến
   trúc (ca hiếm, để review bắt); khóa advisory chờ không hạn (vai trò chạy một lần, ctx giới hạn).

Sau khi sửa: `golangci-lint run ./...` 0 issues; jscpd 0 đoạn mới; `go test -race -count=1 ./internal/... ./tests/...
./tools/...` 8 gói `ok`.

**Vòng 2: CẦN SỬA.** Bốn chỗ sửa của vòng 1 đúng. Phát hiện và cách xử lý:

1. *Phải sửa:* bản viết lại của `sqlusage` coi từ sau `FOR UPDATE` là tên bảng (`SKIP LOCKED`, `NOWAIT`, `OF t` bị báo
   nhầm), trong khi ADR-40/46 bắt buộc dùng mẫu này. → Nhánh `UPDATE` bỏ qua khi từ trước là `FOR` hoặc `KEY`; thêm
   `… FOR UPDATE SKIP LOCKED` và `… FOR NO KEY UPDATE OF t NOWAIT` vào `ok.sql`. Test đỏ trước khi sửa ("bảng thiếu
   tên schema: SKIP", "… OF").
2. *Ghi chú, đã sửa:* danh sách bảng trộn JOIN với dấu phẩy (`FROM a JOIN b ON …, identity.c`) lọt → `list` đi qua
   các từ của mệnh đề JOIN thay vì dừng; thêm `joincomma.sql` (phải báo; đỏ trước khi sửa).
3. *Ghi chú, đã sửa:* `LintContract` dừng ở `-- +goose Down` bỏ sót phần Up đứng sau Down → bỏ đúng đoạn từ Down tới
   Up kế tiếp; thêm ca "Down đứng trước Up" (đỏ trước khi sửa).

Sau khi sửa: `go test -race -count=1 ./tests/... ./internal/platform/db/` 3 gói `ok`; golangci-lint 0 issues; jscpd 0.

**Vòng 3: ĐẠT.** Ba chỗ sửa của vòng 2 đúng, không làm hỏng phần đã duyệt; không còn phát hiện phải sửa. Ghi chú
không bắt buộc, giữ nguyên: `DELETE FROM a USING b, c` báo trùng một vi phạm cho `c` (không ảnh hưởng tính đúng).

Kết luận: ĐẠT
