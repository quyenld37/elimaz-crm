# Task 237 — Xuất Excel: dựng tệp, tải lên kho, đăng ký tệp, đường dẫn tải (M6-14)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 237 = M6-14). Loại việc (quy trình mục 3): A. Vùng rủi ro cao: `internal/platform/files` (chỉ dùng `Register`, `MarkDeleted`, `DownloadURL`, không sửa), `internal/modules/reporting/runs.go` (quyền tải tệp).

Ghi chú điều phối M6 (27/11): chủ shop duyệt kế hoạch M6 và cho bắt đầu khi M5 chưa đóng mốc (221 chờ staging) — điều kiện "M1–M5 đóng" của M6-00 được miễn. Kho tệp xuất là bucket `crm-exports` của Task 515 (`files.BucketExports`, `files.ExportKey`, tiền tố `reports/` 7 ngày, `customers/` 1 ngày); `bootstrap.sh` đã có luật hết hạn từ Task 515, task này không sửa hạ tầng.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (grep cả test, console, docs):
  - `reporting.Deps` (wiring.go): thêm trường `Log *slog.Logger` (nil → `slog.Default()`); 3 chỗ dựng (`cmd/crm/modules.go`, `cmd/crm/wire_m6_test.go`, test reporting) không phải sửa vì trường mới để trống được.
  - `reporting.Run`: thêm `FileID *uuid.UUID` (chỉ module dùng; `reportRunOut` của httpapi không xuất ra API).
  - `reporting.ProductsExportParams` (bản giữ chỗ, 0 chỗ dùng ngoài `products_export.go`): thêm `Status *string`, `SKUPrefix string`.
  - Mới trong `reporting`: `FileLink`, sentinel `ErrRunNotReady`, `ErrRunExpired` (errors.go), hằng `DefaultMaxRows`.
  - `Def.Rows`, `Def.Columns`, `Def.FileName` của 8 báo cáo (dashboard, orders, order_drilldown, carriers, best_sellers, stock, stock_movements, products) được điền; `customers` để Task 238.
  - `RunHandler.executeXLSX` (không xuất ra) thay bản trả `failed/error`; `CleanupHandler.Handle` thêm phần (d) tệp xlsx quá hạn.
  - `httpapi`: `GetSalesReportRunFile`, `GetInventoryReportRunFile` hết 501; `errors_reports.go` thêm 2 dòng (`ErrRunNotReady` 409 `run_not_ready`, `ErrRunExpired` 410 `run_expired`) + 2 dòng `errors_snapshot_test.go`.
