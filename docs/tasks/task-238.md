# Task 238 — Xuất danh sách khách (M6-15, PQ06)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 238 = M6-15). Loại việc (quy trình mục 3): A (vùng cao: dữ liệu cá nhân, quyền riêng).

Điều kiện "M1–M5 đóng" của M6-00 được chủ shop miễn ngày 27/11 (ghi chú điều phối M6): làm M6 khi M5 chưa đóng mốc.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `reporting.CustomerExportParams` (khung rỗng của Task 228) thêm trường `CreatedFrom, CreatedTo, LastOrderFrom, LastOrderTo, Lifecycle, TagIDs, MinOrders` và `Validate` thật; chỗ tham chiếu: chỉ `customersDef()` trong `customers_export.go` (grep `CustomerExportParams`: 1 chỗ mã, 0 chỗ ngoài gói). `customersDef()` gán thêm `Rows, Columns, FileName`. Bốn handler `CreateCustomerExport, ListCustomerExports, GetCustomerExport, GetCustomerExportFile` (chữ ký do ogen sinh, không đổi) thay thân 501 bằng mã thật, dùng lại `createReportRun, listReportRuns, getReportRun, reportRunFile` của `handlers_reports_runs.go` (không đổi chữ ký các hàm đó). Mã sinh sqlc: thêm trường `ReturnRateBp` vào struct model `CustomerCustomersV` ở mọi gói store có schema customer (sinh lại bằng `make gen`, không sửa tay).
- Gói phụ thuộc ngược (go list): `reporting` ← `internal/httpapi`, `cmd/crm`. View `customer.customers_v` thêm cột cuối: câu sqlc đọc view theo tên cột (promotion, ord, conversation, …) không đổi; không câu nào `SELECT *` từ view (arch scancache chặn).
- Operation OpenAPI bị chạm, oasdiff: không đổi `openapi.yaml` (bốn operation `createCustomerExport, listCustomerExports, getCustomerExport, getCustomerExportFile` đã có hợp đồng từ Task 225; `ReportParams` đã ghi tên các khóa customers). Chỉ đổi hành vi 501 → thật; xóa 4 dòng của `tests/arch/testdata/m6_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn Tệp xuất/xuất khách là Task 241); tên khóa tham số khớp hợp đồng `ReportParams` và ghi chú điều phối (createdFrom, createdTo, lastOrderFrom, lastOrderTo, lifecycle, tagIds, minOrders).
- Câu sqlc, bảng, pha migration: không câu sqlc mới; câu đọc dựng lúc chạy trong `customers_export_dynsql.go` (đọc `customer.customers_v`, `customer_identities_v`, `customer_tag_links_v`, `customer_tags_v` — ngoại lệ đọc view của reporting đã có trong `tests/arch/sqlusage_test.go`). G10: view thiếu `return_rate_bp` (grep migrations: chỉ 0117, 0201, 0403 định nghĩa view, cột cuối `name_source`) → tạo `migrations/0605_customers_v_return_rate.sql`, pha mở rộng (CREATE OR REPLACE VIEW nối một cột cuối, giữ GRANT), sửa dòng `customer.customers_v` của `docs/views.md` và chuỗi mong đợi trong `tests/arch/m2contract_test.go` cùng commit (R13). Không bảng mới, không khóa.
- Test sẽ chạy: test mới `reporting` (`TestCustomersExport_*`, `TestCustomersExportProperty`, `TestMigration0605CustomersView`), `httpapi` (`TestCustomerExport_HTTP*`), `tests/arch` (m6 contract, views doc, m2 contract) — ở ci-task (gói đổi + phụ thuộc; vùng cao chạm: không có thư mục HIGH_RISK nào bị sửa, migration mới chỉ thêm → gói dùng view).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (xlsx, files, tenancy); trong module: `rateText` (columns.go, phần vạn → "33,33%", làm tròn xuống như API), `dayStart`/`DayCutoff`/`VN` (period.go), `sqlBuilder` (dynsql), `typed`, `col`, `optStr`, `newDef`, `DecodeParams` (chặn trường lạ); httpapi: `createReportRun/listReportRuns/getReportRun/reportRunFile`; luồng tệp `executeXLSX`/`finishFile`/`FileLink` của Task 237 đã xử lý họ customers (mục đích `customer_export`, tiền tố `customers/`, hạn `CUSTOMER_EXPORT_TTL`, audit `customer.export_*`). Đổi SĐT: `customer.NormalizePhone` chỉ đi chiều `0…` → `+84…` (không có hàm ngược).
- Dùng lại / viết mới vì: dùng lại toàn bộ các hàm trên; viết mới: `customerRows` (câu đọc của báo cáo này), `localPhone` (đổi `+84…` → `0…`, 3 dòng, không có hàm sẵn; giữ trong reporting vì chỉ là cách hiện của tệp xuất), tham số và Validate (kiểu riêng của báo cáo theo mẫu `ProductsExportParams`). Tỷ lệ hoàn không tính lại trong Go: đọc cột `return_rate_bp` của view (một chỗ tính, cùng công thức `customer.statsOf`).

## Soát thiết kế

- 5.1 Chủ dữ liệu: khách thuộc module customer; cột view mới do migration của customer (schema customer). Lần chạy, tệp thuộc reporting (bảng có sẵn).
- 5.2 Không ghi bảng module khác: reporting chỉ đọc; ghi lần chạy/tệp/audit qua mã của Task 235/237 (`files.Register`, `identity.RecordAudit`).
- 5.3 Đọc qua view: chỉ bốn view `customer.*_v` đã công bố (docs/views.md), không đọc bảng `customer.customers`.
- 5.4 Nguyên tử: tạo lần chạy + lệnh + audit một transaction (CreateRun có sẵn); xong tệp một transaction (finishFile có sẵn).
- 5.5 Sau commit: tải tệp lên kho ngoài transaction (executeXLSX có sẵn); báo hiệu xong qua lớp có sẵn.
- 5.6 Gọi ra ngoài: không áp dụng (kho tệp nội bộ, đường có sẵn của Task 237).
- 5.7 Ngữ cảnh shop: câu đọc chạy trong transaction tenant của executeXLSX; view `security_invoker` nên RLS của bảng gốc áp dụng; test tenant B 0 dòng.
- 5.8 Không biết HTTP: module không nhập net/http; handler mỏng gọi hàm chung.
- 5.9 Đường ghi: POST có `Idempotency-Key` và `x-permission: customer.export` (hợp đồng có sẵn), WriteTx của createReportRun.
- 5.10 PostgreSQL nguồn sự thật: đúng, không Valkey.
- 5.11 Thư viện: không thêm.
- 5.12 Theo mẫu: theo `products_export.go`/`products_export_dynsql.go` (Def, keyset, *_dynsql.go có test PostgreSQL thật).
- 6.1 Đồng thời: chỉ đọc trong transaction chỉ đọc (TxReport); ghi lần chạy theo câu điều kiện có sẵn (`status = 'running'`).
- 6.2 Gửi lại: Idempotency-Key ở POST; tệp theo mã băm nội dung (Task 237).
- 6.3 Thứ tự khóa: không khóa mới (đọc view không khóa).
- 6.4 Lỗi tạm: RetryTx của executeXLSX; Rows không giữ trạng thái giữa các lần chạy (biến trang khai trong hàm).
- 6.5 Cách ly: transaction đọc của báo cáo (TxReport, như các báo cáo khác); một câu mỗi trang, keyset theo `(created_at, id)`.
- 6.6–6.8 Việc nền lỗi, 429, chết giữa chừng: theo hàng đợi lệnh có sẵn (Task 235/237); không áp dụng thêm.
- 6.9 Treo lâu: REPORT_BACKGROUND_TIMEOUT của TxReport; trang 1.000 khách, keyset `(created_at, id)` giảm dần dùng chỉ mục có sẵn `customers_roots_created (tenant_id, created_at DESC, id DESC) WHERE id = root_id` (0207); câu con theo `root_id` dùng `customers_root (tenant_id, root_id)` và `customer_identities_customer (tenant_id, customer_id)` có sẵn.
- 6.10 Tiền: Tổng chi `int64` đồng, ô tiền `xlsx.Money`.
- 6.11 Quy luật: rapid 50 lượt — tập mã khách trong tệp = tập tính trong Go; ô tỷ lệ hoàn = định dạng của `customer.StatsFor(...).ReturnRateBP` (mẫu số 0 → "—").
- 6.12 Dữ liệu cá nhân: params chỉ mã, ngày, số, uuid (DecodeParams chặn trường lạ); audit ba bước chỉ bộ lọc, số dòng, mã lần chạy; log chỉ run_id, report, rows, bytes, duration_ms (có sẵn); test quét chuỗi SĐT, tên trong audit, params, log. Tệp không có địa chỉ.
- 6.13 Quyền: `customer.export` là quyền toàn shop (ScopeShop): grant `page:p1` → Can sai → 403 ở guard và ở CreateRun; tải tệp: chỉ người tạo hoặc `report.exports.manage` (GetRun), x-permission customer.export ở cả 4 operation.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Tệp xlsx: chỉ hồ sơ gốc, tenant khác 0 dòng, SĐT xác minh mới nhất dạng 0…, ô chữ chống công thức, tỷ lệ hoàn, nhãn gộp, không địa chỉ, tiền tố customers/, hạn 24 giờ, tên tệp | `TestCustomersExport_File` | `go test -race -count=1 -run '^(TestCustomerRows_Keyset\|TestCustomersExport_File\|TestCustomersExport_Filters\|TestCustomersExport_Permission\|TestCustomersExport_NoPersonalData\|TestMigration0605CustomersView\|TestCustomersExportProperty)$' ./internal/modules/reporting/` | `ok elimaz.com.vn/crm/internal/modules/reporting 6.468s` |
| Bộ lọc ngày tạo, ngày mua cuối, vòng đời, nhãn (OR, cả hồ sơ gộp), số đơn; trường lạ/chữ tự do/giá trị sai/json bị từ chối | `TestCustomersExport_Filters` | như trên | như trên |
| Quyền customer.export toàn shop (page → ErrForbidden, không quyền → ErrForbidden) | `TestCustomersExport_Permission` | như trên | như trên |
| Audit 3 bước, params, log không có tên/SĐT | `TestCustomersExport_NoPersonalData` | như trên | như trên |
| Migration 0605: nối return_rate_bp cuối, công thức, NULL khi mẫu số 0, RLS | `TestMigration0605CustomersView` | như trên | như trên |
| Keyset theo trang, chọn SĐT, gom nhãn trên PostgreSQL thật | `TestCustomerRows_Keyset` | như trên | như trên |
| Quy luật: tập mã khách = tập tính trong Go; ô tỷ lệ hoàn = StatsFor (rapid 200 lượt) | `TestCustomersExportProperty` | như trên | như trên |
| HTTP: page → 403, bad_params 400, 202 xlsx, GET/list, tải file 200 + audit, người khác 404, không quyền 403 | `TestCustomerExport_HTTP` | `go test -race -count=1 -run '^TestCustomerExport_HTTP$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 2.642s` |
| Test cũ của view khách vẫn qua sau khi nối cột | `TestMigration0403CustomersView`, `TestSchemaViewsAndRules` | `go test -count=1 -run '^(TestMigration0403CustomersView\|TestSchemaViewsAndRules)$' ./internal/modules/customer/` | `ok elimaz.com.vn/crm/internal/modules/customer 1.586s` |
| Cổng tĩnh + test gói đổi và phụ thuộc (bản đã gộp main 9d0be8e9) | ci-task | `scripts/agent/ci-task.sh` | `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh 9d0be8e9…)`, run 46158947223: static success, test (1)–(6) success |

