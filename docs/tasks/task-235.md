# Task 235 — Lần chạy nền: tạo, chạy bằng lệnh có tra lại, xem, báo hiệu (M6-12)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 235 = M6-12). Loại việc (quy trình mục 3): A. Vùng rủi ro cao: `internal/modules/reporting/runs.go` (quyền xem lần chạy), `internal/modules/identity` (thêm `LoadUserByID`).

Ghi chú điều phối M6 (27/11): chủ shop duyệt kế hoạch M6 và cho bắt đầu khi M5 chưa đóng mốc (221 chờ staging) — điều kiện "M1–M5 đóng" của M6-00 được miễn.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences / grep cả test):
  - `reporting.RunHandler`, `NewRunHandler`, `CleanupHandler`, `NewCleanupHandler` (bản giữ chỗ của Task 228): chỉ dùng ở `reporting/wiring.go`; thay thân, giữ chữ ký `NewRunHandler(Deps)`, `NewCleanupHandler(Deps)`. `cleanupSchedules()` trả lịch → `cmd/crm/wire_m6_test.go` (mong lịch chỉ có stock_snapshot) phải sửa theo.
  - `reporting.Deps` (wiring.go): thêm trường `Defs map[Key]Def` (test cắm Def thử, nil → `Defs()`) và `Result func(Key, Params, any) (json.RawMessage, error)` (đổi kết quả sang JSON của API); 3 chỗ dựng: `cmd/crm/modules.go`, `cmd/crm/wire_m6_test.go`, test của reporting.
  - Mới trong `reporting`: `RunLimits`, `CreateRunInput`, `Run`, `RunCursor`, `CreateRun`, `GetRun`, `ListRuns`, `JSONTTL`, `MaxResultBytes`, `MaxRunsLimit`, `TopicRunFinished`, hằng `FormatJSON/XLSX`, `RunQueued…RunExpired`, `ReasonTooHeavy/Forbidden/Error`, `ParseRunCursor`, phương thức `RunHandler.Lookup/OnDone/OnTerminal` (lệnh vẫn là `RunCommandKind()` có sẵn, không thêm biến `RunCommand`), sentinel `ErrExportForbidden`, `ErrForbidden`, `ErrTooManyRuns`, `ErrRunNotFound` (errors.go).
  - Mới trong `identity`: `LoadUserByID(ctx, tx, id)` (users.go) + câu sqlc `GetUserByID`.
  - `httpapi/errors_reports.go` + `errors_snapshot_test.go`: 4 dòng lỗi mới (`ErrExportForbidden` 403 export_forbidden, `ErrForbidden` 403 forbidden, `ErrTooManyRuns` 429 too_many_runs, `ErrRunNotFound` 404 run_not_found).
  - Mới trong `httpapi`: `ReportResultJSON(k, p, res)` (xuất ra để `cmd/crm` nối vào `reporting.Deps.Result`); tách phần đổi kết quả của 7 GET báo cáo (dashboard, orders, drilldown, carriers, best_sellers, stock, movements) thành hàm `<x>Out(p, res)` dùng chung giữa GET và lần chạy nền (thay đổi cấu trúc, không đổi hành vi của GET).