- Test dùng chung bị chạm: `runsHTTPEnv` (httpapi) thêm trường `files` cho worker; `idemEnv.ownerSQL` gọi hàm mới `tenantSQL` (gom với test tải tệp); `runsEnv` (reporting) thêm `reload`, `cleanup`, `cleanupQuiet`, `expectRefused` (gom chỗ lặp do jscpd báo). `FileLink` đọc tên tải về bằng `files.Describe` (API có sẵn của files, không sửa files).
- Gói phụ thuộc ngược (go list / grep): `reporting` ← `cmd/crm`, `internal/httpapi`; chỉ test: `internal/platform/files` (purpose_test), `internal/modules/conversation` (filepurposes_test), `tests/arch`. `files` không đổi.
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml` (hợp đồng Task 225 đã có `ReportFileLink`, mã `run_not_ready`, `run_expired`, `export_forbidden` trong x-error-codes). Hiện thực `getSalesReportRunFile`, `getInventoryReportRunFile`; xóa 2 dòng đó khỏi `tests/arch/testdata/m6_pending.txt`. `getCustomerExportFile` để Task 238 (nó gọi cùng `FileLink`).
- Chỗ dùng trên màn hình: `console/src/screens/reports/RunsPage.tsx`, `inventoryData.ts` (`inventoryRunFile`), `MovementsReport.tsx` đã gọi `…/file` theo hợp đồng (Task 241): không sửa console.
- Câu sqlc, bảng, pha migration: `queries/reporting/runs.sql`: câu nhận lần chạy (`MarkRunning`) thêm `RETURNING … created_at` (hàng sqlc chỉ runs.go dùng, cho dòng "Tạo lúc"); `GetRun` thêm cột `file_id` (đổi `store.GetRunRow`, chỉ runs.go dùng); mới `FinishRunFile`, `SetRunFile`, `LockExpiredFileRuns`, `ExpireFileRuns`, `WarehouseNames`. Bảng `reporting.report_runs` (cột `file_id`, `file_bytes`, `truncated` có từ 0601), `platform.files` qua API của gói `files`. Không migration mới. SQL dựng lúc chạy: `products_export_dynsql.go` (+ `products_export_dynsql_test.go` trên PostgreSQL thật).
- Test sẽ chạy: `go test -race -run` các test mới của `internal/modules/reporting` (exports_test, exports_retry_test, columns_test, products_export_test, products_export_dynsql_test, cleanup_test), `internal/httpapi` (handlers_reports_file_test, errors_snapshot_test, TestM6Contract), `tests/arch` (m6_pending) — các gói này ci-task chạy đủ.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (files: `Register`, `ExportKey`, `MarkDeleted`, `DownloadURL`, `MemStore`/`filestest.MemFileStore`; xlsx: `NewWriter`, `Str/Int/Money/Date`, `Note`, `Sanitize`, `xlsxtest`, `Read`), `reporting` (`TxReport`, `ScopeFor`, `OwnSeller`, `DecodeParams`, `failReason`, `runReport`, `sqlBuilder`, `stockFilter`, `bestSellerRows(limit 0)`, `Drilldown`, `rowLabel`, `statusLabels`, `channelLabels`, `carrierLabels`), `identity.RecordAudit`, `identity.Can`, mẫu đăng ký tệp máy chủ ghi `fulfillment` (nhãn vận đơn: Put ngoài tx rồi Register).
- Dùng lại / viết mới vì: dùng lại toàn bộ đường tệp của M1/Task 515 (không tự ký URL, không tự kiểm hạn tệp), bộ ghi luồng `xlsx.Writer` của Task 236 (chặn công thức bằng `Sanitize`), cách đọc của mỗi báo cáo (các `Rows` gọi đúng hàm đọc của bản xem tại chỗ: `ordersReport`, `dashboard`, `carriersReport`, `Drilldown` theo trang, `bestSellerRows(…, 0)`, `stockRows`/`movementRows` theo keyset) để số trong tệp bằng số của GET (một nguồn). Viết mới: `columns.go` (cột và dòng XLSX của từng báo cáo — chỉ module này có), `exports.go` (executeXLSX, FileLink, nhãn phạm vi), câu đọc danh sách sản phẩm (`products_export_dynsql.go`, cần keyset (mã mẫu, SKU) và tổng `available` theo kho trong phạm vi: không câu có sẵn nào làm). Định dạng tỷ lệ phần vạn ra chữ (`rateText`) là hàm mới duy nhất của gói (API trả số, không có hàm chữ để dùng lại).

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

**Mục 5**
- 5.1 Chủ dữ liệu: `reporting.report_runs` của reporting; dòng `platform.files` của gói files (chỉ qua `files.Register`, `files.MarkDeleted`).
- 5.2 Không ghi bảng module khác trực tiếp: ghi `report_runs`; `platform.files` qua API của files; audit qua `identity.RecordAudit`.
- 5.3 Đọc qua view: sản phẩm `catalog.variants_v` ⋈ `catalog.products_v`, tồn `inventory.stock_available_v`, tên kho `inventory.warehouses_v`, tên Page `channel.channel_accounts_v` (câu `FilterPages` có sẵn) — đều có trong `docs/views.md`.
- 5.4 Nguyên tử: ghi `ready` + `files.Register` + gắn `file_id` + audit `report.export_ready` trong một transaction; dọn: `expired` + `MarkDeleted` trong một transaction.
- 5.5 Sau commit: báo hiệu `report.run_finished` vẫn qua OnDone của Runner (Task 235).
- 5.6 Gọi ra ngoài: `Files.Put` (kho S3) chạy **ngoài** mọi transaction, sau khi transaction đọc đóng, trước transaction ghi. Khóa tệp chứa mã băm nội dung nên `Put` lặp lại an toàn: `ErrObjectExists` = cùng nội dung → coi là thành công; lỗi khác → `not_sent` (chạy lại, đối tượng mồ côi tự xóa theo luật tiền tố).
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant`; khóa tệp `files.ExportKey(prefix, tenant, runID/sha.xlsx)`, `Register` đòi đúng tiền tố tenant.
- 5.8 Không biết HTTP: module không nhập `net/http`; `FileLink` trả url + hạn, handler đổi sang `gen.ReportFileLink`.
- 5.9 Đường ghi: `GET …/file` ghi audit (như hợp đồng, Q17) trong một transaction tenant, không cần Idempotency-Key (mỗi lần gọi một dòng audit là đúng ý).
- 5.10 không áp dụng: không Valkey.
- 5.11 Không thư viện mới (excelize có từ Task 236).
- 5.12 Theo mẫu: Task 235 (lần chạy nền, ghi có điều kiện), mẫu tệp máy chủ ghi (Put ngoài tx → Register trong tx), Task 28 (dọn theo khóa tăng dần).

