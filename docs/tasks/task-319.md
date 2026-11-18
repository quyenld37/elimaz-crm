# Task 319 — Không lọt dữ liệu cá nhân qua nội dung lỗi; ingest ghi lỗi (M0-20b)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (tìm tham chiếu bằng grep toàn repo, gopls không có trong phiên): **thêm** trong
  `logging`: `ScrubText`, `ErrorText`, `Err`. **Đổi thân, giữ chữ ký**: `observability.ScrubMessage` (nay gọi
  `logging.ScrubText` rồi cắt 300; 3 chỗ dùng: `PanicError.Error`, `RecordError`, test), `queue.ErrorText` (thêm
  lọc; 6 chỗ dùng ở `queue/worker.go`, `commands/runner.go`), `logging.Redact` (lỗi lồng trong giá trị nay ra chuỗi
  đã lọc; chỗ dùng: handler log, `identity.AuditPayload`, `webhooks` header). Handler log (`redactHandler`, không
  xuất) lọc thêm giá trị chuỗi dưới khóa tên `error`/`err`. Thêm trường `webhooks.IngestOptions.Role`
  (để trống = `ingest`; chỗ dựng: `cmd/crm/main.go`, test webhooks). Thêm `observability.ScrubPanics`;
  `observability.NewPanicError` nhận lại `*PanicError` (chỗ dùng: `httpapi/middleware.go`, `queue/worker.go`, ingest).
- Gói phụ thuộc ngược (go list): `logging` ← `cmd/crm`, `identity`, `webhooks` (sắp thêm: `observability`, `queue`,
  `metrics`, `schedules`, `realtime`, `httpapi`, `cmd/imager`); `observability` ← `cmd/crm`, `cmd/imager`,
  `httpapi`, `queue` (sắp thêm: `webhooks`); `queue` ← `cmd/crm`, `httpapi`, `catalog`, `commands`, `events`,
  `files`, `schedules`, `selftest`, `webhooks`; `webhooks` ← `cmd/crm`, `selftest`. `logging` chỉ nhập `config` và
  otel trace nên các gói trên nhập `logging` không tạo vòng; `webhooks → observability` không vòng (observability
  không nhập webhooks).
- Operation OpenAPI bị chạm, oasdiff: không (ingest không có hợp đồng; body lỗi 500 mới của ingest theo đúng dạng
  `{"error","request_id"}` sẵn có của ingest).
- Chỗ dùng trên màn hình: không có màn mới; `last_error` của việc và `message` của Sự kiện lỗi (M1) sẽ hiện nội dung
  đã lọc.
- Câu sqlc, bảng, pha migration: không đổi (ghi `platform.error_events` qua `observability.RecordError` sẵn có).
- Test sẽ chạy: `./internal/platform/logging/...`, `./internal/platform/observability/...`, `./internal/platform/queue/...`
  (vùng rủi ro cao: cả bộ), `./internal/platform/webhooks/...`, `./internal/httpapi/...`, các gói nhập queue và logging
  (`commands`, `events`, `schedules`, `files`, `selftest`, `metrics`, `realtime`, `identity`, `catalog`), `./cmd/...`,
  `./tests/arch/...` (test kiến trúc mới cấm `err.Error()` trong lời gọi slog).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `logging.Redact`, `logging.SensitiveKey`, kiểu tự che
  `logging.Phone/Address/Secret` (che theo khóa và theo kiểu, không che văn bản tự do); `observability.ScrubMessage`
  (Task 20: chỉ SĐT 8 chữ số liền và email, nằm ngoài `logging` — thành bộ che thứ hai); `queue.ErrorText` (cắt 1000,
  không lọc). Tìm regex số/email/token trong `internal/**`: chỉ có ở `observability/errors.go`.
- Dùng lại / viết mới vì: bộ che văn bản tự do chuyển vào `logging` (cạnh `SensitiveKey`, dùng lại đúng danh sách khóa
  nhạy cảm cho dạng `khóa=giá trị` trong chuỗi), và mọi nơi khác (`ScrubMessage`, `queue.ErrorText`, handler log,
  `Redact`) gọi nó: một bộ che duy nhất. Regex của `observability` bị xóa. Ghi lỗi của ingest dùng lại
  `observability.RecordError` (cùng `Fingerprint` như api, worker), không viết đường ghi riêng.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Chủ dữ liệu: không bảng mới; `error_events` vẫn chỉ `observability` ghi.
