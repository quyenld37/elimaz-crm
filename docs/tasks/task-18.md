# Task 18 — Đường ống trọn vẹn, một dấu vết liền, ba bài thử hỏng

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**. Mới trong `selftest`: `Inbound`, `NewInbound`,
  `Publisher` (interface phía dùng, `realtime.PGHub` hiện thực), `PongTopic`, `Command`, `NewCommand`, `CommandKind`,
  `External` (interface phía dùng: `Send`, `Has`), `Loopback`. `webhooks.Ingest.Register` giữ chữ ký; bên trong
  `POST /hooks/{source}` được bọc `otelhttp` (span nhận webhook, điểm công khai: không nhận dấu vết của bên gửi) —
  1 chỗ gọi (`cmd/crm/main.go` `ingestHandler`) cộng test của `webhooks`. `main.go`: `newInbound` nhận thêm `Publisher`,
  `newCommands` nhận `config.Config` (2 hàm nội bộ của `main`).
- Gói phụ thuộc ngược (go list): `selftest` ← `cmd/crm`, test của `webhooks`; `webhooks` ← `cmd/crm`, `selftest`.
  Gói mới được `selftest` nhập: `realtime`, `commands`, `queue` (không vòng: các gói đó không nhập `selftest`).
- Operation OpenAPI bị chạm, oasdiff: không (chỉ gọi `POST /api/v1/platform/jobs/{id}/retry` có sẵn trong test).
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không câu mới, không migration (dùng `Notify` của realtime, câu của queue/commands).
- Test sẽ chạy: `go test -race ./internal/platform/selftest/... ./internal/platform/webhooks/... ./cmd/crm/...
  ./tests/e2e/...`; `commands` là vùng rủi ro cao nhưng không sửa (chỉ dùng) nên chạy bộ `commands` một lần để chắc;
  `make ci` trước khi review.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `realtime` (`Bus`, `PGHub.Publish`, `Signal`),
  `commands` (`CommandKind`, `Handler`, `Looker`, `NewRunner`, `ResolveInDoubt`), `webhooks` (`InboundHandler`,
  `InboundJobHandlers`, `NewIngest`, `Drainer`, `Spool`), `queue` (`NewWorker`, `RunOnce`, `NewQueue`),
  `telemetry.Setup` + `WithSyncExporter`, `testkit` (`StartPostgres`, `AccessKeyServer`, `OpenSSE`, `FakeExternal`,
  `Staff`), `identity.SeedOwner`, `tenancy.SeedTenant`/`AddRoute`. Tìm `proxy`, `toggle`, `InMemoryExporter` trong mã.
- Dùng lại / viết mới vì: dùng lại toàn bộ đường thật (ingest, split, inbound, worker, runner, hub, SSE, httpapi) và
  `testkit.FakeExternal` (đúng chế độ `hang_after_send`, `Has` cho `Lookup`). Viết mới: (1) `selftest.Inbound` và
  `selftest.Command` (kế hoạch giao); (2) `selftest.Loopback` — "bên ngoài" tại chỗ cho worker dev, vì binary không
  được nhập `testkit` (gói chỉ cho test); (3) một proxy TCP có công tắc trong `tests/e2e` để làm "database ngừng" thật
  cho riêng `ingest` (không có sẵn; `webhooks.SetSave` chỉ dùng được trong gói `webhooks`); (4) phần dựng ba vai trò
  trong tiến trình test (xem Quyết định thay chủ shop 1) — lặp lại vài dòng nối của `main.go`, giữ vì `main` không nhập
  được và tách gói nối chung là việc ngoài phạm vi.

## Soát thiết kế

Mục 5:
- 5.1 Chủ dữ liệu: không bảng mới; `selftest` là mã nền tảng (`internal/platform`), chỉ đụng `jobs` qua queue/commands
  và NOTIFY qua realtime.
- 5.2, 5.3: không ghi, không đọc bảng của module nghiệp vụ.
- 5.4: `selftest.ping` → `Publish` trong chính transaction của việc `inbound.selftest` (báo hiệu chỉ đi khi commit).
- 5.5: không áp dụng: không phát sự kiện nội bộ.
- 5.6 Gọi ra ngoài: `selftest.Command` là lệnh `commands` (`CommandKind`, in_doubt khi không chắc); lỗi bất kỳ của
  `Send` → in_doubt, không bao giờ tự gửi lại; `Lookup` chỉ nói `done` khi bên ngoài xác nhận.