- Gói phụ thuộc ngược (go list): `reporting` ← `cmd/crm`, `internal/httpapi`; chỉ test: `internal/modules/conversation` (filepurposes_test), `internal/platform/files` (purpose_test); `identity` ← gần như mọi gói (thêm hàm, không đổi ký hiệu cũ).
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml`. Hiện thực 6 operation: `createSalesReportRun`, `listSalesReportRuns`, `getSalesReportRun`, `createInventoryReportRun`, `listInventoryReportRuns`, `getInventoryReportRun`; xóa 6 dòng đó khỏi `tests/arch/testdata/m6_pending.txt`. `…/file` (Task 237) và `customers/exports` (Task 238) giữ 501. Mã lỗi đã có trong x-error-codes: `export_forbidden`, `run_not_found`, `too_many_runs`, `unknown_report`, `bad_params`.
- Chỗ dùng trên màn hình: `console/src/screens/reports/RunsPage.tsx`, `runsData.ts`, `useReport.ts` (Task 241 đã viết theo hợp đồng; không sửa console). Báo hiệu `report.run_finished` là chủ đề RunsPage nghe.
- Câu sqlc, bảng, pha migration: tạo `queries/reporting/runs.sql` (bảng `reporting.report_runs` của 0601; đọc `identity.users_v` lấy tên); `queries/identity/users.sql` thêm `GetUserByID`. Không migration mới (chỉ mục `report_runs_requester`, `report_runs_family`, `report_runs_expiry` đã có).
- Test sẽ chạy: `go test -race -run` các test mới của `internal/modules/reporting` (runs_test, runs_property_test, cleanup_test), `internal/httpapi` (handlers_reports_runs_test + test GET báo cáo cũ vì tách hàm đổi kết quả), `internal/modules/identity` (users_test, cả bộ ở ci-task vì vùng cao), `cmd/crm` (TestWireM6), `tests/arch` (m6_pending). Cả gói chạy ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (commands, queue, realtime, tenancy, db, idempotency), mẫu lệnh có tra lại `catalog/imports_validate.go` (Looker + TerminalHook, in_doubt giữ nguyên), `httpapi.WriteTx` (mẫu đường ghi Task 12), `reporting.Interactive`/`TxReport`/`ScopeFor`/`OwnSeller`/`DecodeParams`/`Lookup` (Task 228), `identity.LoadUser` (đọc grant), `identity.RecordAudit`, `realtime.PGHub`, `testkit.OpenStaff`, `reportseed.NineOrders`.
- jscpd lượt đầu báo 2 đoạn trùng: (1) chờ hub LISTEN giống `handlers_servicekeys_test.go` — viết thành `waitListening` riêng của test reporting (gộp vào testkit kéo theo chạy toàn bộ test, không đáng cho 8 dòng test); (2) `runOfJob` giống `cash/internal/statement.target` (đọc tenant + mã từ payload lệnh) — hai module, cùng hình nhưng khác payload và lý do thay đổi, giữ riêng (luật "logic nghiệp vụ ở lại trong module"); so JSON trong test httpapi dùng `reflect.DeepEqual` thay cho chép hàm của test reporting.
- Dùng lại / viết mới vì: dùng lại `commands.CommandKind`/Runner/ResolveInDoubt (không tự viết vòng thử lại), `queue.JobQueue.Enqueue` (DedupeKey = runID), `WriteTx` (chống trùng 50 yêu cầu), `TxReport` + `ScopeFor` + `OwnSeller` của bộ chạy tại chỗ cho phần chạy nền (cùng phạm vi, cùng hạn chỉ đọc), câu `ListUserGrants` có sẵn cho `LoadUserByID` (chỉ thêm câu đọc user theo id). Viết mới: câu SQL của `report_runs` (bảng chỉ module này có), bộ đổi kết quả sang JSON của API đặt ở httpapi (module không được nhập mã ogen) — tách từ các GET có sẵn, không viết lần hai.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

**Mục 5**
- 5.1 Chủ dữ liệu: `reporting.report_runs` thuộc module reporting (schema reporting, 0601).
- 5.2 Không ghi bảng module khác: ghi `report_runs`, `identity.audit_logs` qua `identity.RecordAudit`, `platform.jobs` qua `JobQueue.Enqueue`/Runner. Không gì khác.
- 5.3 Đọc qua view: tên người tạo đọc `identity.users_v` (G15, có trong `docs/views.md`); người bán own qua `catalog.sellers_v` (OwnSeller có sẵn). `LoadUserByID` ở trong identity đọc bảng của chính nó.
- 5.4 Nguyên tử: tạo lần chạy = chèn `report_runs` + Enqueue + audit + lưu kết quả chống trùng trong một `WriteTx`.
- 5.5 Sau commit: chạy báo cáo đi qua lệnh `command.report_run` (hàng đợi), báo hiệu `Bus.Publish` trong tx của OnDone/OnTerminal (chỉ tới client khi commit).
- 5.6 Gọi ra ngoài: không có. Lệnh mode command dùng để có Lookup sau khi worker chết (in_doubt → tra `report_runs` → done/not_sent), không gửi gì ra ngoài; `AutoRetryNotSent = true`.
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant`/`WriteTx`; bảng có RLS từ 0601.
- 5.8 Không biết HTTP: module không nhập ogen; đổi kết quả sang JSON hợp đồng ở httpapi, nối vào `Deps.Result` ở cmd/crm.
- 5.9 Đường ghi: hai POST có `IdempotencyKey` và `x-permission` sẵn trong hợp đồng (Task 225); GET chỉ đọc.
- 5.10 không áp dụng: không dùng Valkey.
- 5.11 Không thư viện mới.
- 5.12 Theo mẫu: Task 12 (WriteTx + audit + test 50 song song), Task 73 (lệnh), catalog imports_validate (Looker/TerminalHook).

