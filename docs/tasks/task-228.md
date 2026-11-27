# Task 228 — Khung chạy báo cáo: sổ đăng ký, xem tại chỗ có chỗ và hạn giờ

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 228 = M6-05). Loại việc (quy trình mục 3): A

Điều phối M6 (27/11): chủ shop duyệt kế hoạch M6 sáng 27/11 và cho bắt đầu dù M5 chưa đóng mốc (Task 221 còn chờ
staging); điều kiện "M1–M5 đóng" của M6-00 coi như được miễn. Làm theo phần "Bổ sung Task 223" của kế hoạch.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn. Thêm mới trong
  `internal/modules/reporting` (0 chỗ tham chiếu ngoài gói trước task): `Family` (+3 hằng), `Key` (+9 hằng), `Def`,
  `Defs`, `Lookup`, `Params`, `DecodeParams`, `MaxDays`, `InteractiveOptions`, `Interactive`, `NewInteractive`,
  `(*Interactive).Run`, `BackgroundError`, `TxReport`, sentinel `ErrUnknownReport`, `ErrNotImplemented`,
  `ErrRunInBackground`; `Deps`, `JobHandlers`, `TxHandlers`, `Schedules`; kiểu tham số giữ chỗ của từng báo cáo
  (`DashboardParams`, `OrdersParams`, `DrilldownParams`, `CarriersParams`, `BestSellersParams`, `StockParams`,
  `MovementsParams`, `ProductsExportParams`, `CustomerExportParams`), `RunHandler`/`NewRunHandler`,
  `RunCommandKind`, `SnapshotHandler`/`NewSnapshotHandler`, `CleanupHandler`/`NewCleanupHandler`.
  `httpapi.Deps` thêm trường `Reports` (2 chỗ dựng Deps: `cmd/crm` serveHTTP, `testServerDeps`; nil → bộ chạy mặc
  định trong `prepare`).
- Gói phụ thuộc ngược (go list): `reporting` hôm nay chỉ có `cmd/crm` nhập (`RegisterFilePurposes`); sau task thêm
  `internal/httpapi` (handler tổng quan, bảng lỗi). Sửa `reporting` kéo test của `httpapi`, `cmd/crm`.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng (Task 225 viết sẵn). Hiện thực khung của `getSalesDashboard`
  (đọc tham số, chạy tại chỗ, `run_in_background`, lỗi ngày); operation vẫn trả 501 tới Task 229 (dòng
  `m6_pending.txt` giữ nguyên). Mã lỗi `bad_date`, `range_too_long`, `bad_params`, `unknown_report` đã có trong
  `x-error-codes`.
- Chỗ dùng trên màn hình: không (console M6 là Task 239–241).
- Câu sqlc, bảng, pha migration: không câu sqlc mới, không migration. Câu SQL duy nhất mới nằm trong test
  (`pg_sleep`, `INSERT` thử, `pg_stat_activity`, `SHOW`).
- Test sẽ chạy: `go test -race -count=1 -run '^(TestInteractive.*|TestDefs.*|TestLookup|TestDecodeParams.*|TestTxReport)$' ./internal/modules/reporting/`;
  `go test -race -count=1 -run '^(TestReportsDashboard.*|TestM6Contract_StatusCodes|TestDomainErrorSnapshot.*|TestReportDateParam.*)$' ./internal/httpapi/`;
  `go test -count=1 -run 'M6|Wire' ./cmd/crm/`; `go test -count=1 ./tests/arch/`; phần còn lại ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`db.TxOptions`,
  `db.Setting`, `db.TransientCode`, `tenancy.WithTenant`, `metrics.Registry`, `cache.AdmissionGate`, `commands.CommandKind`,
  `queue.Kind/Handler`, `schedules.Schedule`, `background.Deps`); `reporting.{ScopeFor, OwnSeller, ParsePeriod}` của
  Task 227; httpapi `matchError`, `errorMapping`, `domainErrorMappers`, `writeHandlerError`; testkit `OpenStaff`,
  `Staff.ChannelAccount` (dựng người bán own), `cfaccesstest`, `serveAs`, `expect`, `rolePerms`.