**Mục 6**
- 6.1 Đồng thời: `FinishRunFile … WHERE status = 'running' RETURNING` — hai lần executeXLSX cùng lần chạy chỉ một bên thắng, bên thua không đăng ký gì; `Register` gặp khóa đã có trả id cũ. Dọn: `FOR UPDATE SKIP LOCKED ORDER BY id LIMIT 500`.
- 6.2 Gửi lại: lệnh chạy lại sau `not_sent` dựng lại tệp; khóa theo mã băm → cùng nội dung trùng khóa (ErrObjectExists = thành công), khác nội dung → khóa khác, chỉ bên thắng `FinishRunFile` được đăng ký.
- 6.3 Thứ tự khóa (ADR-46 12a → 12b): executeXLSX khóa `report_runs` (UPDATE) rồi chèn `files` (dòng mới, không tính); dọn khóa `report_runs` FOR UPDATE trước, rồi `MarkDeleted` theo `file_id` tăng dần. Không đường nào khóa `files` rồi `report_runs`. Test bằng `pgx.QueryTracer` + chạy song song.
- 6.4 Lỗi tạm: transaction đọc trong RetryTx: hàm truyền vào không giữ trạng thái — mỗi lần chạy `Truncate(0)` + `Seek(0)`, bộ băm mới, số dòng và `truncated` đặt lại, kết quả trả bằng giá trị (C19, test `exports_retry_test`). Lỗi database ở bước ghi → `not_sent`.
- 6.5 READ COMMITTED + UPDATE có điều kiện; transaction đọc `transaction_read_only` (TxReport) nên một tệp là một ảnh chụp nhất quán.
- 6.6 Việc nền lỗi: quá hạn (57014) → `failed/too_heavy`; lỗi nghiệp vụ báo cáo → `failed/error`; quá `MaxExportBytes` → `failed/too_heavy`; lỗi tệp tạm, kho, database → `not_sent` (Runner thử lại, hết lượt → dead → OnTerminal → failed/error).
- 6.7 Bên ngoài: kho S3 — không chắc `Put` đã ghi (đứt giữa chừng) thì lần sau `Put` cùng khóa: có rồi → ErrObjectExists (cùng mã băm nên cùng nội dung), chưa → ghi; không bao giờ ghi đè.
- 6.8 Chết giữa chừng: sau `Put` trước ghi ready → lệnh in_doubt → Lookup thấy `running` → chạy lại → tệp giống nhau (dữ liệu không đổi) trùng khóa; dữ liệu đổi → khóa mới, đối tượng cũ mồ côi tự xóa theo tiền tố (không có dòng `files`).
- 6.9 Treo: đọc trong `TxReport(REPORT_BACKGROUND_TIMEOUT, 5 s)` + ctx riêng; `Put` dùng ctx của lệnh (hạn 80 s của lệnh).
- 6.10 Tiền: ô tiền là số nguyên `int64` đồng (`xlsx.Money`), không số thực.
- 6.11 Quy luật: test retry (7 dòng, không 14; mã băm khớp tệp trên kho), hai lần executeXLSX cùng lần chạy → đúng 1 `ready`, 1 dòng `files`.
- 6.12 Dữ liệu cá nhân: log của lần xuất chỉ `run_id`, `report`, `rows`, `bytes`, `duration_ms` (test bắt log); audit `after` chỉ số đếm / họ, báo cáo; danh sách chi tiết không có tên khách, SĐT (Task 230 đã chặn ở cột).
- 6.13 Quyền: `FileLink` = luật xem của `GetRun` (người tạo hoặc `report.exports.manage`, không thì 404) + `report.export` lúc tải với họ sales/inventory (403 `export_forbidden`); lúc chạy nạp lại người tạo (Task 235 `allowed`); phạm vi của tệp là phạm vi của người tạo lúc chạy.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Báo cáo đơn care_by ra Excel: ready, 1 dòng files (bucket exports, report_export, hạn = hạn lần chạy, tên theo kỳ), khóa reports/<tenant>/<run>/<sha>.xlsx, mã băm khớp, 3 dòng + tổng bằng số của bản xem tại chỗ, ô tiền số #,##0, dòng ghi kỳ/cơ sở/phạm vi/giờ tạo, audit report.export_ready | `TestExport_OrdersCareBy` | `go test -count=1 -timeout 90s -run '^(TestExport_OrdersCareBy\|TestExport_PutFailsThenRetries\|…)$' ./internal/modules/reporting/` (10 test) | `ok` (chỉ TestProductsExport_File đỏ vì dữ liệu thử, sửa ở dòng dưới) |
| Danh sách chi tiết placed: đủ 8 đơn O1–O6, O8, O9 (bỏ limit 2 của tham số, đọc theo trang) | `TestExport_DrilldownPlaced` | `go test -count=1 -run '^(TestRateText\|TestDefs_XLSX\|TestProductRows_Keyset\|TestExport_OrdersCareBy\|TestExport_DrilldownPlaced\|TestExport_Truncated)$' ./internal/modules/reporting/` | qua (chỉ OrdersCareBy đỏ vì sai số khoảng hạn trong test, đã sửa) |
| REPORT_MAX_ROWS = 5 → truncated, 5 dòng + ghi chú "Đã cắt ở 5 dòng…" | `TestExport_Truncated` | như trên | qua |
| Kho lỗi lần đầu → not_sent, vẫn running, 0 dòng files; lần sau ready, 1 dòng | `TestExport_PutFailsThenRetries` | như dòng 1 | qua |
| Put cùng khóa → ErrObjectExists = thành công, 1 dòng files cùng khóa, bucket exports | `TestExport_ObjectExistsIsSuccess` | như dòng 1 | qua |
| Hai lần executeXLSX, dữ liệu đổi ở giữa → 1 ready, 1 dòng files, tệp của bên thắng, 1 audit | `TestExport_TwoExecutionsOneWinner` | `go test -count=1 -timeout 90s -run '^(TestExport_TwoExecutionsOneWinner\|TestExport_CleanupLockOrder)$' ./internal/modules/reporting/` | `ok … 1.106s` |
| Tên sản phẩm là công thức → ô bắt đầu bằng ', không <f> | `TestExport_FormulaEscaped` | như dòng 1 | qua |
| Dọn: xlsx quá hạn → expired + files.deleted_at; chưa hạn giữ nguyên | `TestExport_CleanupExpiresFiles` | như dòng 1 | qua |
| Thứ tự khóa dọn (12a trước 12b), SKIP LOCKED, không deadlock | `TestExport_CleanupLockOrder` | như dòng 6 | `ok` |
| Log chỉ run_id/report/rows/bytes/duration_ms, không tên/mã đơn | `TestExport_LogHasNoData` | như dòng 1 | qua |
| Thử lại giữa chừng (C19): 7 dòng không 14, sha khớp; truncated tính lại | `TestExportRetry_FileRebuiltFromScratch`, `TestExportRetry_TruncatedRecomputed` | như dòng 1 | qua |
| Danh sách sản phẩm: 5 dòng own, không partner, Có thể bán theo kho trong phạm vi, cột như tệp mẫu nhập, xlsx.Read đọc lại đúng, không giá vốn; lọc status/q; tham số lạ, json → bad_params; không report.export → ErrExportForbidden | `TestProductsExport_File`, `TestProductsExport_Params` | `go test -count=1 -timeout 90s -run '^(TestProductsExport_File\|TestProductsExport_Params)$' ./internal/modules/reporting/` | `ok … 1.045s` |
| Keyset (mã mẫu, SKU) theo trang = đọc một lần; bỏ mẫu xóa, người bán khác | `TestProductRows_Keyset` | như dòng 2 | qua |
| Tỷ lệ làm tròn xuống 66,66%, "—" khi nil; 7 báo cáo có Rows/Columns/FileName, cột theo cơ sở và mức | `TestRateText`, `TestDefs_XLSX` | như dòng 2 | qua |
| Lượt dọn ra `expiredFiles` | cleanup_test (`Cleanup*`) | `go test -count=1 -timeout 120s -run 'Cleanup\|XLSX\|Xlsx\|Purpose' ./internal/modules/reporting/` | `ok … 0.947s` |
| GET …/file: 200 + url ký 5 phút + expiresAt, tên tải về, audit mỗi lần; người khác/họ khác 404; manage 200 audit mang mã người đó; gỡ report.export 403; json 400; chưa chạy 409; quá hạn 410; họ tồn 200 | `TestReportRunFile_HTTPLink`, `TestReportRunFile_HTTPErrors` | `go test -count=1 -timeout 200s -run '^(TestReportRunFile_HTTPLink\|TestReportRunFile_HTTPErrors\|TestReportRuns_HTTP.*)$' ./internal/httpapi/` | `ok … 2.119s` |
| Bảng lỗi run_not_ready 409, run_expired 410 | errors_snapshot_test (`TestErrors*`) | như trên | `ok` |
| Cổng tĩnh (lint, jscpd, test kiến trúc gồm m6_pending/TestM6Contract) + test -race httpapi, reporting và test thường cmd/crm, httpapi/web, conversation, files, realtime | cổng kiểm của task | ci-task (lượt 4, sau gộp main 533de1ef) | `REMOTE-TEST XANH …/runs/46134896282`, `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh 533de1ef…)` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Bucket `exports` (`files.BucketExports`, Task 515) thay cho `BucketPrivate`/`crm-private` của kế hoạch; dòng `files` có `bucket = 'exports'`. Không sửa `bootstrap.sh`: luật hết hạn theo tiền tố đã có từ Task 515.
- Hạn của tệp tính từ mốc lấy **trước** `Put` (ghi chú điều phối M6: sửa theo 515), không phải `now()` lúc ghi ready.
- Test thử lại (C19) dùng một `Def` thử trả lỗi 40001 ở lần đầu thay cho `pgx.QueryTracer` ép lỗi: cùng đường `RetryTx`, không phụ thuộc thứ tự câu SQL.
- URL của kho bộ nhớ dùng tham số `exp` (không `X-Amz-Expires`): test HTTP kiểm hạn ký theo `exp`.
- Tên tải về lấy từ `original_name` đã đăng ký (`files.Describe`) thay vì gọi lại `Def.FileName` lúc tải: tên theo "hôm nay" (danh sách sản phẩm, tồn hôm nay) không đổi khi tải ngày sau.
- Chữ ký `executeXLSX(ctx, rj runJob, def Def)` theo khung `Execute` của Task 235 (runJob đã mang runRow và người tạo nạp lại), không `(r runRow, u AuthUser)`.

