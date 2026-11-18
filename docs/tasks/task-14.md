# Task 14 — Nhận webhook ở ingest, có tệp tạm

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Không ký hiệu xuất ra ngoài nào **có sẵn** bị đổi định nghĩa. Hai gói mới (0 chỗ dùng trước task này):
    `internal/platform/webhooks` (`InboundEvent`, `Route`, `SplitResult`, `Source`, `SplitKind`, `Entry`, `Save`,
    `Spool` với `Write/List/Drain/Stats`, `Drainer`, `Lockout`, `LockoutOptions`, `NewLockout`, `Ingest`,
    `IngestOptions`, `NewIngest`, `Ingest.Register`, `ClientIP`, `CleanupDeliveries`) và `internal/platform/selftest`
    (`Source`, `NewSource`).
  - `internal/platform/internal/store` (mã sinh): **thêm** câu của `queries/platform/webhooks.sql`; câu cũ không đổi.
    `internal/modules/identity/internal/store/models.go` (mã sinh) có thể thêm struct `PlatformWebhookDelivery` vì hai
    khối sqlc dùng chung `migrations`; identity không dùng nó.
  - `cmd/crm` (`serveHTTP`, nhánh ingest; `serve` chạy thêm `Drainer`): hàm không xuất.
- Gói phụ thuộc ngược (go list): `internal/platform/internal/store` được `db`, `idempotency`, `metrics`, `queue`,
  `tenancy` nhập (chỉ thêm hàm). `webhooks`, `selftest` mới chỉ được `cmd/crm` nhập. `cmd/crm` không gói nào nhập
  (test e2e chạy tệp chạy). `tests/e2e/boot_test.go`: `roleEnv` giữ nguyên (`SPOOL_DIR=os.TempDir()`, an toàn vì
  `Spool.List` chỉ nhận tên đúng mẫu `<epochMs>-<uuid>.json`); test e2e mới `tests/e2e/ingest_test.go` đặt
  `SPOOL_DIR` riêng của test.
- Operation OpenAPI bị chạm, oasdiff: không. `/hooks/*` và `/health` của ingest không thuộc hợp đồng `openapi.yaml`
  (ingest là tiến trình riêng, không có route của api).
- Gói test dùng chung: thêm `testkit.Do` (`internal/testkit/http.go`, gửi một yêu cầu HTTP có hạn 30 s, đọc hết
  thân) để test của `webhooks` và `tests/e2e` không chép cùng một đoạn (jscpd báo trùng).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: **mở rộng**. `migrations/0009_webhook_deliveries.sql` tạo
  `platform.webhook_deliveries` đúng Interfaces của kế hoạch; `crm_app` được `SELECT, INSERT, DELETE` (DELETE cho dọn
  30 ngày). Bảng nền tảng không RLS (spec 04 mục 11 liệt kê `webhook_deliveries`; bảng không có cột `tenant_id` nên
  không cần thêm vào `platformTables` của `schemarules_test.go`). Thứ tự khóa: đã có trong ADR-46 "Bảng M0 ngoài thứ
  tự nhóm" (a) — chỉ chèn `ON CONFLICT DO NOTHING`, rồi `platform.jobs` cuối transaction. Câu mới:
  `queries/platform/webhooks.sql` (`InsertWebhookDelivery`, `DeleteOldWebhookDeliveries`).
  Số 0009 nhảy qua 0008 (Task 13, worktree khác); gộp theo thứ tự 12 → 13 → 14 nên goose không gặp tệp số nhỏ đến sau.