- 5.2, 5.3: không ghi, không đọc bảng module khác.
- 5.4 Nguyên tử: không áp dụng: không có thay đổi nghiệp vụ.
- 5.5 Sau commit: ingest ghi lỗi **ngoài** transaction lưu webhook (RecordError có transaction và hạn 2 s riêng).
- 5.6 Gọi ra ngoài: không áp dụng.
- 5.7 Ngữ cảnh shop: không áp dụng: `error_events` là bảng nền tảng không RLS.
- 5.8 Nghiệp vụ không biết HTTP: không chạm module nghiệp vụ.
- 5.9 Đường ghi: không có operation mới.
- 5.10 Valkey: không áp dụng.
- 5.11 Thư viện: chỉ thư viện chuẩn (`regexp`) và pgx sẵn có (`pgconn.PgError` để phân biệt database trả lỗi với
  database ngừng).
- 5.12 Module mẫu: log một lần ở mép; lỗi không đổi kiểu; test trên PostgreSQL thật, rapid cho quy luật che.
- 6.1 Đồng thời: bộ che là hàm thuần, không trạng thái; regex biên dịch một lần ở cấp gói (an toàn đồng thời).
- 6.2 Gửi lại: không áp dụng.
- 6.3 Thứ tự khóa: không đổi (upsert một dòng của Task 20).
- 6.4 Lỗi tạm: không đổi.
- 6.5 READ COMMITTED mặc định.
- 6.6 Việc nền lỗi: lịch thử lại không đổi; `last_error` chỉ đổi nội dung (đã lọc).
- 6.7 429: không áp dụng.
- 6.8 Tiến trình chết: panic trong handler POST của ingest được bắt (trước đây net/http bắt và in giá trị panic cùng vết
  ra stderr, không lọc); trả 500, webhook chưa lưu thì bên gửi gửi lại.
- 6.9 Treo lâu: ingest không ghi `error_events` khi database ngừng hay quá hạn (chỉ khi database trả `PgError`, hoặc
  tệp tạm lỗi), để không cộng thêm 2 s chờ cho mỗi webhook đúng lúc database ngừng; regex RE2 chạy thời gian tuyến
  tính, không bị chuỗi dài làm treo.
- 6.10 Tiền: không áp dụng.
- 6.11 Quy luật: rapid: mọi SĐT (liền hoặc có dấu cách, chấm, gạch), email, JWT, chuỗi token dài trộn vào văn bản
  ngẫu nhiên đều biến mất sau `ScrubText`; kết quả là UTF-8 hợp lệ.
- 6.12 Dữ liệu cá nhân: đây là mục chính của task: một bộ che áp cho log (mọi lời gọi slog), `error_events`,
  `last_error`; span không mang nội dung lỗi (test quét span); test kiến trúc cấm `err.Error()` thô trong lời gọi slog.
- 6.13 Quyền: không áp dụng.

## Bằng chứng