**Mục 6 (câu 1, 2, 6, 8, 9, 13 theo kế hoạch; các câu khác)**
- 6.1 Đồng thời: mọi đổi trạng thái là UPDATE có điều kiện (`MarkRunning … status IN ('queued','running')`, `FinishReady … status = 'running'`, `Fail … status IN ('queued','running')`): hai worker cùng một lần chạy chỉ một bên ghi `ready`. Dọn dùng `FOR UPDATE SKIP LOCKED ORDER BY id`, tối đa 500 dòng mỗi phần.
- 6.2 Gửi lại: `Idempotency-Key` qua WriteTx (50 yêu cầu → 1 dòng, 1 việc, 1 audit); thêm `DedupeKey = runID` của việc.
- 6.3 Thứ tự khóa: một đường chỉ khóa `report_runs` (12a) rồi `jobs` (13) (Enqueue sau INSERT; Runner đổi jobs sau OnDone/OnTerminal ghi report_runs). Dọn chỉ khóa `report_runs` (phần b đọc trạng thái lệnh bằng `commands.Status`, không khóa).
- 6.4 Lỗi tạm: WriteTx/RetryTx chạy lại; Execute gặp lỗi database hay `db.ErrRetryLater` → `not_sent` (chạy lại an toàn).
- 6.5 READ COMMITTED mặc định + câu có điều kiện.
- 6.6 Việc nền lỗi: lỗi nghiệp vụ (forbidden, too_heavy, báo cáo lỗi) ghi `failed` có điều kiện rồi `Outcome{done}` — không thành `dead`, không cảnh báo hệ thống. Lỗi hạ tầng → `not_sent` (lịch thử lại của Runner, 8 lượt; hết lượt → dead + OnTerminal → failed/error). Hết chỗ → `not_sent` RetryAfter 15 s; quá 1 giờ → expired → OnTerminal → failed/busy.
- 6.7 không áp dụng: không bên ngoài.
- 6.8 Chết giữa chừng: hết giữ chỗ → in_doubt (OnTerminal in_doubt không đổi report_runs) → ResolveInDoubt gọi Lookup: ready/failed/expired → done (không chạy lại); queued/running → not_sent → chạy lại. Đã ghi `ready` thì lần chạy lại thấy MarkRunning 0 dòng → done, không ghi lần hai.
- 6.9 Treo: chạy nền `TxReport(REPORT_BACKGROUND_TIMEOUT 60 s, lock 5 s)`, ctx riêng hạn + 1 s; lệnh Timeout 80 s < lease 90 s.
- 6.10 không áp dụng: không đổi tiền.
- 6.11 Quy luật: rapid `runs_property_test` (mỗi lần chạy đúng một trạng thái cuối, không hai lần sang ready, ≥ 1 báo hiệu khi kết thúc).
- 6.12 Dữ liệu cá nhân: `params` chỉ mã, ngày, khóa lọc (DecodeParams chặn trường lạ); audit `after` chỉ family/report/format/params; kết quả không vào log, span, audit; payload của việc chỉ `{"runId"}`; `Outcome.Result` chỉ `{"rows": n}`.
- 6.13 Quyền: POST cần quyền đọc của họ (x-permission) + phạm vi không rỗng lúc tạo (`ErrForbidden`), `xlsx` cần `report.export` (ScopeShop); GET/List: người tạo, hoặc `report.exports.manage` (không thì 404, không lộ tồn tại); tenant khác 404 (RLS). Lúc chạy nạp lại người tạo (`LoadUserByID`): khóa, mất quyền đọc/phạm vi → `failed/forbidden`.