## E2E viết sẵn, chưa chạy

(không có — kế hoạch Task 238 không có e2e; đủ ca ở test gói reporting và httpapi)

## Test bị xóa hoặc tắt

- Không xóa, không tắt. Thu hẹp một test: `TestMigration0403CustomersView` (`internal/modules/customer/contact_test.go`) chỉ xét hai cột nối thêm của 0403 thay vì mọi cột cuối (0605 nối cột thứ ba); phần "không cột nào khác" do `TestMigration0605CustomersView` và `TestSchemaViewsAndRules` kiểm đúng từng cột.

## Khác kế hoạch

- C6 "tên 300 ký tự": bảng `customer.customers` có `CHECK (length(full_name) BETWEEN 1 AND 120)` từ M1 (0117), không chèn được 300 ký tự; test dùng tên 120 ký tự (độ dài tối đa có thể có), kiểm ô giữ nguyên đủ tên.
- Thuộc tính rapid chạy 200 lượt (kế hoạch ghi 50): `TestMain` của gói reporting đặt `testkit.SetRapidChecks(200)` cho cả gói; mỗi lượt một tenant mới, cả test ~3 s nên giữ 200.
- Thêm `customers_export_dynsql_test.go` (luật `tests/arch/dynsql_test.go`: mỗi `*_dynsql.go` có test cùng tên trên PostgreSQL thật): keyset theo trang 2 dòng, chọn SĐT, gom nhãn.
- Sửa `internal/modules/customer/contact_test.go` (`TestMigration0403CustomersView`): test migrate tới cuối rồi đòi đúng 2 cột nối thêm; 0605 nối cột thứ 3 nên test chỉ xét tới hai cột của 0403 (như `schema_test.go` của 0201 đã làm "chỉ xét 15 cột đầu"). Không nới điều kiện của 0403: vẫn đòi hai cột đó đúng tên, đúng thứ tự, cột cũ giữ nguyên.
- Sửa chuỗi mong đợi `customer.customers_v` trong `tests/arch/m2contract_test.go` (R13: dòng docs/views.md đổi cùng migration).
- Ô "Mua cuối" để trống khi khách chưa mua (ô ngày không có giá trị), "Tỷ lệ hoàn" ghi "—" khi mẫu số 0.
- Mã sinh sqlc: thêm trường `ReturnRateBp int32` vào model `CustomerCustomersV` ở 16 gói store (sinh lại bằng `make gen`, không dùng ở đâu).