Lệnh chung: `go test -race -count=1 -run '<tên>' <gói>`.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Bộ che văn bản: SĐT (liền, có dấu cách/chấm/gạch, +84), email, địa chỉ (số nhà + tên đường; ngõ/phố/phường/quận; nhãn "địa chỉ"), `khóa=giá trị`, JSON, chi tiết PostgreSQL, JWT, Bearer, token dài đều biến mất; phần không nhạy cảm còn | `TestScrubTextRemovesPersonalData` (15 ca con) | lệnh chung, `./internal/platform/logging/` | `--- PASS` |
| Không che nhầm lỗi thường (UUID, SQLSTATE, số 7 chữ số, tên hàm, "đường ghi", lỗi đăng nhập PostgreSQL) | `TestScrubTextKeepsOrdinaryErrors` | như trên | `--- PASS` |
| Quy luật (rapid): SĐT, email, JWT, token trộn vào văn bản ngẫu nhiên không còn; UTF-8 hợp lệ | `TestScrubTextProperty` | như trên | `--- PASS` |
| `RecoverPanics` (lớp recover chung của api, ingest): `onPanic` nhận `*PanicError` vết gốc, `wrote` đúng trước/sau khi trả lời, `ErrAbortHandler` ném tiếp, Flush qua `Unwrap` | `TestRecoverPanics`; test panic sẵn có `TestPanicRecordsScrubbedError` (httpapi) | lệnh chung, `./internal/platform/observability/` và `./internal/httpapi/` | `--- PASS` |
| `ErrorText`, `Err` (khóa `error`) lọc lỗi bọc hai lớp; logger CRM lọc cả `slog.Any(error)`, chuỗi dưới khóa `*_error`, lỗi lồng trong map | `TestErrorTextAndErr`, `TestLoggerScrubsErrorValues` | như trên | `--- PASS` (trước khi sửa handler: FAIL, còn `0912345678`) |
| `error_events.message`, `PanicError.Error` không còn SĐT, địa chỉ, token | `TestRecordErrorScrubsPersonalData` | lệnh chung, `./internal/platform/observability/` | `--- PASS` (trước khi sửa: FAIL) |
| Panic qua `ScrubPanics`: giá trị panic lại đã lọc, vết gọi vẫn ở chỗ panic gốc (dấu vân tay không đổi), `ErrAbortHandler` nguyên vẹn | `TestScrubPanicsKeepsPanicSite` | như trên | `--- PASS` |
| HTTP: lỗi 500 và panic mang dữ liệu cá nhân → câu trả lời, `error_events`, log, span sạch | `TestInternalErrorPersonalDataNotLeaked` | lệnh chung, `./internal/httpapi/` | `--- PASS` (trước khi sửa: FAIL ở log của `writeHandlerError` và ở sự kiện `exception` của span otelhttp) |
| Worker: lỗi tạm và `ErrPermanent` mang dữ liệu cá nhân → `last_error`, `error_events`, log, span sạch | `TestHandlerErrorPersonalDataNotLeaked` | lệnh chung, `./internal/platform/queue/` | `--- PASS` (trước khi sửa: FAIL ở `last_error`) |
| Ingest: database trả `PgError` → dòng `error_events` role `ingest`, gộp dấu vân tay như api, worker (2 lần → count 2), `errors{ingest}` = 2; log, dòng lỗi, span sạch | `TestIngestDatabaseErrorRecordedAndScrubbed` | lệnh chung, `./internal/platform/webhooks/` | `--- PASS` |
| Địa chỉ dưới khóa `address=`/JSON `"address":` có dấu phẩy được che hết (lượt 1 của reviewer) | `TestScrubTextRemovesPersonalData` (3 ca con mới) | lệnh chung, `./internal/platform/logging/` | `--- PASS` (trước khi sửa: FAIL, còn "Thanh Xuân, Hà Nội") |
| Ingest: panic → 500 `internal`, dòng panic role ingest, log/span sạch, phục vụ tiếp; database ngừng (quá hạn ctx, `PgError` 57014, 57P01, 53300, 08006, lỗi lúc nối) → không ghi; tệp tạm lỗi → ghi | `TestIngestPanicRecordedAndScrubbed`, `TestIngestDatabaseDownNotRecorded`, `TestIngestSpoolErrorRecorded` | như trên | `--- PASS` (trước khi sửa: panic làm rớt kết nối `EOF`, không có dòng) |
| Test kiến trúc cấm `x.Error()` và biến lỗi thô trong lời gọi slog | `TestRawErrorLogReportsSample` (4 mẫu), `TestRawErrorLogProductCode` | lệnh chung, `./tests/arch/` | `ok` (trước khi sửa: FAIL, 9 chỗ ở `cmd/crm`, `cmd/imager`, `metrics`, `realtime`, `schedules`) |
| Toàn bộ mã Go không vỡ (sau khi gộp main lần hai tới `6099b06`; lần ba tới `f76f26e` chỉ đổi tài liệu và test-guard) | cả bộ | `CRM_S3_PORT=9119 go test -race -count=1 ./internal/... ./cmd/... ./tests/arch/...` | 25 gói `ok`; 2 gói đỏ ở test nhạy thời gian khi mọi gói chạy cùng lúc (`imager`: `TestImager_HangKilledWithinDeadline`, `TestExecRunner_TimeoutKillsProcessGroup` — gói không bị task chạm; `queue`: `TestStopAfterLongHandlerReleases`), chạy riêng 3/3 `ok`, cả bộ `queue` và `imager` chạy lại `ok`. Lần gộp đầu (`8f437db`): `TestBestEffortConcurrentConversation` đỏ 1 lần tương tự, chạy riêng 3/3 `ok` |
| Lint toàn mã | golangci-lint | `golangci-lint run ./...` | `0 issues.` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ngoài log, `error_events` và span, `jobs.last_error` (qua `queue.ErrorText`, cả `commands`) cũng đi qua bộ che: cùng
  loại nội dung lỗi, hiện trên màn hình việc nền.
- Thêm `observability.ScrubPanics` đặt ngay trong otelhttp (api và ingest): test phát hiện SDK OpenTelemetry ghi giá
  trị panic thô vào sự kiện `exception` của span khi panic đi qua otelhttp. Thứ tự chuỗi của kế hoạch (requestID →
  recover → otelhttp → …) giữ nguyên; chỉ thêm một lớp bên trong otelhttp. `NewPanicError` nhận lại `*PanicError` đã có.