- 5.7 Ngữ cảnh shop: handler chạy trong tx có tenant của việc; báo hiệu mang `TenantID` của sự kiện.
- 5.8: `selftest` đã nhập `net/http` (chữ ký webhook); phần mới không dùng HTTP.
- 5.9: không áp dụng: không đường ghi HTTP mới.
- 5.10: không áp dụng: không Valkey.
- 5.11: không thêm thư viện (`otelhttp`, `tracetest` đã có trong go.mod).
- 5.12: handler theo mẫu `InboundHandler` (Task 15) và lệnh theo mẫu `commands.Handler` (Task 12).

Mục 6:
- 6.1 Hai worker cùng một việc: queue `SKIP LOCKED`; test workercrash dùng hai worker thật trên một lệnh.
- 6.2 Gửi lại: webhook trùng thân/`eventId` → một việc (Task 14, 15); 50 yêu cầu `retry` cùng khóa → một kết quả (test).
- 6.3: không thêm khóa mới; thứ tự khóa của queue/commands giữ nguyên.
- 6.4, 6.5: `RetryTx`/READ COMMITTED như cũ.
- 6.6 Việc nền lỗi: `ping` thiếu `eventId` trong dữ liệu → `ErrPermanent` (dead ngay); lỗi `Publish` → thử lại.
- 6.7: không áp dụng: bên ngoài giả, không 429.
- 6.8 Chết giữa chừng: test workercrash (lệnh → in_doubt, `Calls` = 1), test dboutage (ingest → tệp tạm → nạp lại).
- 6.9 Treo: lệnh selftest `Timeout` 30 s, hết hạn 3 phút (kế hoạch); test hủy worker bị bỏ khi xong.
- 6.10: không áp dụng: không có tiền.
- 6.11: quy luật "một lệnh không gửi hai lần" đã có rapid ở Task 12; ở đây là test tình huống e2e.
- 6.12 Dữ liệu cá nhân: báo hiệu chỉ có `eventId` (mã thử); span `otelhttp` của ingest không ghi thân, header, query.
- 6.13 Quyền: báo hiệu `Requires: realtime.subscribe`; test SSE dùng thẻ owner thật (`identity.SeedOwner`).

## Bằng chứng