## Quyết định thay chủ shop

- QĐ-238-1 — Bộ lọc nhãn `tagIds` là "có ít nhất một nhãn trong danh sách" (OR), xét nhãn của mọi hồ sơ cùng `root_id`. Vì sao: lọc nhóm khách để chăm sóc thường gom nhiều nhãn; khớp cách đếm nhãn gom theo nhóm gộp của cột Nhãn. Phương án khác: phải có đủ mọi nhãn (AND) — thêm trường riêng sau nếu cần, không phá tham số cũ.
- QĐ-238-2 — SĐT chính: số xác minh mới nhất trong nhóm gộp; nhóm không có số xác minh nào thì lấy số mới tạo nhất (thứ tự `verified_at DESC NULLS LAST, created_at DESC`). Vì sao: kế hoạch ghi "mới xác minh nhất (verified_at, rồi created_at)"; bỏ trống khi chưa xác minh sẽ làm mất SĐT của đa số khách nhập tay. Phương án khác: chỉ số đã xác minh.
- QĐ-238-3 — Thứ tự dòng: khách mới tạo trước (keyset `created_at, id` giảm dần, đúng chỉ mục có sẵn `customers_roots_created`). Phương án khác: theo mã khách (không có chỉ mục, mã dài hơn 6 chữ số sắp sai theo chữ).
- QĐ-238-4 — Giới hạn tham số: tối đa 50 nhãn, số đơn tối thiểu 0–1.000.000; khoảng ngày không giới hạn độ dài (danh sách khách không theo kỳ báo cáo 366 ngày), mỗi đầu có thể bỏ; ngày sai → `bad_date`, từ sau tới → `from_after_to` (cùng mã của các báo cáo khác).
- QĐ-238-5 — Tên tệp tải về `danh-sach-khach_<ngày hôm nay>.xlsx`; dòng đầu tệp "Phạm vi: Toàn shop · Tạo lúc …" (không ghi bộ lọc vào tệp, tránh mã nhãn vô nghĩa với người đọc).