- Dùng lại / viết mới vì: dùng lại `ParsePeriod` (kiểm ngày), `ScopeFor`/`OwnSeller`, `tenancy.WithTenant` +
  `db.TxOptions.Settings` (bốn thiết lập của transaction báo cáo, đặt bằng `set_config(…, true)` nên không rò),
  `db.TransientCode` (đọc SQLSTATE 57014), `commands.CommandKind`, bảng lỗi theo vùng (`errors_reports.go` mới theo
  Task 441). Chỗ (slot) viết mới bằng kênh có bộ đệm `chan struct{}` + `select default`: `cache.AdmissionGate` là
  cổng Valkey theo mặt hàng của flash sale (module nghiệp vụ không được nhập `cache`), không phải giới hạn trong tiến
  trình; không có semaphore dùng chung trong `internal/platform`.

## Soát thiết kế

- 5.1 Chủ dữ liệu: không bảng mới; `reporting` chỉ đọc trong khung này.
- 5.2 Không ghi bảng module khác: transaction báo cáo đặt `transaction_read_only = on`, mọi câu ghi → 25006 (test).
- 5.3 Đọc qua view: khung không tự đọc bảng nào ngoài `OwnSeller` (view `catalog.sellers_v`, Task 227).
- 5.4 Sau commit: không có (chỉ đọc). 5.5, 5.6 Gọi ra ngoài: không; lệnh `command.report_run` chỉ đăng ký kiểu,
  thân lệnh là Task 235.
- 5.7 Ngữ cảnh shop: `Run` chạy trong `tenancy.WithTenant(u.TenantID, TxReport(…))`; người bán own đọc trong cùng tx.
- 5.8 Không HTTP trong module: `reporting` không nhập `net/http`; đổi lỗi sang HTTP ở `errors_reports.go`.
- 5.9 Ghi qua `WriteTx`: không áp dụng (GET chỉ đọc). 5.10 Valkey: không. 5.11 Thư viện mới: không (otel có sẵn).
- 5.12 Theo module mẫu: sentinel trong `errors.go`; kiểu lỗi `BackgroundError` trong module; handler mỏng; lỗi đổi
  một chỗ; log chỉ ở mép HTTP (không log trong `Run`).
- 6.1–6.4 Khóa, thứ tự khóa, deadlock: không khóa dòng (chỉ đọc); `LockTimeout` 1 s chặn chờ khóa bảng (DDL).
- 6.5 READ COMMITTED: mỗi câu báo cáo một ảnh chụp; hàm của báo cáo tự lo câu nhiều phần (Task 229+).
- 6.6 Chạy lại: hàm `JSON` chạy trong `RetryTx` (lỗi 40P01/55P03/40001 chạy lại tới 3 lần) nên kết quả trả bằng giá
  trị, không cộng dồn biến ngoài; `Run` chỉ gán kết quả sau khi tx xong. 57014 và 25006 không chạy lại (RetryTx).
- 6.7, 6.8 Hàng đợi, in_doubt: không áp dụng ở khung (lệnh, việc, lịch giữ chỗ; xem QĐ-228-1).
- 6.9 Treo lâu: tối đa `Slots` (REPORT_API_SLOTS = 2) báo cáo cùng lúc mỗi tiến trình api, lấy chỗ không chờ (hết chỗ →
  `busy` ngay); `statement_timeout` = `REPORT_INTERACTIVE_TIMEOUT` và ctx có hạn cùng giá trị (gồm cả chờ kết nối của
  pool); hết hạn → `timeout`; chỗ nhả bằng `defer` ở mọi đường (lỗi, panic, hủy). `max_parallel_workers_per_gather =
  0`, `work_mem = 32MB` giới hạn CPU, bộ nhớ của database.
- 6.10 Tiền: không tính tiền. 6.11 Quy luật rapid: không áp dụng (không có quy luật tập; luật chỗ kiểm bằng test đồng
  thời có số đo).
