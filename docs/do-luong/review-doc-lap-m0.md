# Review độc lập M0 (Task 23 bước 6) — nhánh task-23, 18/11/2026

Phạm vi: đối chiếu mã và test của nhánh với spec 2.1 (07-adr.md: ADR-14→20, 25→27, 30, 35, 37, 38, 41, 42, 44, 46, 47)
và review 12/11 (A1–A9, C1–C6). Không đọc `docs/tasks/*`. Không chạy Docker hay bộ test; tên test dưới đây đã kiểm bằng
grep (`func TestX` có thật), kết quả chạy lấy từ `docs/do-luong/thu-tai-m0.md` khi có trích.

**Tổng: 34 mục — có mã, có test: 29 · có mã, thiếu test: 1 · thiếu mã: 2 · sai so với spec: 1 · thuộc mốc sau: 1.**

Ghi chú chung: nền tảng (tenancy/RLS, hàng đợi, lệnh ra ngoài, ingest, realtime, quyền, chống trùng, RetryTx) được làm kỹ
và test trên PostgreSQL thật, có test sinh tình huống (rapid) ở tenancy, queue, commands, idempotency. Các mục chưa đạt tập
trung ở: hiệu năng câu nhận việc, cấu hình chạy thật (log), và các sản phẩm của chính Task 23 (CI, RUNBOOK, mutation).

## Bảng đối chiếu

