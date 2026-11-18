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

Lượt 2:

10. **`not_sent` quá hạn → `expired` có ưu tiên** trước hẹn lại và trước `dead`, kể cả ở lần thử cuối và với loại không
    tự gửi lại (theo quyết định của điều phối viên, spec 04 mục 5.3). `not_sent` còn trong hạn nhưng loại không tự gửi
    lại (tạo vận đơn) hoặc hết lượt thì vẫn `dead` ("hết lượt thử"). Đảo ngược dễ: một nhánh `case past` trong
    `Runner.notSent`. Phương án khác: chỉ `expired` khi loại tự gửi lại — để lệnh quá hạn thành `dead` lẫn với lệnh bị
    từ chối, trái ý spec.
11. **Việc tra lại là lệnh `command` (chạy qua `Runner`), không phải handler transactional.** `Lookup` gọi bên ngoài,
    handler transactional cấm gọi mạng và sẽ giữ transaction mở suốt lần tra. Chính sách riêng: `OnLeaseExpired =
    retry` (chạy lại an toàn, chỉ đọc), 3 lượt, hạn sống 30 s (một kỳ), `Timeout` 25 s, mỗi lần tối đa 50 lệnh. Tra lỗi
    → `not_sent` lý do cố định `lookup_failed` (không chép chuỗi lỗi bên ngoài vì có thể chứa dữ liệu cá nhân).
    Tên loại `task.platform.resolve_in_doubt` theo cách đặt tên việc định kỳ của Task 13. Phương án khác: thêm một
    chế độ thứ ba vào `queue` (vùng rủi ro cao, ngoài phạm vi) hoặc vòng lặp riêng trong `runWorker` (trái luật của
    `schedules`: không tự viết vòng `time.Ticker`).
12. **`cmd/crm` nối `WorkerOptions.Commands` ngay ở M0** (danh sách lệnh thật rỗng, `newCommands()`), vì việc tra lại
    cần `Runner`. Module của mốc sau chỉ thêm handler vào `newCommands`.
13. **Bổ sung ghi chú "khi nào không dùng" của `webhooks`, `selftest`** trong `docs/danh-muc-ghi-chu.yaml`: lượt 1
    để `CẦN GHI CHÚ` nên `make catalog` (một phần của `make ci`) đỏ sau gộp.

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
| Lượt 2 — gộp: không mất phần nào của hai bên, mọi gói qua | toàn bộ test của các gói chạm tới | `go test -race -count=1 ./internal/platform/... ./internal/httpapi/... ./cmd/... ./tests/...` (trước commit gộp 2561f90) | `ok` cho mọi gói (commands 25.3s, queue 43.9s, schedules 15.2s, webhooks 13.5s, e2e 22.6s, arch 11.1s) |
| Lượt 2 — not_sent khi đã quá hạn (tự gửi lại / không / lần thử cuối) → expired, OnTerminal("expired") 1 lần, không gửi lại | `TestNotSentPastExpiryExpires` | `go test -race -count=1 -v -run 'TestNotSent\|TestCommandExpires\|Resolver' ./internal/platform/commands/` | đỏ trước sửa: `muốn expired … {Status:dead …} terminals […:dead]` (3 ca); sau sửa `--- PASS: TestNotSentPastExpiryExpires (0.58s)`; `TestNotSent` (no-auto → dead), `TestNotSentLastAttemptDead`, `TestCommandExpires` vẫn PASS |
| Lượt 2 — việc tra lại: in_doubt đã gửi → done, chưa gửi → pending, không gửi thêm; kết quả số đếm | `TestInDoubtResolverJob` | như trên | đỏ trước (không biên dịch: `undefined: commands.NewInDoubtResolver`); sau `--- PASS: TestInDoubtResolverJob (0.32s)` |
| Lượt 2 — tra lỗi → việc tra lại pending, `last_error` = `not_sent: lookup_failed` (không chép chuỗi lỗi có SĐT) | `TestInDoubtResolverJobLookupFails` | như trên | `--- PASS: TestInDoubtResolverJobLookupFails (0.22s)` |
| Lượt 2 — vai trò worker: 3 lịch (2 dọn + `platform.resolve_in_doubt`), việc tra lại done, bản webhook 31 ngày bị xóa | e2e `TestWorkerRoleRunsSchedules` | `go test -race -count=1 -run TestWorkerRoleRunsSchedules ./tests/e2e/` | đỏ khi bỏ phần nối trong `main.go`: `worker không chạy lịch dọn: … deadline exceeded`; sau `--- PASS: TestWorkerRoleRunsSchedules (1.37s)` |
| Lượt 2 — gói đổi, gói phụ thuộc, vùng rủi ro cao | cả bộ | `go test -race -count=1 ./internal/platform/commands/... ./internal/platform/queue/... ./internal/platform/schedules/... ./internal/platform/webhooks/... ./cmd/... ./tests/...` | `ok` commands 19.8s, queue 30.0s, schedules 10.3s, webhooks 11.9s, arch 10.8s, e2e 22.2s |
| Lượt 2 — cổng kiểm | lint, gen-check, vuln, jscpd, gitleaks, oasdiff, catalog, test-guard | `make lint gen-check vuln test-guard gitleaks oasdiff catalog jscpd` (gen-check sau khi stage) | `0 issues.`, gen-check thoát 0, `No vulnerabilities found.`, `Found 0 clones.`, `no leaks found`, catalog thoát 0 |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Thêm tệp `internal/platform/webhooks/deliveries.go` (`Entry`, `Save`, `CleanupDeliveries`, dùng chung cho đường
  thường và `Drain`) và `internal/testkit/http.go`; thêm test `drainer`, dọn, `selftest`, e2e ngoài danh sách test
  của kế hoạch.