## Việc phát hiện thêm

- sqlc sinh `ReturnRateBp int32` (không rỗng) cho cột `return_rate_bp` của `customer.customers_v`, vì sqlc không biết `CASE` có thể ra NULL. Hiện không câu sqlc nào đọc cột này; task nào thêm câu sqlc đọc nó phải đặt kiểu rỗng được (override `sqlc.yaml` hoặc `sqlc.narg`), không thì quét NULL sẽ lỗi.

## Reviewer

Kết luận: ĐẠT (agent reviewer, 28/11, trên `git diff --cached` sau khi gộp main 9d0be8e9; ci-task XANH run 46158947223).
- Không có phát hiện ảnh hưởng tính đúng. Đủ Interfaces của kế hoạch; tham số khớp màn 241; tỷ lệ làm tròn xuống qua `rateText`; 0605 chép đúng 17 cột của 0403 rồi nối cột cuối, công thức khớp `customer.statsOf`; 0605 vào sau 0606 của main hợp lệ (`WithAllowOutofOrder`).
- Ghi chú không bắt buộc đã xử lý: sửa số gói store 18 → 16; thêm dòng thu hẹp test vào "Test bị xóa hoặc tắt"; ghi kiểu sqlc của `return_rate_bp` vào "Việc phát hiện thêm". Ghi chú log lúc tải (đường tải HTTP dùng chung `reportRunFile` có sẵn, đã có test của Task 237) giữ nguyên.