- 6.12 Dữ liệu cá nhân: span chỉ có `report.key`, `report.days`, `report.rows` (không giá trị lọc); chỉ số nhãn là khóa
  báo cáo; không log kết quả.
- 6.13 Quyền: guard của hợp đồng (`x-permission`) chặn trước handler (test 403 với người chỉ có
  `report.inventory.read`); phạm vi dữ liệu dựng bằng `ScopeFor` trong `Run`; mỗi `Def` khai quyền đọc của họ.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Test viết trước, chạy → FAIL (chưa có `Def`, `Interactive`, `Deps.Reports`…) | các test dưới | `go test ./internal/modules/reporting/ ./internal/httpapi/` | FAIL: lỗi biên dịch `undefined: reporting.Def`, `busyDeps.Reports undefined` (trước khi viết mã) |
| `Slots: 1`: hai `Run` cùng lúc (lần đầu ngủ 300 ms) → lần hai `BackgroundError{busy}` < 50 ms, `errors.Is(ErrRunInBackground)`; lần đầu ra kết quả; hàm chạy 1 lần; `report_busy` = 1, `report_ms` 1 lần đo, gauge `report_slots_in_use` cuối 0 cao nhất 1 | `TestInteractiveBusy` | `go test -race -count=3 -run '^(TestInteractive.*\|TestDefs\|TestLookup\|TestDecodeParams\|TestTxReport)$' ./internal/modules/reporting/` | `ok elimaz.com.vn/crm/internal/modules/reporting 3.442s` (3 lượt) |
| `Timeout: 200ms`, ngủ 1 s → `BackgroundError{timeout}` < 400 ms; `AcquiredConns()` = 0 ngay sau khi trả; `report_timeout` = 1; hàm chạy 1 lần (57014 không chạy lại); chạy lại được | `TestInteractiveTimeout` | như trên | ok (lượt đầu đỏ `còn 1 kết nối đang mượn` khi hạn ctx = hạn câu → sửa: ctx = Timeout + 1 s, xem Khác kế hoạch) |
| Trong lúc chạy `pg_stat_activity` có đúng 1 phiên `crm-report`; trong tx `transaction_read_only=on`, `max_parallel_workers_per_gather=0`, `work_mem=32MB`, `application_name=crm-report`, `lock_timeout=1s`, `statement_timeout=7s`; mọi kết nối rảnh sau đó không còn các giá trị này | `TestInteractiveSettings` | như trên | ok |
| `Def` có `INSERT` → SQLSTATE 25006, chạy 1 lần, không phải `BackgroundError` | `TestInteractiveReadOnly` | như trên | ok |
| `ctx` bị hủy giữa chừng → `context.Canceled`, không phải `BackgroundError`; chỗ được nhả | `TestInteractiveCanceled` | như trên | ok |
| Người bán own + phạm vi `ScopeFor` dựng trong tx; 367 ngày → `ErrRangeTooLong` trước khi lấy chỗ; JSON nil → `ErrNotImplemented`; khóa ngoài sổ → `ErrUnknownReport` | `TestInteractiveScopeAndErrors` | như trên | ok |
| `TxReport` có đúng bốn Settings và hai hạn | `TestTxReport` | như trên | ok |
| `Defs()` đủ chín khóa, đúng họ, quyền đọc (`sales`/`inventory`/`customer.export`), mỗi khóa có kiểu tham số; `products` không có JSON | `TestDefs` | như trên | ok |
| `Lookup("inventory","orders")`, `(sales, products)`, `(customers, dashboard)`, khóa lạ, họ lạ → `ErrUnknownReport` | `TestLookup` | như trên | ok |
| `DecodeParams("orders", {…,"x":1})` → `ErrBadParams`; JSON hỏng, mảng, phần thừa, rỗng → `ErrBadParams`; ngày sai/thiếu → `ErrBadDate`; from > to → `ErrFromAfterTo`; 367 ngày → `ErrRangeTooLong`; khóa lạ → `ErrUnknownReport` | `TestDecodeParams` | như trên | ok |
| HTTP `GET /api/v1/reports/sales/dashboard`: hết chỗ → 200 `{"status":"run_in_background","reason":"busy"}` (đúng 2 trường); quá hạn → 200 `…"timeout"`; `from=2026-13-01`, `to=2026-02-30`, thiếu `from`, from > to → 400 `bad_date`; 367 ngày → 400 `range_too_long`; chỉ có `report.inventory.read` → 403 | `TestReportsDashboard_RunInBackground` | `go test -race -count=1 -run '^(TestReportsDashboard_RunInBackground\|TestM6Contract_StatusCodes)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 3.512s` |
| 20 route M6 vẫn qua auth, guard; `getSalesDashboard` còn trong `m6_pending.txt` vẫn trả 501 (bộ chạy mặc định của `prepare`, Def giữ chỗ) | `TestM6Contract_StatusCodes` | như trên | ok |
| Sáu lỗi báo cáo đổi đúng mã (trần và đã bọc), nằm trong `reportsError` | `TestDomainErrorSnapshot`, `TestDomainErrorSnapshot_TableMembership` | `go test -count=1 -run '^TestDomainErrorSnapshot' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 0.685s` |
| `wireM6` nối một lần lệnh `command.report_run` (hết hạn 1 giờ, 80 s), việc `reporting.stock_snapshot`, `reporting.cleanup`; lịch rỗng (QĐ-228-1); sổ sự kiện, lịch M2 không đổi | `TestWireM6`, `TestNewModulesRegistersCash` | `go test -count=1 -run '^(TestWireM6\|TestNewModules.*)$' ./cmd/crm/` | `ok elimaz.com.vn/crm/cmd/crm 0.724s` |
| Kiến trúc (ranh giới, SQL, view, dynsql) | `tests/arch` | `go test -count=1 ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 74.114s` |
| Mã lặp, lint | jscpd, golangci-lint | `make jscpd`; `golangci-lint run ./internal/modules/reporting/... ./internal/httpapi/ ./cmd/crm/` | lần đầu 8 bản sao (`xxxDef()`) → gom bằng `newDef[P]`; sau sửa `Found 0 clones`, `0 issues` |
| Cổng tĩnh + test gói bị ảnh hưởng (race: `cmd/crm`, `internal/httpapi`, `internal/modules/reporting`; thường: `httpapi/web`, `conversation`, `platform/files`, `platform/realtime`) | ci-task | `scripts/agent/ci-task.sh` (từ xa, github; blacksmith hết phút, ubicloud không nhận máy) | Lượt 1 bị cắt ở giới hạn 10 phút của lệnh nền (không phải lỗi mã). Lượt 2: run 45962145649 `static: success`, `test (1)–(4): success`, `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh 9634b595…)` |

