# Task 20 — Gom lỗi ứng dụng và tóm tắt sức khỏe

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi ký hiệu nào đã có. Mới trong
  `observability`: `ScrubMessage`, `Fingerprint`, `RecordError`, `ErrorContext`, `StackFrames`, `PanicFrames`,
  `PanicError`, `ErrorRules`, `Summary`, `HealthSummary` (+ kiểu con). Thêm trường `Deps.Metrics` (httpapi, 4 chỗ
  dựng `Deps`: `cmd/crm/main.go` và test serve/stream — trường mới để trống vẫn chạy) và
  `WorkerOptions.Role` (queue; 1 chỗ dựng ngoài test: `cmd/crm/main.go`). `queue.Worker.runJob` (không xuất)
  thêm bắt panic của handler.
- Gói phụ thuộc ngược (go list): `observability` ← `cmd/crm`, `cmd/imager`, `httpapi` (sắp thêm: `queue`);
  `queue` ← `cmd/crm`, `httpapi`, `commands`, `events`, `schedules`, `webhooks`; `httpapi` ← `cmd/crm`.
  `observability` chỉ nhập `config`, `db`, `internal/store` (+ `metrics` mới) nên `queue → observability` không tạo vòng.
- Operation OpenAPI bị chạm, oasdiff: `getPlatformHealth` đã có trong hợp đồng (chưa có handler, đang 501). Thêm
  `firstSeenAt`, `message` vào `ErrorSummary` (thêm trường trả về: oasdiff không coi là phá vỡ) cho màn Sự kiện lỗi
  của M1 (Task 49 ghi ở "Việc phát hiện thêm"). `make gen` sinh lại `internal/httpapi/gen`, `console/src/api`.
- Chỗ dùng trên màn hình: chưa có màn nào gọi `getPlatformHealth` ở M0; mockup M1 `suc-khoe.html`, `su-kien-loi.html`
  (Task 49, 53) sẽ dùng. Client sinh `console/src/api` đổi theo hợp đồng.
- Câu sqlc, bảng, pha migration: `0011_error_events.sql` (pha mở rộng: bảng nền tảng mới `platform.error_events`,
  không RLS, có trong bảng thứ tự khóa nhóm (b)); `queries/platform/errors.sql` (upsert lỗi, dòng mới trong 60 s, tổng
  `errors` 5 phút từ `metrics_minute`, top 10 lỗi 24 giờ, đếm hàng đợi, nhịp sống, gauge `spool_files`). Đọc bảng
  của Task 19 (`backup_runs`, `alert_events`) khi bảng đó đã vào main.
- Test sẽ chạy: `go test -race ./internal/platform/observability/... ./internal/httpapi/... ./internal/platform/queue/...`
  (queue là vùng rủi ro cao → cả bộ của gói), các gói nhập queue (`commands`, `events`, `schedules`, `webhooks`),
  `./cmd/crm/...`, test kiến trúc `./tests/...`, `routes_test`, `CheckSchemaRules`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `logging.Redact`/`SensitiveKey` (che theo tên khóa,
  không che văn bản tự do — danh mục ghi rõ "khi nào không dùng"); tìm regex số/email trong `internal/**`: không có;
  `queue.ErrorText` (cắt 1000 ký tự, không lọc); `store.OldestPendingAgeSeconds`, `metrics.HeartbeatAge`; `metrics.Registry.Inc`.
- Dùng lại / viết mới vì: dùng lại `store.OldestPendingAgeSeconds` cho tuổi việc chờ, `metrics.Registry` cho
  `errors`, `writeError` cho 500, gói store chung của platform. Viết mới `ScrubMessage` vì chưa có hàm lọc văn bản tự do
  (logging chỉ che theo khóa). Gauge `spool_files` của ingest đọc từ `metrics_minute` (không gọi webhooks, ingest là
  tiến trình khác).

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Chủ dữ liệu: `error_events` là bảng nền tảng (spec 05, Global Constraints), schema `platform`, chỉ
  `internal/platform/observability` nhắc tên.