## Gộp vào main

Phiên gộp tuần tự 28/11, main 813204cc (sau Task 262, 528) ← `task-238` (76db4351).
- Tệp `docs/tasks/task-238.md` mẫu do `new-task.sh` tạo (chưa track) chặn merge vì nhánh đã có tệp: xóa bản mẫu rồi merge lại.
- Xung đột duy nhất: `docs/views.md`, hai dòng liền nhau. Giữ dòng `inventory.opening_status_v` của main (Task 262 thêm "mọi người bán, tính lúc đọc") và dòng `customer.customers_v` của nhánh (nối `return_rate_bp`, Task 238, 0605). Chuỗi cột của `customers_v` khớp `tests/arch/m2contract_test.go` của nhánh.
- `models.go` của 16 gói store tự gộp; `make gen` sau gộp không chênh. Migration 0605 không trùng số (main tới 0707; chạy ngoài thứ tự hợp lệ, `WithAllowOutofOrder`). Không định nghĩa lại `customers_v` ở migration nào khác sau 0403. `m6_pending.txt`: nhánh xóa 4 operation xuất khách, main không đụng.
- Kiểm trên máy: `make gen` (không chênh), `go build ./...`, `go vet` gói reporting, customer, httpapi, tests/arch, cmd/crm: sạch.
- Test từ xa trên cây gộp a01c9048 so với HEAD 813204cc (`CRM_CI_POOL_SLOTS=vps=0 CRM_CI_LABEL=integrate-main-238 scripts/ci/merge-test.sh`): gói `./cmd/crm`, `./internal/httpapi`, `./internal/modules/reporting`, `./internal/modules/customer`, `./tests/arch`; `-race` cho reporting, customer, httpapi. Kết quả: static success, test (1)–(5) success, `MERGE-TEST XANH (github)`, run 46161883055. Không có test chập chờn.
- Reviewer (agent reviewer, 28/11, trên `git diff --cached` của bản gộp): ĐẠT. Không phát hiện ảnh hưởng tính đúng; mọi thay đổi của main từ điểm tách nhánh còn nguyên (views.md: Task 262, 528, 524); 16/16 `models.go` có `ReturnRateBp`; chỉ 0117, 0201, 0403, 0605 nhắc `customers_v`. Ghi chú không bắt buộc: `ReturnRateBp int32` không nhận NULL (đã ghi ở "Việc phát hiện thêm").
