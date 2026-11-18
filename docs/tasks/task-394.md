# Task 394 — Câu nhận việc không quét cả bảng jobs; hạn giữ việc xong (M0-22b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `schedules.NewCleanup` đổi chữ ký (thêm `CleanupOptions`), 3 chỗ
  gọi: `cmd/crm/main.go` (runWorker), `internal/platform/schedules/cleanup_test.go` (5 chỗ). Thêm mới (không đổi cái cũ):
  `schedules.CleanupOptions`, `schedules.DefaultJobRetention`, trường `config.Config.JobsDoneRetentionDays`.
  `store.ClaimJobs` giữ nguyên chữ ký (chỉ đổi thân SQL), 1 chỗ gọi `queue.(*Worker).claim`.
- Gói phụ thuộc ngược (go list): `internal/platform/schedules` ← `cmd/crm`; `internal/platform/config` ← `cmd/crm` và các
  gói nhận `config.Config`; `internal/platform/queue` ← events, webhooks, commands, schedules, files, các module, cmd/crm
  (không đổi API của queue).
- Operation OpenAPI bị chạm, oasdiff: không có.
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration: `queries/platform/jobs.sql` `ClaimJobs` (câu con "việc đứng trước" so tenant bằng
  `coalesce(tenant_id, nil-uuid)` thay cho `IS NOT DISTINCT FROM`); `queries/platform/schedules.sql` `DeleteFinishedJobs`:
  `id IN (câu con)` → `id = ANY (ARRAY(câu con))` (test phát hiện planner chọn Hash Semi Join quét cả bảng `jobs` ở bước
  xóa; chữ ký sqlc giữ nguyên, 1 chỗ gọi trong `cleanup.go`). Bảng `platform.jobs`. Migration mới `0014_jobs_claim_index.sql` (dải M0, số kế tiếp chưa dùng), **pha mở rộng**:
  thêm index biểu thức `jobs_order_key` (câu dọn đã có index `jobs_finished` từ 0008, test xác nhận câu dọn dùng nó); index cũ `jobs_order` không còn câu nào
  dùng, xóa ở pha thu hẹp sau (ghi Việc phát hiện thêm).
- Test sẽ chạy: vùng rủi ro cao → cả bộ `./internal/platform/queue/...`, `./internal/platform/events/...`,
  `./internal/platform/webhooks/...`, `./internal/platform/commands/...`; thêm `./internal/platform/schedules/...`,
  `./internal/platform/config/...`, `./cmd/crm/...`, `./internal/platform/migrations/...` (nếu có test migrate);
  `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` mục queue, schedules,
  testkit; `testkit.ExplainAsApp`, `testkit.RequireNoSeqScan`; test `TestClaimPlanUsesIndexes`, `claimSQL` trong queue.
- Dùng lại / viết mới vì: dùng lại `claimSQL` (đọc đúng câu Worker chạy), `newEnv`, hạ tầng `Cleanup`/`DeleteFinishedJobs`,
  `reader.integer` của config. Viết mới một hàm thử nhỏ `explainAnalyze` trong test queue vì `testkit.ExplainAsApp` dùng
  `TIMING OFF` và không trả thời gian chạy, không trả tên bảng của nút Seq Scan — tiêu chí của task cần cả hai (< 50 ms,
  "không Seq Scan trên jobs"). Không đổi testkit (gói dùng chung, ngoài phạm vi).

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Đúng chủ dữ liệu: `platform.jobs` thuộc `platform`, chỉ `internal/platform/queue` và lịch dọn của `schedules` chạm (như cũ).
- 5.2, 5.3: không ghi, không đọc bảng module khác.
- 5.4, 5.5, 5.6: không áp dụng: không đổi đường nghiệp vụ, không gọi ra ngoài.
- 5.7 Ngữ cảnh shop: `jobs` không RLS (Global Constraints, worker nhận việc mọi tenant) — không đổi. Câu nhận việc vẫn
  tách theo (tenant, order_key): tenant NULL chỉ so với tenant NULL, thay `IS NOT DISTINCT FROM` bằng
  `coalesce(tenant_id, '00000000-0000-0000-0000-000000000000')` (uuid nil không bao giờ là id tenant: `uuidv7()` không sinh
  ra nó). Test `TestNullTenantOrdering` giữ đúng hai chiều.