- 5.2 Không ghi bảng module khác: chỉ ghi `platform.error_events`; đọc `jobs`, `process_heartbeats`, `metrics_minute`
  (cùng tầng platform, gói store chung).
- 5.3 Đọc module khác: không đọc bảng module nghiệp vụ nào.
- 5.4 Nguyên tử: không áp dụng: ghi lỗi một câu lệnh độc lập.
- 5.5 Sau commit: `RecordError` của worker chạy **sau** khi transaction của handler đã lùi, trên ctx ghi riêng; không
  ghi trong transaction nghiệp vụ (lỗi nghiệp vụ không bị lùi theo, và không kéo dài transaction).
- 5.6 Gọi ra ngoài: không áp dụng.
- 5.7 Ngữ cảnh shop: `error_events` không có `tenant_id` (bảng nền tảng không RLS theo Global Constraints); tóm tắt sức
  khỏe là của toàn hệ thống, quyền `platform.health.read` phạm vi `none`.
- 5.8 Nghiệp vụ không biết HTTP: `observability` không nhận `*http.Request`; httpapi truyền mã yêu cầu, khung gọi.
- 5.9 Đường ghi: không có operation ghi mới; `getPlatformHealth` là GET có `x-permission`.
- 5.10 Valkey: không áp dụng.
- 5.11 Thư viện: chỉ thư viện chuẩn (`crypto/sha256`, `regexp`, `runtime`).
- 5.12 Module mẫu: lỗi sentinel không cần (RecordError không trả lỗi); log một lần ở mép; test PostgreSQL thật.
- 6.1 Đồng thời: upsert `ON CONFLICT (fingerprint) DO UPDATE SET count = count + 1` — một câu lệnh, PostgreSQL khóa
  dòng, không mất lần đếm khi nhiều tiến trình ghi cùng lúc (test 20 goroutine).
- 6.2 Gửi lại: không áp dụng (không có đường ghi API).
- 6.3 Thứ tự khóa: một dòng mỗi câu, ngoài mọi transaction khác (nhóm (b) bảng 0046).
- 6.4 Lỗi tạm: `RecordError` dùng `db.RetryTx` qua đường chuẩn; hết lượt thì chỉ log, không trả lỗi.
- 6.5 READ COMMITTED mặc định.
- 6.6 Việc nền lỗi: lịch thử lại giữ nguyên; panic của handler nay thành lỗi thường (thử lại theo lịch), trước đây
  làm sập tiến trình worker.
- 6.7 429: không áp dụng.
- 6.8 Tiến trình chết: không áp dụng (ghi lỗi tốt nhất có thể; database ngừng thì bộ kiểm từ ngoài báo, spec 04).
- 6.9 Treo lâu: ghi lỗi chạy với hạn riêng 2 s.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: `ScrubMessage` có test rapid (mọi chuỗi ≥ 8 chữ số và email biến mất, độ dài ≤ 300 ký tự).
- 6.12 Dữ liệu cá nhân: `message` qua `ScrubMessage`; cảnh báo chỉ có 8 ký tự đầu của dấu vân tay; log khi ghi lỗi
  thất bại không chứa nội dung lỗi gốc; giá trị panic không vào log (chỉ vào `message` đã lọc).
