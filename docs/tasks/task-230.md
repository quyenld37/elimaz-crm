# Task 230 — Báo cáo đơn theo trạng thái, nguồn, nhân viên, kênh, Page, hãng; danh sách chi tiết (BC02)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 230 = M6-07). Loại việc (quy trình mục 3): A

Điều kiện "M1–M5 đóng" của M6-00 được chủ shop miễn ngày 27/11 sáng (cho bắt đầu M6 dù M5 chưa đóng, 221 chờ staging).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences):
  - Commit cấu trúc (QĐ-223-4): `queries.orderLabelCond` (không xuất, 1 chỗ gọi: `orderListSQL` trong
    `internal/queries/orders_dynsql.go`) đổi tên thành `queries.OrderLabelCond`, không đổi thân hàm. Không ai ngoài gói
    đang dùng.
  - `reporting.OrdersParams`, `reporting.DrilldownParams` (tệp giữ chỗ của Task 228): thêm trường. Chỗ dùng:
    `registry.go` (`newDef`), `registry_test.go`, `interactive_test.go` (chỉ dùng `DashboardParams`), handler
    `handlers_reports_orders.go` (đang trả 501).
  - Mới trong `reporting`: `OrdersRow`, `OrdersReport`, `DrillCursor`, `ParseDrillCursor`, `DrillItem`, `DrillPage`,
    `Drilldown`, `FilterOptions`, `FilterOption`, `Filters`, `(*Interactive).Filters`, `ErrPageForbidden`.
  - `reporting.cellCond` (không xuất): chiều `status` đổi từ trạng thái thô sang nhãn gộp (`orderview.LabelKey`) — đúng
    ghi chú của Task 227 ("Task 230 đổi sang nhãn gộp"). Bí danh vận đơn hiện hành trong `orderSet` đổi `sh` → `s`
    để dùng nguyên văn `queries.OrderLabelCond` (đoạn SQL gắn bí danh `o.`, `s.status`). Chỗ dùng: `orders_dynsql.go`,
    `orders_dynsql_test.go` (một dòng ô "trạng thái thô confirmed" đổi thành ô nhãn). Task 229 (song song) gọi
    `orderSet` qua cột của CTE (`carrier_key`, `ship_status`), không nhắc bí danh bên trong → không ảnh hưởng.
  - `httpapi.reportParamError` (không xuất) → trả thêm mã: tham số ngày → `bad_date` như cũ, tham số query khác của
    route báo cáo (enum `groupBy`, `metric`, `limit`…) → `bad_params` thay cho `bad_request` chung. Một chỗ gọi:
    `errors.go` `writeOgenError`.
- Gói phụ thuộc ngược (go list): `internal/queries` ← `internal/httpapi`, `cmd/crm` (qua httpapi);
  `internal/modules/reporting` ← `internal/httpapi`, `cmd/crm`. Không gói nào khác.
- Operation OpenAPI bị chạm, oasdiff: hiện thực `getSalesOrdersReport`, `listSalesOrdersDrilldown`,
  `getSalesReportFilters` (xóa ba dòng khỏi `tests/arch/testdata/m6_pending.txt`). Không đổi `openapi.yaml` (hợp đồng
  của Task 225 đủ: mã `forbidden`, `bad_params` đã có trong `x-error-codes`) → oasdiff không chênh, không `make gen`.
- Chỗ dùng trên màn hình: chưa có (màn Báo cáo đơn là Task 240). `console/src/api` không đổi.
- Câu sqlc, bảng, pha migration: không câu sqlc mới, không migration. SQL dựng lúc chạy trong
  `orders_dynsql.go`, `filters_dynsql.go` (đọc view: `ord.orders_v`, `ord.order_status_history_v`,
  `ord.traffic_sources_v`, `fulfillment.shipments_v`, `fulfillment.carrier_accounts_v`, `channel.channel_accounts_v`,
  `customer.customers_v` (chỉ `code`), `identity.users_v`) — đều đã trong `viewReadExceptions["reporting"]`/`moduleCalls`.