- Thêm chỉ số ngoài kế hoạch: `hooks_busy`, `hooks_spooled`, `hooks_spool_failed` (đếm, nhãn nguồn).
- `ErrInvalidOptions` (NewIngest kiểm tùy chọn), `ErrCorruptSpoolFile` (Quyết định 4) là sentinel mới.

## Việc phát hiện thêm

- ~~Khi gộp sau Task 13: đăng ký `webhooks.CleanupDeliveries` vào `RegisterCleanup`~~ — đã làm ở lượt 2.
- `cf-connecting-ip` chỉ đáng tin khi `crm-ingest` chỉ nhận kết nối từ Cloudflare (spec 04 mục 11). Lúc cài máy chủ
  (Traefik của Coolify) cần kiểm cổng của ingest không mở thẳng ra ngoài; nếu không, kẻ gửi đổi header để né bộ chặn.
- Thư mục `SPOOL_DIR` không được kiểm lúc khởi động: thiếu hoặc không ghi được chỉ lộ ra khi database lỗi (503) và ở
  `/health` (`degraded`). Có thể thêm kiểm lúc khởi động hoặc vào `/health/ready` (Task 19, 20).

- (Lượt 2) Kế hoạch M0 bản 5, Interfaces Task 12 còn ghi "`not_sent` … ngược lại `dead`": cần sửa chữ thành
  "quá `expires_at` → `expired`" theo quyết định của điều phối viên (tệp kế hoạch nằm ngoài repo `apps/crm`, chưa sửa).
- (Lượt 2) Việc tra lại chết giữa chừng 3 lần (giữ chỗ hết hạn) thành `dead` không tenant; việc dọn chỉ xóa
  `done`/`expired` nên dòng đó ở lại (hiếm; chỉ số `jobs_dead` báo). Lỗi tra chỉ thấy qua `last_error` mã cố định,
  chưa có chỉ số riêng (Task 19 cảnh báo).
- (Lượt 2) Chưa có chỉ số `jobs_expired` cho lệnh hết hạn (cả quét pending lẫn ca mới); Task 19 nên thêm nếu cần báo.

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

## Lượt 2 — gộp main (Task 12, 13) và nối việc còn treo

Việc giao: (1) `git merge main` (không rebase), giải xung đột `cmd/crm/main.go`; (2) nối lịch
`platform.resolve_in_doubt` của Task 13 vào `commands.ResolveInDoubt` của Task 12, đăng ký
`webhooks.CleanupDeliveries` vào lịch dọn, xử lý mọi chỗ "chờ Task 12/14" trong `docs/tasks/task-13.md`; (3) quyết
định của điều phối viên: lệnh chưa gửi mà quá hạn theo loại → `expired`, không `dead` (spec 04 mục 5.3:
`pending --> expired: quá hạn của loại lệnh`; `dead` chỉ khi bên ngoài từ chối hoặc hết lượt thử).

### Ảnh hưởng (lượt 2)

- Gộp: xung đột duy nhất ở khối import của `cmd/crm/main.go` (HEAD thêm `selftest`, main thêm `schedules`); giữ cả
  hai. Các tệp khác tự gộp (`docs/danh-muc-dung-chung.md`, hai `store/models.go` sinh mã). `make gen` sau gộp không
  đổi tệp nào. Migration: main có `0008_events_schedules.sql`, nhánh này `0009_webhook_deliveries.sql`: đúng thứ tự,
  không trùng số.
- `internal/platform/commands` (vùng rủi ro cao): `Runner.notSent` đổi hành vi ở ca quá `expires_at` (→ `expired` +
  `OnTerminal("expired")` thay cho `dead`); câu SQL mới trong `queries/platform/commands.sql` (đọc đã quá hạn chưa,
  đổi `running` → `expired` có điều kiện `locked_by`), sinh lại `internal/platform/internal/store/commands.sql.go`.
  Ký hiệu xuất ra mới: `commands.NewInDoubtResolver`, `commands.ResolveInDoubtSchedule` (tên lịch, chu kỳ). Không ký
  hiệu xuất ra nào cũ đổi chữ ký. Gói nhập `commands`: chỉ `cmd/crm` (`go list`). Không đổi operation OpenAPI, màn
  hình, migration.