- 6.13 Quyền: `/api/v1/platform/health` kiểm `platform.health.read` bằng guard (mặc định từ chối), test 403.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `ScrubMessage("gọi 0912345678 lỗi, mail a@b.vn")` không còn hai chuỗi đó | `TestScrubMessageRemovesPhoneAndEmail`, `TestScrubMessageProperty` (rapid) | `go test -race -count=1 -run '<tên>' ./internal/platform/observability/` | `--- PASS` cả hai |
| Cùng chỗ → cùng dấu vân tay, `count = 2`; chỗ khác → dòng thứ hai | `TestRecordErrorGroupsBySite`, `TestFingerprintIgnoresLineAndDepthBeyondThree` | như trên | `--- PASS` |
| Nhiều tiến trình ghi cùng lúc không mất lần đếm | `TestRecordErrorConcurrentCountsEveryOccurrence` (20 goroutine → count 20, một isNew) | như trên | `--- PASS` |
| `RecordError` khi database lỗi → không lỗi, một dòng log, không lộ nội dung | `TestRecordErrorDatabaseDownOnlyLogs` | như trên | `--- PASS` |
| `Inc("errors", role)`; `sample_job_id` | `TestRecordErrorJobSampleAndMetrics` | như trên | `--- PASS` |
| Route thử panic `"boom 0912345678"` → 500 không vết lỗi, `message` đã lọc, `sample_request_id` đúng | `TestPanicRecordsScrubbedError` | `go test -race -count=1 -run '<tên>' ./internal/httpapi/` | `--- PASS` |
| Chỉ lỗi 500 được ghi (404, client đóng kết nối: không) | `TestHandlerErrorRecordsOnlyInternal` | như trên | `--- PASS` |
| Handler việc lỗi → `sample_job_id` đúng, role worker; mỗi loại việc một dấu vân tay | `TestHandlerErrorRecordsErrorEvent`, `TestHandlerErrorFingerprintPerKind` | `go test -race -count=1 -run '<tên>' ./internal/platform/queue/` | `--- PASS` |
| Panic ở worker được bắt, ghi như lỗi (việc thử lại, tiến trình không sập, `last_error` đã lọc) | `TestHandlerPanicIsRecoveredAndRecorded` (queue); `TestPanicErrorUsesPanicSiteFrames` (`./internal/platform/observability/`) | như trên / lệnh của observability | `--- PASS` |
| `ErrPermanent` không ghi `error_events`, việc vẫn dead; ghi trạng thái việc không bị ghi lỗi ăn vào hạn | `TestPermanentErrorNotRecorded`, `TestRecordAfterLongHandler` (hạn 50 ms, có sẵn) | `go test -race -count=1 ./internal/platform/queue/...` | `ok` (cả bộ queue) |
| Dòng mới → `new-error:<8 ký tự>`; 2 phút sau không | `TestErrorRulesNewError` | `go test -race -count=1 -run '<tên>' ./internal/platform/observability/` | `--- PASS` |
| 51 lỗi trong 5 phút → `error-spike`; 50, 49 → không; dòng 10 phút trước không tính | `TestErrorRulesSpike` | như trên | `--- PASS` (3 ca con) |
| `/api/v1/platform/health` không quyền 403; có quyền đủ sáu khóa; `errors` tối đa 10 dòng, có `firstSeenAt`, `message` | `TestPlatformHealth` (httpapi); `TestSummary` (`./internal/platform/observability/`) | `go test -race -count=1 -run '<tên>' ./internal/httpapi/` và lệnh của observability | `--- PASS` |
| `routes_test`, `CheckSchemaRules`, kiến trúc, lint toàn mã | `./internal/httpapi/...`, `./tests/arch/...` (`TestLintCleanOnRealCode`, `TestSqlcVet`) | `go test -race -count=1 ./internal/httpapi/... ./tests/arch/...` | `ok` cả hai |
| Gói phụ thuộc ngược không vỡ (sau khi gộp main ba lần: tới `3cfbcfc`, gồm Task 27) | queue (cả bộ, vùng cao), commands, events, schedules, webhooks, cmd | `go test -race -count=1 ./internal/platform/{queue,commands,events,schedules,webhooks}/... ./cmd/...` | `ok` tất cả (commands: một lần đỏ `TestRecordAfterLongExecute` do máy bận, chạy lại `ok`; xem Việc phát hiện thêm 5) |
| Hợp đồng không phá vỡ; mã sinh khớp | `make oasdiff`, `make gen` | như tên | apicheck không báo; console `tsc`, `eslint`, `npm test` 35/35 qua |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Câu đọc cho tóm tắt sức khỏe nằm ở `queries/platform/health.sql` (kế hoạch chỉ nêu `errors.sql`): tách việc đọc
  hàng đợi, nhịp sống, tệp tạm khỏi việc gom lỗi.