- 5.8, 5.9: không áp dụng: không có HTTP, không đường ghi mới.
- 5.10: không dùng Valkey.
- 5.11: không thêm thư viện.
- 5.12: theo mẫu: SQL tĩnh trong `queries/**` qua sqlc, migration mới (không sửa cũ), cấu hình chỉ đọc trong `config`.
- 6.1 Đồng thời: câu nhận việc vẫn `FOR UPDATE OF j SKIP LOCKED` + index duy nhất `jobs_best_effort_running` là chốt chặn
  cuối; chỉ đổi cách tìm "việc đứng trước", không đổi điều kiện chặn của strict/best_effort. Câu dọn vẫn `FOR UPDATE SKIP
  LOCKED`, tối đa 5.000 dòng mỗi transaction; chỉ xóa done/expired (không ai khác khóa các dòng đó ngoài đường
  `…/retry` với dòng expired, chờ tối đa lock_timeout 5 s).
- 6.2: không áp dụng (không đường yêu cầu mới); chống trùng tầng 1 của `jobs` chỉ còn giữ 7 ngày cho việc đã xong — tầng 2
  (khóa tự nhiên của bảng nghiệp vụ) vẫn chặn xử lý lại (spec 04 mục 5.2); việc định kỳ có dedupe theo kỳ, kỳ ≤ 1 ngày.
- 6.3 Thứ tự khóa: không đổi (ADR-46 nhóm 13, chỉ dòng `platform.jobs`).
- 6.4, 6.5: không đổi (RetryTx, READ COMMITTED).
- 6.6, 6.7, 6.8: không đổi.
- 6.9 Treo lâu: mục đích chính của task — câu nhận việc phải < 50 ms ở 100.000 việc chờ (trước: 2,4 s, quá hạn). Câu dọn
  dùng index `jobs_finished` (0008), không quét bảng trong transaction dọn (test `TestCleanupJobsPlanUsesIndex`).
- 6.10: không áp dụng (không có tiền).
- 6.11 Quy luật: test rapid sẵn có của queue (`property_test.go`) và mọi test thứ tự Task 11, 314 chạy lại nguyên.
- 6.12: không đổi payload, log.
- 6.13: không áp dụng (không có quyền mới).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test thất bại trước (câu cũ) | `TestClaimLongQueueSameOrderKey` (4 ca) | `go test -race -count=1 -run 'TestClaimLongQueueSameOrderKey\|TestNullTenantOrdering' ./internal/platform/queue/` trên mã cũ | FAIL: 5049 ms, 5654 ms, 4964 ms, 4006 ms (trần 50 ms) |
| 100.000 việc chờ, 10.000 cùng order_key: không Seq Scan trên jobs, < 50 ms, ≤ 60.000 trang | `TestClaimLongQueueSameOrderKey` strict/best_effort × việc đầu chờ/running | `go test -race -count=1 -run TestClaimLongQueueSameOrderKey -v ./internal/platform/queue/` | PASS: 15,77 / 15,25 / 19,07 / 17,59 ms; 41.231 / 40.949 / 41.257 / 40.945 trang; Seq Scan [] (máy tải 14–21 trên 10 nhân; một lần đo khác lúc tải 21: 31–69 ms) |
| Việc tenant NULL vẫn đúng thứ tự, không chặn/không bị chặn bởi tenant khác | `TestNullTenantOrdering/strict`, `/best_effort` | như trên | PASS |
| Mọi test thứ tự Task 11, 314 vẫn qua; vùng rủi ro cao chạy cả bộ | toàn bộ queue (gồm `TestOrderingBySeq`, `TestStrictDeadBlocksAndAlerts`, `TestBestEffort*`, `TestClaimPlanUsesIndexes`, property), events, webhooks, commands | `go test -race -count=1 ./internal/platform/queue/... ./internal/platform/events/... ./internal/platform/webhooks/... ./internal/platform/commands/... ./internal/platform/config/... ./cmd/crm/...` | ok queue 90,2 s; events 29,9 s; webhooks 44,5 s; commands 58,6 s; config; cmd/crm |
| Dọn việc done theo hạn mới 7 ngày; dead, in_doubt, pending không bị dọn | `TestCleanupRemovesOnlyOldRows` | `go test -race -count=1 ./internal/platform/schedules/...` | PASS (ok 16,1 s) |
| Hạn giữ cấu hình được | `TestCleanupJobRetentionConfigurable`, `TestLoadJobsDoneRetentionDays` | như trên; `go test ./internal/platform/config/` | PASS |
| Cổng kiểm toàn bộ sau khi gộp main ee109bc (Task 321) | `make ci` | `make ci`; chạy lại riêng phần hỏng | build, vet, lint, gen-check, sqlc-vet, govulncheck ("No vulnerabilities found"), catalog-check, jscpd, gitleaks, console-check: qua; `go test -race ./...`: mọi gói ok (queue 268 s, schedules 67 s) trừ `tests/arch TestPackagePatternSkipsStrayDirs` hỏng vì "parallel golangci-lint is running" (phiên khác), chạy lại riêng: ok. Lần `make ci` trước: `internal/imager TestImager_HangKilledWithinDeadline` hỏng một lần vì máy quá tải (gói không phụ thuộc phần đổi), chạy lại riêng 3/3 ok. `oasdiff` và `test-guard` (so với `main`) báo các operation/test của Task 56 gộp vào main **sau** lần gộp của task này (834370f); task này không đổi `openapi/` và không xóa test nào |
| Dọn theo lô không khóa lâu: ≤ 5.000 dòng/transaction, dùng index, không quét bảng; chu kỳ 5 phút | `TestCleanupBatchLimit`, `TestCleanupJobsPlanUsesIndex`, `TestCleanupEveryFiveMinutes` | như trên | PASS (trước khi sửa `DeleteFinishedJobs`: FAIL, kế hoạch có `Seq Scan on jobs` dưới Hash Semi Join) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch Task 13 ghi lịch `platform.cleanup` 3.600 s; task này đổi hai lịch dọn thành 300 s (xem Quyết định 3).
- Spec 04 mục 7 ghi "`jobs` đã xong giữ 30 ngày"; kế hoạch Task 394 cho đổi (ví dụ 7 ngày, ghi lý do): mặc định 7 ngày,
  đặt được bằng `JOBS_DONE_RETENTION_DAYS` (1–30) của vai trò worker. Spec cần sửa câu này ở lần sửa spec sau.