**Thêm cho M6**
- View: `identity.users_v` (tên người tạo), `catalog.sellers_v` (OwnSeller). Cả hai có trong `docs/views.md`.
- Hàm dựng điều kiện: lần chạy nền gọi đúng `Def.JSON` của báo cáo (cùng hàm của GET), không điều kiện thứ hai.
- EXPLAIN: câu mới chỉ chạm `report_runs` theo khóa chính / chỉ mục `report_runs_requester`, `report_runs_family`, `report_runs_expiry`; không chạm orders, stock_ledger.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| POST → 202 queued; 50 song song cùng khóa → 1 dòng, 1 lệnh, 1 audit, 202 cùng thân hoặc 409; result = đúng thân GET /reports/sales/orders; liệt kê không kèm result; người khác 404 run_not_found, report.exports.manage 200, họ khác 404 | `TestReportRuns_HTTPCreateAndRun`, `TestReportRuns_HTTPView` | `go test -race -count=1 -run '^TestReportRuns_' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 3.237s` |
| xlsx không report.export → 403 export_forbidden, không dòng/lệnh; inventory/runs report orders → 400 unknown_report; trường lạ → 400 bad_params; không quyền đọc → 403; lần 6 → 429 too_many_runs, người khác 202; con trỏ hỏng → 400 bad_params | `TestReportRuns_HTTPErrors` | như trên | như trên |
| Tạo → queued, 1 lệnh dedupe_key = runID, audit after.params; worker → ready, result = kết quả xem tại chỗ; người tạo nhận report.run_finished trong 2 s | `TestRuns_CreateAndRun` | `go test -race -count=1 -run '^TestRuns_' ./internal/modules/reporting/` | `ok elimaz.com.vn/crm/internal/modules/reporting 21.271s` |
| Lỗi tạo (export_forbidden, unknown_report, bad_params, định dạng lạ, products json, forbidden); 5 lần → lần 6 ErrTooManyRuns, người khác tạo được | `TestRuns_CreateErrors` | như trên | như trên |
| Slots 1, Def ngủ 300 ms → lần sau not_sent RetryAfter 15 s, KeepAttempt, vẫn queued; chạy lại → ready | `TestRuns_WorkerBusy` | như trên | như trên |
| Timeout 200 ms, ngủ 1 s → failed/too_heavy, lệnh done, không việc dead | `TestRuns_TooHeavy` | như trên | như trên |
| Worker chết sau ready → in_doubt → ResolveInDoubt → done; một lần ready, result không đổi, báo cáo chạy 1 lần; Execute lặp → done | `TestRuns_InDoubtAfterReady` | như trên | như trên |
| Worker chết lúc running → in_doubt → tra lại not_sent → chạy lại → ready | `TestRuns_InDoubtWhileRunning` | như trên | như trên |
| Người tạo bị khóa / mất report.sales.read → failed/forbidden, lệnh done, có báo hiệu | `TestRuns_ForbiddenAtRun` | như trên | như trên |
| Lệnh hết hạn khi luôn hết chỗ → expired → failed/busy, có báo hiệu | `TestRuns_ExpiredBusy` | như trên | như trên |
| GetRun người khác / tenant B / họ khác → ErrRunNotFound, manage thấy; 120 lần chạy trang 50 → 120 mã khác nhau, người thường chỉ của mình (1), manage 121 | `TestRuns_Get`, `TestRuns_List` | như trên | như trên |
| Dọn: json ready quá hạn → expired, result NULL; ready chưa hạn giữ; queued treo mà lệnh xong → failed/error, lệnh còn chờ → giữ; 91 ngày xóa, 89 ngày còn; lịch mỗi giờ theo tenant | `TestRuns_Cleanup` | như trên | như trên |
| Rapid (số lượt của gói, 200): bước ngẫu nhiên (chạy, worker chết, tra lại, hết chỗ, dừng giữa chừng) trên 1–5 lần chạy → đúng một trạng thái cuối, ready không đổi lần hai, ≥ 1 báo hiệu | `TestRuns_LifecycleProperty` | như trên | như trên |
| LoadUserByID: grant đúng, tenant khác / không có → ErrUnknownUser, khóa → ErrLocked | `TestLoadUserByID` | `go test -race -count=1 -run '^TestLoadUserByID$' ./internal/modules/identity/` | `ok elimaz.com.vn/crm/internal/modules/identity 2.215s` |
| GET báo cáo cũ không đổi sau khi tách hàm đổi kết quả | `TestReports*` | `go test -count=1 -run '^TestReports' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.657s` |
| Lịch reporting.cleanup nối ở wireM6 | `TestWireM6` | `go test -count=1 -run '^TestWireM6$' ./cmd/crm/` | `ok elimaz.com.vn/crm/cmd/crm 0.727s` |
| Cổng tĩnh (lint, jscpd, kiến trúc, oasdiff, gitleaks…) + test gói bị ảnh hưởng (race: cmd/crm, httpapi, identity, reporting; thường: gói phụ thuộc) | ci-task (lượt 2; lượt 1 đỏ ở cổng tĩnh: gocognit, gochecknoglobals, nolintlint, gofumpt, jscpd 2 đoạn trùng — đã sửa, test lượt 1 đều xanh) | ci-task của task, từ xa trên GitHub | `REMOTE-TEST XANH https://github.com/quyenld37/elimaz-crm/actions/runs/46109953561`; `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh f8b007e1…)` |