## Test bị xóa hoặc tắt

(không có)

## Quyết định thay chủ shop

- **QĐ-228-1 — lịch `reporting.stock_snapshot`, `reporting.cleanup` chưa bật.** `wiring.go` nối sẵn lệnh, hai việc
  và hàm `Schedules()`, nhưng lịch lấy từ `snapshotSchedules()` (snapshot.go) và `cleanupSchedules()` (runs.go) đang trả
  rỗng. Lý do: thân việc là bản giữ chỗ trả lỗi; bật lịch bây giờ thì mỗi 15 phút mỗi tenant sinh một việc `dead` và
  cảnh báo hệ thống. Task 233, 235 trả lịch ngay trong tệp của mình (không sửa `wiring.go`, `cmd/crm`). Phương án
  khác: khai lịch ngay trong `wiring.go` (trái mục tiêu "không chạy việc hỏng"). Đảo ngược: trả lịch trong hai hàm đó.
- **QĐ-228-2 — `getSalesDashboard` vẫn trả 501 tới Task 229.** Handler đã có khung (đọc kỳ, chạy tại chỗ,
  `run_in_background`, lỗi ngày); bản giữ chỗ không có `JSON` nên `Run` trả `ErrNotImplemented` → 501, dòng trong
  `m6_pending.txt` giữ nguyên. Task 229 thêm `JSON`, đổi kết quả sang `gen.SalesDashboardReport`, xóa dòng.