## Quyết định thay chủ shop

- Ghi chú điều phối 27/11: chủ shop duyệt M6 và cho bắt đầu khi M5 chưa đóng mốc; điều kiện "M1–M5 đóng" của M6-00 được miễn.
- QĐ-237-1: tệp xuất dùng bucket `exports` của Task 515 (tiền tố `reports/`, `customers/`), không `crm-private` như kế hoạch — theo ghi chú điều phối M6 và luật hết hạn đã có; phương án khác: thêm luật vào crm-private (đụng hạ tầng, không được làm trong task).
- QĐ-237-2: giờ "Tạo lúc" trong tệp = `created_at` của lần chạy, không phải giờ dựng tệp — để chạy lại cùng dữ liệu ra đúng cùng nội dung, cùng mã băm (ErrObjectExists = thành công). Phương án khác: giờ dựng (mỗi lần chạy lại ra tệp khác, thêm đối tượng mồ côi).
- QĐ-237-3: tổng quan ra một bảng 4 cột (Chỉ số, Số đơn, Doanh số, Phí ship thu khách): các chỉ số, rồi dòng theo kênh ("Kênh Facebook · Đơn lên/Chưa hủy"), theo ngày ("dd/mm/yyyy · Chưa hủy/Thành công") — vì Writer chỉ có một bộ cột; phương án khác: nhiều trang (Writer chưa hỗ trợ).
- QĐ-237-4: tiêu đề cột theo chữ màn hình; báo cáo đơn dùng "Tiền hàng", "Giao thành công", "Chưa hủy" (cơ sở completed bỏ Chưa hủy/Hủy); danh sách sản phẩm dùng đúng tên cột của tệp mẫu nhập M1 (có dấu *) để nhập lại được; trạng thái ghi chữ của tệp mẫu nhập (Đang nhập, Đang bán, Ngừng bán). Tỷ lệ làm tròn xuống (66,66%) theo chốt điều phối 27/11, không 66,67% như kế hoạch.
- QĐ-237-5: `GET …/file` chạy trong một transaction tenant thường (`db.TxAPI`), không qua `WriteTx`/Idempotency-Key: mỗi lần gọi là một lần tải, mỗi lần một dòng audit (Q17).