## Test bị xóa hoặc tắt

(không có)

## Quyết định thay chủ shop

- **QĐ-235-1 (kết quả lưu đúng JSON của API):** theo ghi chú điều phối, worker đổi kết quả `Def.JSON` sang đúng thân mà GET báo cáo trả, qua `reporting.Deps.Result` do `cmd/crm` nối `httpapi.ReportResultJSON` (module không được nhập mã ogen). GET báo cáo và lần chạy nền gọi chung một hàm đổi (`dashboardOut`, `ordersOut`, `drilldownOut`, `carriersOut`, `bestSellersOut`, `stockOut`, `movementsOut`). Phương án khác: lưu JSON của kiểu Go và đổi khi đọc (phải giải mã ngược từng kiểu, dễ lệch).
- **QĐ-235-2 (in_doubt không làm hỏng lần chạy):** `OnTerminal("in_doubt")` không đổi `report_runs` (như `catalog.ImportValidator`); tra lại (`Lookup`) quyết định: ready/failed/expired → done, queued/running → chạy lại. Kế hoạch ghi `OnTerminal` đặt `failed/error` với mọi trạng thái khác `expired`; làm vậy thì worker chết giữa chừng làm hỏng lần chạy dù chạy lại được. dead → failed/error, expired → failed/busy giữ đúng kế hoạch.
- **QĐ-235-3 (hết chỗ không tính lượt):** hết chỗ trả `not_sent`, `RetryAfter 15 s`, `KeepAttempt: true`; không vậy thì 8 lượt × 15 s thành `dead` sau 2 phút thay cho chờ tới hạn 1 giờ rồi `failed/busy` như kế hoạch muốn. Trần thời gian là `ExpiresAfter` 1 giờ của lệnh.
- **QĐ-235-4 (phạm vi của báo hiệu):** `report.sales.read`, `report.inventory.read` là quyền theo đích, bus từ chối báo hiệu không có phạm vi. Báo hiệu lấy phạm vi `all` nếu người tạo có, không thì phạm vi grant đầu tiên của người tạo cho quyền đó (người cùng Page/kho cũng nhận "tải lại danh sách", không có dữ liệu; mở lần chạy của người khác vẫn 404). `customer.export` (toàn shop) không phạm vi. Phương án khác: báo hiệu theo người dùng trong bus (đổi `internal/platform/realtime`, ngoài phạm vi task).
- **QĐ-235-5 (giới hạn số lần chờ là giới hạn mềm):** đếm `queued/running` rồi chèn trong cùng transaction, không khóa thêm; hai yêu cầu khác khóa chống trùng cùng lúc có thể vượt 1 lần. Giới hạn để chặn tải, không phải quy luật dữ liệu; thêm khóa tư vấn là nhóm khóa mới ngoài ADR-46.
- **QĐ-235-6 (lỗi của báo cáo lúc chạy nền):** ngoài phạm vi Page/kho → `forbidden`; tham số, ngày, chưa có bản chụp, chưa làm → `error`; 57014 hay quá hạn ctx riêng → `too_heavy`, trừ khi ctx của lệnh đã hủy (worker dừng: chạy lại). Lỗi khác (database) → `not_sent`.
- **QĐ-235-7:** `CreateRunInput` thêm `RequestID` (ghi vào audit như mọi đường ghi); `reporting.Deps` thêm `Defs` (test cắm Def thử) và `Result` (QĐ-235-1); `params` lưu là bản đã đọc lại (`json.Marshal` của kiểu tham số), chỉ trường đã biết.

## Khác kế hoạch

