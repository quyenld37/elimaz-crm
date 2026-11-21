# Task 119 — M3-28: Danh sách đơn có vận đơn và tiền COD; dòng thời gian trên đơn

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m3-van-chuyen.md` (Task 119, M3-28). Loại việc (quy trình mục 3): A

Ca kiểm: phần hiển thị của VC03, VC05, VC06, TI01.

Hai commit (CLAUDE.md "cấu trúc và hành vi ở hai commit khác nhau"):

1. **Cấu trúc:** dời câu danh sách đơn từ `internal/modules/order/list_dynsql.go` sang `internal/queries/orders_dynsql.go`
   (đọc view), hành vi M2 giữ nguyên; migration 0330 nối cột vào ba view.
2. **Hành vi:** bộ lọc và cột vận đơn/COD, nhãn `sending`, dòng thời gian có vận đơn, thu, phiếu trả và nguồn.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences / grep, đếm ngày 21/11):
  - `order.List`, `order.ListFilter`, `order.OrderRow` (1 chỗ dùng ngoài test: `internal/httpapi/handlers_order_read.go`;
    test: `list_dynsql_test.go`, `list_explain_test.go`, `export_list_test.go`): **xóa**, thay bằng `queries.OrderList`,
    `queries.OrderFilter`, `queries.OrderListRow` (gói `internal/queries`). Phần cần module `customer` (phân loại q, tìm
    tên, gốc khách) ở lại `order`, tách thành `order.ResolveListSearch` → `order.ListSearch`.
  - `order.ListSQL` (export_list_test.go, 1 chỗ: `list_explain_test.go`): xóa; `queries.OrderListSQL` (export_test của
    queries) thay. `list_explain_test.go` dời sang `internal/queries/orders_dynsql_test.go` (`TestListPlans` → `TestOrderListPlans`).
  - `order.DefaultListLimit`, `order.MaxListLimit` (0 chỗ dùng ngoài order): xóa, thay bằng
    `queries.DefaultOrderListLimit`, `queries.MaxOrderListLimit`. `OrderListRow.CustomerName` là `string` (queries không
    nhập customer.Name) — bù bằng `OrderListRow.LogValue` chỉ ghi id, mã.
  - `order.Cursor`, `order.ParseCursor`: giữ (lớp HTTP vẫn đọc con trỏ bằng nó), đổi sang `queries.OrderCursor` ở lớp HTTP.
  - `order.ErrLabelNotFilterable`: commit 1 giữ hành vi (queries trả `queries.ErrLabelNotFilterable`, lớp HTTP bọc
    sentinel của order → 422 như cũ). Commit 2 mọi nhãn lọc được → không còn nơi sinh; ánh xạ ở `errors.go:483` không sửa
    được trong task này (Task 441 tách errors.go) → ghi "Việc phát hiện thêm".
  - `orderview.Label` (6 chỗ gọi: `order/read.go`, `order/duplicates.go`, `order/edit.go` qua `state.Editable`,
    `httpapi/handlers_order.go`, list): thêm nhánh `confirmed` + vận đơn `creating` → `LabelSending`. Nơi gọi không truyền
    `ShipmentStatus` thật (read, duplicates, handlers_order truyền rỗng; edit/state truyền "attached") → không đổi kết quả.
  - `orderview.Labels()` (2 chỗ: `list_dynsql_test.go`, console `labels.ts` theo thứ tự): 16 → 17 nhãn, `sending` sau
    `packed`.
  - `order.Timeline`: giữ chữ ký; thêm mục `return` (bảng `ord.returns` của chính order). Thêm `order.SortTimeline`
    (lớp HTTP trộn mục của `queries.OrderShippingTimeline` rồi sắp lại cùng một quy tắc).
  - Mới: `queries.OrderShippingTimeline` (mục `shipment`, `payment` từ view của fulfillment, cash),
    `fulfillment.StatusLabel` (nhãn hiển thị của vận đơn cho lớp HTTP; dùng lại `shipstate.Display` + `manual.Label`;
    `fulfillment/area_create.go` `viewOf` gọi lại hàm này thay biểu thức cũ, cùng kết quả).
  - `httpapi.timelineEntry` (`handlers_customer_timeline.go`, 2 chỗ dùng: dòng thời gian khách, đơn) thêm `ActorType`;
    dòng thời gian khách để rỗng (không gửi `actorType`).
  - `order.OrderTimelineItem.ActorType` nay có ở mọi mục: edit, note → staff (edit không người làm → system).
- Gói phụ thuộc ngược (go list): `internal/queries` ← `internal/httpapi`; `internal/queries/orderview` ← `httpapi`,
  `modules/order`, `modules/order/internal/state`; `modules/order` ← `cmd/crm`, `httpapi`, `modules/cash`,
  `modules/cash/internal/refunds`, `modules/fulfillment`, `testkit/codtest`, `testkit/shiptest`; `modules/fulfillment` ← nhiều
  (chỉ thêm hàm, không đổi gì có sẵn).
- Operation OpenAPI bị chạm, oasdiff: `listOrders` (tham số `carrierKey`, `shipmentStatus`, `codState`, `packingStatus` và
  trường `shipment`, `cod` đã có trong hợp đồng từ task hợp đồng M3; task này hiện thực), `OrderLabel` thêm giá trị
  `sending` (enum trả về thêm giá trị: oasdiff báo mức cảnh báo, không phá vỡ yêu cầu), `TimelineItem` thêm trường tùy chọn
  `actorType` (thêm trường trả về, không phá vỡ). `getOrderTimeline`, `getCustomerTimeline` dùng chung `TimelineItem`
  (khách không đặt `actorType`).
- Chỗ dùng trên màn hình: `console/src/screens/don/labels.ts` (`LABEL_ORDER`, `LABEL_TEXT` kiểu `Record<OrderLabel>` → phải
  thêm `sending` "Đang gửi hãng", nằm trong nhóm nhãn vận chuyển), `labels.test.ts`; `OrderList.tsx` dùng `LABEL_BAR` (không
  đổi vì `sending` là nhãn vận chuyển). Màn danh sách mới (cột vận đơn, COD) không thuộc task này.
- Câu sqlc, bảng, pha migration: migration mới `0330_order_list_views.sql`, pha mở rộng: `CREATE OR REPLACE VIEW` nối cột
  vào cuối `ord.orders_v` (`ship_phone_reversed`, cho tìm đuôi SĐT qua view bằng index `orders_ship_phone_reversed`),
  `fulfillment.shipment_events_v` (`actor_id`), `cash.payments_v` (`actor_id`) — tên người làm ở dòng thời gian (I09).
  Không đổi bảng. Câu sqlc mới `queries/ord/timeline_m3.sql` (`ListOrderReturnsForTimeline`, bảng `ord.returns` của order).
  `docs/views.md` sửa ba dòng. Test cột view: `order/schema_test.go` (orders_v 37 → 38 cột),
  `fulfillment/schema_m3_test.go` (`shipment_events_v`). Không đọc `province_code` của `shipments_v` (lỗi Task 108 không
  ảnh hưởng, không sửa ở đây).
- Test sẽ chạy: `go test -race ./internal/queries/... ./internal/modules/order/... ./internal/httpapi/... -run
  'Order|List|Timeline'`, `./internal/modules/fulfillment/ -run 'SchemaM3|StatusLabel'`, `./internal/modules/cash/ -run
  Schema`, `./tests/arch/...`; console `npm test -- labels`, `npx tsc --noEmit`, `npx eslint .`; rồi
  `scripts/agent/ci-task.sh` (order là vùng rủi ro cao → cả bộ order).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (con trỏ keyset: chỉ
  `customer.ListCursor`, đã dùng qua `order.Cursor`); `internal/queries` (mẫu `AgedRefs`, `scanEach`: gói ghép view cho
  màn danh sách); `shipstate.Display`, `manual.Label` (nhãn vận đơn); `actorNamesOf`, `timelineItemOut` (lớp HTTP);
  `testkit.StubShipment`, `testkit/shiptest` (vận đơn thật trong test).
- Dùng lại / viết mới vì:
  - Câu danh sách dời (không viết lại): `listSQL`, `labelCond` chuyển nguyên sang queries, chỉ đổi bảng sang view.
  - Nhãn gộp chỉ ở `orderview.Label` (A13, A20): queries tính nhãn dòng bằng hàm này; `labelCond` là điều kiện SQL khớp
    bảng đó (có test "mỗi nhãn lọc ra đúng đơn có nhãn đó").
  - `fulfillment.StatusLabel` bọc đúng biểu thức đang có ở `area_create.go:413` (`manual.Label(…, shipstate.Display(…))`)
    vì lớp HTTP không nhập được gói internal của fulfillment.
  - Sắp xếp dòng thời gian: một hàm `order.SortTimeline` dùng cho cả `Timeline` và lớp HTTP (không viết bản thứ hai).
  - jscpd (lượt ci-task đầu báo 4 đoạn mới, đã gộp hết): vòng "mỗi nhãn đúng một lần" của test M2 và M3 gộp thành
    `checkEachLabelOnce` (`list_test.go`); vòng chạy bảng nhãn gộp thành `checkLabelCases` (`label_test.go`); `nonEmpty`
    của queries thay bằng `maps.DeleteFunc` (không chép `nonEmptyMap` của order); đoạn chép mục queries → order trong test
    `timeline_m3_test.go` là bản của test (dựng như handler), giữ riêng.
  - jscpd/dupl: kiểu `order.ListSearch` và trường tìm của `queries.OrderFilter` giống nhau bề ngoài nhưng hai gói không
    nhập được nhau (luật (f)); giữ riêng.

## Soát thiết kế

- 5.1 Chủ dữ liệu: không bảng mới; cột view thuộc module chủ (ord, fulfillment, cash), migration chỉ nối cột vào cuối.
- 5.2 Không ghi bảng module khác: chỉ đọc.
- 5.3 Đọc qua view: danh sách và mục vận đơn/thu của dòng thời gian nằm ở `internal/queries` (spec 04 mục 3 điểm 2, C5:
  "danh sách đơn lọc theo hãng, trạng thái vận đơn và tiền COD"), chỉ nhắc view `_v` trong `docs/views.md`; module order
  không nhắc schema của fulfillment, cash (order không được gọi hai module đó, `moduleCalls`). Phiếu trả đọc ở order (bảng
  của chính nó).
- 5.4, 5.5, 5.6 không áp dụng: chỉ đọc, không có việc ghi, sự kiện hay gọi ra ngoài.
- 5.7 Ngữ cảnh shop: mọi câu chạy trong tx của `readAsDefaultSeller` (tenancy); view `security_invoker`; seller_id luôn ở
  điều kiện (ADR-29), vận đơn lọc thêm `s.seller_id = o.seller_id`.
- 5.8 Module không biết HTTP: queries, order không nhập net/http; lớp HTTP chỉ dịch tham số.
- 5.9 Không có đường ghi mới.
- 5.10, 5.11 không áp dụng: không Valkey, không thư viện mới.
- 5.12 Theo mẫu: `internal/queries/*_dynsql.go` + `*_dynsql_test.go` trên PostgreSQL thật; sqlc cho câu tĩnh của order.
- 6.1–6.9 không áp dụng: chỉ đọc, READ COMMITTED một câu mỗi phần; trang keyset (created_at, id) như M2, đơn mới không chen.
- 6.10 Tiền: số dư COD đọc từ `cash.shipment_cod_v` (int64, `coalesce(…, 0)` có sẵn ở view); `codState` so `> 0` theo kế
  hoạch; không cộng tiền ở Go.
- 6.11 Quy luật: mỗi nhãn lọc ra đúng đơn có nhãn đó (test trên mọi nhãn kể cả nhãn vận đơn); rapid của `orderview.Label`
  giữ.
- 6.12 Dữ liệu cá nhân: q không vào lỗi, log; summary dòng thời gian chỉ mang mã, trạng thái, loại, số tiền (không tên, SĐT,
  địa chỉ, không `raw`).
- 6.13 Quyền: `order.read` như M2 (hợp đồng có sẵn); người bán mặc định; không lộ đơn của người bán khác (test).

## Quyết định thay chủ shop

1. **Câu danh sách ở `internal/queries`, không ở module order.** Kế hoạch ghi sửa `order/list_dynsql.go` và ghép
   `fulfillment.shipments_v`, `cash.shipment_cod_v` "đúng luật sqlusage_test"; nhưng luật đó (và Global Constraints M3
   "Ranh giới module": order chỉ gọi inventory, customer, catalog, promotion) chặn order nhắc view của fulfillment, cash.
   Spec 04 mục 3 điểm 2 đặt đúng việc này cho gói `queries` (lý do C5 nêu đích danh danh sách đơn lọc theo hãng, vận đơn,
   COD). Chọn theo spec: dời câu sang queries (commit cấu trúc riêng), không thêm ngoại lệ vào `viewReadExceptions` (nới
   luật kiến trúc). Phương án khác: thêm order vào ngoại lệ "chỉ đọc view" — nhỏ hơn nhưng nới một test kiến trúc.
2. **Thêm cột vào ba view (migration 0330):** `ord.orders_v.ship_phone_reversed` (tìm đuôi SĐT qua view giữ index);
   `shipment_events_v.actor_id`, `payments_v.actor_id` (I09: dòng nhân viên có tên). Chỉ nối cột cuối, đảo ngược được bằng
   migration sau.
3. **Nhãn `sending` thêm vào enum `OrderLabel`** (sau `packed`) và màn hình (`labels.ts`, nhóm nhãn vận chuyển). Hai ca
   "ngoài bảng" của M2 trong `label_test.go` ("vận đơn creating, đang lấy/đã gói" → packing/confirmed) nay ra `sending` theo
   kế hoạch; sửa kỳ vọng hai ca đó (ghi ở "Khác kế hoạch").
4. **`TimelineItem.actorType`** (tùy chọn: staff, system, carrier, customer, service): kế hoạch đòi "dòng hãng ghi
   actorType = carrier" mà hợp đồng chưa có trường; thêm trường trả về tùy chọn (không phá vỡ).
5. **Mục `shipment`:** lấy sự kiện `applied = true`, hoặc bị bỏ qua với lý do khác `duplicate_state` (đọc "chỉ applied và
   ignored_reason khác duplicate_state" là: bỏ sự kiện trùng trạng thái; sự kiện bỏ qua vì lý do khác vẫn hiện để nhân viên
   biết hãng báo gì). Nguồn: `carrier_api`/`carrier_webhook` → carrier, `staff` → staff (có `actor_id`), `system` → system.
6. **`codState=none`:** đơn không có vận đơn hiện hành, hoặc cả ba ngăn `to_collect`, `carrier_holding`, `shop_received`
   đều ≤ 0. `cod` trong dòng là null khi đơn không có vận đơn hiện hành hoặc vận đơn chưa có bút toán COD nào.
7. **`shipment.label`** = `fulfillment.StatusLabel` (nhãn của `getShipment`, cùng biểu thức).
8. **Mục `return`:** một mục lúc tạo phiếu (người tạo hoặc hệ thống khi trống) và một mục lúc nhận hàng (nếu đã nhận).
9. **Mục `payment`:** mỗi phiếu thu/chi một mục lúc `received_at`, người làm là nhân viên ghi phiếu; summary có `method`,
   `amount`.
10. **Dòng duyệt oasdiff** (`openapi/breaking-approved.yaml`, 11 operation có `OrderLabel` trong thân trả về): thêm
   giá trị enum trả về `sending` là "phá vỡ" với client đọc enum chặt; màn hình (client duy nhất) cập nhật cùng commit.
   Ghi `approvedOn: 2026-11-21` theo ủy quyền của chủ shop đêm 21/11 (kế hoạch đòi nhãn này). Điều phối viên xóa 11 dòng
   sau khi gộp (luật của tệp). Phương án khác: không thêm nhãn (trái kế hoạch).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| C1 (commit 1) Danh sách đơn dời sang queries, hành vi M2 giữ: lọc, nhãn, keyset, người bán khác | `TestList_Filters`, `TestList_LabelMatchesRowLabel`, `TestList_KeysetPagination` (order, `list_test.go`, qua `ResolveListSearch` + `queries.OrderList`) | `go test -race ./internal/modules/order/...` | `ok elimaz.com.vn/crm/internal/modules/order 251.006s` |
| C1 Kế hoạch câu qua view không Seq Scan trên orders (200.000 đơn) | `TestOrderListPlans` (`internal/queries/orders_dynsql_test.go`) | `go test -race ./internal/queries/...` | `ok elimaz.com.vn/crm/internal/queries 117.080s` |
| C1 HTTP listOrders giữ mã lỗi M2 (nhãn vận đơn 422, q ngắn 422, con trỏ 400, 403) | `TestOrderRead_List` | `go test -race ./internal/httpapi/ -run TestOrderRead -count=1` | `ok elimaz.com.vn/crm/internal/httpapi 5.327s` |
| C1 View nối cột cuối, docs/views.md khớp | `TestSchemaViewsAndRules`, `TestSchemaExtendsM1Orders` (order), `TestSchemaM3_*` (fulfillment), `TestViewsDocMatchesDatabase`, `TestM2Contract_ViewsDoc`, `TestSQLUsage*` (arch) | `go test -race ./internal/modules/fulfillment/ -run TestSchemaM3`; `go test ./tests/arch/...` | `ok …/fulfillment 2.917s`; `ok elimaz.com.vn/crm/tests/arch 22.269s` |
| C2 Lọc `shipmentStatus=shipping` → đúng 2; `codState=holding` → đúng đơn đã giao chưa bảng kê; `carrierKey=fake_b` → đúng 1; collecting, received, none, packingStatus; người bán thứ hai không lẫn | `TestListM3_Filters` (`internal/modules/order/list_m3_dynsql_test.go`) | `go test -race ./internal/modules/order/ -run 'TestTimeline\|TestListM3\|TestList_' -count=1` | `ok elimaz.com.vn/crm/internal/modules/order 5.390s` |
| C2 Dòng có `shipment` (mã, hãng, mã hãng, trạng thái, kẹt, cần xem) và `cod` ba ngăn; không vận đơn → nil; nhãn dòng từ `orderview.Label` có trục vận đơn | `TestListM3_RowShipmentAndCOD` | như trên | như trên |
| C2 Mọi nhãn (kể cả nhãn vận đơn, `sending`) lọc được, mỗi đơn đúng một nhãn | `TestListM3_EveryLabelFilterable`, `TestList_LabelMatchesRowLabel`, `TestList_Filters` ("label shipping (M3)" rỗng) | như trên | như trên |
| C2 Nhãn: packed không vận đơn → Đã đóng gói; creating → sending; awaiting_pickup; delivery_failed; bảng M2 vẫn đúng | `TestLabel_ShipmentAxisM3` (`label_m3_test.go`), `TestLabel_Table`, `TestLabels_OrderAndConstants`, `TestLabel_AlwaysKnownForValidStatus` | `go test -race ./internal/queries/... -count=1` | `ok elimaz.com.vn/crm/internal/queries/orderview 1.846s` |
| C2 Dòng thời gian D1: mốc theo thời gian, hãng → `actorType` carrier không người làm, nhân viên → staff có người làm, bỏ `duplicate_state`, phiếu thu, phiếu trả, người bán kia không thấy sự kiện vận đơn | `TestTimelineM3_ShipmentPaymentReturn` (`timeline_m3_test.go`) | `go test -race ./internal/modules/order/ -run 'TestTimeline…'` | `ok … 5.390s` |
| C2 EXPLAIN 200.000 đơn (160.000 có vận đơn) lọc `shipmentStatus`, hãng, COD, nhãn vận đơn → không Seq Scan trên `orders` hay `shipments` | `TestOrderListPlans` (14 ca, kiểm số dữ liệu mẫu) | `go test -race ./internal/queries/... -count=1` | `ok elimaz.com.vn/crm/internal/queries 12.805s` |
| C2 HTTP: lọc qua tham số, khối `shipment` (nhãn "Đang chuyển"), `cod`, codState lạ → 400; timeline có `actorType` và tên nhân viên | `TestOrderRead_ListTimelineM3`, `TestOrderRead_List`, `TestOrderRead_TimelineNotes` | `go test -race ./internal/httpapi/ -run 'TestOrderRead_ListTimelineM3\|TestOrderRead_List$\|TestOrderRead_TimelineNotes' -count=1` | `ok elimaz.com.vn/crm/internal/httpapi 2.496s` |
| C2 `OrderShippingTimeline` trên vận đơn thật (shiptest): bỏ duplicate_state, staff có người làm, carrier không, phiếu chi âm, người bán kia/tenant B/đơn trống không thấy | `TestOrderShippingTimeline` (`internal/queries/order_timeline_dynsql_test.go`) | `go test -race ./internal/queries/ -run TestOrderShippingTimeline -count=1` | `ok elimaz.com.vn/crm/internal/queries 2.297s` |
| C2 Nhãn vận đơn của fulfillment cho lớp HTTP | `TestStatusLabel` | `go test ./internal/modules/fulfillment/ -run TestStatusLabel` | `ok …/fulfillment 0.494s` |
| C2 Màn hình: 17 nhãn đúng enum, chữ "Đang gửi hãng"; tsc | `labels.test.ts` | `npx vitest run src/screens/don/labels.test.ts`; `npx tsc --noEmit` | `Tests 7 passed (7)`; tsc không lỗi |
| C2 Cổng tĩnh và toàn bộ test (đổi migration → cả bộ): build, vet, lint, gen-check, sqlc-vet, catalog, jscpd, gitleaks, oasdiff (11 dòng duyệt), test-guard, console-check xanh; 76 gói ok, 1 gói đỏ là `TestDeepHealth` (lỗi có sẵn trên 2818f9e, xem Việc phát hiện thêm) | cả bộ | `ci-task.sh` lượt 3 (lượt 1 đỏ jscpd, oasdiff, thiếu test dynsql — đã sửa; lượt 2 hỏng vì PostgreSQL chung 5600 bị khởi động lại giữa chừng) | `ok …/modules/order 387.043s`, `ok …/queries 31.271s`, `ok …/httpapi 122.399s`, `ok …/fulfillment 256.496s`, `ok …/cash 104.615s`, `ok …/tests/arch 33.805s`; `CI-TASK ĐỎ: test` chỉ do `FAIL …/platform/observability` (`TestDeepHealth`) |
| C1 Lint | `golangci-lint run` 4 gói | `golangci-lint run ./internal/queries/... ./internal/modules/order/... ./internal/httpapi/... ./tests/arch/...` | `0 issues.` |

## Test bị xóa hoặc tắt

- Kỳ vọng đổi (không nới, theo kế hoạch), chi tiết ở "Khác kế hoạch": hai ca creating của `TestLabel_Table`, ca nhãn vận đơn
  của `TestList_Filters`, `TestList_LabelMatchesRowLabel` (bỏ nhánh bỏ qua nhãn vận đơn), `TestOrderRead_List` (422 → 200).

- `internal/modules/order/export_list_test.go` (commit 1): xóa. Tệp chỉ có hàm phụ `ListSQL` (không có `func Test…`) cho
  test kế hoạch câu; thay bằng `queries.OrderListSQL` trong `internal/queries/export_test.go` (câu đã dời sang queries).
  Không mất phép kiểm nào: `TestListPlans` dời thành `TestOrderListPlans` (`internal/queries/orders_dynsql_test.go`) đủ 8 ca.

## Khác kế hoạch

- Tệp: câu danh sách ở `internal/queries/orders_dynsql.go` thay cho `order/list_dynsql.go` (Quyết định 1); test lọc M3 vẫn ở
  `internal/modules/order/list_m3_dynsql_test.go` (cần dữ liệu đơn của order); `list_explain_test.go` dời sang `internal/queries/orders_dynsql_test.go`.
- Dòng thời gian M3 (vận đơn, thu) ở `internal/queries/order_timeline_dynsql.go`; phiếu trả ở `order/timeline.go`.
- `label_test.go`: hai ca ngoài bảng đổi kỳ vọng sang `sending` (Quyết định 3); `TestLabels_OrderAndConstants` 17 nhãn.
- `list_test.go` (M2): ca "nhãn vận đơn → ErrLabelNotFilterable" đổi thành "nhãn vận đơn lọc được, rỗng" (kế hoạch: nhãn
  vận đơn nay chạy được); `TestOrderRead_List`: 422 → 200 rỗng, cùng lý do.
- Commit 1 quên `make gen`: `models.go` sinh của sqlc (9 gói, cột view mới) cập nhật ở commit 2; commit 1 riêng lẻ không qua
  gen-check, trạng thái cuối nhánh qua.
- Ngoài danh sách tệp của kế hoạch: `fulfillment/status_label.go` (+ `area_create.go` dùng lại),
  `httpapi/handlers_customer_timeline.go` (`timelineEntry.ActorType`), `console/src/screens/don/labels.ts` (enum mới bắt buộc),
  `queries/ord/timeline_m3.sql`.

## Việc phát hiện thêm

- ci-task lượt 1 (toàn bộ test vì đổi migration) có ba test đỏ ngoài phạm vi: `TestApply_AllOrNothingProperty` (catalog,
  rapid "flaky test, can not reproduce", chạy lại qua), `TestLabels_RetryThenTerminal` (fulfillment, "chưa có mã hãng",
  chạy lại 3 lần qua) — chập chờn khi máy tải nặng, thuộc Task 443; `TestDeepHealth` (platform/observability,
  `LastBackupAgeH`) đỏ cả trên mã gốc 2818f9e (worktree tạm, lượt chạy 21/11) — lỗi có sẵn, không do task này.
- `openapi/breaking-approved.yaml`: 11 dòng của task này (enum `sending`) phải xóa sau khi gộp vào main (luật đầu tệp).
- `order.ErrLabelNotFilterable` không còn nơi sinh (mọi nhãn lọc được); ánh xạ ở `internal/httpapi/errors.go:483` để Task
  441 (tách errors.go) xóa cùng sentinel — task này không được sửa errors.go.
- `getOrder` (`order.GetDetail`) và đơn trùng vẫn tính nhãn trên ba trục M2 (không đọc trạng thái vận đơn): đơn đang chuyển
  hiện "Đã xác nhận/Đã đóng gói" ở chi tiết trong khi danh sách hiện "Đang chuyển". Cần task riêng (đọc qua queries).
- Màn danh sách đơn (console) chưa hiện cột vận đơn, COD và bộ lọc M3 (`LABEL_BAR` vẫn ẩn nhãn vận chuyển): task màn hình.

- `TestOrderRead_EditCancelTimeline` (httpapi) chập chờn một lần ở lượt chạy đầu (mục edit thiếu `lines`,
  `shipAddress`), chạy lại qua: lỗi đã biết (trộn đồng hồ Go và PostgreSQL, Task 436), không do task này.

## Reviewer

Kết luận: ĐẠT (commit 2, hành vi; lần 1) — commit 1 (cấu trúc) ĐẠT ở lần 2

- Commit 1 lần 1: CẦN SỬA — mục "Test bị xóa hoặc tắt" thiếu `export_list_test.go` (test-guard sẽ chặn); ghi chú: tên tệp
  `orders_dynsql_test.go`, thêm `DefaultListLimit`/`MaxListLimit` và `CustomerName string` vào Ảnh hưởng. Đã sửa cả ba.
- Commit 1 lần 2: ĐẠT — không còn phát hiện; hành vi M2 giữ (422 nhãn vận đơn qua `orderListErr`), migration 0330 chỉ nối
  cột cuối, test lọc/keyset/kế hoạch câu đi đúng đường của handler.
- Commit 2 lần 1: ĐẠT — không có phát hiện phải sửa. Ghi chú: `order.ErrLabelNotFilterable` và ánh xạ ở `errors.go:483` là
  mã chết (task này không được sửa errors.go, đã ghi "Việc phát hiện thêm" cho Task 441); nên tham chiếu các kỳ vọng test
  đổi ở mục "Test bị xóa hoặc tắt" (đã thêm); Quyết định 5 (sự kiện bỏ qua vì lý do khác vẫn hiện) cần chủ shop xác nhận;
  thứ tự số quyết định (đã sửa).