- Hai luật mới nằm ở `observability/errorrules.go`, hàm `ErrorRules(ctx, d, now) ([]RuleHit, error)`, chưa ở
  `rules.go`: `rules.go` và `CheckRules`/`Alerter` thuộc Task 19, chưa vào main khi task này xong (xem Quyết định).
- `ErrorSummary` của hợp đồng thêm `message`, `firstSeenAt` (mở rộng, không phá vỡ) cho màn Sự kiện lỗi M1.
- Worker trước đây không bắt panic của handler (panic làm sập tiến trình); kế hoạch nói "panic được bắt ở middleware
  và worker" nên thêm `handleSafely` trong `queue`.

## Quyết định thay chủ shop

1. **Task 19 chưa vào main** (đang làm song song): `backup` trong tóm tắt sức khỏe trả rỗng (ba trường đều tùy chọn
   trong hợp đồng) và `alerts` là mảng rỗng; `ErrorRules` trả `[]RuleHit` để `CheckRules` của Task 19 gọi và gửi qua
   `Alerter`. Vì sao: không tạo trước bảng `backup_runs`, `alert_events` của task khác (sẽ trùng định nghĩa ở 0010), dễ
   nối thêm. Phương án khác: chờ Task 19 (không biết bao lâu); tự tạo bảng bằng `IF NOT EXISTS` (dễ lệch định nghĩa).
   Việc nối còn lại ghi ở "Việc phát hiện thêm" mục 1.
2. **Dấu vân tay của lỗi trả về (không phải panic):** lỗi `error` trong Go không mang vết gọi nơi sinh ra. HTTP: lấy
   vết tại chỗ trả 500, bỏ khung của chính lớp HTTP → khung đầu là hàm ogen sinh cho từng operation (mỗi operation
   một nhóm). Worker: khung là `queue.handler <loại việc>` (mỗi loại việc một nhóm). Kiểu lỗi là kiểu của lỗi trong
   cùng chuỗi bọc. Tên tệp trong dấu vân tay chỉ lấy tên cuối (không thư mục dựng) để bản dựng khác máy không đổi dấu
   vân tay. Phương án khác: chỉ kiểu lỗi (gộp quá nhiều), hay nội dung lỗi (tách quá nhiều vì mã, số trong chuỗi).
3. **Mẫu mã yêu cầu, mã việc** giữ lần gần nhất (log giữ 30 ngày, mẫu mới tra được); tên, nội dung, khung giữ lần đầu.
4. **`errors` trong tóm tắt** xếp theo `count` toàn thời gian của các loại lỗi gặp trong 24 giờ (bảng không giữ số
   theo giờ). Phương án khác: đếm 24 giờ từ `metrics_minute` (không có nhãn theo dấu vân tay).
5. **Panic ở worker** thành lỗi thường (lùi transaction, thử lại theo lịch), `last_error` mang giá trị panic đã lọc.
6. **`ErrPermanent` của handler không ghi `error_events`** (theo ghi chú reviewer lượt 1): handler tự báo là lỗi đã
   lường trước (spec chỉ gom "lỗi không lường trước"); việc thành `dead` và đã có cảnh báo `dead:<kind>` của Task 19,
   ghi thêm thì báo hai lần. Phương án khác: ghi mọi lỗi.

## Việc phát hiện thêm

1. **Nối với Task 19 khi nó vào main** (người gộp làm, hoặc task sau): thêm hai câu đọc `backup_runs` (tuổi `base`
   thành công gần nhất, `restore_drill` gần nhất kèm `ok`) và `alert_events` 24 giờ vào `Summary`; `CheckRules` gọi
   `ErrorRules` và gửi từng `RuleHit` qua `Alerter`. Nếu Task 19 vào main trước khi task này commit, làm ngay trong task.
2. Spec 04 nói gom lỗi "ở cả ba tiến trình"; kế hoạch chỉ nối `api` và `worker`. Vai trò `ingest` (lỗi không lường
   trước trong webhook handler) chưa gọi `RecordError`.