- Kế hoạch gợi ý "migration index mới nếu cần": thêm 0014 chỉ một index (`jobs_order_key`); câu dọn đã có index
  `jobs_finished` (0008), không cần index mới.
- Tiêu chí "< 50 ms" kèm thêm tiêu chí không phụ thuộc tải máy: câu nhận việc chạm ≤ 60.000 trang ở bộ dữ liệu thử.

## Quyết định thay chủ shop

1. **Cách viết lại điều kiện:** so tenant bằng `coalesce(tenant_id, uuid nil)` + index biểu thức `jobs_order_key`
   (migration 0014). Vì sao: giữ nguyên câu con `NOT EXISTS` của Task 314 (chỉ đổi một dòng so tenant), không lặp lại
   logic strict/best_effort hai lần. Phương án khác: tách hai nhánh tenant NULL / không NULL (không cần migration nhưng
   chép đôi điều kiện thứ tự, dễ lệch); viết LATERAL theo khóa cho PostgreSQL dùng Memoize — đã thử, planner gộp lại
   thành câu con, số trang đọc không đổi (~40.000), bỏ. Đảo ngược: migration mới xóa index, trả câu cũ.
2. **Hạn giữ việc done/expired: 7 ngày, cấu hình được** (`JOBS_DONE_RETENTION_DAYS`, worker, 1–30). Vì sao: đủ tra lại
   việc của tuần trước; chống trùng tầng 2 (khóa tự nhiên của bảng nghiệp vụ) vẫn chặn xử lý lại sau khi dòng `jobs` bị
   xóa; 30 ngày làm bảng `jobs` lớn ~1,2 GiB/ngày ở tải 1×. Việc dead, in_doubt không bao giờ bị dọn (test). Phương án
   khác: 2 ngày như ghi chú thử tải (tra sự cố cuối tuần không kịp); giữ 30 ngày và sửa tiêu chí ngâm (bảng vẫn lớn).
   Đảo ngược: đặt biến môi trường = 30.
3. **Chu kỳ dọn 5 phút thay vì 1 giờ.** Vì sao: mỗi lần dọn tối đa 5.000 dòng (transaction ngắn, không khóa lâu); với
   1 giờ thì dọn được tối đa 5.000 việc/giờ, ít hơn ~36.000 việc/giờ sinh ra ở tải 1× của thử tải → bảng vẫn lớn dù hạn
   ngắn. 5 phút → tới 60.000 việc/giờ, vẫn mỗi transaction ≤ 5.000 dòng qua index `jobs_finished`. Phương án khác: lặp
   nhiều lô trong một transaction (transaction dài, giữ khóa lâu hơn); lịch dọn riêng cho `jobs` (thêm một lịch và
   handler, nhiều mã hơn mà lợi ích như nhau). Đảo ngược: đổi hằng `cleanupEvery`.