## Việc phát hiện thêm

- `TestDashboard_ExplainNoSeqScan` (reporting, Task 229, task này không chạm) đỏ một lần ở lượt cổng kiểm thứ 2 (quá 60 s khi dựng 100.000 đơn trên bể GitHub), xanh ở lượt 1 và lượt 4 — chập chờn đã biết (ghi ở lần gộp Task 139); để điều phối viên xem.

## Reviewer

Kết luận: ĐẠT (agent reviewer, trên phần đã stage sau khi gộp main 533de1ef).

- Phát hiện phải sửa: không có. Đã kiểm riêng: `Files.Put` nằm ngoài mọi transaction (sau `buildFile`, trước `finishFile`); thứ tự khóa ADR-46 12a → 12b đúng ở `finishFile` và lượt dọn (`TestExport_CleanupLockOrder`).
- Ảnh hưởng: khớp; thiếu một dòng cho `RETURNING created_at` của câu nhận lần chạy — đã thêm vào mục Ảnh hưởng.
- Bảng bằng chứng: mọi dòng trỏ test có thật, kiểm đúng tiêu chí.
- Ghi chú không bắt buộc (không sửa): log "report export ready" vẫn ghi khi bên thua ở `FinishRunFile` (thừa một dòng log); tên tệp tồn/danh sách sản phẩm theo "hôm nay" lúc ghi xong có thể lệch ngày nếu lần chạy vắt qua nửa đêm (không ảnh hưởng số); phần (c) xóa lần chạy quá 90 ngày đứng trước (d) — chỉ đáng kể nếu lịch dọn ngừng ~83 ngày.
- Bằng chứng ci-task (lượt 4) chạy trước một sửa nhỏ một dòng ở `columns.go` (ngày "hôm nay" của tệp tồn lấy `time.Now()`); reviewer đã chạy `go vet` reporting, httpapi trên bản đã stage: qua.