- Ingest trước đây không bắt panic (net/http bắt, in giá trị panic và vết gọi ra stderr); nay có `recoverPanic` riêng
  của ingest, mã lỗi mới `internal` trong bảng mã của ingest.
- Trợ giúp test mới trong `testkit`: `PersonalDataError`, `AssertNoPersonalData`, `SpanText`, `CaptureSpans`, `SyncBuffer`.

## Quyết định thay chủ shop

1. **Nhận ra địa chỉ trong văn bản tự do bằng mẫu** (số nhà + từ hai từ viết hoa; từ chỉ đơn vị như ngõ, phố, phường,
   quận rồi từ viết hoa hay số; nhãn "địa chỉ"; khóa `address=`), vì kế hoạch đòi lỗi bọc địa chỉ không lọt. Vì sao:
   lỗi của Go viết thường theo quy ước nên cụm viết hoa sau số gần như chỉ là tên riêng. Giới hạn: địa chỉ viết thường
   không số nhà và tên người không nhận ra; vẫn giữ luật "mã không đưa dữ liệu khách vào chuỗi lỗi" và kiểu tự che.
   Phương án khác: bỏ hẳn nội dung lỗi khỏi log (chỉ dấu vân tay): an toàn tuyệt đối nhưng mất khả năng tra lỗi qua
   log; đổi được ở một chỗ (`logging.ErrorText`) nếu chủ shop muốn.
2. **Che thừa hơn che thiếu:** ngày dạng `2026-11-18`, số tiền `10.000.000` (từ 8 chữ số có dấu ngăn) cũng bị che; UUID
   giữ nguyên để tra mã việc. Dấu che thống nhất `[Redacted]` (trước đây `error_events` dùng `#`, `@`).
