# Task 229 — Tổng quan trong ngày (BC01)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 229 = M6-06). Loại việc (quy trình mục 3): A

Điều kiện bắt đầu "M1–M5 đóng" của M6-00 được chủ shop miễn (27/11 sáng duyệt kế hoạch M6, cho bắt đầu khi M5 chưa đóng
mốc vì Task 221 còn chờ staging).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `reporting.DashboardParams` (đang nhúng `PeriodParams`; 3 chỗ
  dùng: `dashboard.go`, `handlers_reports_dashboard.go`, `handlers_reports_dashboard_test.go` — giữ nguyên hình dạng,
  không đổi chỗ dùng). Thêm mới: `reporting.Amounts`, `DashboardReport` (và phương thức `ReportRows()`, `Interactive.Run` gọi qua interface cho thuộc tính `report.rows` của span), hằng `StatusReady`, `CustomerReturns`, `Pending`, `ChannelRow`,
  `DayRow`, `Period.MarshalJSON` (kỳ ra JSON đúng `ReportPeriod` của hợp đồng `{from, to}`; trước đó `Period` không
  được mã hóa JSON ở đâu — grep `json.Marshal` trên `Period`: 0 chỗ). `dashboardDef()` thêm `JSON`.
- Ký hiệu không xuất ra ngoài: `orderSetSelect` mới (thân SELECT của `orderSet`, commit dọn code; `orderSet` giữ chữ ký
  và chuỗi trả, chỗ gọi: chỉ test `orders_dynsql_test.go`), `dashboardSQL`, `dashboardDaySQL`,
  `dashboardQuery`, `cellBranch`, `dayBranch`; httpapi: `dashboardResponse`, `reportDate`, `reportAmounts`
  (`handlers_reports_dashboard.go`). Test: `outsiders`, `bulk`, `checkCells`, `checkScoped` (gói reporting, dùng ở
  `dashboard_dynsql_test.go` và `orders_dynsql_test.go`), `checkDashboardAll` (httpapi).