- `cmd/crm/main.go` (`runWorker`): nối `WorkerOptions.Commands` = `commands.NewRunner` (M0 chưa có lệnh thật, chỉ có
  việc tra lại), định nghĩa lịch `platform.resolve_in_doubt` 30 s, đăng ký `webhooks.CleanupDeliveries` bằng
  `Cleanup.RegisterCleanup`.
- Bảng: `platform.jobs` (cập nhật trạng thái lệnh), `platform.schedules` (thêm một dòng lịch lúc chạy),
  `platform.webhook_deliveries` (xóa quá 30 ngày; `crm_app` đã có `DELETE` từ 0009).
- Test sẽ chạy: `./internal/platform/commands/...`, `./internal/platform/queue/...` (vùng rủi ro cao, cả bộ),
  `./internal/platform/schedules/...`, `./internal/platform/webhooks/...`, `./cmd/...`, `./tests/...` (e2e vai trò
  worker, kiến trúc), cộng hook Stop.

### Tái dùng (lượt 2)

- Đã đọc `docs/danh-muc-dung-chung.md` mục `commands`, `queue`, `schedules`, `webhooks`.
- Dùng lại: `commands.ResolveInDoubt` (không viết lại phần tra), `commands.Runner` + `queue.WorkerOptions.Commands`
  để chạy việc tra lại ngoài transaction, `schedules.Registry.Define`, `Cleanup.RegisterCleanup`,
  `webhooks.CleanupDeliveries`, `queue.ErrorText`, `apply`/`record`/`terminalFn` sẵn có trong `runner.go`.
- Viết mới: một `commands.Handler` mỏng (`inDoubtResolver`) bọc `ResolveInDoubt`, vì handler transactional của queue
  không được gọi mạng (Lookup gọi bên ngoài) nên không dùng `queue.Handler` được. Đặt trong `commands` (không ở
  `schedules`) để `schedules` không phụ thuộc `commands`.

### Soát thiết kế (lượt 2)

1. Tiền: không chạm.
2. Quyền, tenant: lệnh có tenant ghi kết quả trong `WithTenant` của lệnh (giữ `runTx`); việc tra lại không tenant.
3. Hàng đợi: việc tra lại là việc `command` (chạy ngoài transaction) nhưng `OnLeaseExpired = retry` vì chạy lại an
   toàn (chỉ đọc bên ngoài, kết quả tra chỉ áp khi `finished_at` không đổi); hết hạn sống 30 s (một kỳ) để kỳ cũ
   chưa chạy thành `expired` (bị dọn sau 30 ngày), không dồn.
4. Không gửi lần hai: ca mới `expired` chỉ áp khi `Execute` báo `not_sent` (chắc chắn chưa gửi); `expired` không tự
   chạy lại (chỉ người có quyền `retry`). Câu đổi trạng thái có điều kiện `running` + `locked_by` như các câu khác.
5. Khóa (ADR-46): hook `OnTerminal` chạy trước câu đổi `jobs`; câu đọc "đã quá hạn chưa" là `SELECT` không khóa, dùng
   cùng `now()` của transaction với câu hẹn lại, nên hai câu không thể cho kết quả trái nhau.
6. Log, dữ liệu cá nhân: kết quả của việc tra lại chỉ có số đếm; lý do lỗi là mã cố định, không chép chuỗi lỗi của
   bên ngoài.

### Reviewer (lượt 2)

Commit gộp 2561f90 (merge main): **ĐẠT** (agent reviewer, lần 1, trên `git diff --cached`). Không có phát hiện phải
sửa. Reviewer so ba chiều `cmd/crm/main.go` (ingest giữ Drainer + `ingestHandler` của HEAD; worker giữ Cleanup,
Ticker, handlers của main), hai `store/models.go` là hợp của hai bên, migration 0008/0009 liên tục, `git diff --cached
--check` sạch. Ghi chú: hai mục danh mục `CẦN GHI CHÚ` (đã sửa ở commit hành vi, Quyết định 13); worker chưa có
handler `webhook.split` (Task 15).

Commit hành vi lượt 2: **ĐẠT** (agent reviewer, lần 1, trên `git diff --cached`). Không có phát hiện phải sửa.
Reviewer kiểm: `ExpireNotSent` có điều kiện `running` + `locked_by`, 0 dòng thì lùi cả hook; hook `OnTerminal` trước
câu đổi `jobs` (ADR-46); `CommandPastExpiry` và `RescheduleNotSent` cùng `now()` nên bù nhau; `expired` chỉ sau
`not_sent` và không gửi lại; việc tra lại chạy chồng vẫn an toàn (`finished_at` đã thấy), lỗi tra chỉ ghi mã cố định;
`OnLeaseExpired = retry` hợp lệ với mode command. Ghi chú không bắt buộc (đã có ở Việc phát hiện thêm): chưa có chỉ số
`expired`; lỗi gốc của lần tra không lưu; `Timeout` thực tế ≤ Lease/2; chữ của kế hoạch Task 12 cần sửa.