3. **Ingest ghi `error_events` khi database đang chạy mà trả lỗi (`PgError` ngoài nhóm SQLSTATE 08, 53, 57 và không
   phải lỗi lúc nối `*pgconn.ConnectError`), khi tệp tạm lỗi và khi panic;** database ngừng, quá tải hay quá hạn
   (kể cả `statement_timeout` 57014, đang tắt 57P01, quá nhiều kết nối 53300) thì không (ghi sẽ hỏng theo và mỗi webhook phải chờ thêm tới 2 s đúng lúc cần nhanh; spec 04: lỗi "database
   ngừng" do bộ kiểm từ ngoài và nhịp sống báo). Bộ nạp tệp tạm (`Drainer`) chỉ đổi log sang bộ che, chưa ghi
   `error_events` (lỗi của nó gần như luôn là database ngừng). Phương án khác: ghi mọi lỗi lưu.
4. **Logger CRM lọc thêm mọi giá trị chuỗi dưới khóa chứa `err`** (`error`, `db_error`…) để vẫn che khi mã mới lỡ ghi
   `err.Error()`; test kiến trúc chặn việc đó từ trước khi commit.
5. **Gộp phần chung của hai `recoverPanic` vào `observability.RecoverPanics`** (hook Stop chặn đoạn trùng 14 dòng giữa
   httpapi và ingest): bắt panic, ném tiếp `http.ErrAbortHandler`, gói `*PanicError`, nhớ đã gửi header (`headerWriter`
   có `Unwrap` cho Flush của SSE, chuyển từ httpapi sang). Mỗi máy chủ giữ hàm `onPanic` riêng (ghi lỗi, log, dạng body
   lỗi của mình); ingest vẫn không nhập `httpapi`. Đây là thay đổi cấu trúc của `httpapi/middleware.go` nằm chung commit
   hành vi (luật muốn hai commit) vì hook không cho báo xong khi còn đoạn trùng; hành vi của api giữ nguyên (test
   panic sẵn có của Task 20 vẫn qua). Đoạn khung của `tests/arch/errlog_test.go` giống `txusage_test.go` theo mẫu chung
   của test kiến trúc: giữ (hook không báo).
6. **Sửa 3 dòng test của `catalog` hỏng sẵn trên main** (sau khi gộp main tới `6099b06`): `classification_test.go`
   (Task 34) dùng `e.bus.signals`, trong khi `b3efe8c` (task song song) đã đổi `e.bus` sang `testkit.RecordingBus`
   (chỉ có `Signals()`). Gói `catalog` không biên dịch được test, chặn hook và golangci-lint của mọi task nhập `queue`.
   Sửa: đếm `len(e.bus.Signals())` trước bước cần kiểm rồi so phần mới; không đổi điều test kiểm. Vì sao: nhỏ, chỉ
   trong test, dễ đảo; phương án khác (chờ điều phối viên sửa) làm task này không qua được cổng kiểm.

## Việc phát hiện thêm

1. Cổng S3 của phiên (`CRM_S3_PORT=8719`) trùng cổng kho ngoài của `crm-t19`; phiên này chạy SeaweedFS ở 9119. Bảng
   cổng của điều phối viên nên tránh để S3 chính của task này trùng kho ngoài (`87NN`) của task khác.
2. (đã làm trong task, Quyết định 5) phần chung của hai `recoverPanic` nay là `observability.RecoverPanics`.
3. `queue.TestBestEffortConcurrentConversation` đỏ một lần khi máy rất bận (nhiều gói, nhiều phiên song song), chạy riêng
   qua 3/3: nên xem lại hạn thời gian trong test (cùng loại với Task 317).
4. jscpd trên main đã có các đoạn trùng mới ngoài task này (`console/e2e`, `db/migrate_test.go`, test của `queue`) chưa
   vào mốc nền.
5. Main tại `6099b06` không biên dịch test `catalog` (Quyết định 6): người gộp nên chạy `go vet ./...` sau mỗi lần gộp
   hai task chạm cùng gói test.

## Reviewer

Lượt 1: CẦN SỬA.
- Phải sửa (đã sửa): (1) ingest coi mọi `PgError` là "database còn chạy" → vẫn ghi `error_events` khi quá hạn câu lệnh
  (57014), database đang tắt (57P01/57P03), quá nhiều kết nối (53300), lỗi lúc nối bọc `PgError`. Sửa:
  `unexpectedDBError` loại nhóm 08, 53, 57 và `*pgconn.ConnectError`; `TestIngestDatabaseDownNotRecorded` thêm 5 ca.
  (2) giá trị dưới khóa `address=`/`"address":` chỉ bị che tới dấu phẩy đầu, lộ phường/quận/thành phố. Sửa:
  `addressKeyPattern` riêng cho khóa chứa `address`, giá trị đi qua dấu phẩy; 3 ca test mới. Mục Ảnh hưởng ghi nhầm
  `Drainer.Role` (không thêm): đã sửa câu chữ.
- Ghi chú giữ nguyên (không ảnh hưởng tính đúng, lớp thứ hai đã che): test kiến trúc không bắt `logger.With(...).Error`,
  `fmt.Sprint(err)`, biến lỗi tên khác (logger CRM vẫn che giá trị `error`); bộ che không nhận địa chỉ viết hoa toàn bộ,
  chữ dạng tổ hợp NFD, SĐT có hai ký tự ngăn, `Basic <base64 ngắn>`; địa chỉ IPv4 dài bị che như SĐT (che thừa,
  Quyết định 2); ghi lỗi khi tệp tạm lỗi vẫn có thể chờ 2 s (Quyết định 3); log `stack` của panic HTTP nay chỉ tới
  chỗ panic lại trong `ScrubPanics` (vết gốc và dấu vân tay vẫn đúng trong `error_events`).

Lượt 2 (bản gộp tới `6099b06`): ĐẠT, không phát hiện phải sửa. Xác nhận `unexpectedDBError` (nhóm 23 vẫn ghi, mã ngắn
không panic), `addressKeyPattern`, sửa test `catalog` không nới phép kiểm. Ghi chú: ca "lỗi lúc nối" của test không
phủ nhánh `ConnectError` bọc `PgError` (logic đúng, không dựng được lỗi đó trong test vì trường bên trong không xuất);
`addressKeyPattern` cũng che `ip_address=` (che thừa); cập nhật mốc gộp trong bảng bằng chứng (đã sửa).

Sau lượt 2: hook Stop chặn đoạn trùng giữa hai `recoverPanic` → gộp vào `observability.RecoverPanics` (Quyết định 5,
test `TestRecoverPanics`), gộp main tới `f76f26e` → cần review lại.

Lượt 3 (bản gộp tới `f76f26e`): ĐẠT, không phát hiện phải sửa. Xác nhận hành vi api giữ nguyên (ghi lỗi cùng
`*PanicError`, log đủ trường, chỉ trả 500 khi chưa gửi header), Flush của SSE qua `Unwrap`, `debug.Stack()` vẫn chạy
trong lúc xử lý panic, dấu vân tay không đổi, không còn `headerWriter` cũ. Ghi chú: lần gộp tới `f76f26e` chỉ đổi tài
liệu và `test-guard.sh` (không đổi mã); sau đó đã chạy lại test của observability, webhooks, httpapi, tests/arch: `ok`.

Kết luận: ĐẠT