- Test sẽ chạy: gói mới `internal/platform/webhooks/...`, `internal/platform/selftest/...`; gói nhập
  `internal/platform/internal/store` (`db`, `idempotency`, `metrics`, `queue`, `tenancy`); vì chạm `queue` (vùng rủi
  ro cao, chỉ gọi `Enqueue`, không sửa) chạy cả `./internal/platform/queue/...`; `./tests/arch/...` (ranh giới, SQL,
  sqlc vet), `./tests/e2e/...` (main.go đổi), `./cmd/...`. Dự án compose `crm-t14`, cổng 5514. Cuối: `make ci` phần
  chạy được trên máy.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` có `db.RetryTx`,
  `db.TxIngest`, `queue.JobQueue`/`NewQueue`/`Kind`/`DefaultPolicy`, `logging.Redact`, `metrics.Registry`,
  `observability.Health`, `config.Config.SpoolDir`/`SelftestWebhookSecret` (đã có từ Task 3), `testkit.OpenAppDB`.
  Tìm `hooks`, `spool`, `fsync`, `hmac`, `lockout`, `MaxBytesReader`, `cf-connecting-ip`, `semaphore` trong
  `internal/`, `cmd/`: có `httpapi/ratelimit.go` (giới hạn theo người dùng, cửa sổ đếm yêu cầu của api) và
  `MaxBytesReader` trong `httpapi/middleware.go`.
- Dùng lại / viết mới vì:
  - Transaction qua `db.RetryTx` (giới hạn `TxIngest` của pool vai trò ingest); việc tách gói ghi bằng
    `queue.NewQueue().Enqueue` trong cùng transaction; che header bằng `logging.Redact`; chỉ số bằng
    `metrics.Registry`; `/health/live`, `/health/ready` giữ của `observability.Health`.
  - `Lockout` viết mới, không dùng `httpapi` ratelimit: khác mục đích (đếm **tin sai chữ ký** theo địa chỉ rồi chặn
    600 s; tin đúng không giới hạn) và khác lý do thay đổi (ADR-30 lớp cổng nhận tin, so với 300 yêu cầu/phút/nhân
    viên của cổng làm việc). `webhooks` cũng không được nhập `httpapi` (kéo theo identity, ogen).
  - Lỗi HTTP của ingest: không dùng `httpapi.writeError` (không xuất, nằm sau ogen); ingest viết cùng dạng body
    `{"error","request_id"}` ở một chỗ trong `ingest.go` (xem Quyết định thay chủ shop).
  - Không thêm thư viện (HMAC, fsync, base64 bằng thư viện chuẩn).

## Soát thiết kế

Mục 5 (khớp kiến trúc):

1. Chủ dữ liệu: đường vào webhook là của `platform` (04 mục 5.2, 11): bảng `platform.webhook_deliveries`, mã trong
   `internal/platform/webhooks`; nguồn thử `selftest` nằm ở `internal/platform/selftest` theo kế hoạch.
2. Không ghi bảng module khác: chỉ ghi `platform.webhook_deliveries` và (qua `JobQueue`) `platform.jobs`.
3. Đọc dữ liệu module khác: không áp dụng.
4. Việc nguyên tử: một transaction chèn bản nhận và việc `webhook.split` (`Enqueue` trong cùng tx); trùng thân thì
   không có việc thứ hai.
5. Việc không cần nguyên tử: tách gói chạy sau commit ở worker (Task 15), không làm trong request.
6. Gọi ra ngoài: không áp dụng (ingest chỉ nhận; không gọi Meta).
7. Ngữ cảnh shop: bảng nền tảng không có `tenant_id`, không RLS (04 mục 11: phải nhận trước khi biết tenant); việc
   tách gói `TenantID = nil` (việc chưa định tuyến, 04 mục 5.1). Payload việc chỉ mang `deliveryId`.
8. Nghiệp vụ không biết HTTP: `webhooks` là gói platform phục vụ mép HTTP của ingest (như `observability`), không
   phải module nghiệp vụ; `Source.Split` không đụng HTTP. Luật (b) của test ranh giới chỉ áp cho `internal/modules`.
9. Mọi đường ghi có khóa chống trùng và khai quyền trong `openapi.yaml`: không áp dụng cho `/hooks/*` (không phải
   API của nhân viên; xác thực bằng chữ ký của nguồn). Chống trùng bằng `UNIQUE (source, body_sha256)` và
   `DedupeKey = deliveryID` của việc tách gói.
10. PostgreSQL là nguồn sự thật: tệp tạm chỉ là chỗ giữ khi database không ghi được, nạp lại vào database theo thứ tự
    tên rồi mới xóa. `Lockout` trong bộ nhớ chỉ là điều tiết, không quyết định nghiệp vụ.
11. Không thêm công nghệ: không.
12. Theo module mẫu: SQL trong `queries/platform/*.sql` qua sqlc; test gói `_test` trên PostgreSQL thật
    (`testkit.OpenAppDB`); lỗi bọc một lần, log một lần ở mép (handler ingest, Drainer).

Mục 6 (đúng khi chạy đồng thời):

1. Hai yêu cầu cùng thân cùng lúc: `INSERT … ON CONFLICT DO NOTHING RETURNING id`; yêu cầu sau chờ dòng chưa commit
   của yêu cầu trước rồi không chèn, không có `RETURNING` → không `Enqueue`. Test đồng thời ở `ingest_test`.
2. Gửi lại: như 1; tệp tạm nạp lại cũng đi đúng đường đó nên trùng với dòng đã có thì không thêm, rồi xóa tệp.
3. Thứ tự khóa: ADR-46 (a) — `webhook_deliveries` chỉ chèn, `jobs` cuối transaction. Không khóa dòng có sẵn.
4. Lỗi tạm: `RetryTx` chạy lại tối đa 3 lần; vẫn lỗi (hay database chết) thì ghi tệp tạm, trả 200; tệp tạm cũng lỗi
   thì 503 để bên gửi thử lại. Mỗi lần lưu có hạn 5 s, tách khỏi việc hủy của yêu cầu (bên gửi ngắt kết nối không
   làm mất bản đã nhận).
5. Mức cách ly: READ COMMITTED mặc định.
6. Việc nền lỗi: việc `webhook.split` theo `DefaultPolicy` (Task 15 viết handler). Drainer gặp database lỗi thì dừng
   lượt, giữ tệp, thử lại sau 5 s.
7. Bên ngoài báo 429: không áp dụng (ingest không gọi ra ngoài).
8. Tiến trình chết giữa chừng: transaction tự lùi, bên gửi không nhận 200 nên gửi lại. Tệp tạm ghi tên tạm → fsync →
   rename → fsync thư mục, nên không có tệp dở; chết giữa lúc nạp (đã commit, chưa xóa tệp) → lần nạp sau trùng thân,
   không thêm dòng, xóa tệp.
9. Treo lâu: `TxIngest` (khóa 500 ms, câu 2 s) + hạn lưu 5 s; thân tối đa 1 MiB; tối đa 200 yêu cầu POST cùng lúc
   (quá thì 503).
10. Tiền: không áp dụng.
11. Quy luật phải luôn đúng: "một thân một dòng một việc" kiểm bằng test đồng thời; vùng rủi ro cao không bị sửa.
12. Dữ liệu cá nhân: thân webhook (tin nhắn của khách) chỉ vào cột `body` và tệp tạm, không vào log, chỉ số, dấu
    vết; header lưu qua `logging.Redact` (che `authorization`, `cookie`…). Log chỉ ghi nguồn, mã yêu cầu, lỗi.
    Tệp tạm tạo quyền 0600.
13. Quyền: `/hooks/*` xác thực bằng chữ ký HMAC so bằng `hmac.Equal`; bước GET so mã xác minh thời gian hằng; nguồn lạ
    404; địa chỉ gửi sai chữ ký quá ngưỡng bị chặn trước khi đọc thân.

## Quyết định thay chủ shop

1. **Bảng `platform.webhook_deliveries` không RLS, không `tenant_id`.** Chọn theo spec 04 mục 11 (liệt kê bảng này
   trong "Bảng nền tảng" không RLS; ingest lưu trước khi biết tenant). Bù lại: chỉ `internal/platform/webhooks` đụng
   bảng, payload việc chỉ mang `deliveryId`. Phương án khác: thêm `tenant_id` NULL + RLS — trái spec, vô ích vì
   ingest không biết tenant.
2. **Lỗi HTTP của ingest đổi mã ở `ingest.go`, không ở `internal/httpapi/errors.go`.** Ingest không có hợp đồng
   OpenAPI, không qua ogen; nhập `httpapi` sẽ kéo identity và ogen vào ingest. Giữ đúng dạng body
   `{"error","request_id"}` và vẫn chỉ một chỗ đổi cho ingest. Đảo ngược dễ: chuyển hàm `writeError` sang gói khác.
3. **Địa chỉ người gửi** lấy từ `cf-connecting-ip`, thiếu thì địa chỉ kết nối (đúng Global Constraints của kế hoạch).
   Header này giả được nếu máy chủ nhận kết nối không qua Cloudflare — xem Việc phát hiện thêm.
4. **Tệp tạm hỏng** (không giải mã được) được đổi tên thêm đuôi `.corrupt` rồi nạp tiếp các tệp sau, thay vì chặn
   cả hàng nạp mãi mãi; tệp vẫn còn để người vận hành xem; `Drain` trả lỗi `ErrCorruptSpoolFile`, Drainer ghi log.
   Phương án khác: dừng hẳn (an toàn dữ liệu như nhau nhưng một tệp hỏng làm mọi webhook sau kẹt trong tệp tạm).
5. **Lưu vào database có hạn 5 s, tách khỏi việc hủy của yêu cầu** (`context.WithoutCancel`): bên gửi ngắt kết nối
   giữa chừng không làm mất bản đã nhận; quá hạn thì ghi tệp tạm và trả 200.
6. **Bộ chặn quét bỏ địa chỉ hết hạn tối đa một lần mỗi cửa sổ 60 s, khi giữ từ 10.000 địa chỉ.** Quét ở mỗi lần sai
   sẽ thành O(số địa chỉ) cho mỗi tin khi bị gọi dồn từ nhiều địa chỉ (test `TestLockoutForgetsManyAddresses` đã treo
   với bản đầu).
7. **Ingest ghi thẳng database khi database đã trở lại, không đợi nạp xong tệp tạm.** Spec nói "nạp các tệp theo thứ
   tự cũ trước"; Drainer nạp theo tên tăng dần ngay khi database trở lại (mỗi 5 s), còn thứ tự xử lý từng sự kiện do
   `OrderKey`/`OrderSeq = OccurredAt` của Task 15 quyết, không phụ thuộc thứ tự dòng `webhook_deliveries`. Chặn
   đường thường cho tới khi hết tệp sẽ làm mỗi yêu cầu chờ thêm mà không đổi kết quả.
8. **`CleanupDeliveries` viết và test ở task này** (xóa bản quá 30 ngày, tối đa 5.000 dòng, chữ ký khớp hàm dọn của
   `RegisterCleanup`), nhưng **chưa đăng ký** vào lịch `platform.cleanup`: `RegisterCleanup` thuộc Task 13 đang ở
   worktree khác, chưa có trên nhánh này. Việc đăng ký là một dòng khi gộp sau Task 13 (xem Việc phát hiện thêm).
9. **Thêm `testkit.Do`** thay vì để hai hàm test giống nhau ở `webhooks` và `tests/e2e` (jscpd chặn đoạn trùng mới).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Chữ ký đúng → 200, 1 dòng `body` bằng byte gốc, 1 việc `webhook.split` (tenant NULL, payload `deliveryId`); gửi lại → vẫn 1, 1 | `TestPostValidStoresRawBodyAndOneSplitJob` | `go test -race -count=1 -v ./internal/platform/webhooks/` | `--- PASS: TestPostValidStoresRawBodyAndOneSplitJob (0.34s)` |
| 8 yêu cầu cùng thân đồng thời → đều 200, 1 dòng, 1 việc | `TestConcurrentSameBodyOneRowOneJob` | như trên | `--- PASS: TestConcurrentSameBodyOneRowOneJob (0.32s)` |
| Chữ ký sai 401 (0 dòng, `hooks_bad_signature`), nguồn lạ 404, 1 MiB + 1 → 413, đúng 1 MiB → 200 | `TestRejects` | như trên | `--- PASS: TestRejects (0.81s)` |
| Body lỗi dạng `{"error","request_id"}` | `TestErrorBodyShape` | như trên | `--- PASS: TestErrorBodyShape (0.48s)` |
| GET đúng mã → `abc` text/plain; sai → 403; nguồn lạ → 404 | `TestChallengeGET` | như trên | `--- PASS: TestChallengeGET (0.31s)` |
| Cột `headers` không chứa nguyên văn `authorization`, `cookie` | `TestHeadersStoredRedacted` | như trên | `--- PASS: TestHeadersStoredRedacted (0.34s)` |
| `GET /api/v1/stream`, `/api/v1/me` trên ingest → 404 | `TestIngestServesOnlyHooksAndHealth`, e2e `TestIngestServesHooks` | như trên; `go test -race -count=1 -v -run TestIngestServesHooks ./tests/e2e/` | `--- PASS: TestIngestServesOnlyHooksAndHealth (0.32s)`, `--- PASS: TestIngestServesHooks (1.46s)` |
| Database luôn lỗi: POST → 200, đúng 1 tệp; `/health` → `spoolFiles: 1` | `TestDatabaseDownSpoolsAndHealthCounts` | như trên | `--- PASS: TestDatabaseDownSpoolsAndHealthCounts (0.01s)` |
| Tệp tạm quá 300 s → `/health` 200 `degraded` | `TestHealthDegradedWhenSpoolOld` | như trên | `--- PASS: TestHealthDegradedWhenSpoolOld (0.02s)` |
| Database và thư mục tệp tạm đều lỗi → 503 | `TestDatabaseAndSpoolDownReturns503` | như trên | `--- PASS: TestDatabaseAndSpoolDownReturns503 (0.00s)` |
| Ngưỡng 3: 3 lần sai → lần 4 đúng chữ ký 429; địa chỉ khác 200; 100 tin đúng liên tiếp 200; hết 200 ms → 200; `hooks_blocked` | `TestLockoutOverHTTP` | như trên | `--- PASS: TestLockoutOverHTTP (1.15s)` |
| Cửa sổ 60 s, mặc định 20 lần / 600 s, quên địa chỉ cũ | `TestLockoutCountsFailuresInWindow`, `TestLockoutDefaults`, `TestLockoutForgetsManyAddresses`, `TestClientIP` | như trên | 4 × `--- PASS` |
| Giới hạn đồng thời 2, lưu bị giữ chậm: yêu cầu thứ 3 → 503 | `TestInFlightLimit` | như trên | `--- PASS: TestInFlightLimit (0.27s)` |
| Spool: 3 tệp → `Drain` 3 dòng đúng thứ tự `received_at` gốc, thư mục rỗng, tệp quyền 0600 | `TestSpoolWriteThenDrainKeepsOrderAndReceivedAt` | như trên | `--- PASS: TestSpoolWriteThenDrainKeepsOrderAndReceivedAt (0.41s)` |
| `Drain` khi database lỗi → tệp còn, trả 0 | `TestSpoolDrainDatabaseDownKeepsFiles` | như trên | `--- PASS: TestSpoolDrainDatabaseDownKeepsFiles (0.02s)` |
| Tệp trùng thân dòng đã có → không thêm dòng, tệp bị xóa | `TestSpoolDrainDuplicateOfExistingRow` | như trên | `--- PASS: TestSpoolDrainDuplicateOfExistingRow (0.40s)` |
| Thư mục không ghi được → `Write` lỗi | `TestSpoolWriteUnwritableDir` | như trên | `--- PASS: TestSpoolWriteUnwritableDir (0.00s)` |
| Tệp lạ bị bỏ qua, tệp hỏng giữ lại `.corrupt` | `TestSpoolIgnoresForeignAndCorruptFiles` | như trên | `--- PASS: TestSpoolIgnoresForeignAndCorruptFiles (0.27s)` |
| Drainer nạp tệp, đặt gauge `spool_files` | `TestDrainerLoadsSpoolAndReportsGauge` | như trên | `--- PASS: TestDrainerLoadsSpoolAndReportsGauge (0.51s)` |
| Dọn: 31 ngày bị xóa, 29 ngày còn | `TestCleanupDeliveriesOlderThan30Days` | như trên | `--- PASS: TestCleanupDeliveriesOlderThan30Days (0.34s)` |
| `selftest`: chữ ký HMAC, `Challenge`, `Split` (ping, loại lạ, thiếu trường, không phải JSON) | `TestKeyIsSelftest`, `TestVerifySignature`, `TestChallenge`, `TestSplit` | `go test -race -count=1 -v ./internal/platform/selftest/` | 4 × `--- PASS`, `ok … selftest 1.593s` |
| Gói phụ thuộc và vùng rủi ro cao không vỡ | các bộ test của gói | `go test -race -count=1 ./tests/... ./cmd/... ./internal/platform/{queue,db,tenancy,metrics,idempotency}/...` | `ok` cho arch, e2e, queue (41.8s), db, tenancy, metrics, idempotency |
| Cổng kiểm `make ci` phần chạy được trên máy | build, vet, lint, sqlc vet (trong tests/arch), govulncheck, oasdiff, jscpd, test-guard, gitleaks, catalog | `make build vet lint vuln oasdiff jscpd test-guard gitleaks catalog` | `0 issues.`, `No vulnerabilities found.`, `Found 0 clones.`, `no leaks found` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm tệp `internal/platform/webhooks/deliveries.go` (`Entry`, `Save`, `CleanupDeliveries`, dùng chung cho đường
  thường và `Drain`) và `internal/testkit/http.go`; thêm test `drainer`, dọn, `selftest`, e2e ngoài danh sách test
  của kế hoạch.
- Thêm chỉ số ngoài kế hoạch: `hooks_busy`, `hooks_spooled`, `hooks_spool_failed` (đếm, nhãn nguồn).
- `ErrInvalidOptions` (NewIngest kiểm tùy chọn), `ErrCorruptSpoolFile` (Quyết định 4) là sentinel mới.

## Việc phát hiện thêm

- Khi gộp sau Task 13: đăng ký `webhooks.CleanupDeliveries` vào `RegisterCleanup` ở `cmd/crm` (vai trò worker), đúng
  dòng "`webhook_deliveries` quá 30 ngày (từ Task 14)" của Interfaces Task 13. Chưa làm vì nhánh này chưa có Task 13.
- `cf-connecting-ip` chỉ đáng tin khi `crm-ingest` chỉ nhận kết nối từ Cloudflare (spec 04 mục 11). Lúc cài máy chủ
  (Traefik của Coolify) cần kiểm cổng của ingest không mở thẳng ra ngoài; nếu không, kẻ gửi đổi header để né bộ chặn.
- Thư mục `SPOOL_DIR` không được kiểm lúc khởi động: thiếu hoặc không ghi được chỉ lộ ra khi database lỗi (503) và ở
  `/health` (`degraded`). Có thể thêm kiểm lúc khởi động hoặc vào `/health/ready` (Task 19, 20).

## Reviewer

Kết luận: ĐẠT (agent reviewer, lần 1, trên `git diff --cached`).

- Phát hiện phải sửa: không có. Reviewer soát: không mất webhook (lưu có hạn 5 s tách khỏi hủy yêu cầu, lỗi thì tệp
  tạm, cả hai lỗi thì 503; `Drain` xóa tệp sau commit), không trùng khi đồng thời (`ON CONFLICT DO NOTHING
  RETURNING`, `Enqueue` cùng transaction), fsync/rename của tệp tạm, thứ tự nạp, so chữ ký thời gian hằng, chặn trước
  khi đọc thân, không log thân/header, Drainer có chủ chờ, khớp Interfaces kế hoạch, ADR-46 (a), số 0009.
- Ghi chú không bắt buộc, giữ nguyên kèm lý do:
  - Chặn ở lần sai thứ 20 (`>=`) trong khi spec viết "quá 20": theo đúng test của kế hoạch ("ngưỡng 3 → lần 4 bị 429").
  - Bảng địa chỉ có thể vượt 10.000 trong một cửa sổ 60 s: đánh đổi có chủ ý (Quyết định 6), lớp Cloudflare chặn ngoài.
  - `t.Fatalf` từ goroutine phụ trong hai test đồng thời: test vẫn đỏ khi lỗi; không ảnh hưởng tính đúng.
  - Test e2e không đếm `platform.jobs`: tiêu chí này đã có ở test gói.
  - Gauge `spool_files` đặt trước lượt nạp, trễ tới 5 s: chấp nhận được với cảnh báo 5 phút.