3. Log "lỗi xử lý yêu cầu" sẵn có ở `writeHandlerError` vẫn ghi `err.Error()` chưa lọc (có từ Task 6); có thể chứa SĐT
   nếu mã đưa SĐT vào lỗi. Nên đổi sang `ScrubMessage` hoặc chỉ ghi dấu vân tay.
4. Database dev đã chạy tới 0113/0124 sẽ từ chối 0010, 0011 (goose mặc định không chạy migration số nhỏ đến sau): dựng
   lại database dev (`make db-reset` rồi `make db-up`). Database thử của test dựng mới nên không bị.
5. `commands.TestRecordAfterLongExecute` (hạn ghi 50 ms) chập chờn khi máy bận: đỏ 1/20 lần trên chính main (không có
   thay đổi của task này, worktree tạm), 0/20 trên nhánh này; đỏ một lần khi chạy cùng lúc mọi gói. Nên nới hạn trong test
   hoặc đo theo cách khác (task của `commands`).
6. Spec 04 (F): "lỗi cũng được ghi vào dấu vết của yêu cầu" — chưa làm (kế hoạch Task 20 không đòi); `otelhttp` đã đặt
   trạng thái lỗi cho span theo mã 500 nhưng chưa có dấu vân tay trên span.

## Reviewer

Lượt 1: CẦN SỬA.
- Phải sửa (đã sửa): `worker.go` ghi `error_events` trước câu đổi trạng thái việc, dùng chung hạn `recordTimeout` →
  database chậm thì `RescheduleJob`/`KillJob` có thể hết hạn, việc nằm `running` tới hết giữ chỗ; phá bất biến "hạn ghi
  tính từ lúc bắt đầu ghi" (Task 11). Sửa: tách `recordFailure` (ghi trạng thái với hạn riêng) rồi mới `recordError`
  (RecordError có hạn 2 s riêng).
- Ghi chú đã làm: bảng bằng chứng ghi sai gói hai dòng (sửa); cửa sổ `error-spike` lấy gần 6 phút (bỏ `Truncate`, nay
  các phút bắt đầu từ `now − 5 phút`); `ErrPermanent` vẫn bị ghi là lỗi ứng dụng (bỏ, Quyết định 6, test
  `TestPermanentErrorNotRecorded`); mục (F) của spec (lỗi vào dấu vết) ghi ở Việc phát hiện thêm 6.
- Ghi chú giữ nguyên: `ScrubMessage` chỉ bắt chuỗi số liền (đúng Interfaces của kế hoạch, comment đã nói); lỗi 500 ở
  HTTP chờ ghi `error_events` đồng bộ tối đa 2 s khi database treo (có giới hạn, ngoài transaction nghiệp vụ).

Lượt 2 (trên bản đã gộp main tới `75f8e58`): ĐẠT, không còn phát hiện phải sửa. Xác nhận: ghi trạng thái việc trước,
ghi lỗi sau với hạn riêng; Stop quá grace và mất giữ chỗ không ghi lỗi; `ErrPermanent` không ghi; cửa sổ `error-spike`
đúng 5 phút. Ghi chú không bắt buộc: log `writeHandlerError` còn `err.Error()` chưa lọc (Việc phát hiện thêm 3); hardStop
xảy ra đúng giữa lúc ghi trạng thái và lúc ghi lỗi thì lỗi đó không vào `error_events` (đang tắt, chấp nhận).

Lượt 3: main có thêm Task 27 sau lượt 2 → gộp lại (xung đột `Deps`: giữ cả `Events` và `Metrics`), chạy lại test, review
lại bản đã gộp.

Lượt 3 (bản đã gộp tới `3cfbcfc`): ĐẠT, không phát hiện phải sửa. Xác nhận `Deps` giữ đủ Files, Events, Metrics;
`cmd/crm` vẫn nối Events; lỗi catalog đã đổi mã không bị ghi thành 500; ba models sqlc khớp migration 0011.

Kết luận: ĐẠT