4. **Index cũ `jobs_order` giữ lại** (pha mở rộng); xóa ở pha thu hẹp sau khi bản này chạy (xem Việc phát hiện thêm).

## Việc phát hiện thêm

- Gộp main: main là hậu duệ trực tiếp của HEAD nên gộp = tiến thẳng (fast-forward) lên ee109bc; `git merge` từ chối vì
  tệp con trỏ task đang đổi (script quản lý, không được sửa tay), nên dời HEAD bằng `git reset --soft main` + lấy tệp
  của main cho mọi tệp task này không sửa, sinh lại mã và danh mục. Kết quả giống hệt một lần gộp tiến thẳng.
- Từ reviewer: trần thời gian 50 ms của `TestClaimLongQueueSameOrderKey` có thể chập chờn trên máy bận (xét khi nối CI,
  Task 23); `TestCleanupJobsPlanUsesIndex` nên đọc hằng `deleteFinishedJobs` từ mã sinh; index trên bảng lớn nên tạo
  `CONCURRENTLY` ở tệp `NO TRANSACTION`; việc dọn lỗi lặp sinh dead nhanh hơn với chu kỳ 5 phút.
- `internal/imager TestImager_HangKilledWithinDeadline` hỏng thất thường khi máy quá tải (script giả chưa chạy trong 1 s).

- Pha thu hẹp: migration sau xóa index `jobs_order` (0007), không còn câu nào dùng từ Task 394 (`-- contract:` ghi bản
  Task 394). Index thừa làm chậm mọi lần ghi `jobs`.
- Câu nhận việc vẫn tốn ~4 trang cho mỗi việc bị chặn đứng trước trong hàng chờ (10.000 việc cùng khóa → ~40.000 trang,
  15–65 ms tùy máy bận). Hàng chờ 100.000+ việc cùng một khóa sẽ lại chậm tuyến tính; nếu thử tải trên máy thử (Task 23
  bước 7.9) cho thấy cần, xét cột/bảng "đầu hàng theo khóa" — thay đổi thiết kế, cần chủ shop.
- Spec 04 mục 7 còn ghi `jobs` đã xong giữ 30 ngày: sửa khi sửa spec.
- `webhook_deliveries` giữ 30 ngày cũng tăng theo tải như `jobs` (thử tải chưa đo riêng); xét hạn giữ ở task khác.

## Reviewer

Kết luận: ĐẠT (agent reviewer, lần 1, trên `git diff --cached` sau khi tiến lên main ee109bc). Không phát hiện chặn.

- Đã kiểm: `coalesce(tenant_id, uuid nil)` giữ đúng nghĩa `IS NOT DISTINCT FROM` (khóa ngoại tới tenants, `Enqueue` và
  `WithTenant` từ chối uuid nil); nhánh strict/best_effort của Task 314 không đổi; biểu thức index `jobs_order_key` khớp
  câu; `id = ANY(ARRAY(... SKIP LOCKED))` cùng nghĩa, ≤ 5.000 dòng mỗi transaction; chu kỳ mới ghi đè `every_seconds` ở
  database đang chạy; dead/in_doubt không bị dọn; migration chỉ thêm index, 0014 không trùng.
- Ghi chú không chặn (chuyển sang Việc phát hiện thêm, không sửa trong task): (1) trần 50 ms có thể chập chờn trên máy
  bận — trần số trang là tiêu chí không phụ thuộc máy; khi nối CI (Task 23) cân nhắc chỉ ghi thời gian; (2)
  `TestCleanupJobsPlanUsesIndex` chép tay câu SQL, nên đọc hằng sinh như `claimSQL`; (3) `CREATE INDEX` không
  `CONCURRENTLY` — chưa có dữ liệu thật nên được, bảng lớn thì dùng tệp `NO TRANSACTION`; (4) việc dọn `MaxAttempts: 1`
  với chu kỳ 5 phút: lỗi lặp sinh việc dead nhanh gấp 12 lần, theo dõi khi thử tải Task 23.