- **QĐ-228-3 — tham số ngày sai dạng do ogen bắt → 400 `bad_date`** (không phải `bad_request` chung): hợp đồng M6 ghi
  "sai dạng … → 400 bad_date", mà tham số `format: date` bị ogen từ chối trước handler. `reportParamError`
  (errors_reports.go) chỉ áp cho tham số `from`, `to`, `date` của đường `/api/v1/reports/`.

## Khác kế hoạch

- `Def` thêm trường `NewParams func() Params` (kế hoạch không có): `DecodeParams` cần biết kiểu tham số của từng khóa;
  mỗi tệp báo cáo khai kiểu của mình qua `newDef[P]`. Thêm `PeriodParams` (from, to, `Validate`, `Period`,
  `ReportDays`) dùng chung, `MaxDays = 366`.
- `InteractiveOptions` thêm `Defs map[Key]Def` (nil → `Defs()`) để test cắm `Def` thử (`pg_sleep`) mà không cần biến
  toàn cục; test HTTP dùng cùng cách.
- ctx của một lần chạy có hạn `Timeout + 1 s` (kế hoạch: `ctx` hết hạn do `Timeout`): khi hạn ctx bằng hạn câu, pgx
  hủy câu từ phía máy khách và đóng kết nối (đo: `AcquiredConns` còn 1 sau khi trả). Câu SQL vẫn dừng đúng `Timeout`
  bằng `statement_timeout`; hạn ctx chỉ chặn phần ngoài câu SQL.
- Kiểu tham số của các báo cáo khác (`OrdersParams`, `StockParams`, …) là bản giữ chỗ tối thiểu (kỳ from/to, hay rỗng);
  task của từng báo cáo thêm trường trong tệp của mình. `exports.go`, `columns.go` chỉ có chú thích (chưa có hàm nào
  để giữ chỗ mà không thành mã chết).
- `cmd/crm`: nối qua `wire_m6.go` (`wireM6`, `reportInteractive`) và trường `modules.M6`; không gọi `reporting.Defs()`
  ở `main.go` (bộ chạy tự dùng sổ). `newCommands` nhận thêm `m6Parts`.
- `main` không có commit mới so với điểm tách (9634b595) lúc gộp trước review: không có gì để gộp.

## Việc phát hiện thêm

- Task 229: `reporting.ErrNoOwnSeller` chưa có mã HTTP (tenant chưa có người bán own → 500 khi đường tổng quan chạy
  thật); chọn mã khi bật `JSON` (ghi chú của reviewer).
- Gauge `report_slots_in_use` ghi sau `inUse.Add` không nguyên tử: hai lần nhả chỗ cùng lúc có thể để giá trị cuối là
  1 tới lần chạy sau (chỉ lệch số đo). Task 243 (hiệu năng) xem lại nếu dùng gauge này làm ngưỡng.

## Reviewer

Kết luận: ĐẠT (lượt 1, agent reviewer trên phần đã stage).

- Phát hiện phải sửa: không có.
- Ảnh hưởng khớp; ký hiệu mới chưa nêu tên trong mục Ảnh hưởng: `ReasonBusy`, `ReasonTimeout`, `RunCommandName`,
  `CleanupKindName`, `SnapshotKindName` (mới, chưa nơi nào tham chiếu). Mẫu `TestReportDateParam.*` trong lệnh test của
  mục Ảnh hưởng không có test nào (ca ngày sai nằm trong `TestReportsDashboard_RunInBackground`).
- Bảng bằng chứng: mọi dòng trỏ tới test có thật và kiểm đúng tiêu chí.
- Ghi chú không bắt buộc: gauge không nguyên tử; `ErrNoOwnSeller` chưa có mã HTTP (chép vào Việc phát hiện thêm).