Lệnh chung: `E2E='TestPipeline|TestDatabaseOutage|TestWorkerCrash|TestRetryFifty|TestBinaryWorker'`;
`go test -race -count=3 -run "$E2E" -v ./tests/e2e/` (PostgreSQL crm-t18, cổng 5518). Trước khi viết mã: hai gói
không dựng được (`undefined: selftest.NewInbound`, `undefined: selftest.CommandKind`); sau khi có handler, trước khi
bọc otelhttp ở ingest, test đường ống đỏ: `cannot scan NULL into *string` (việc tách gói không có `traceparent`).

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `pipeline`: POST có chữ ký, 1 ping → SSE `changed` `selftest.pong` đúng `eventId` dưới 2.000 ms, 10 lần, in thời gian lớn nhất | `TestPipelineUnderTwoSecondsOneTrace` | lệnh chung | 3/3 PASS; "thời gian lớn nhất của 10 lần là 345ms / 436ms / 349ms (ngưỡng 2s)" |
| Span nhận webhook (ingest), `job webhook.split`, `job inbound.selftest`, `pg_notify` cùng một `TraceID` (exporter trong bộ nhớ) | `TestPipelineUnderTwoSecondsOneTrace` | lệnh chung | "dấu vết 64434335… có 22 span: [… POST /hooks/{source} … job webhook.split … job inbound.selftest]", span SQL `pg_notify` tìm thấy qua thuộc tính câu lệnh |
| `dboutage`: bật lỗi → 3 webhook 200, 3 tệp tạm, 0 dòng; tắt lỗi → trong 10 s: 0 tệp, 3 dòng, 3 `selftest.pong` mỗi `eventId` 1 lần (chờ thêm 1 s bắt lặp) | `TestDatabaseOutageKeepsWebhooks` | lệnh chung | 3/3 PASS; "đủ 3 báo hiệu sau 1.37s" (gồm 1 s chờ bắt lặp) |
| `workercrash`: `hang_after_send`, worker một bị bỏ, giữ chỗ về quá khứ, worker hai `RunOnce` ×3 → `in_doubt`, `Calls` = 1; `ResolveInDoubt` → `done`, `Calls` = 1; worker một tỉnh lại không ghi đè | `TestWorkerCrashDoesNotSendTwice` | lệnh chung | 3/3 PASS |
| `idempotency`: 50 `POST …/retry` song song cùng khóa qua HTTP thật → 1 dòng audit, pending một lần (chạy lại 1 lần, bên ngoài nhận 1 lần), mọi câu 200 cùng giá trị JSON hoặc 409 | `TestRetryFiftyParallelOneResult` | lệnh chung | 3/3 PASS; "1 trả 200 cùng thân, 49 trả 409" (một lượt trước có câu 200 phát lại: khác byte, cùng giá trị — xem Việc phát hiện thêm) |
| Việc treo Task 15: worker thật (tệp chạy) xử lý `inbound.selftest` và `command.selftest` thành `done`, 0 việc `dead` | `TestBinaryWorkerHandlesSelftest` | lệnh chung | 3/3 PASS |
| `selftest.Inbound`: ping → 1 báo hiệu đúng tenant/topic/quyền/đối tượng; dữ liệu hỏng, thiếu `eventId`, loại lạ → `ErrPermanent`; lỗi Publish → lỗi thường | `TestInboundPingPublishesPong`, `TestInboundRejectsBadEvents` | `go test -race -count=1 ./internal/platform/selftest/` | `ok … selftest 1.5s` |
| `CommandKind` = `CommandKind("command.selftest", 3m, 30s)`; Execute ok → done + ref; mọi lỗi → lỗi (in_doubt); Lookup; Loopback | `TestCommandKind`, `TestCommandExecuteAndLookup`, `TestLoopback` | như trên | PASS |
| Gói phụ thuộc, `commands` (vùng cao, chỉ dùng) | cả bộ | `go test -race -count=1 ./internal/platform/{selftest,webhooks,commands}/... ./cmd/crm/... ./tests/e2e/...` | `ok … webhooks 10.3s`, `ok … commands 28.5s`, `ok … selftest`, `ok … e2e` |
| Toàn bộ cổng, sau khi gộp `main` (83b9d2b) | `make ci` | `make ci` | build, vet, golangci-lint, gen-check, sqlc-vet qua; `go test -race ./...` mọi gói `ok` (gồm `selftest`, `webhooks`, `e2e`, `queue`, `db`, `commands`) trừ hai test không thuộc task (dưới); `make vuln oasdiff catalog-check jscpd test-guard gitleaks` exit 0: "No vulnerabilities found", "Found 0 clones", "no leaks found" |
| Hai test đỏ trong `make ci`, không thuộc task | `TestLintCleanOnRealCode` (tests/arch), `TestImager_HangKilledWithinDeadline` | chạy lại riêng | lint: đỏ vì quét `console/node_modules/flatted/…/flatted.go`; bỏ `node_modules` tạm ra ngoài → `ok … tests/arch 3.0s`. imager `-count=3` → `ok`. Các lượt `make ci` trước còn gặp `TestRetryTxLockTimeoutRecoversWhenLockReleased` (db), `TestRecordAfterLongHandler` (queue) lúc máy load 20; chạy lại riêng `-count=3` → `ok` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `var SelftestCommand = commands.CommandKind(…)` thành hàm `selftest.CommandKind()` (luật không biến toàn cục,
  `gochecknoglobals`); giá trị đúng như kế hoạch (test so bằng).
- Thêm span máy chủ `otelhttp` cho `POST /hooks/{source}` trong `webhooks.Ingest.Register`: kế hoạch đòi "span của
  ingest (nhận webhook)" nhưng ingest chưa có span nào (api đã bọc otelhttp từ Task 6).
- Thêm `selftest.Loopback` (bên ngoài tại chỗ cho worker dev) vì tệp chạy không được nhập `testkit.FakeExternal`.
- Thêm `TestBinaryWorkerHandlesSelftest` (tệp chạy thật) để kiểm phần nối trong `main.go`, vì ba bài thử chính chạy
  trong tiến trình test.
- `idempotency`: "200 cùng body" kiểm theo giá trị JSON, không theo byte (xem Quyết định 3).

## Quyết định thay chủ shop

1. **Ba vai trò chạy trong tiến trình test, không phải ba tiến trình con.** Kế hoạch đòi "exporter trong bộ nhớ" cho
   span của cả ingest, api, worker; exporter trong bộ nhớ chỉ thấy span cùng tiến trình. Test dựng ingest, api, worker
   bằng đúng các hàm khởi tạo công khai mà `main.go` dùng (NewIngest, Drainer, httpapi.NewServer, PGHub, NewWorker,
   NewRunner, NewSplitHandler, InboundJobHandlers), mỗi vai trò pool riêng, HTTP thật qua TCP; thẻ Access thử (máy chủ
   khóa TLS thử, tệp chạy con không tin được). Phần nối của `main.go` được kiểm riêng bằng tệp chạy thật
   (`TestBinaryWorkerHandlesSelftest`). Phương án khác: chạy tệp chạy con xuất OTLP tới một máy nhận OTLP giả trong
   test (thêm giải mã protobuf, chậm hơn); đổi lại được mà không sửa mã sản phẩm.