- QĐ-235-2, QĐ-235-3 (xem trên).
- `GET …/file` và `customers/exports` giữ 501 (Task 237, 238 như kế hoạch). Dạng `xlsx` tạo được lần chạy (kiểm `report.export`); lúc chạy `executeXLSX` giữ chỗ trả `failed/error` tới Task 237 (đúng kế hoạch).
- Kế hoạch ghi "client SSE" nhận báo hiệu: test đăng ký thẳng `realtime.PGHub.Subscribe` của người tạo (cùng đường lọc tenant, quyền với SSE) thay cho mở kết nối HTTP SSE.
- Ca "`CheckRules` không có `dead:command.report_run`": kiểm bằng "không việc nào `dead`" (cảnh báo dead đọc từ trạng thái việc).
- Tách hàm đổi kết quả của các GET báo cáo nằm cùng commit với hành vi mới (kỳ của carriers, movements nay lấy từ `rep.Period` mà module đọc từ chính tham số đó; best_sellers điền mặc định sku/placed như module — cùng giá trị trả ra): phần tách nhỏ, chỉ có lý do tồn tại là lần chạy nền dùng chung; test GET cũ (`TestReports*`) chạy lại nguyên vẹn.

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 235 không có test e2e; đủ ca ở test của gói reporting và httpapi)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (agent reviewer, lượt 1, trên phần đã stage sau khi gộp main f8b007e1 và ci-task xanh run 46109953561).

- Phát hiện phải sửa: không có. Đã kiểm thêm theo kế hoạch: không lời gọi mạng trong hàm truyền cho WithTenant/RetryTx/WriteTx (`Bus.Publish` là `pg_notify` trong tx); thứ tự khóa report_runs (12a) trước jobs (13) khớp Runner/Worker.
- Ghi chú không bắt buộc (không sửa): (1) tách hàm đổi kết quả: kỳ của carriers, movements lấy từ `rep.Period` của module thay cho tham số query, best_sellers điền mặc định sku/placed bằng `cmp.Or` — cùng giá trị, `TestReports*` xanh (đã ghi ở Khác kế hoạch); (2) `TestRuns_ExpiredBusy` chờ 150 ms thật cho `expires_at` (cùng một đồng hồ database); (3) người có `report.exports.manage` xem được kết quả lần chạy của người khác dù grant đọc theo Page — đúng kế hoạch/hợp đồng; (4) báo hiệu theo phạm vi grant đầu tiên (QĐ-235-4) không mang dữ liệu.

## Gộp vào main

- Nhánh `task-235` (1 commit 82eae93d) gộp vào main 53591060 (sau Task 88, 260).
- Xung đột: chỉ `cmd/crm/modules.go`: main thêm `M7: wireM7(d, fs)` (Task 260) vào literal `modules{…}`, nhánh thêm
  `Result: httpapi.ReportResultJSON` vào `reporting.Deps` của `wireM6`. Giải: giữ cả hai (`Result` trong Deps của M6,
  rồi `M7: wireM7(d, fs)`).
- `tests/arch/testdata/m6_pending.txt`: tự gộp, bỏ 6 thao tác lần chạy nền đã có mã (create/list/get Sales/Inventory
  ReportRun); main không đổi tệp này và không đổi `internal/modules/reporting` từ điểm tách.
- `make gen` không chênh; `go build ./...` và `go vet` (cmd/crm, httpapi, reporting, identity, tests/arch) qua.

### Reviewer (gộp vào main)

Kết luận: ĐẠT (agent reviewer, lượt 1, trên phần đã stage của lần gộp). Không có phát hiện phải sửa: 24/26 tệp trùng
từng byte với bản đã ĐẠT trên nhánh; `modules.go` giữ cả `Result: httpapi.ReportResultJSON` (nhánh) và
`M7: wireM7(d, fs)` (main); `m6_pending.txt` bỏ đúng 6 thao tác đã có mã, 2 thao tác `…/file` còn 501 vẫn giữ;
tên mới trong httpapi không trùng tệp handler M7 của main.

### Bằng chứng test (gộp vào main)

- `CRM_CI_POOL_SLOTS=vps=0 CRM_CI_LABEL=integrate-main-235 merge-test.sh a3cba975 HEAD <gói> <race>`
  (gói: cmd/crm, internal/httpapi, internal/modules/reporting/..., internal/modules/identity/..., tests/arch; -race:
  cmd/crm, reporting, identity) → `MERGE-TEST XANH (github)`, run 46111110882: static success, test 1–6 success.
  Không có test chập chờn.