- Test sẽ chạy: `go test -race -run` từng test mới/sửa của `./internal/modules/reporting/`
  (`TestOrdersReport_*`, `TestOrdersProperty`, `TestDrilldown_*`, `TestFilters_*`, `TestStatusLabelCond_*`,
  `TestOrderSet_*`), `./internal/queries/` (`TestOrderList*` chạy lại nguyên vẹn sau đổi tên),
  `./internal/httpapi/` chỉ `TestReportsOrders_*`, `TestM6Contract*` của `./tests/arch/`, `go test ./tests/arch/`;
  cả gói ở `scripts/agent/ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (con trỏ, làm tròn, nhãn
  đơn); `internal/queries/orderview` (`Label`, `Labels`, `Axes`); `internal/queries/orders_dynsql.go`
  (`orderLabelCond`, `shipmentLabelStatuses`); con trỏ keyset của `order.DuplicateCursor`, `order.ReturnCursor`,
  `queries.BalanceCursor`; `reporting.orderSet`, `sqlBuilder`, `cellCond`, `ParseDimension`/`ParseMetric`/`ParseBasis`,
  `Interactive.Run` (Task 227, 228); `httpapi.runReport`, `reportDay`; `reportseed.NineOrders`.
- Dùng lại / viết mới vì:
  - Dùng lại `orderSet` cho mọi số của bảng, mọi ô và danh sách chi tiết (Global Constraints "một hàm dựng điều kiện").
  - Nhãn trạng thái: nhóm SQL theo bốn trục, Go gọi `orderview.Label`; chiều ngược (ô được bấm) dùng
    `queries.OrderLabelCond` (xuất trong commit cấu trúc riêng, QĐ-223-4) — không viết bảng ngược thứ hai. Test trên
    PostgreSQL thật đối chiếu hai hướng trên đủ miền 7 × 4 × 3 × 10 bộ trục.
  - Con trỏ danh sách chi tiết viết mới trong `reporting` theo đúng mẫu `DuplicateCursor` (base64url của chuỗi chấm):
    mỗi module giữ con trỏ của mình, không có gói chung; thêm trường `Total`.
  - Không có hàm làm tròn nửa lên dùng chung trong `internal/platform`; viết hàm nhỏ trong `reporting`
    (`avgHalfUp`). Task 229 (song song) có thể viết hàm tương tự trong `dashboard*.go`: khi gộp, nếu trùng thì giữ một
    (ghi ở "Việc phát hiện thêm").
  - Tên hiển thị của hãng, nhãn trạng thái tiếng Việt: không có bảng Go dùng chung (`fulfillment/internal/shipstate`
    nội bộ, `reporting` không nhập `fulfillment`); viết bảng hằng nhỏ trong `labels.go` của `reporting`, khóa vẫn là
    `LabelKey`/`carrier_key` để màn hình tự đổi nếu cần.

## Soát thiết kế

Khớp kiến trúc (5.x):
- 5.1, 5.2: không bảng mới, không ghi gì (báo cáo chỉ đọc, transaction `transaction_read_only`).
- 5.3: chỉ đọc view `_v` đã công bố (danh sách ở mục Ảnh hưởng; test kiến trúc `sqlusage_test`).
- 5.4–5.6: không áp dụng: không ghi, không gọi ra ngoài.
- 5.7: mọi câu chạy trong `tenancy.WithTenant` (qua `Interactive.Run`/`Interactive.Filters`); test tenant B không thấy.
- 5.8: module không nhập `net/http`, ogen; handler mỏng đổi kiểu.
- 5.9: không áp dụng: chỉ GET; quyền `report.sales.read` đã khai ở hợp đồng.
- 5.10, 5.11: không áp dụng (không Valkey, không thư viện mới).
- 5.12: theo mẫu đường đọc (identity) và Task 227/228: sentinel trong `errors.go`, map ở `errors_reports.go`.

Đúng khi chạy đồng thời (6.x):
- 6.1–6.3: không áp dụng: không ghi, không khóa.
- 6.4: `RetryTx` có thể chạy lại hàm: hàm JSON trả kết quả bằng giá trị trả về, gán lại mọi biến trong thân.
- 6.5: READ COMMITTED, các số của một báo cáo lấy trong **một câu** (một ảnh chụp), nên tổng = các dòng trong cùng
  câu. Danh sách chi tiết chạy ở câu khác: giữa hai lần bấm dữ liệu có thể đổi (chấp nhận, như Nhanh; property test chạy
  trên dữ liệu đứng yên).
- 6.6–6.8: không áp dụng (không việc nền ở task này).
- 6.9: chạy trong khung 2 chỗ, 30 s của Task 228; `Filters` không lấy chỗ nhưng chạy cùng `TxReport` 30 s.
- 6.10: tiền `int64`, tỷ lệ `int32` phần vạn làm tròn xuống, trung bình làm tròn nửa lên bằng số nguyên; tổng tiền
  `sum` trên tập rỗng dùng `coalesce(…, 0)`.
- 6.11: test thuộc tính rapid 200 lượt (`orders_property_test.go`): tổng các dòng = Totals, ô = Total của danh sách
  chi tiết, tổng tiền của danh sách chi tiết = cột tiền, không đơn nào ở hai dòng của cùng một cột.
- 6.12: kết quả và danh sách chi tiết không có SĐT, địa chỉ, tên khách (chỉ mã đơn, mã khách); test so khóa JSON.
  Span chỉ có `report.key`, `report.days`, `report.rows`.
- 6.13: phạm vi Page theo `DataScope` (Task 227); lọc `pageId` ngoài phạm vi → 403 `forbidden` (hợp đồng); đơn không
  gắn Page chỉ người phạm vi `all` thấy; `Filters` chỉ trả Page, nguồn, nhân viên trong phạm vi.

## Quyết định thay chủ shop

| # | Chỗ chưa rõ | Chọn | Vì sao | Phương án khác |
|---|---|---|---|---|
| QĐ-230-1 | Cơ sở `completed`: đơn vừa có dòng "giao xong" vừa có dòng "hoàn" trong kỳ (hãng sửa "đã giao" → hoàn, M3-06) | Cột `Delivered`, `Returned` theo đúng tập của Task 227 (theo dòng sự kiện trong kỳ); `Orders` = `Delivered` + `Returned` (số lượt hoàn tất), `GoodsAmount`, `ShippingFeeCharged` của tập giao xong | Tổng các dòng luôn bằng Totals và mỗi ô bằng danh sách chi tiết của cột đó; trường hợp hai sự kiện rất hiếm | Đếm đơn không trùng: Totals có thể khác tổng các dòng khi nhóm theo ngày |
| QĐ-230-2 | Cơ sở `placed`: phí ship tính trên đơn nào | `ShippingFeeCharged` của đơn lên chưa hủy (cùng tập với `GoodsAmount`) | Khớp mockup Task 224 (An 120.000, tổng 150.000) | Tính cả đơn hủy |
| QĐ-230-3 | Nhãn dòng (`label`) | Tiếng Việt: nhãn trạng thái theo chú thích của `orderview`, hãng (`vtp` Viettel Post, `best` BEST Express, `spx` SPX Express, `self` Tự giao, khóa khác giữ nguyên), kênh (Facebook, Website, Nhập tay), ngày `dd/MM/yyyy`; nhóm không xác định: "Chưa phân công" (phụ trách), "Chưa gửi hãng" (hãng), "Không xác định" (còn lại); tên người, nguồn, Page lấy từ view | Đúng chữ của mockup; khóa (`key`) vẫn là mã máy để màn hình đổi được | Trả khóa, màn hình tự dịch |
| QĐ-230-4 | Tham số query sai (enum `groupBy`, `metric`, `limit` ngoài miền) | 400 `bad_params` cho mọi route `/api/v1/reports/` (ngày vẫn `bad_date`) | Kế hoạch đòi `groupBy=foo` → 400 `bad_params`; hợp đồng: "trường lạ hay sai → 400 bad_params" | Giữ `bad_request` chung |
| QĐ-230-5 | `Filters` của người phạm vi Page: nhân viên nào | Nhân viên `active` cộng người từng phụ trách/tạo đơn trong phạm vi của mình trong 366 ngày; nguồn: chỉ nguồn từng gắn đơn trong phạm vi 366 ngày (người `all`: thêm mọi nguồn `active`) | Đúng chữ kế hoạch; tên nhân viên không phải dữ liệu khách | Chỉ người từng phụ trách đơn trong phạm vi |
| QĐ-230-6 | `placedAt`, `completedAt` của một dòng danh sách chi tiết | `placedAt` = mốc lên đơn (dòng lịch sử lên đơn sớm nhất); `completedAt` = dòng giao xong/hoàn muộn nhất nếu có; sắp xếp keyset theo mốc của cột (`event_at`) giảm dần | Màn hình hiện "Lên lúc" cho mọi cột; mốc keyset là mốc của tập | Chỉ trả mốc của cột |

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit cấu trúc: đổi tên `orderLabelCond` → `OrderLabelCond`, thân hàm giữ nguyên, danh sách đơn của M2 lọc nhãn như cũ | `TestOrderListPlans` (gồm ca lọc nhãn chờ khách, đang chuyển, đang gửi hãng) | `go test -count=1 -run 'TestOrderList' ./internal/queries/` | `ok elimaz.com.vn/crm/internal/queries 58.877s` |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch Task 230 không có test e2e)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Commit cấu trúc (đổi tên `OrderLabelCond`): **ĐẠT** lần 1 — không đổi hành vi, chỗ gọi duy nhất đã đổi, tên cũ chỉ còn
trong tài liệu lịch sử của task trước; bằng chứng `TestOrderListPlans` khớp.
