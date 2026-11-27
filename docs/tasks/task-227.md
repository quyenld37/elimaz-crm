# Task 227 — Khoảng ngày, phạm vi, người bán, hàm dựng tập đơn, dữ liệu thử

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m6-bao-cao.md` (Task 227 = M6-04). Loại việc (quy trình mục 3): A

Điều phối M6 (27/11): chủ shop duyệt kế hoạch M6 sáng 27/11 và cho bắt đầu dù M5 chưa đóng mốc (Task 221 còn chờ
staging); điều kiện "M1–M5 đóng" của M6-00 coi như được miễn. Làm theo phần "Bổ sung Task 223" của kế hoạch (G3:
`OwnSeller` theo luật `catalog.DefaultSeller`; G8: `from_status` đọc qua view; G14: phạm vi hai trường `Scope{Type, ID}`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu có sẵn. Thêm mới trong gói
  `internal/modules/reporting` (0 chỗ tham chiếu ngoài gói trước task): `VN`, `Period`, `ParsePeriod`, `Today`,
  `DayCutoff`, `DataScope`, `ScopeFor`, `OwnSeller`, `Basis`, `Dimension`, `Metric`, `ParseBasis`, `ParseDimension`,
  `ParseMetric`, `OrderFilter`, `Cell`, sentinel `ErrBadDate`, `ErrFromAfterTo`, `ErrRangeTooLong`, `ErrBadParams`,
  `ErrNoOwnSeller`. Gói mới `internal/testkit/reportseed` (`New`, `Seed` và các phương thức, `OrderSpec`, `Item`, `Step`,
  `ShipmentSpec`, `ReturnSpec`, `ReturnLine`, `Scenario`, `NineOrders`). Không xuất: `orderSet`, `sqlBuilder`.
- Gói phụ thuộc ngược (go list): `internal/modules/reporting` hôm nay chỉ có `cmd/crm` nhập (`RegisterFilePurposes`)
  và test của `files`, `conversation`; task không đổi ký hiệu họ dùng. `reportseed` mới, chỉ test của `reporting` nhập; nó nhập `internal/modules/customer` (`customer.Create`), nên sửa
  `customer` kéo test của `reportseed`, `reporting` chạy lại. Ký hiệu thêm của `reportseed`: `FakeCarrierFee`,
  `Seed.Tenant`, `Seed.Own`, `Seed.Pages`.
- Operation OpenAPI bị chạm, oasdiff: không (hợp đồng M6 có từ Task 225; HTTP nối ở Task 228). Tên giá trị `Metric`,
  `Dimension`, `Basis` lấy đúng enum `ReportMetric`, `ReportDimension`, `ReportBasis` của `openapi.yaml`.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không migration, không đổi bảng. Một câu sqlc mới `queries/reporting/sellers.sql`
  (`OwnSeller`, đọc `catalog.sellers_v`) và khối sqlc mới → mã sinh `internal/modules/reporting/internal/store`
  (`make gen`). SQL dựng lúc chạy chỉ ở `orders_dynsql.go` (đọc `ord.orders_v`, `ord.order_status_history_v`,
  `ord.returns_v`, `fulfillment.shipments_v`; đều nằm trong `viewReadExceptions["reporting"]` của Task 226).
  `reportseed` ghi thẳng bảng gốc bằng role chủ bảng (chỉ test; luật SQL của `tests/arch` bỏ qua `internal/testkit`).
- Test sẽ chạy: `go test -race -count=1 -run '…' ./internal/modules/reporting/` (test mới), `./internal/testkit/reportseed/`;
  `go test -count=1 ./tests/arch/` (dynsql, sellerfilter, sqlusage, boundaries); phần còn lại ở ci-task.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (`db`, `tenancy`,
  không có gói dựng tham số `$n` hay múi giờ dùng chung); `identity.Can`, `identity.ScopesFor`, `identity.Effective`,
  hằng `ScopeAll/ScopePage/ScopeWarehouse`; `catalog.DefaultSeller` (câu `GetDefaultSeller`); `internal/queries`
  `sqlArgs` (không xuất), `orderLabelCond`; `fulfillment.NewShipmentCode`, giờ `vietnam` của fulfillment;
  `testkit.{OpenStaff, Staff.Exec, Staff.User, Staff.Grant, Staff.ChannelAccount, ExplainAsApp, RequireNoSeqScan,
  SetRapidChecks}`.
- Dùng lại / viết mới vì: dùng lại `identity.Can` (test thuộc tính so với nó), `testkit.OpenStaff`, `ExplainAsApp`,
  `RequireNoSeqScan`, `SetRapidChecks`. Không dùng `identity.ScopesFor`: gom `ID` của mọi loại phạm vi nên mã kho lọt
  vào danh sách Page (G14 sau Task 223). `OwnSeller` viết câu sqlc riêng cùng luật với `GetDefaultSeller` vì `reporting`
  không được nhập `catalog` (ranh giới); câu đọc view `catalog.sellers_v`. `sqlBuilder` là bản riêng của module (kiểu
  một dòng như `sqlArgs` của `internal/queries`; gói đó không xuất, gộp thì phải tạo gói chung cho ba dòng mã).
  `VN` là hằng của kế hoạch M6 (giống `vietnam` của fulfillment, khác gói, không xuất). Mã vận đơn trong `reportseed`
  sinh cùng mẫu với `fulfillment.NewShipmentCode` nhưng đuôi từ bộ đếm (đuôi ngẫu nhiên 30 bit của hàm đó trùng được
  khi `Bulk` sinh hàng chục nghìn mã, ghi chú của reviewer lượt 1). Khách tạo bằng `customer.Create` (chỉ module customer
  ghi bảng khách). Mã đơn, mã phiếu trả thử ở dải `DH8…`, `TH8…`, không gọi sequence của M2 (luật
  `TestCodeSequenceSingleSource`). `reportseed` viết mới: các
  bộ dựng có sẵn (`invtest`, `catalogtest`, `shiptest`) đi qua public API của module (chậm, cần đủ luồng), kế hoạch
  đòi chèn thẳng bảng gốc để dựng nhanh kịch bản và 100.000 đơn.

## Soát thiết kế

- 5.1 Chủ dữ liệu: không bảng mới. `reporting` chỉ đọc.
- 5.2 Không ghi bảng module khác: mã sản phẩm không ghi gì. `reportseed` ghi bảng gốc của M1–M4 nhưng chỉ trong
  test, bằng role chủ bảng, trên database thử riêng.
- 5.3 Đọc qua view: `orders_dynsql.go` chỉ đọc `ord.orders_v`, `ord.order_status_history_v`, `ord.returns_v`,
  `fulfillment.shipments_v`; `OwnSeller` đọc `catalog.sellers_v` (sqlusage_test kiểm).
- 5.4–5.6: không áp dụng (chỉ đọc, không có việc sau commit, không gọi ra ngoài).
- 5.7 Ngữ cảnh shop: câu chạy trong transaction của bên gọi (`tenancy.WithTenant`, Task 228); RLS của view
  `security_invoker` lọc tenant. Test chạy trong `WithTenant` của crm_app, có ca tenant khác.
- 5.8 Không HTTP: gói chỉ nhập `identity`, `platform/db`, pgx, uuid.
- 5.9, 5.10, 5.11: không áp dụng (không đường ghi, không Valkey, không thư viện mới; rapid có sẵn).
- 5.12 Theo module mẫu: sentinel trong `errors.go`; SQL tĩnh qua sqlc (khối riêng), SQL dựng lúc chạy chỉ trong
  `*_dynsql.go` kèm `*_dynsql_test.go` trên PostgreSQL thật; mọi giá trị người dùng là `$n`, biểu thức SQL chỉ lấy từ
  `map` hằng (Global Constraints M6); điều kiện `seller_id` ở mọi lần nhắc `ord.orders_v` (ADR-29, sellerfilter_test).
- 6.1–6.4, 6.6–6.8: không áp dụng (chỉ đọc; không khóa; không việc nền).
- 6.5 READ COMMITTED: câu một lệnh, đọc một ảnh chụp; transaction chỉ đọc do Task 228 đặt.
- 6.9 Treo lâu: hạn câu lệnh do `TxReport` của Task 228; ở đây giữ câu dùng được chỉ mục: lọc thời gian bằng khoảng UTC
  trên `occurred_at`/`received_at` (chỉ mục 0603), không `::date` trên cột; test `EXPLAIN` không `Seq Scan` trên 100.000
  đơn.
- 6.10 Tiền: không tính tiền ở task này (cột tiền đọc `bigint`); `reportseed` tính `subtotal`, `discount_total`,
  `allocated_order_discount`, `net_amount`, `grand_total`, `cod_amount` bằng `int64` đúng CHECK và trigger
  `check_order_totals` (test dựng kịch bản qua các ràng buộc).
- 6.11 Quy luật (rapid): `scope.go` là vùng cao (kế hoạch M6) → test thuộc tính 200 lượt: Page/kho thấy được ⇔
  `identity.Can` đúng, gồm mã không phải uuid và uuid viết không chuẩn.
- 6.12 Dữ liệu cá nhân: tập đơn không có SĐT, địa chỉ, tên khách (chỉ mã, trạng thái, tiền, kênh, khóa); không log.
- 6.13 Quyền: mặc định từ chối — không grant thì `NoSales()`/`NoInventory()` đúng; không `AllPages` thì đơn không gắn
  Page bị loại; `PageIDs` rỗng thì `= ANY('{}')` ra 0 dòng; grant phạm vi khác loại bị bỏ; quyền đọc xét ở Task 228.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `ParsePeriod("2026-11-01","2026-11-30",366)` → Start 2026-10-31T17:00Z, End 2026-11-30T17:00Z (UTC), 30 ngày | `TestParsePeriod` | `go test -race -count=1 ./internal/modules/reporting/` | `ok elimaz.com.vn/crm/internal/modules/reporting 30.708s` |
| from > to → `ErrFromAfterTo`; 367 ngày → `ErrRangeTooLong`; 366 ngày được (2 ca); `2026-02-30`, sai dạng, có giờ, rỗng → `ErrBadDate` | `TestParsePeriodErrors` | như trên | ok |
| `DayCutoff("2026-11-17")` → 2026-11-17T17:00Z; `Today` ở biên 16:59:59Z/17:00Z; `VN` là UTC+7 | `TestTodayAndDayCutoff` | như trên | ok |
| `ScopeFor`: page p1, p2 (+ trùng) và `audit.read` all → `AllPages=false`, `PageIDs=[p1,p2]`; `warehouse:w1` của quyền bán hàng không vào `PageIDs`; mã không phải uuid bỏ; all có mã không phải all; không grant → `NoSales()` | `TestScopeFor` | như trên | ok |
| Thuộc tính 200 lượt: Page/kho thấy được ⇔ `identity.Can` (grant ngẫu nhiên 1–4, all/page/warehouse, mã không phải uuid, uuid viết hoa, `{uuid}`) | `TestScopeForMatchesCan` | `go test -count=1 -v -run '^TestScopeForMatchesCan$' ./internal/modules/reporting/` | `[rapid] OK, passed 200 tests`; bỏ tạm kiểm dạng chuẩn của uuid → FAIL (đã thử) |
| `OwnSeller`: own active id nhỏ nhất; chỉ partner/own ngừng → `ErrNoOwnSeller`; tenant khác không tính | `TestOwnSeller` | như dòng đầu | ok |
| Biên ngày: lên 16:59:59Z thuộc 01/11, 17:00:00Z thuộc 02/11; ô ngày 02/11 trong kỳ hai ngày | `TestOrderSet_DayBoundary` | như dòng đầu | ok |
| Chín đơn: `placed` = 8 mã không O7; `cancelled` = {O6}; P1 → {O1,O2,O8,O9}, P1 không thấy O4, O5 (chờ xác nhận, chờ gửi = 0), yêu cầu hủy {O8}; all thấy O4, O5; các cột số khác, bộ lọc, ô (gồm `unknown`); người bán partner và tenant B không lọt | `TestOrderSet_NineOrders` | như dòng đầu | ok; bỏ tạm điều kiện phạm vi Page → 6 dòng FAIL (đã thử) |
| Đơn ba dòng hàng, hai vận đơn, quay confirmed → cancel_requested → confirmed: một dòng, mốc là lần lên đơn; ngày quay lại không phải lần lên | `TestOrderSet_OneRowPerOrder` | như dòng đầu | ok; bỏ tạm `from_status IN ('', 'draft')` → FAIL (đã thử) |
| `"status; DROP TABLE x"` (ParseDimension và chiều của ô), Metric, Basis, khóa ô, bộ lọc lạ, thiếu kỳ, thiếu người bán → `ErrBadParams`, 0 câu SQL (đếm bằng `pgx.QueryTracer`); câu hợp lệ thì đếm > 0 | `TestOrderSet_BadParams` | như dòng đầu | ok |
| `EXPLAIN` bằng crm_app tập `placed` 31 ngày (all, một Page) và `delivered` trên `Bulk(100000)`: không Seq Scan (Bitmap Index Scan lịch sử → Index Scan đơn → Index Scan vận đơn) | `TestOrderSet_ExplainNoSeqScan` | như dòng đầu | ok (~16 s) |
| `reportseed`: chín đơn qua mọi CHECK và trigger tổng tiền; số tổng của kịch bản (1.910.000; ship 150.000, xem QĐ-227-3; hủy 200.000; hai giao xong; khách trả 1 cái 150.000…) | `TestNineOrders` | `go test -race -count=1 ./internal/testkit/reportseed/` | `ok elimaz.com.vn/crm/internal/testkit/reportseed 3.483s` |
| `Ledger` làm số dư bằng tổng sổ từng ngăn, mốc sổ theo `at` | `TestLedgerKeepsBalance` | như trên | ok |
| `Bulk(500)`: đúng n đơn, dòng lúc tạo trong khoảng, trạng thái = dòng lịch sử cuối, có dòng hàng, vận đơn, Page, đơn giao xong | `TestBulk` | như trên | ok |
| Luật một chỗ sinh mã đơn/phiếu trả vẫn đúng (reportseed không gọi sequence của M2) | `TestCodeSequenceSingleSource` | `go test -count=1 -run '^TestCodeSequenceSingleSource$' ./internal/modules/order/` | ok (lượt ci-task đầu đỏ ở đây, đã sửa) |
| Kiến trúc: SQL chỉ ở sqlc/`*_dynsql.go`, seller_id, đọc view, ranh giới, chỉ module customer ghi khách | `tests/arch` | `go test -count=1 ./tests/arch/` | lần 1 đỏ `TestCustomerWriter_OnlyCustomerModule` (reportseed chèn khách) → đổi sang `customer.Create`; chạy lại trong ci-task ; bản cuối (sau sửa mã vận đơn của reviewer lượt 1): `ok elimaz.com.vn/crm/tests/arch 25.402s` |
| Cổng tĩnh + toàn bộ test (đổi `internal/testkit` → ALL), bản đã gộp `main` (`main` không đổi từ điểm tách 68a0f7a4) | ci-task | ci-task.sh (từ xa, ubicloud) | Lượt 1 (run 45947222370) bị cắt khi chờ (giới hạn lệnh nền). Lượt 2 (run 45948232978): cổng tĩnh xanh, đỏ một test `TestCodeSequenceSingleSource` (reportseed gọi `nextval` mã đơn) → sửa. Lượt 3 (run 45950151809, tree 36786004): **cổng tĩnh xanh, test (2)(3)(4) xanh, test (1) đỏ đúng một test `TestExportConfig_KeepsEightAndUploads`** (`tests/deploy`, `export-config.sh: exit status 141` = SIGPIPE; test này xanh ở lượt 2 và xanh 3/3 lần chạy lẻ trên máy; task không đụng `scripts/deploy` hay `tests/deploy`) — lỗi chập chờn có sẵn, ghi ở "Việc phát hiện thêm", không chạy lượt thứ tư (~40 phút máy) |

## Quyết định thay chủ shop

- **QĐ-227-1 — "hủy" = đơn lên trong kỳ đang `cancelled`** (mốc là lần lên đơn), không theo dòng lịch sử
  `to_status = 'cancelled'` như câu tóm tắt của `orderSet` trong kế hoạch. Lý do: Task 229 định nghĩa `Cancelled` =
  `placed` đang `cancelled` và đòi `Placed = Active + Cancelled`; theo ngày hủy thì hai số lệch khi hủy khác ngày lên.
  Đảo ngược: đổi dòng `MetricCancelled` trong bảng `metricRules` sang một `anchor` lịch sử mới.
- **QĐ-227-2 — vai của `Basis`:** cơ sở `placed`: tập là đơn lên trong kỳ, `delivered`/`returned` lọc theo trạng thái
  hiện tại (cột của Task 230); cơ sở `completed`: `delivered`/`returned` theo dòng lịch sử giao xong/hoàn trong kỳ, còn
  `placed`/`placed_active`/`cancelled` → `ErrBadParams` (Task 230: các cột đó `nil` khi `completed`). Ba cột chờ xử lý
  là số hiện tại (không theo kỳ, mốc `created_at`), `customer_returned` theo `received_at` của phiếu khách trả. Tổng
  quan (Task 229) gọi `Delivered`/`Returned` với `completed`. Đảo ngược: sửa bảng `metricRules`.
- **QĐ-227-3 — ship thu khách của kịch bản chín đơn là 150.000**, không phải 120.000 như dòng "Số đúng" của kế hoạch:
  bảng đơn ghi ship 30.000 cho năm đơn chưa hủy O1, O2, O4, O8, O9 (5 × 30.000). Giữ đúng bảng từng đơn (nguồn chi
  tiết), số tổng ghi ở `NineOrders` và `TestNineOrders`. Task 229 phải dùng 150.000. Đảo ngược: đổi ship của một đơn.
- **QĐ-227-4 — chiều `status` của ô tạm so trạng thái thô `o.status`**; Task 230 đổi sang nhãn gộp của `orderview`
  (G9) như kế hoạch. Không viết bảng ngược nhãn ở task này.
- **QĐ-227-5 — `OwnSeller` theo luật `catalog.DefaultSeller`** (own, active, id nhỏ nhất; G3 sau Task 223), không báo
  lỗi khi có hơn một người bán own như chữ của kế hoạch: báo cáo phải đọc đúng người bán mà đơn đã ghi.

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `orderSet` trả `(string, error)` (kế hoạch ghi chỉ `string`) để giá trị lạ → `ErrBadParams` trước khi có câu SQL.
  CTE có cột `order_id, event_at, code, customer_id, status, pending_reason, packing_status, channel, page_id,
  source_id, care_by, created_by, goods_amount, shipping_fee_charged, created_at, carrier_key, ship_status` (một mốc
  `event_at` theo cơ sở/cột số thay cho hai cột `placed_at`, `completed_at`).
- Thêm `ParseBasis`, `ParseDimension`, `ParseMetric`, `UnknownKey`, `Period.Days`, `Period.IsZero` (Task 228 dùng
  khi đọc tham số).
- `OwnSeller` qua câu sqlc (khối mới `queries/reporting`), vì luật `dynsql_test` cấm SQL tĩnh ngoài sqlc.
- `reportseed.New(t, owner *db.DB, tenant)` nhận pool chủ bảng (`testkit.Staff.Owner`) thay cho `ownerURL`, vì
  `OpenStaff` không trả URL. `Variant(seller, product, sku)` giữ chữ ký; giá của dòng đơn ở `Item{Variant, Qty, Price}`.
  `Seed.Own()`, `Seed.Pages()` thêm. Mã nằm ở `seed.go`, `order.go`, `bulk.go`, `scenario.go`.
- `Bulk` chèn bằng `INSERT … SELECT FROM unnest` thay cho `COPY`: PostgreSQL từ chối `COPY FROM` vào bảng có RLS
  (FORCE áp cả chủ bảng). `ANALYZE` từng bảng ngay trong transaction sau khi chèn, vì kiểm khóa ngoại trên bảng chưa
  có thống kê làm 100.000 dòng hàng chạy quá 30 s (đo: 10.000 dòng 0,6 s, 100.000 dòng > 30 s; sau sửa cả lượt ~11 s).
- Test `groupBy = "status; DROP TABLE x"` chạy trên `ParseDimension` và trên `Cell.Dimension` của `orderSet` (chưa có
  tham số `groupBy` ở task này; Task 230 nhận nó qua `ParseDimension`).

## Việc phát hiện thêm

- Kế hoạch M6, dòng "Số đúng của kịch bản": ship thu khách 120.000 sai cộng (bảng cho 150.000), xem QĐ-227-3; Task
  229 (test `Active = {7, 1.910.000, ship 120.000}`) cần sửa thành 150.000.
- `tests/deploy` `TestExportConfig_KeepsEightAndUploads` chập chờn trên bể ubicloud: `export-config.sh` thoát 141
  (SIGPIPE, có lẽ một ống `… | head` dưới `pipefail`) ở run 45950151809; xanh ở run 45948232978 và trên máy. Ngoài phạm
  vi task (script triển khai), cần một task sửa script.
- Đơn lên trong `Bulk` có trạng thái cuối theo đường đi ngẫu nhiên nhưng mỗi đơn đúng một dòng hàng; Task 243 (gấp 10
  lần) có thể cần nhiều dòng hàng mỗi đơn.

## Reviewer

Kết luận: ĐẠT (lượt 1 trên bản đã gộp `main`, `main` không đổi từ điểm tách 68a0f7a4; lượt 2 sau sửa ghi chú 3, xem dưới).

- Lượt 1: phát hiện phải sửa: không có. Đã kiểm `scopeIDs` khớp `identity.Can` (all chỉ khi mã rỗng, so mã nguyên văn,
  không dùng `ScopesFor`), `orderSet` (một đơn một dòng, seller_id ở mọi lần nhắc `orders_v` và `shipments_v`, phạm vi
  Page với mảng rỗng, giá trị lạ → `ErrBadParams`, chỉ biểu thức hằng), năm quyết định thay chủ shop có căn cứ.
- Ghi chú không bắt buộc đã xử lý: (3) mã vận đơn của `reportseed` đuôi ngẫu nhiên 30 bit có thể trùng trong
  `Bulk(100000)` → đổi sang đuôi từ gốc ngẫu nhiên + bộ đếm; (4) mục Ảnh hưởng thêm phụ thuộc `customer` và ký hiệu
  `FakeCarrierFee`, `Seed.Tenant`; (1) sửa lý do ở mục Tái dùng (không phải luật ranh giới mà là trùng mã). Ghi chú (2)
  (bộ đếm câu SQL của `TestOrderSet_BadParams` đúng theo cấu trúc hàm test): giữ, phần thật sự kiểm là `ErrBadParams`.
- Sau sửa: test `reportseed`, `reporting`, `tests/arch`, `TestCodeSequenceSingleSource` chạy lại xanh trên máy; không
  chạy lượt ci-task thứ tư (sửa chỉ ở mã dựng dữ liệu thử).
- Lượt 2 (toàn bộ phần đã stage sau sửa): **ĐẠT**, phát hiện phải sửa: không có. Ghi chú: lượt ci-task cuối có một test
  `tests/deploy` chập chờn và chưa chạy lại sau bản sửa mã vận đơn (đã chạy `golangci-lint`, `make jscpd`, `go vet`,
  `tests/arch` trên máy cho bản sửa: sạch); `TestOwnSeller` dựa vào `uuidv7()` tăng dần giữa các transaction (đúng trên
  PostgreSQL 18); hai `Seed` cùng tenant vẫn có thể trùng mã vận đơn (rất hiếm, chưa test nào làm vậy).