- Gói phụ thuộc ngược (go list): `internal/modules/reporting` ← `internal/httpapi`, `cmd/crm`.
- Operation OpenAPI bị chạm, oasdiff: `getSalesDashboard` hiện thực (hợp đồng Task 225 không đổi, oasdiff không chênh);
  xóa dòng `getSalesDashboard` khỏi `tests/arch/testdata/m6_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn M6 là Task 239–241).
- Câu sqlc, bảng, pha migration: không có câu sqlc, không migration. SQL dựng lúc chạy trong
  `dashboard_dynsql.go` (đọc `ord.orders_v`, `ord.order_status_history_v`, `ord.returns_v`, `fulfillment.shipments_v`
  qua `orderSet` của Task 227).
- Test sẽ chạy: `go test -race -run '^TestDashboard' ./internal/modules/reporting/`;
  `go test -race -run '^(TestReportsDashboard.*|TestM6Contract_StatusCodes)$' ./internal/httpapi/`; `./tests/arch/`;
  cả gói ở ci-task (reporting là vùng thường; httpapi chạm handler).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (tiền, làm tròn: không có
  hàm chia làm tròn nửa lên dùng chung cho int64), `reporting.orderSet`/`sqlBuilder`/`anchorSQL` (Task 227),
  `reporting.Period`, `PeriodParams`, `Interactive.Run` (Task 228), `reportseed.NineOrders`, `Bulk` (Task 227),
  `testkit.ExplainAsApp`, `RequireNoSeqScan`.
- Dùng lại / viết mới vì: mọi ô số dựng bằng đúng `orderSet` với `Metric` tương ứng (kế hoạch bắt buộc); điều kiện của
  phiếu khách trả để cộng tiền hoàn lấy lại từ `anchorSQL[anchorCustomerReturn]`, không viết lại. Viết mới: câu gộp
  các tập theo kênh (`dashboardSQL`), câu theo ngày, `avgGoods` (một dòng, chỉ báo cáo dùng; Task 230 và hãng
  dùng chung trong cùng gói khi tới).

## Soát thiết kế

- 5.1–5.3: chỉ đọc view công bố của `ord` và `fulfillment` (`docs/views.md`), không ghi gì. 5.4–5.6, 5.9: không áp
  dụng (chỉ đọc, không ghi, không gọi ra ngoài). 5.7: chạy trong `tenancy.WithTenant` của `Interactive.Run` (transaction
  chỉ đọc `TxReport`). 5.8: module không nhập `net/http`/ogen; handler chỉ đổi kiểu. 5.10, 5.11: không áp dụng. 5.12:
  theo mẫu `orderSet` của Task 227 và khung Task 228.
- 6.1–6.3, 6.6–6.8: không áp dụng (không ghi, không khóa, không việc nền). 6.4: `RetryTx` có thể chạy lại hàm JSON —
  kết quả chỉ trả bằng giá trị, không cộng dồn vào biến ngoài. 6.5: một câu SQL cho mọi ô số → mọi số cùng một ảnh chụp
  (READ COMMITTED, cùng câu); câu theo ngày là câu thứ hai trong cùng transaction chỉ đọc (có thể lệch nếu đơn đổi giữa
  hai câu — chấp nhận, như kế hoạch). 6.9: statement_timeout 30 s của Interactive. 6.10: tiền `int64` đồng, `sum` ép
  `::bigint`, tập rỗng `coalesce(…, 0)`, trung bình làm tròn nửa lên bằng số nguyên (không float). 6.11: không vùng
  cao; quy luật "tổng các dòng kênh = Active", "tổng ByDay = Active/Delivered", "mỗi ô = tập orderSet" có test. 6.12:
  kết quả không có SĐT, địa chỉ, tên khách. 6.13: phạm vi Page qua `DataScope` (không grant → tập rỗng), test người
  chỉ có P1.

## Quyết định thay chủ shop

- **QĐ-229-1 — Ship thu khách của `Active` là 150.000, không phải 120.000.** Bảng ở Step 1 của kế hoạch ghi
  `Active = {7, 1.910.000, ship 120.000}`, nhưng kịch bản chín đơn của Task 227 (`reportseed.NineOrders`, test
  `TestNineOrders` đã qua) có ship 30.000 ở O1, O2, O4, O8, O9 = 150.000 và chú thích "đúng bảng của kế hoạch". Chọn
  150.000 (khớp dữ liệu, khớp câu SQL độc lập của test). Phương án khác: sửa kịch bản cho ra 120.000 — làm đổi số của
  Task 227 đã gộp, không có lý do nghiệp vụ.
- **QĐ-229-2 — `CustomerReturns.Items` là số đơn có phiếu khách trả nhận trong kỳ.** Kế hoạch đòi "mỗi số tương ứng một
  Metric … số đơn = total của danh sách chi tiết cùng metric" (`customer_returned`), nên `Items` = số đơn của tập
  `customer_returned`; `RefundAmount` = tổng `refund_amount` của các phiếu khách trả (không hủy) nhận trong kỳ của các
  đơn đó. Phương án khác: số chiếc trả (`sellable_qty + damaged_qty`) — kịch bản cho cùng số 1 nên không phân biệt
  được; khi màn hình cần số chiếc thì thêm trường mới.
- **QĐ-229-3 — `ByChannel` chỉ có kênh có đơn lên trong kỳ, theo thứ tự facebook, web, manual, unknown; `ByDay` đủ
  mọi ngày của kỳ (ngày không đơn là 0) khi kỳ dài hơn 1 ngày, rỗng `[]` khi 1 ngày.** Kế hoạch: "kỳ 3 ngày → 3 dòng".
- **QĐ-229-4 — Ngày của `ByDay`:** `Active` theo ngày giờ VN của mốc lên đơn, `Delivered` theo ngày giờ VN của mốc giao
  thành công (cùng cơ sở với ô `Delivered`), đổi ngày trong SQL bằng `event_at AT TIME ZONE 'UTC' + 7 giờ` (VN cố định
  UTC+7 như `reporting.VN`, không cần bảng múi giờ của máy chủ).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Kịch bản chín đơn, phạm vi all: Placed {8; 2.110.000; 150.000}, Active {7; 1.910.000; 150.000}, Avg 272.857, Cancelled {1; 200.000}, Delivered {2; 750.000; 60.000}, Returned {1; 250.000; 30.000}, CustomerReturns {1; 150.000}, Pending {1,1,1}, ByChannel facebook {6 / 5; 1.300.000}, web {1; 360.000}, manual {1; 250.000}, tổng kênh = Active; đơn người bán partner và tenant B không lọt | `TestDashboard_NineOrders` | `go test -race -count=1 -run '^(TestDashboard_(NineOrders\|EmptyDay\|ByDay\|AvgRoundHalfUp\|JSONShape)\|TestOrderSet_NineOrders)$' ./internal/modules/reporting/` | `ok elimaz.com.vn/crm/internal/modules/reporting 4.487s` |
| Mỗi ô (9 Metric) = số đơn của `orderSet` cùng Metric = câu đếm độc lập bằng role chủ bảng trên bảng gốc `ord.orders`, `ord.order_status_history`, `ord.returns` | `TestDashboard_NineOrders` (`checkCells`) | như trên | ok |
| Người chỉ có `report.sales.read` trên P1: Placed 4, Active {4; 1.150.000}, Pending {0,0,1}; không grant nào → Placed, Active, Delivered, Returned, CustomerReturns, Pending đều 0, ByChannel rỗng, Avg nil | `TestDashboard_NineOrders` (`checkScoped`) | `go test -race -count=1 -run '^TestDashboard_NineOrders$' ./internal/modules/reporting/` (sau review lần 1) | `ok elimaz.com.vn/crm/internal/modules/reporting 3.703s` |
| Ngày không có đơn: mọi số 0, `AvgGoodsAmount = nil`, `ByChannel = []`; chờ xử lý vẫn là số hiện tại | `TestDashboard_EmptyDay` | như trên | ok |
| Kỳ 3 ngày: `ByDay` 3 dòng (ngày không đơn = 0), tổng = Active và Delivered, mốc sát biên 00:00 giờ VN; kỳ 1 ngày: `ByDay = []` | `TestDashboard_ByDay`, `TestDashboard_NineOrders` | như trên | ok |
| Trung bình làm tròn nửa lên bằng số nguyên; JSON đúng 13 khóa của `SalesDashboardReport`, kỳ `{from,to}`, `avgGoodsAmount: null` | `TestDashboard_AvgRoundHalfUp`, `TestDashboard_JSONShape` | như trên | ok |
| EXPLAIN (thiết lập TxReport) hai câu 31 ngày trên `Bulk(100000)`, phạm vi all: không Seq Scan | `TestDashboard_ExplainNoSeqScan` | `go test -race -count=1 -run '^TestDashboard_ExplainNoSeqScan$' ./internal/modules/reporting/` | `ok elimaz.com.vn/crm/internal/modules/reporting 131.402s` |
| Tách `orderSetSelect` khỏi `orderSet` không đổi hành vi: các test của Task 227 qua nguyên vẹn (kể cả EXPLAIN, tham số sai không gửi câu nào) | `TestOrderSet_*` | `go test -race -count=1 -run '^(TestDashboard_(NineOrders\|EmptyDay\|ByDay)\|TestOrderSet_.*)$' ./internal/modules/reporting/` | `ok elimaz.com.vn/crm/internal/modules/reporting 43.352s` |
| HTTP 200 đúng hình dạng hợp đồng, tiền số nguyên JSON, người P1, ngày rỗng → `avgGoodsAmount: null`, kỳ 3 ngày → `byDay` 3 dòng; `getSalesDashboard` không còn 501 (đã xóa khỏi `m6_pending.txt`) | `TestReportsDashboard_Ready`, `TestM6Contract_StatusCodes`, `TestReportsDashboard_RunInBackground` | `go test -race -count=1 -run '^(TestReportsDashboard_Ready\|TestReportsDashboard_RunInBackground\|TestM6Contract_StatusCodes)$' ./internal/httpapi/` | `ok elimaz.com.vn/crm/internal/httpapi 6.044s` |
| Luật kiến trúc SQL (schema, CTE, người bán, dynsql, M6) | `TestSQLUsage*`, `TestSellerFilter*`, `TestDynSQL*`, `TestSQLMode*`, `TestM6Contract_*` | `go test -count=1 -run '^(TestM6Contract_Operations\|…\|TestSQLModeProductCode)$' ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 1.253s` |
| Cổng tĩnh tự soát | — | `golangci-lint run ./internal/modules/reporting/... ./internal/httpapi/`; `make jscpd` | `0 issues.`; `Found 0 clones.` |
| Cổng kiểm của task (cổng tĩnh đủ như make ci, test -race reporting + httpapi, test thường gói phụ thuộc) | — | `scripts/agent/ci-task.sh` | `CI-TASK XANH (từ xa trên github …)`, run 45976893813: static success, test (1)–(4) success |

## E2E viết sẵn, chưa chạy

(không có — kế hoạch Task 229 không có test e2e)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Ship của `Active` 150.000 thay 120.000 (QĐ-229-1).
- Hai commit: (1) dọn code — tách thân SELECT của `orderSet` thành `orderSetSelect` (Tidy First; `orderSet` trả đúng
  chuỗi cũ). Lý do: luật kiến trúc `TestSQLUsageProductCode` nhận tên CTE theo từng chuỗi, nên chuỗi có
  `FROM order_set` phải tự chứa `WITH order_set AS (`; câu tổng quan dùng mẫu `(WITH order_set AS (%s) …)` với thân từ
  `orderSetSelect`. (2) tổng quan.
- Hai hàm giúp test `outsiders`, `bulk` (dashboard_dynsql_test.go) dùng chung cho `TestOrderSet_NineOrders`,
  `TestOrderSet_ExplainNoSeqScan` (jscpd báo trùng); hai test cũ giữ nguyên dữ liệu và khẳng định.
- EXPLAIN chạy với đúng thiết lập của `TxReport` (không song song, work_mem 32MB) và chỉ phạm vi all — xem Việc phát
  hiện thêm.

## Việc phát hiện thêm

- **EXPLAIN phạm vi một Page, nhánh giao thành công/hoàn (cơ sở completed) của `orderSet` (Task 227):** với thiết lập
  `TxReport` (không song song, work_mem 32MB) trên `Bulk(100000)`, planner chọn `Hash Join` + `Seq Scan on orders`
  (lọc `channel_account_id = ANY(…)`) thay cho Nested Loop qua `orders_pkey`, vì hai chi phí sát nhau (≈13,6k so với
  ≈14,3k; mặc định `random_page_cost = 4`). Phạm vi all, và phạm vi Page với cơ sở placed, không Seq Scan. Không sai
  số liệu, chỉ là tốc độ khi bảng đơn rất lớn. Hướng xử lý cho điều phối viên/Task 230 chọn: đặt
  `random_page_cost = 1.1` trong `TxReport` (dữ liệu nằm trong RAM/SSD), hoặc chỉ mục `ord.orders (tenant_id,
  channel_account_id)`. Task 230 (EXPLAIN mỗi chiều nhóm, cơ sở completed) sẽ gặp cùng điều này.
- Nhánh "chờ gửi hãng" (số hiện tại, không theo kỳ) trên `Bulk` ra Seq Scan khi EXPLAIN chạy với song song bật (mặc
  định của kết nối thử, khác `TxReport`): `Bulk` để 12,5% đơn ở `confirmed` không vận đơn suốt 2 năm, khác dữ liệu thật.

## Reviewer

Commit 1 (dọn code, `orders_dynsql.go`: tách `orderSetSelect`) — Kết luận: ĐẠT. Không phát hiện phải sửa; reviewer đối
chiếu ba nhánh (không `outer`, có `outer`, lỗi) cho thấy `orderSet` trả đúng chuỗi cũ, thứ tự tham số không đổi. Ghi chú
không bắt buộc: lệnh của dòng bằng chứng chạy trên working tree có `orders_dynsql_test.go` đã sửa (thuộc commit 2).

Commit 2 (tổng quan) — lần 1: CẦN SỬA, chỉ ở tệp task (tên `dashboardStatement`, `roundHalfUp` trong mục Tái dùng không
có trong mã; `StatusReady` xếp sai nhóm; thiếu `DashboardReport.ReportRows()` trong Ảnh hưởng); ghi chú không bắt buộc:
ca không grant của `checkScoped` chưa khẳng định Delivered, Returned, CustomerReturns. Đã sửa cả hai, chạy lại
`TestDashboard_NineOrders`. Lần 2: **Kết luận: ĐẠT**, không phát hiện phải sửa. Ghi chú: EXPLAIN mới kiểm phạm vi all
(phạm vi Page ở Việc phát hiện thêm); `period.go` ngoài danh sách tệp của M6-06 — lưu ý xung đột khi gộp.
