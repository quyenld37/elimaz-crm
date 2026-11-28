# Task 242 — Kịch bản đơn thử qua API thật: số báo cáo khớp số đếm (M6-19)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 242 = M6-19). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu nào của mã sản phẩm. Chỉ thêm tệp test trong `tests/e2e` (gói `e2e`, không ai nhập) và thêm tùy chọn `m6` vào `stackOptions` ở `tests/e2e/stack_test.go` (gồm một nhánh nối `reporting.JobHandlers/TxHandlers` vào worker của stack và `Deps.Reports` mặc định cho api; không ai khác dùng `m6`).
- Gói phụ thuộc ngược (go list): không có (gói `tests/e2e` là gói test cuối). Phụ thuộc mới của test: `internal/modules/reporting`, `httpapi.ReportResultJSON`, `excelize` (đọc lại tệp xuất), `testkit.Staff`.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Test gọi: createOrder, confirmOrder, cancelOrder, createShipment, cancelShipment, createReturn/receiveReturn (qua phiếu hoàn của hãng), listOrders, getSalesDashboard, getSalesOrdersReport, listSalesOrdersDrilldown, getSalesCarriersReport, getSalesBestSellers, getInventoryStock, createSalesReportRun, getSalesReportRun, getSalesReportRunFile.
- Chỗ dùng trên màn hình: không (không đổi console).
- Câu sqlc, bảng, pha migration: không câu sqlc, không migration. Câu đếm độc lập của test (chủ bảng, trong `tenancy.WithTenant`) đọc bảng gốc `ord.orders`, `ord.order_status_history`, `ord.order_items`, `fulfillment.shipments`, `inventory.stock_balances`.
- Test sẽ chạy: `go build ./... && go vet ./tests/e2e/` trên máy; bản thân `TestReportsGolden*` là e2e, theo luật chủ shop 24/11 và 28/11 **không chạy trong task** (điều phối chạy sau khi gộp main). Phần so số của kịch bản chín đơn ở mức module/HTTP đã có trong test của Task 229–232 (`TestDashboard…`, `TestOrdersReport…`, `TestCarriersReport…`, `TestBestSellers…`).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: đọc `docs/danh-muc-dung-chung.md` (không có gì cho test e2e); tìm trong `tests/e2e`: `orderRun`, `newM3Run`, `mustCall`, `productWithStock`, `pickedUp`, `advance`, `awaitingTracking`, `receiveReturn`, `fakeMode`, `waitFor`, `inTenant`, `count`; `testkit.Staff.User/Grant`; `files.MemStore` (có `ServeHTTP`, `SetBaseURL`); `internal/platform/xlsx` (đọc lại tệp bằng `excelize` trực tiếp, vì `xlsx.Read` đòi tiêu đề cố định của mẫu nhập).
- Dùng lại / viết mới vì: dùng lại toàn bộ `orderRun` và bộ đẩy hãng giả. Viết mới: `reportsRun` (bọc `orderRun`, thêm sản phẩm kịch bản, chín đơn, gọi GET bằng thẻ khác), vì không có sẵn bộ dựng chín đơn qua API (`reportseed` chèn thẳng bảng, đúng chỗ của nó ở test module, không dùng ở đây: kế hoạch đòi dữ liệu đi qua API thật).

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

5.1–5.12 và 6.1–6.13: không áp dụng cho mã sản phẩm vì task chỉ thêm test; những điểm có liên quan tới test:
- Không ghi bảng ngoài đường API, trừ ba chỗ chủ bảng ghi thẳng (chỉ trong test, trong `tenancy.WithTenant`): tạo hai Page (`channel.channel_accounts`), gán `ord.orders.channel_account_id` cho đơn của Page (API chỉ gán Page qua hội thoại M4, quá nặng cho kịch bản này), và tạo người chỉ có quyền Page P1 (`testkit.Staff.User/Grant`).
- Đúng khi chạy đồng thời: test chờ theo sự kiện (`waitFor`), không ngủ; mỗi test một stack và database riêng; không `t.Parallel` (stack đặt tracer provider chung, Task 484).
- Cổng chờ báo cáo: kỳ là ngày hôm nay giờ VN lấy một lần lúc bắt đầu; nếu test chạy qua nửa đêm giờ VN thì đỏ (chấp nhận, ghi ở Khác kế hoạch).
- Báo cáo đọc view; câu đếm độc lập đọc bảng gốc (lớp (b)); không có SQL mới của sản phẩm, không cần EXPLAIN.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| (a) số năm báo cáo = golden viết tay; người chỉ có P1 thấy 4 đơn | `TestReportsGolden` | `go vet ./tests/e2e/`; ci-task (github run 46141252093) | vet sạch; cổng tĩnh, lint, test kiến trúc XANH; e2e chạy thật do điều phối |
| (b) số = câu đếm độc lập trên bảng gốc; (c) số chờ = `listOrders`, ô = total của drilldown; tồn A-S = bảng số dư | `TestReportsGolden_MatchIndependentCounts` | như trên | như trên |
| Xuất Excel → worker → tải → đọc lại = JSON | `TestReportsGolden_ExcelExportMatchesJSON` | như trên | như trên |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Chiều hãng: hãng giả chỉ có khóa `fake`, nên golden ghi `fake`/`unknown` thay cho VTP/SPX/Best; phí vận đơn không ghi cứng (hãng giả tự báo), so với câu đếm độc lập.
- Page và người chỉ có P1 do chủ bảng dựng (API chỉ gán Page qua hội thoại M4); O8 giữ ở `cancel_requested` bằng chế độ `rate_limited` của thao tác hủy ở hãng giả.
- Tỷ lệ thành công 2/3 = 6666 (làm tròn xuống theo hợp đồng, điều phối 27/11), không 6667 như bảng kế hoạch.
- Kỳ là ngày hôm nay giờ VN; chạy qua nửa đêm thì đỏ.
- Đủ ca ở mức module/HTTP nằm sẵn trong test Task 229–232; phần e2e này chưa chạy (luật chủ shop).

## E2E viết sẵn, chưa chạy

`TestReportsGolden`, `TestReportsGolden_MatchIndependentCounts`, `TestReportsGolden_ExcelExportMatchesJSON` (tests/e2e/reports_golden_test.go; thêm `stackOptions.m6`). Cột/bảng giả định chưa kiểm bằng chạy: `ord.return_items.return_id`, `fulfillment.shipments.cod_amount/fee/fee_actual`, tham số `limit`/`label` của listOrders.

## Quyết định thay chủ shop

- Điều kiện "M1–M5 đóng" miễn theo ghi chú điều phối 27/11.
- Golden dùng 6666 (xem trên); không chạy e2e theo luật 24/11, 28/11.

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (reviewer, bản đã gộp main). Không có phát hiện ảnh hưởng tính đúng; đối chiếu tay chín đơn với golden khớp. Các giả định chưa chạy nằm ở mục E2E; RegisterFilePurposes trong startAPI là idempotent.
