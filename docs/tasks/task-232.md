# Task 232 — Sản phẩm bán chạy theo SKU và theo mẫu

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 232 = M6-09). Loại việc (quy trình mục 3): A

Điều phối M6 (27/11): chủ shop duyệt kế hoạch M6 sáng 27/11 và cho bắt đầu dù M5 chưa đóng mốc (Task 221 còn chờ
staging); điều kiện "M1–M5 đóng" của M6-00 coi như được miễn.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): `reporting.BestSellersParams` (giữ chỗ của Task 228, 0 chỗ
  tham chiếu ngoài gói; trong gói: `bestSellersDef`) thêm trường `Level`, `Basis`, `Limit` (JSON `level`, `basis`,
  `limit`, bỏ trống được), giữ nhúng `PeriodParams`. Thêm mới: `BestSellerRow`, `BestSellersReport`
  (+`ReportRows`), hằng `LevelSKU`, `LevelProduct`, `SalesBasisPlaced`, `SalesBasisDelivered`,
  `DefaultBestSellersLimit`, `MaxBestSellersLimit`. `bestSellersDef()` gán `JSON`. Handler
  `(*handler).GetSalesBestSellers` thay thân (chữ ký ogen không đổi).
- Gói phụ thuộc ngược (go list): `reporting` được `internal/httpapi`, `cmd/crm` nhập.
- Operation OpenAPI bị chạm, oasdiff: không đổi hợp đồng. Hiện thực `getSalesBestSellers` (Task 225 viết sẵn), xóa
  dòng `getSalesBestSellers` khỏi `tests/arch/testdata/m6_pending.txt`.
- Chỗ dùng trên màn hình: chưa có (màn bán chạy là Task 240/M6-17).
- Hằng mới không xuất `orderSetName` ("order_set") trong `orders_dynsql.go`: `orderSet` (Task 227) dựng tên CTE bằng
  hằng này, câu bán chạy nhắc tên qua hằng (luật `TestSQLUsageProductCode` soát từng chuỗi; CTE khai ở chuỗi khác).
  Không đổi hành vi `orderSet` (test `TestOrderSet*` chạy lại).
- Câu sqlc, bảng, pha migration: không sqlc mới, không migration. Câu dựng lúc chạy mới trong
  `bestsellers_dynsql.go` (một chuỗi hằng `bestSellersTemplate`, thay chỗ bằng `strings.NewReplacer`; chỉ phần hằng và
  `$n`) (đọc view `ord.order_items_v`, `ord.returns_v`, `ord.return_items_v`, `ord.orders_v`,
  `catalog.variants_v`, `catalog.products_v`, cùng CTE `order_set` của Task 227); chỉ mục dùng: `order_items_position_key`
  (tenant, seller, order, …), `returns_received` (0603), `return_items_item_key`.