| Mục | Yêu cầu (một dòng) | Mã | Test chứng minh | Kết luận |
|---|---|---|---|---|
| ADR-14 | Policy NULLIF; tenant_routes; worker chỉ crm_app; unique/FK ghép tenant_id; tìm theo tên qua SECURITY DEFINER; EXPLAIN bằng role app | migrations/0002_rls_helpers.sql:10-23; 0001_platform.sql:22; internal/platform/tenancy/tenancy.go:35; queries/platform/schemarules.sql; migrations/0123_catalog_search.sql:28-104 | tenancy_test.go `TestReusedConnectionOutsideContextSeesNothing`, `TestChildPointingToOtherTenantParentViolatesForeignKey`, `TestPropertyQueriesOnlySeeOwnTenant`; schemarules_test.go `TestCheckSchemaRulesCleanSchema`; identity/users_test.go `TestSchemaRulesStillClean`; queries/products_dynsql_test.go `TestProducts` (SearchOtherTenant, SearchOutsideTenant); queries/explain_test.go `TestPlansHaveNoSeqScan`; tenancy/search_test.go `TestSearchPlans` | có mã, có test |
| ADR-15 | Một bảng jobs; sự kiện có thứ tự; lệnh ra ngoài in_doubt; việc định kỳ theo tenant; RetryTx trong handler | migrations/0007_jobs.sql, 0012; queries/platform/jobs.sql:63-93; internal/platform/queue/worker.go:397,437; commands/runner.go:68-111; schedules/ticker.go:57 | queue/worker_test.go `TestTwoWorkersHandleEachJobOnce`, `TestLostLeaseRollsBack`, `TestExpiredLeaseInDoubt`; ordering_test.go `TestOrderingBySeq`, `TestStrictDeadBlocksAndAlerts`; shutdown_test.go `TestStopReturnsUnfinishedJob`; commands/property_test.go `TestPropertyNeverSentTwice`; schedules_test.go `TestTickPerTenant`; e2e `TestWorkerCrashDoesNotSendTwice` | có mã, có test (hiệu năng: xem ADR-42, phát hiện 1) |
| ADR-16 | ingest riêng: GET xác minh, kiểm chữ ký, tệp tạm fsync khi DB lỗi, tách gói từng sự kiện, loại lạ bỏ qua và đếm | internal/platform/webhooks/ingest.go (Register, receive, store); spool.go:41,130; split.go:79; deliveries.go Save | ingest_test.go `TestChallengeGET`, `TestDatabaseDownSpoolsAndHealthCounts`, `TestIngestServesOnlyHooksAndHealth`; spool_test.go `TestSpoolWriteThenDrainKeepsOrderAndReceivedAt`; split_test.go `TestSplitOneFailureDoesNotBlockPacket`; rules_test.go `TestRuleSpool`; e2e `TestDatabaseOutageKeepsWebhooks` | có mã, có test |
| ADR-17 | SSE chỉ báo hiệu + resync; hub nối lại LISTEN và phát resync; client watchdog 60 s, băng mất kết nối; bỏ realtime_events | internal/platform/realtime/pghub.go:129-200; sse.go:48 (ping 20 s, gộp 300 ms); console/src/realtime/client.ts:41; console/src/shell/Banner.tsx | realtime/reconnect_test.go `TestReconnectResyncsClients`; silent_test.go `TestSilentDeadConnectionDetected`; sse_test.go `TestStreamTenantIsolation`, `TestStreamBatchDedupe`; console client.test.ts (watchdog, 502, 302) | có mã, có test |
| ADR-18 | Quyền là cặp (quyền, phạm vi), không có dòng = không quyền; guard toàn cục, danh sách trắng; JWT chỉ từ header; ghi phải JSON + Origin | migrations/0003_identity.sql:45-58; identity/permissions.go:121; httpapi/server.go:91-112, guard.go, origin.go, auth.go | identity/permissions_test.go `TestCanTable`, `TestCanProperty`; tests/arch/routes_test.go `TestRoutesPublicSetAndPermissions`, `TestRoutesAllServed`; middleware_test.go `TestOriginRejectsWrites`; auth_test.go `TestAuthTokens` (chỉ cookie → 401), `TestWarehouseScopedRole` | có mã, có test (khóa dịch vụ website: M5) |
| ADR-19 | Kiểm từ ngoài; cảnh báo archiver, pg_wal > 2 GB, sao lưu > 26 h, tệp tạm, LISTEN rớt; WAL-G mã hóa; ghim digest; metrics_minute, bỏ /metrics; cảnh báo không PII | scripts/external-probe/probe.go:139; observability/pgwatch.go:41-90; rules.go:84-311; docker/compose.prod.yml (WALG_LIBSODIUM_KEY); docker/postgres/Dockerfile:7; metrics/flusher.go:72 | rules_test.go `TestRuleBackupAge`, `TestRuleListenDown`, `TestRuleSpool`; rules_eval_test.go `TestPGRuleThresholds`; probe_test.go; deploy `TestProdCompose_PostgresVolume`, `TestProdCompose_ImagesPinnedNotBuilt`; scripts/backup/recreate-check.sh; queue `TestHandlerErrorPersonalDataNotLeaked` | có mã, có test (dịch vụ ngoài thật: bước 7) |
| ADR-20 | Không partition; dọn theo lô; báo cáo trên view; module công bố view | migrations (không PARTITION); schedules/cleanup.go:41-64; views `*_v` ở 0113–0125; internal/queries | cleanup_test.go `TestCleanupBatchLimit`; tests/arch/viewsdoc_test.go `TestViewsDocMatchesDatabase`; sqlusage_test.go `TestSQLUsageViewRules` | có mã, có test |
| ADR-25 | Danh sách vùng cao trong repo; property test; mutation testing trên đúng thư mục vùng cao trước khi đóng mốc; review độc lập | Makefile:162-171 (MUTATION_DIRS); .gremlins.yaml (chưa commit) | property test có (rapid); **không có kết quả mutation** (`docs/mutation.md` chưa có) | có mã, thiếu test → phát hiện 3 |
| ADR-26 | Expand/contract có ghi chú; build một lần ở CI; lùi = deploy lại image (RUNBOOK); timeout ở mọi kết nối; cảnh báo tx 5 phút, dead tuples, đĩa; quét secret/lỗ hổng, không root | db/contract.go:11-42; db/db.go:52-79; db/tx.go (TxAPI/TxIngest/TxWorker); pgwatch.go:59-81; Makefile `gitleaks`, `vuln`; Dockerfile:11 (distroless nonroot) | migrate_test.go `TestMigrateRejectsContractWithoutMarker`; db_test.go `TestOpenSetsConnectionLimits`; retry_test.go `TestRetryTxLimitsDoNotLeakToConnection`; scripts/ops/image-check.sh | **thiếu mã** (mục 2, 3: không có workflow CI, RUNBOOK chưa có thủ tục lùi) → phát hiện 4 |
| ADR-27 | Mỗi module một schema, không bảng trong public; test quét SQL; view security_invoker; test view không lộ tenant khác | schemarules.sql luật (e), (f); migrations 0112–0125 | tests/arch/sqlusage_test.go `TestSQLUsageProductCode`; schemarules_test.go `TestCheckSchemaRulesViewsAndSeller`; identity/staff_test.go `TestUsersView`; files/variants_view_test.go `TestFileVariantsView` | có mã, có test |
| ADR-30 | 1 chặn dồn dập; 2 log tệp theo ngày trên volume riêng 30 ngày/200 MB; 3 error_events; 4 API sức khỏe; 5 xuất cấu hình + sổ dựng lại máy; 7 hai mức cảnh báo, nhắc 30 phút, tổng kết sáng | webhooks/lockout.go:13; httpapi/ratelimit.go:15; logging/dailyfile.go:16-17; observability/errors.go, summary.go, alerter.go, digest.go; scripts/ops/export-config.sh | lockout_test.go `TestLockoutOverHTTP`; ratelimit_test.go; dailyfile_test.go; errors_test.go; alerter_test.go `TestUrgentRepeatsEvery30Minutes`; digest_test.go | **thiếu mã** (mục 2 không nối ở compose máy chủ; mục 5 thiếu REBUILD.md) → phát hiện 2, 4 |
| ADR-35 | Kiểm chữ ký thẻ Access và dấu bước hai; khóa tài khoản có hiệu lực ngay; đường đăng nhập riêng (trước ngày mở) | adapters/cfaccess/cfaccess.go:99-125; identity/users.go:36-60 (ErrLocked) | cfaccess `TestVerify`, `TestKeyRotation`; identity `TestCheckMFA`, `TestCheckMFAProperty`, `TestLoadUserErrors` | có mã, có test (đường riêng: M7 theo kế hoạch) |
| ADR-37 | WAL-G libsodium; archive_timeout ≤ 5 phút; diễn tập khôi phục bấm giờ; kiểm tuần ghi backup_runs; ra thẳng kho ngoài | docker/postgres/conf.d/crm.conf:5 (60 s); scripts/backup/*.sh; migrations/0010_ops.sql backup_runs | scripts/backup/verify-drill.sh, restore-drill.sh (`make verify-drill`); rules_test.go `TestRuleBackupVerify` | có mã, có test (chạy trên kho ngoài thật: bước 7.6) |
| ADR-38 | OTel, trace_context qua jobs, Jaeger, log có trace id, giới hạn log Docker, nhịp sống ra ngoài | telemetry/telemetry.go; queue/trace.go; metrics/pinger.go:27; compose Jaeger | queue/trace_test.go `TestTraceFlowsThroughQueue`; e2e `TestPipelineUnderTwoSecondsOneTrace`; telemetry_test.go; pinger_test.go | có mã, có test (riêng giới hạn log Docker: phát hiện 2) |
| ADR-41 | Idempotency-Key cho mọi ghi; ghi nhận commit riêng; xong + kết quả cùng tx nghiệp vụ; 409/422/400; nhận lại sau 60 s; "thử lại" không lưu | platform/idempotency/idempotency.go:80-175; queries/platform/idempotency.sql; httpapi/idempotency.go:80,205 | idempotency_test.go (httpapi) `TestIdempotencyFiftyParallelSameKey`, `TestIdempotencySameKeyDifferentBody422`, `TestIdempotencyDeadProcessReclaimedAfterLock`, `TestIdempotencyRetryAnswerNotStored`; routes_test.go `TestRoutesIdempotencyModes`; e2e `TestRetryFiftyParallelOneResult` | có mã, có test |
| ADR-42 | Mục tiêu tốc độ đo được; thử tải 1×/10×/ngâm/hỏng giữa tải từ M0; JobQueue, RealtimeBus là interface; ngân sách kết nối | queue/queue.go (JobQueue); realtime/bus.go (Bus); config.go:209-232; loadtest/ | loadtest/run.sh, k6; kết quả docs/do-luong/thu-tai-m0.md: 10× **sụp** (ClaimJobs 2,37 s, Seq Scan trên jobs) | **sai so với spec** → phát hiện 1 |
| ADR-44 | Go; lint/vet/sqlc vet/gen-check; test khởi động; SQL đều có kiểm; module không nhập net/http hay internal của module khác; cấm simple protocol | Makefile `ci`; db/db.go:64 (CacheStatement); tests/arch/* | tests/arch `TestBoundariesRealCode`, `TestSQLModeProductCode`, `TestDynSQLProductCode`, `TestSqlcVet`, `TestLintCatchesPlantedMistakes`; e2e `TestBootEachRoleThenGracefulStop` | có mã, có test (chưa nối vào CI: phát hiện 4) |
| ADR-46 | RetryTx 40001/40P01/55P03, tối đa 3 lần lại, READ COMMITTED; không mở tx ngoài platform/db; bảng thứ tự khóa; 503 retry_later không còn khóa | db/retry.go; db/tx.go:39-100; docs/adr/0046-thu-tu-khoa.md (mọi bảng trong migrations đều có mặt — đã kiểm); httpapi/errors.go:158-160 | retry_test.go `TestRetryTxRealDeadlockBothSucceed`, `TestRetryTxExhaustedReturnsRetryLater`, `TestRetryTxCallsEqualTransientErrorsPlusOne`; tests/arch/txusage_test.go `TestTxUsageProductCode`; handlers_jobs_test.go `TestRetryJobRetryLaterNotStored` | có mã, có test |
| ADR-47 | `x-idempotency: exempt`/`no-replay` khai trên từng operation, routegen kiểm, middleware bỏ qua đúng | tools/routegen/main.go; httpapi/server.go:93 | routegen `TestRejectsBadContracts`, `TestGeneratesIdempotencyModes`; httpapi `TestIdempotencyExempt`, `TestIdempotencyNoReplay` | có mã, có test |
| A1 | NULLIF trong policy | 0002_rls_helpers.sql:18-19 | `TestReusedConnectionOutsideContextSeesNothing` | có mã, có test |
| A2 | Tenant ở mọi đường vào: định tuyến, việc mang tenant, NOTIFY kèm tenant, việc định kỳ theo tenant, worker không quyền owner | tenancy/routes.go:17; split.go:130; realtime/bus.go:105; schedules/cleanup.go (per-tenant); config.go:191 (owner chỉ ở migrate) | `TestSplitRoutesEvents`, `TestHandlerRunsInTenantContext`, `TestTickPerTenant`, `TestStreamTenantIsolation`, db_test.go `TestAppRoleCannotCreateTable` | có mã, có test |
| A3 | UNIQUE (tenant_id, id), FK ghép, unique bắt đầu bằng tenant_id; test đọc catalog | schemarules.sql; mọi migration nghiệp vụ | `TestCheckSchemaRulesEachRule`, `TestSchemaRulesStillClean` | có mã, có test |
| A4 | Tìm không Seq Scan dưới RLS: hàm SECURITY DEFINER, khoảng ~>=~; EXPLAIN bằng role app trong CI | 0123_catalog_search.sql; testkit/explain.go | `TestPlansHaveNoSeqScan`, `TestSearchFunctionSeesOnlyOwnTenant`, `TestRequireNoSeqScanFails` | có mã, có test (đuôi SĐT khách: M2) |
| A5 | Realtime báo hiệu + tải lại, nối lại LISTEN, watchdog | như ADR-17 | như ADR-17 | có mã, có test |
| A6 | Tách sự kiện nội bộ (thứ tự, tự thử lại) khỏi lệnh ra ngoài (in_doubt, tra trước khi gửi lại) | queue/kind.go:67; commands/runner.go; commands/indoubt.go | `TestTimeoutAfterSendInDoubtNeverResent`, `TestResolveStaleLookupIgnored`, `TestPropertyNeverSentTwice` | có mã, có test |
| A7 | Quyền theo cặp, guard toàn cục, CSRF (JSON + Origin), host webhook chỉ /hooks/*, khóa dịch vụ website | như ADR-18; ingest.go Register | như ADR-18; `TestIngestServesOnlyHooksAndHealth` | có mã, có test (khóa dịch vụ: M5) |
| A8 | Ingest riêng, tệp tạm, tách gói, GET hub.challenge, SIGTERM tắt êm, 30 s mỗi handler | như ADR-16; queue/kind.go:67 (Timeout 30 s); cmd/crm/main.go:99,475 | như ADR-16; `TestHandlerTimeout`; e2e `TestBootEachRoleThenGracefulStop` | có mã, có test |
| A9 | Nhịp sống ra ngoài, cảnh báo WAL/sao lưu, mã hóa sao lưu, ghim digest, tạo lại container còn dữ liệu | như ADR-19, 37 | như ADR-19, 37 | có mã, có test |
| C1 | Một lõi hàng đợi, bỏ pg-boss | platform.jobs + queue + schedules | queue, schedules tests | có mã, có test |
| C2 | Không partition | migrations (không PARTITION) | (không cần test riêng; dọn theo lô `TestCleanupRemovesOnlyOldRows`) | có mã, có test |
| C3 | Bỏ realtime_events | không có bảng | `TestReconnectResyncsClients` | có mã, có test |
| C4 | Báo cáo là SQL trên view | — | — | thuộc mốc sau (M6) |
| C5 | Module công bố view chỉ đọc, lớp truy vấn ghép view | views `*_v`; internal/queries; docs/views.md | `TestViewsDocMatchesDatabase`, `TestSQLUsageViewRules` | có mã, có test |
| C6 | Bỏ /metrics; chỉ số theo phút + nhịp sống | metrics/flusher.go; không có route /metrics | flusher_test.go `TestFlushHistogram`, `TestHeartbeat`, `TestFlushPingsAfterWrite` | có mã, có test |

## Phát hiện phải sửa (theo mức nghiêm trọng)

1. **Cao — câu nhận việc quét cả bảng `jobs`, hàng đợi sụp ở tải 10×** (ADR-15, ADR-42).
   `queries/platform/jobs.sql:71`: câu con `NOT EXISTS` dùng `b.tenant_id IS NOT DISTINCT FROM j.tenant_id`, PostgreSQL
   không dùng được index `jobs_order` cho biểu thức này nên quét tuần tự `jobs` cho mỗi ứng viên. Đã đo
   (`docs/do-luong/thu-tai-m0.md`): 2.370 ms mỗi lần nhận khi có 78.000 việc chờ, worker hết hạn nhận liên tục, hàng
   chờ không tự hồi. Vì sao quan trọng: một Page hay hội thoại nóng trong ca live dựng đúng tình huống này; tin vào
   (đã nhận 200) bị xử lý trễ hàng phút tới hàng giờ, báo hiệu tới nhân viên trễ theo. `TestClaimPlanUsesIndexes`
   (besteffort_test.go:364) không bắt được vì dữ liệu nhỏ. Sửa tối thiểu (đúng Task 394 đã lên kế hoạch): tách điều kiện
   thành `(j.tenant_id IS NULL AND b.tenant_id IS NULL OR b.tenant_id = j.tenant_id)` hoặc chuẩn hóa tenant NULL về một
   uuid cố định + index biểu thức; thêm test 100.000 việc / 10.000 cùng `order_key` đòi không `Seq Scan` trên `jobs`.
   Nên làm trước khi đóng M0, vì đây là vùng cao và thử tải M0 hiện không đạt.

2. **Trung bình–cao — log ở máy chủ không có giới hạn dung lượng, chưa có volume log** (ADR-30 mục 2, ADR-38 dòng Log).
   `docker/compose.prod.yml:16-35` (anchor `x-crm-hardening`) và mọi service: không có `logging:` (max-size/max-file),
   không đặt `LOG_DIR`, không có volume log; container `read_only: true` nên `logging.NewDailyFile` không thể bật nếu
   không thêm volume. Mặc định Docker `json-file` không xoay vòng → stdout của ba vai trò (mỗi webhook, mỗi việc) lớn
   dần tới đầy đĩa; đầy đĩa làm PostgreSQL dừng ghi (ADR-26 nêu đúng kịch bản này). Sửa: thêm
   `logging: {driver: local, options: {max-size: "50m", max-file: "5"}}` cho mọi service (cả postgres, cloudflared,
   jaeger, seaweedfs, imager); thêm `LOG_DIR: /var/log/crm` và volume `crm-logs` cho ba vai trò; thêm test
   `TestProdCompose_LogLimits` trong `tests/deploy/compose_test.go`.

3. **Trung bình — danh sách vùng cao cho mutation testing thiếu phần đã có mã, chưa có kết quả** (ADR-25).
   `Makefile:162` `MUTATION_DIRS` (và `HIGH_RISK_DIRS` của hook, test deploy giữ hai nơi khớp) chỉ có tenancy, queue,
   commands, idempotency, identity, guard.go. ADR-25 xếp vùng cao cả "migration", "tồn kho và giữ hàng", "tiền (COD)",
   "trạng thái đơn và vận đơn"; nhánh đã có `internal/platform/db` (migrate, RetryTx), `internal/platform/webhooks`
   (đường không được mất webhook), `internal/modules/{inventory,cash,order,fulfillment}`. `docs/mutation.md` chưa tồn tại
   nên không có bằng chứng ngưỡng 80% đã đạt. Vì sao: mutation là chốt duy nhất cho thấy test bắt được lỗi cố ý ở chỗ
   sai là mất tiền/hàng. Sửa: thêm các thư mục trên vào cả hai danh sách (M1 có thể để lần đóng M1 nhưng
   `platform/db`, `platform/webhooks` thuộc M0), chạy `make mutation`, ghi kết quả và đột biến còn sống vào
   `docs/mutation.md`.

4. **Trung bình — sản phẩm của Task 23 bước 1–5 chưa có** (ADR-26 mục 2–3, ADR-25 mục 4, ADR-30 mục 5, ADR-44 "CI đỏ khi").
   Không có workflow CI (thư mục `.github/` không tồn tại; `make ci` chỉ chạy tay) → các cổng của ADR-44 (lint, sqlc vet,
   gen-check, oasdiff, test-guard, gitleaks) không chặn được push; "build một lần ở CI" (ADR-26.2) chưa có. `docs/RUNBOOK.md`
   chưa có thủ tục lùi bản (ADR-26.3), thứ tự deploy, xử lý từng `key` cảnh báo, lộ secret. Thiếu `docs/REBUILD.md`
   (ADR-30.5; `scripts/ops/export-config.sh` đã có), `docs/threat-model-m0.md`, `docs/adr/0014…0047`. Sửa: hoàn tất các bước
   1–5 của Task 23 trước khi đóng M0; mỗi `docs/adr/*` ghi trường "Kiểm bằng" trỏ đúng tên test ở bảng trên.

## Ghi chú không bắt buộc

- `scripts/external-probe/probe.go:168-173` chỉ xét mã HTTP; `/health` của ingest trả 200 kèm `"status":"degraded"` khi
  tệp tạm quá 5 phút (`webhooks/ingest.go`, hàm `health`). Luật `spool` chạy trong worker và đọc chỉ số qua database, nên
  lúc database chết chỉ còn `/health/deep` của api báo. Đề xuất: probe đọc trường `status`, hoặc ingest trả 503 khi degraded.
- `queries/platform/schemarules.sql`: luật chỉ xét bảng có cột `tenant_id`; một bảng nghiệp vụ mới quên cột này sẽ không
  bị báo (và không có RLS). Đề xuất thêm luật: mọi bảng ngoài schema `platform` phải có `tenant_id`.
- ADR-14.3 chỉ được giữ bằng cấu hình compose: không có kiểm lúc khởi động rằng role của `DATABASE_URL` không phải chủ bảng
  và không `BYPASSRLS`. Đề xuất một câu kiểm trong `cmd/crm` cho ba vai trò, từ chối chạy nếu sai.
- `queries/inventory/suppliers.sql:33` dùng `LIKE '%q%'` trên bảng có RLS (Seq Scan dưới role app, đúng kiểu A4); bảng
  nhỏ nên chưa hại, nhưng không nằm trong `TestPlansHaveNoSeqScan`.
- COMMIT bị hủy vì `lock_timeout` (55P03) do khóa hàng NOTIFY lúc commit (thu-tai-m0.md phát hiện 3): đã được RetryTx thử
  lại, nhưng là dấu hiệu trần của `pg_notify` per-row trên `jobs` (trigger `jobs_notify`); nên đo lại trên máy thử.
- `platform.add_staff_route` (0113) cho tenant đầu tiên giữ một email trên toàn hệ thống; đúng với một tenant, cần xem lại
  khi có tenant thứ hai (đúng điều kiện "Xem lại khi" của ADR-14).
- Console tải lại cả truy vấn đang mở khi có báo hiệu (`console/src/app/queryClient.ts:49-54`) thay vì "tải phần mới theo
  con trỏ lùi 30 s" của ADR-17; đủ ở quy mô M1, cần làm khi có danh sách hội thoại dài (M4).
- `crm_app` được `pg_monitor` (0010_ops.sql) để đọc thống kê; rộng hơn mức cần (đọc mọi setting, mọi phiên). Chấp nhận
  được ở một tenant; ghi vào threat model.