2. **"Database ngừng" là proxy TCP có công tắc trước PostgreSQL, chỉ cho pool của ingest.** Tắt: cắt mọi kết nối
   đang mở, đóng ngay kết nối mới — giống database chết thật với ingest, worker và api vẫn chạy. Phương án khác: dừng
   container PostgreSQL (ảnh hưởng phiên song song, chậm).
3. **Câu phát lại của khóa chống trùng so theo giá trị JSON.** Lượt chạy đầu có một câu 200 là bản phát lại (đọc từ
   cột `jsonb`) khác bản gốc về thứ tự khóa và khoảng trắng, cùng giá trị. Spec ADR-41 nói phát lại "kết quả đã lưu";
   màn hình đọc JSON nên không sai nghĩa. Sửa cho trùng byte (đổi cột sang `bytea`/`json`) là việc của vùng rủi ro cao
   `idempotency` và cần migration thu hẹp — ghi ở Việc phát hiện thêm, không làm ở đây. Phương án khác: so byte và để
   test đỏ chờ task sửa.
4. **Lệnh thử coi mọi lỗi của bên ngoài là "không biết" (in_doubt), không phân biệt not_sent/rejected.** An toàn nhất
   theo 04 mục 5.3 (không bao giờ tự gửi lại); lệnh thử không cần chính sách gửi lại. `Lookup` dựa vào `External.Has`
   (nói thật): có → done, chưa → not_sent. `Loopback.Has` luôn đúng (không có gì thật được gửi).
5. **Worker dựng `PGHub` chỉ để `Publish` (không `Run`, không LISTEN):** Publish chỉ chạy `pg_notify` trong tx của
   việc. Phương án khác: một kiểu phát riêng không có phần nghe (thêm mã, cùng câu sqlc).

## Việc phát hiện thêm

- **Phát lại của `Idempotency-Key` không trùng byte với câu trả lời gốc** (`platform.idempotency_keys.response_body`
  là `jsonb`: PostgreSQL sắp lại khóa, thêm khoảng trắng). Cùng giá trị JSON. `TestRetryJobFiftyParallel` (httpapi)
  so byte nhưng không bắt được vì ở đó không có câu nào tới sau khi xong (đều 409). Đề xuất một task vùng `idempotency`:
  đổi cột sang `bytea` (mở rộng: cột mới + đọc cột mới trước, thu hẹp sau) hoặc ghi rõ trong ADR-41 rằng phát lại chỉ
  cùng giá trị.
- **Test kiến trúc `TestLintCleanOnRealCode` quét cả `console/node_modules`** (gói npm `flatted` có tệp Go): sau Task
  17, mọi phiên đã chạy `npm install` (make ci làm) đều đỏ. Cần loại `console/node_modules` khỏi phần quét của
  `tests/arch` (việc của Task 17/chủ test kiến trúc).
- Các test nhạy thời gian `db.TestRetryTxLockTimeoutRecoversWhenLockReleased`, `queue.TestRecordAfterLongHandler`,
  `imager.TestImager_HangKilledWithinDeadline` đỏ khi máy tải cao (nhiều phiên song song, load ~20), chạy lại riêng
  thì qua.
- `config` chưa có hằng cho `CRM_ENV=production` (`main.go` và `config.go` cùng so chuỗi `"production"`).

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, trên `git diff --cached` sau khi gộp main 83b9d2b).

- Không có phát hiện phải sửa. Đã kiểm: lệnh thử không gửi hai lần (mọi lỗi → in_doubt, `AutoRetryNotSent` false,
  `Lookup` chỉ done khi `Has`); lệnh thử không đăng ký ở production (`CRM_ENV` bắt buộc); mọi goroutine của test có
  chủ và dừng; không race; span `otelhttp` không ghi query, chỉ bọc POST; `PGHub` không `Run` an toàn; các quyết định
  thay chủ shop không trái spec; mỗi test kiểm đúng tiêu chí của kế hoạch.
- Ghi chú không bắt buộc (không sửa): chưa có test cho nhánh production của `newCommands`; `Loopback.Has` luôn đúng —
  không làm mẫu cho bộ nối thật (đã ghi trong doc của gói).