- Test sẽ chạy: `go test -race -count=1 -run '^TestBestSellers' ./internal/modules/reporting/`;
  `go test -race -count=1 -run '^(TestReportsBestSellers.*|TestM6Contract_StatusCodes)$' ./internal/httpapi/`;
  `go test -count=1 ./tests/arch/`; phần còn lại (cả gói reporting, httpapi) ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md`; trong gói
  `reporting`: `orderSet` + `sqlBuilder` (tập đơn dùng chung, Task 227), `filterConds` (điều kiện phạm vi Page),
  `PeriodParams`, `Def`/`newDef` (`checkEnum` đã xem; mức và cơ sở có giá trị mặc định khi bỏ trống nên `Validate` so trực tiếp); httpapi `runReport`, `reportDay`; testkit `reportseed.NineOrders`,
  `reportseed.Bulk`, `ExplainAsApp`, `RequireNoSeqScan`.
- Dùng lại / viết mới vì: tập đơn lấy bằng `orderSet` (placed → `MetricPlacedActive` cơ sở placed; delivered →
  `MetricDelivered` cơ sở completed), không tự viết điều kiện đơn; điều kiện phạm vi Page của phần hàng khách trả lấy
  bằng `filterConds(s, OrderFilter{}, b)` (cùng luật với tập đơn). Câu gộp theo biến thể/mẫu viết mới: chưa báo cáo
  nào gộp theo dòng hàng.

## Soát thiết kế

- 5.1 Chủ dữ liệu: không bảng mới; `reporting` chỉ đọc.
- 5.2 Không ghi bảng module khác: chỉ SELECT, transaction `TxReport` chỉ đọc (Task 228).
- 5.3 Đọc qua view: chỉ đọc `ord.*_v`, `catalog.*_v` (G3, G8 của kế hoạch), không đọc bảng gốc.
- 5.4–5.6 Sau commit, gọi ra ngoài: không áp dụng (đọc).
- 5.7 Ngữ cảnh shop: chạy trong `tenancy.WithTenant` của `Interactive.Run`; mọi lần nhắc đơn, dòng đơn, biến thể, mẫu
  có `seller_id` = người bán own (ADR-29), phần hàng trả nối `orders_v` để lọc người bán và Page.
- 5.8 Không HTTP trong module: đúng; handler mỏng đổi kết quả sang `gen.SalesBestSellersReport`.
- 5.9 Ghi: không áp dụng (GET). 5.10 Valkey: không. 5.11 Thư viện mới: không.
- 5.12 Module mẫu: lỗi là sentinel có sẵn (`ErrBadParams`, ngày); SQL dựng lúc chạy chỉ trong `*_dynsql.go`, giá trị
  người dùng chỉ qua `$n`; level chọn giữa hai câu hằng; test trên PostgreSQL thật.
- 6.1–6.4 Khóa, deadlock: không khóa (chỉ đọc).
- 6.5 Một câu SQL duy nhất cho cả dòng và tổng số dòng (`count(*) OVER ()`) nên cùng một ảnh chụp.
- 6.6 Chạy lại: hàm JSON trả kết quả bằng giá trị, không biến ngoài.
- 6.7, 6.8 Hàng đợi: không áp dụng.
- 6.9 Treo lâu: kỳ tối đa 366 ngày, `Limit` ≤ 500, khung 228 lo hạn giờ, chỗ; EXPLAIN không Seq Scan (test).
- 6.10 Tiền: `GoodsAmount` = Σ `net_amount` (int64 đồng, `sum(...)::bigint`); tập rỗng không ra dòng (không NULL).
- 6.11 Quy luật: không áp dụng rapid; các ca số liệu chín đơn kiểm trực tiếp.
- 6.12 Dữ liệu cá nhân: không đọc SĐT, địa chỉ, tên khách; span chỉ có khóa, số ngày, số dòng.
- 6.13 Quyền: `x-permission: report.sales.read` chặn ở guard; phạm vi Page áp cho cả tập đơn và phần hàng trả (người
  chỉ xem Page P1 không thấy hàng trả của đơn Page P2); người bán thứ hai không xuất hiện (test).

## Quyết định thay chủ shop

- QĐ-232-1 Cơ sở `delivered` = đơn giao thành công **trong kỳ** (mốc dòng lịch sử `delivered`, cơ sở completed của
  `orderSet`), theo Q6 "theo đơn thành công trong kỳ". Khác: đơn lên trong kỳ đang ở trạng thái delivered. Đảo: đổi một
  dòng chọn quy tắc trong `bestSellersSQL`.
- QĐ-232-2 Hàng khách trả lọc theo người bán own và phạm vi Page của người xem (qua đơn của phiếu), không theo tập
  đơn (Q6). Khác: không lọc Page (lộ hàng trả của Page không được xem). Đảo: bỏ điều kiện Page trong câu.
- QĐ-232-3 Biến thể (mẫu) chỉ có hàng trả trong kỳ mà không bán trong kỳ vẫn hiện một dòng với số lượng 0, để cột
  hàng trả không bị mất số. Khác: chỉ hiện dòng có bán. Đảo: lọc `quantity > 0`.
- QĐ-232-4 `limit` 501 ở HTTP do ogen chặn theo hợp đồng (`maximum: 500`) → 400 `bad_request`; ở module (tham số lần
  chạy nền) `Limit` ngoài 0–500 → `ErrBadParams` (400 `bad_params`). Không sửa `errors_reports.go` của khung.
  `Limit` 0 (bỏ trống) = 50.
- QĐ-232-5 Phần xuất tệp (`Def.Rows`, cột XLSX) để Task 237 (cột ở `columns.go`); task này để sẵn hàm đọc với
  `limit <= 0` = mọi dòng cho tệp xuất.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Mức SKU, cơ sở placed: số lượng, số đơn, tiền hàng, hàng trả theo ngày nhận (thiếu không tính, phiếu chưa nhận không tính), người bán thứ hai không hiện; mặc định sku/placed | `TestBestSellers_SKUPlaced` | `go test -race -count=1 -run '^(TestBestSellers.*\|TestOrderSet.*)$' ./internal/modules/reporting/` | `ok elimaz.com.vn/crm/internal/modules/reporting 37.878s` |
| Mức mẫu, sắp theo SL rồi tiền; limit 2 → 2 dòng, TotalRows 3 | `TestBestSellers_ProductPlaced` | như trên | ok |
| Cơ sở delivered = giao thành công trong kỳ (QĐ-232-1), dòng SL 0 có hàng trả (QĐ-232-3) | `TestBestSellers_Delivered` | như trên | ok |
| Phạm vi Page cho cả đơn và hàng trả (QĐ-232-2); không Page → 0 dòng | `TestBestSellers_PageScope` | như trên | ok |
| Tham số sai → ErrBadParams / ErrBadDate; DecodeParams limit 501 | `TestBestSellers_BadParams` | như trên | ok |
| EXPLAIN 100.000 đơn (2 mức, 2 cơ sở, Page): không Seq Scan | `TestBestSellers_ExplainNoSeqScan` | như trên | ok |
| HTTP: mặc định, product limit 2, delivered; limit 501/0, level/basis lạ → 400 bad_request; from > to → bad_date; chỉ quyền tồn → 403; hết 501 | `TestReportsBestSellers`, `TestM6Contract_StatusCodes` | `go test -race -count=1 -run '^(TestReportsBestSellers\|TestM6Contract_StatusCodes)$' ./internal/httpapi/` | `--- PASS: TestReportsBestSellers (0.52s)`, `--- PASS: TestM6Contract_StatusCodes (0.55s)` |
| Luật kiến trúc (SQL dựng lúc chạy chỉ ở `*_dynsql.go`, có `_dynsql_test.go`, schema/CTE) | `TestDynSQLProductCode`, `TestSQLUsageProductCode`, cả `tests/arch` trừ Lint | `go test -count=1 -skip 'Lint' ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 3.595s` |
| Cổng tĩnh (lint, jscpd, gen, oasdiff, test-guard…) + test gói đổi và phụ thuộc (race: httpapi, reporting) | ci-task | `ci-task.sh` (cổng kiểm task) | `REMOTE-TEST XANH https://github.com/quyenld37/elimaz-crm/actions/runs/45980690195`, `CI-TASK XANH (từ xa trên github, so với điểm tách nhánh fb864a8e)` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Lần đầu chạy `tests/arch` đỏ (Query ngoài `*_dynsql.go`; thiếu `bestsellers_dynsql_test.go`; tên CTE `order_set`,
  `lines`, `grouped` bị coi là bảng thiếu schema vì câu cắt thành nhiều chuỗi). Sửa: dời `bestSellerRows` sang
  `bestsellers_dynsql.go`, đổi tên tệp test, câu thành một chuỗi hằng, tên CTE tập đơn qua hằng `orderSetName`.
  Không nới luật kiến trúc.

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (vòng 1, trên bản đã gộp main fb864a8e).

- Không có phát hiện phải sửa. Đã kiểm: tập đơn hai cơ sở qua `orderSet`, không đếm trùng dòng, hàng trả
  sellable + damaged theo ngày nhận và không phiếu hủy (cùng luật `anchorCustomerReturn`), `TotalRows` trước LIMIT,
  mọi lần nhắc view lọc `seller_id`, phạm vi Page cho phần hàng trả, giá trị người dùng chỉ qua `$n`, quyền 403.
- Ghi chú không bắt buộc đã sửa trong tệp task: tên hàm ở QĐ-232-1 (`bestSellersSQL`), mô tả `checkEnum` ở Tái dùng.
  Ghi chú `limit 501` ở HTTP là `bad_request` đã nằm ở QĐ-232-4.
