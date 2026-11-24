# Task 196 — Tra đơn web, luồng thay đổi đơn web, khách hủy từ website

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 196, M5-13, làn L1 sau M5-12).
Loại việc (quy trình mục 3): A

Đối chiếu trước khi viết (Step 0): kế hoạch gọi `order.Lock`, `order.Cancel`, `CreateShipment`; mã thật là
`order.LockSnapshot(ctx, tx, sellerID, id)`, `Writer.Cancel(ctx, tx, sellerID, id, version, reason, note, a, now)`,
`(*fulfillment.Service).Ship` (bảng đối chiếu M5-00, dòng M5-13). `Ship` gọi `lockShippable` → khóa đơn (nhóm 7) trước
khi chèn vận đơn, rồi `AttachShipmentBy` (đơn phải confirmed) — khớp dòng C1: hai đường cùng khóa dòng đơn trước nên
tranh hủy/gửi hãng tuần tự hóa. `CreateCancelRequest` đã có từ Task 195 (`requests_web.go`).

## Ảnh hưởng

(Viết lại sau lần chạy test kiến trúc đầu: module `order` không được nhắc view của `fulfillment` — cột "Được gọi" của
spec 04 mục 3, `TestSQLUsageProductCode`. Phần vận đơn và luồng thay đổi chuyển sang gói đọc chung mới
`internal/queries/weborders`, mẫu `internal/queries/webcatalog`; xem "Khác kế hoạch".)

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký có sẵn.
  - `order` (tệp mới `webview.go`): kiểu `WebOrderView`, `WebViewLine`, `CancelOutcome`; hàm `GetWebOrder`; phương
    thức `Writer.CancelFromWeb`; sentinel `ErrCannotCancel` (`errors.go`).
  - `order` (sửa hành vi): `checkOrderForRequest` (không xuất) cho yêu cầu hủy nhận thêm đơn `cancel_requested`
    (Q4) — ảnh hưởng `Writer.CreateRequestFromWeb` loại cancel (handler `WebCreateRequest`, mô tả `webCreateRequest`
    trong `openapi.yaml` sửa theo) và `Writer.CreateCancelRequest` (chỉ `CancelFromWeb` gọi). `exchangeRemaining` tách
    khỏi `checkExchangeQty` ở commit cấu trúc riêng (191d5c7).
  - `internal/queries/weborders` (gói mới): `Shipment`, `ShipmentOf`, `Change`, `Cursor`, `Page`, `Changes`,
    `MaxChanges`, `ErrBadLimit`, `ErrNotFound`; sqlc khối mới `queries/weborders` → `internal/queries/weborders/internal/store`
    (ghi đè `timestamptz` rỗng được → `*time.Time`, không cần hàm đổi `pgtype` riêng — jscpd).
  - Tài liệu: `docs/threat-model-m5.md` dòng 52 đổi tên test theo test thật; `docs/views.md` dòng `ord.orders_v`.
  - Mã sinh: `models.go` của mọi gói store sqlc thêm trường `UpdatedAt` của `OrdOrdersV` (view đổi).
  - `internal/queries/orderview`: thêm `CustomerLabel(Axes) LabelKey` (chỉ thư viện chuẩn như cả gói).
  - `httpapi`: 3 handler mới do ogen sinh interface (`WebGetOrder`, `WebOrderChanges`, `WebCancelOrder` trong
    `handlers_web_orders.go`); `errors_web.go` thêm `order.ErrCannotCancel` → 422 `cannot_cancel`,
    `weborders.ErrBadLimit` → 400, `weborders.ErrNotFound` → 404; `errors_snapshot_test.go` ba dòng.
    `httpapi/web/orders.go`: `OrderViewOut`, `ChangesOut`. `webRequestWrite` (Task 195) dựng `order.Writer` bằng
    `h.orderWriter()` (thêm Events, Queue cạnh Realtime; hàm yêu cầu chỉ dùng Realtime nên không đổi hành vi) để
    `WebCancelOrder` dùng chung (jscpd); `isOrderRequestRefusal` thêm `order.ErrCannotCancel` (chỉ `CancelFromWeb` trả).
  - Test kiến trúc `tests/arch/sqlusage_test.go`: thêm luật cho gói mới `weborders` (chỉ view `_v` đã công bố của
    `ord`, `fulfillment`) bằng cách tổng quát `checkWebcatalogRef` → `checkViewOnlyRef`; mẫu thử
    `testdata/sqlusage/queries/weborders/{ok,bad}.sql`, một dòng view mẫu `fulfillment.shipments_v`.
- Gói phụ thuộc ngược (go list): `order` ← `cmd/crm`, `httpapi`, `httpapi/web`, `cash`, `cash/internal/refunds`,
  `fulfillment`, `testkit/codtest`, `testkit/shiptest`, `wiring/webcache`; `orderview` ← `order`, `internal/queries`,
  `weborders`, `httpapi/web`; `weborders` ← `httpapi`, `httpapi/web` (gói mới). Không nhập mới nào vào `order`.
- Operation OpenAPI bị chạm, oasdiff: chỉ thêm. `webGetOrder` `GET /api/v1/web/orders/{externalRef}`
  (`web.orders.read`), `webOrderChanges` `GET /api/v1/web/orders/changes?since=&afterId=&limit=` (`web.orders.read`),
  `webCancelOrder` `POST /api/v1/web/orders/{externalRef}/cancel` (`web.orders.cancel`, `Idempotency-Key` bắt buộc,
  `x-auth: service_key`). Schema mới `WebOrderView`, `WebOrderLine`, `WebOrderShipment`, `WebOrderChanges`,
  `WebOrderChange`, `WebCancelOrderBody`, `WebCancelOutcome`. Mã lỗi mới `cannot_cancel` trong `x-error-codes` (câu
  tiếng Việt ở `console/src/app/errors.ts`). Mô tả `webCreateRequest` thêm `cancel_requested`.
- Chỗ dùng trên màn hình: không có màn hình mới (website là bên gọi, M5-30). `console/src/api/**` sinh lại bằng
  `make gen`; thêm câu lỗi `cannot_cancel` vào `errors.ts` (`errors.test.ts` kiểm đủ mã: 17 test qua).
- Câu sqlc, bảng, pha migration: `queries/ord/web_view.sql` (khối `order`): `OpenCancelRequestRef`.
  `queries/weborders/weborders.sql` (khối mới): `WebShipment` (`fulfillment.shipments_v` ⟕ `carrier_accounts_v`),
  `WebOrderChanges` (`ord.orders_v` ∪ `shipments_v`, keyset). Dùng lại `WebOrderByExternalRef`, `OrderDeliveredAt`,
  `OrderItemQuantities`, `HeldRequestLines`, `Get`. Migration `0509_web_order_changes_index.sql` (pha mở rộng):
  `CREATE OR REPLACE VIEW ord.orders_v` nối `updated_at` vào cuối (Q-X2; `docs/views.md` sửa cùng commit) và hai index
  `ord.orders (tenant_id, seller_id, updated_at, id) WHERE channel = 'web'`,
  `fulfillment.shipments (tenant_id, seller_id, last_status_at)`; số sửa chữa đầu của dải 0509–0519. Hai test ghim cột
  `orders_v` sửa theo (38 → 39 cột): `TestSchemaExtendsM1Orders`, `TestSchemaViewsAndRules` (`order/schema_test.go`),
  `TestM2Contract_ViewsDoc`.
- Test sẽ chạy: `order -run 'GetWebOrder|CancelFromWeb|TestRequest_|SchemaExtendsM1Orders'`; `orderview -run
  CustomerLabel`; `httpapi/web -run 'WebGetOrder|WebCancelOrder|WebOrderChanges'` (gồm tranh 30 lần);
  `httpapi -run 'ErrorSnapshot|ErrorCode|Route|Contract|WebRoute'`; `tests/arch` (nhóm ranh giới, SQL, view, route);
  console `errors.test.ts`; cả gói và vùng rủi ro cao (`order`) ở `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `orderview.Label` (nhãn gộp 17 khóa từ ba trục,
  dùng ở danh sách đơn), `fulfillment.StatusLabel`; `order.LockSnapshot`, `Writer.Cancel`, `Writer.CreateCancelRequest`;
  câu `WebOrderByExternalRef`, `OrderDeliveredAt`, `OrderItemQuantities`, `HeldRequestLines`; `order.Get`;
  `internal/queries/webcatalog` (mẫu gói đọc chung cho website); `refusableWrite`, `orderWriter`, `webActor`,
  `auditService`, `catalogRead`; `fulfillment.New`, `EnsureCarrierAccount` (test tranh); `env`, `serviceKey`,
  `orderEnv` của test `httpapi/web`; `shipM3`, `reqEnv` của test order.
- Dùng lại / viết mới vì: dùng lại tất cả các mục trên. Viết mới: `orderview.CustomerLabel` (gộp bốn nhãn chỉ nội bộ
  về `new`, còn lại `Label`) đặt cạnh `Label` để `weborders` và `httpapi/web` cùng dùng; gói `weborders` vì
  `orders_dynsql.go` là danh sách nhân viên (bộ lọc, COD, bút toán, SQL dựng lúc chạy) — khác mục đích, khác lý do thay
  đổi, còn câu của website là SQL tĩnh; `checkViewOnlyRef` tổng quát luật có sẵn của `webcatalog` thay vì chép.
  `exchangeRemaining` tách từ `checkExchangeQty` để kiểm lúc tạo yêu cầu và số còn đổi trên trang đơn cùng một cách tính.

## Soát thiết kế

- 5.1 Chủ dữ liệu: đơn, yêu cầu thuộc `order`; vận đơn thuộc `fulfillment` (chỉ đọc qua view công bố).
- 5.2 Không ghi bảng module khác: hủy ghi qua `Writer.Cancel` của order (nhả hàng qua `inventory.Writer`, trả lượt mã
  qua `promotion.Release` — đường có sẵn); yêu cầu qua `CreateCancelRequest`; audit ở lớp HTTP.
- 5.3 Đọc module khác: order không đọc gì của fulfillment; phần vận đơn và luồng thay đổi ở gói đọc chung
  `internal/queries/weborders`, chỉ view `_v` đã công bố (`ord.orders_v`, `fulfillment.shipments_v`,
  `carrier_accounts_v`); ghép ở lớp HTTP.
- 5.4 Nguyên tử: hủy/tạo yêu cầu + audit + câu trả lời chống trùng trong một transaction (`refusableWrite`/`WriteTx`).
- 5.5 Không nguyên tử: báo hiệu, sự kiện `order.cancelled` đi qua hàng đợi như đường hủy của nhân viên.
- 5.6 Gọi ra ngoài: không có (vận đơn đã gửi hãng thì chỉ tạo yêu cầu cho nhân viên; không tự gọi hãng).
- 5.7 Ngữ cảnh shop: mọi câu trong `tenancy.WithTenant`; câu đọc `ord.orders` có `seller_id` (luật sellerfilter);
  đơn tenant khác → 404 (RLS).
- 5.8 Không biết HTTP: order không nhập `net/http`; handler mỏng.
- 5.9 Đường ghi: `webCancelOrder` có `Idempotency-Key` bắt buộc, `x-permission: web.orders.cancel`; hai route đọc có
  `x-permission: web.orders.read`.
- 5.10 PostgreSQL là nguồn sự thật: quyết định hủy/yêu cầu dưới khóa dòng đơn.
- 5.11 Không thêm thư viện.
- 5.12 Theo module mẫu: hàm đọc nhận `sellerID`, đường ghi là phương thức `Writer`, sentinel trong `order/errors.go`,
  đổi HTTP ở `errors_web.go`; test `_test` trên PostgreSQL thật, `t.Parallel()`.
- 6.1 Hai người cùng sửa: khách hủy và nhân viên gửi hãng (`Ship`) cùng khóa dòng đơn (`LockSnapshot` / `lockShippable`,
  nhóm 7) trước khi đọc trạng thái; bên sau thấy kết quả bên trước: hủy trước → `Ship` gặp đơn cancelled, từ chối;
  gửi trước → hủy thấy `current_shipment_id`, tạo yêu cầu. Version lấy dưới khóa, `Cancel` so lại.
- 6.2 Gửi lại: `Idempotency-Key` (middleware trả câu trả lời đã lưu); khóa mới khi đã có yêu cầu hủy đang mở → trả
  lại đúng `requestRef` đó (không tạo trùng); đơn đã hủy → 422 `cannot_cancel`.
- 6.3 Thứ tự khóa: `orders` (7) → (Cancel) `coupons`/lượt mã (10) → số dư tồn (11) — y như `Writer.Cancel`; nhánh yêu
  cầu: `orders` → chèn `order_requests`. `Ship`: `orders` → `shipments`. Không đường nào khóa ngược.
- 6.4 Lỗi tạm: `WriteTx` chạy lại cả transaction; fn gán lại mọi biến kết quả.
- 6.5 Cách ly: READ COMMITTED + khóa dòng đơn.
- 6.6–6.8: không có việc nền mới.
- 6.9 Treo lâu: chờ khóa mặc định của `db`; `limit` của luồng thay đổi 1–500.
- 6.10 Tiền: hủy dùng nguyên đường của M2 (nhả held, trả lượt mã theo KM03); tra đơn chỉ đọc số tiền đã lưu.
- 6.11 Quy luật: "không lần nào vừa hủy vừa có vận đơn chưa hủy" — test tranh 30 lần; luồng thay đổi: mọi đơn đổi sau
  `since` xuất hiện đúng một lần khi đi hết các trang (test phân trang với cùng `changed_at`).
- 6.12 Dữ liệu cá nhân: `WebOrderView` không có địa chỉ, SĐT, tên người nhận, ghi chú nội bộ, tên nhân viên, tên khách
  (QĐ25) — test quét chuỗi JSON; lời nhắn hủy của khách chỉ vào `cancel_note`/`customer_note`, audit chỉ ghi `hasNote`.
- 6.13 Quyền: khóa dịch vụ `web.orders.read` (đọc), `web.orders.cancel` (hủy); chỉ đơn `channel = 'web'` của đúng
  `externalRef`; đơn tay → 404; test 403 khi thiếu quyền.

## Quyết định thay chủ shop

- **Q1 — `displayStatus` là khóa nhãn gộp** của `orderview.Label` (vd. `awaiting_pickup`, `shipping`, `delivered`),
  không phải chữ tiếng Việt: chữ hiển thị là việc của website (M5-30). Bốn nhãn chỉ nội bộ (`draft`,
  `awaiting_discount_approval`, `awaiting_customer`, `out_of_stock`) gộp về `new` để khách không thấy quy trình nội bộ.
  Phương án khác: trả chữ tiếng Việt — gắn câu chữ khách thấy vào CRM.
- **Q2 — Tên hãng = tên tài khoản hãng** (`carrier_accounts_v.name`, cùng nguồn với danh sách hãng web M5-15) kèm
  `carrierKey`; **không trả `trackingUrl`**: CRM không có nguồn link tra vận đơn (adapter không công bố); website tự
  dựng link từ `carrierKey` + `trackingCode` như hiện nay. Thêm sau là thay đổi chỉ thêm.
- **Q3 — Luồng thay đổi có con trỏ đủ** `since` + `afterId` (keyset `(changedAt, id)`, `changedAt` = lúc muộn hơn của
  `ord.orders.updated_at` và `shipments_v.last_status_at` của vận đơn hiện hành), trả `nextSince`, `nextAfterId`,
  `hasMore`. Kế hoạch chỉ trả một mốc giờ ("client lùi 30 s"): khi một trang đầy mà nhiều đơn cùng mốc giờ, chỉ mốc giờ
  sẽ lặp mãi hoặc bỏ sót; client vẫn lùi 30 s khi `hasMore = false`. Đổi vận đơn (Chờ thu gom → Đang chuyển) không đổi
  dòng đơn nên phải tính cả mốc của vận đơn. Thêm migration 0509 hai index (chỉ thêm).
- **Q4 — Đơn `cancel_requested`** (nhân viên đã yêu cầu hãng hủy): khách bấm hủy → tạo yêu cầu hủy (như kế hoạch:
  "có vận đơn hiện hành hoặc cancel_requested → CreateCancelRequest"); vì vậy luật yêu cầu hủy của Task 195 nhận thêm
  `cancel_requested`. Đã có yêu cầu hủy đang mở (khách bấm lại với khóa mới) → trả lại `requestRef` của yêu cầu đó,
  không 422 `duplicate_open`.
- **Q5 — Lý do hủy** `customer_changed_mind` (QĐ193); lời nhắn tùy chọn của khách ≤ 500 ký tự (giới hạn `cancel_note`)
  vào `cancel_note` khi hủy ngay, vào lời nhắn của yêu cầu khi tạo yêu cầu. Đơn `draft` (web không tạo) → `cannot_cancel`.
- **Q6 — Trang đơn có dữ liệu nút "Yêu cầu đổi hàng"** (ghi chú của Task 195): `deliveredAt` (lần cuối đơn chuyển
  delivered, cùng cách tính hạn đổi của 195), `exchangeUntil` (deliveredAt + `exchange_window_days` ngày) và
  `lines[].exchangeableQty` (số mua − số trong yêu cầu đổi/trả chưa bị từ chối hay rút, cùng hàm với lúc kiểm). Thuộc
  phạm vi "tra đơn web" nên làm ở đây; M5-30 chỉ việc hiện.
- **Q8 — Phần vận đơn, luồng thay đổi ở gói đọc chung `internal/queries/weborders`** thay vì `order/webview.go` +
  `queries/ord/web_view.sql` như kế hoạch: kế hoạch cho order đọc `fulfillment.shipments_v`, trái bảng "Được gọi" của
  spec 04 mục 3 (order không gọi fulfillment; test `TestSQLUsageProductCode` chặn). Theo mẫu `webcatalog` (M5-07):
  gói chỉ đọc view, ghép ở lớp HTTP; `ord.orders_v` nối `updated_at` (migration 0509, chỉ thêm). `order.WebOrderView`
  vì vậy không có `Shipment`, `DisplayStatus` (trả ba trục và `CurrentShipmentID` để lớp HTTP ghép); kiểu
  `WebShipment`, `WebOrderChange` của kế hoạch thành `weborders.Shipment`, `weborders.Change`. Phương án khác: thêm
  ngoại lệ "order đọc shipments_v" vào `viewReadExceptions` — nới luật kiến trúc, không chọn.
- **Q7 — Test tranh đặt ở `internal/httpapi/web/cancel_race_test.go`** thay vì `tests/e2e/web_cancel_race_test.go`:
  `tests/e2e` dựng tiến trình thật và không có hãng giả trong tiến trình; test cần `fulfillment.Service.Ship` (lệnh
  ghi vào `jobs`) chạy song song với `webCancelOrder` qua HTTP trong cùng tiến trình.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| [WB03] Tra đơn web của mình: đủ trường (mã, trạng thái, dòng, tiền, ngày tạo, cancellable), ba trục; có vận đơn → `CurrentShipmentID`; đơn đã giao → `deliveredAt`, `exchangeUntil` = D + 7 ngày, `exchangeableQty` trừ yêu cầu đổi mở, quá hạn → 0 (ghi chú Task 195) | `TestGetWebOrder_Fields` | `go test -race -count=1 -run '^(TestGetWebOrder_Fields\|TestGetWebOrder_OnlyOwnWeb\|TestCancelFromWeb_Direct\|TestCancelFromWeb_Request\|TestSchemaExtendsM1Orders\|TestSchemaViewsAndRules\|TestRequest_.*)$' ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 5.094s` |
| [WB03] Đơn nhập tay → 404; mã không có → 404; tenant khác → 404; bản xem không có SĐT, địa chỉ, tên (quét chuỗi JSON) | `TestGetWebOrder_OnlyOwnWeb` (module), `TestWebGetOrder_HTTP` (HTTP: 200 đủ trường, 403 khóa thiếu quyền, 404, quét SĐT, địa chỉ, tên, ghi chú khách) | như trên; `go test -race -count=1 -run 'Web\|OrderRequest\|ErrorSnapshot' ./internal/httpapi/...` | `ok …/order 5.094s`; `ok …/httpapi 3.930s`, `ok …/httpapi/web 16.606s` |
| [WB03] Phần vận đơn cho khách: hãng giả, tên tài khoản, nhãn `sending` khi vận đơn creating; chờ thu gom có mã → `awaiting_pickup`, `trackingCode`; không địa chỉ giao | `TestWebGetOrder_Shipment` | như trên (httpapi) | `ok …/httpapi/web 16.606s` |
| Luồng thay đổi: đổi trạng thái vận đơn (dòng đơn không đổi) → đúng đơn đó, nhãn shipping; vận đơn giao xong có `deliveredAt`; trang 1 dòng ra đủ, mỗi đơn một lần, kể cả cùng `changedAt`; đơn tay và đơn kênh web không mã ngoài không vào (sau review: trước khi lọc `external_ref IS NOT NULL` test này trả 500 — đã chạy thử bản chưa sửa: `changes: 500 {"error":"internal"…}`); `nextSince`/`nextAfterId` = dòng cuối; limit 0, 501 → 400 | `TestWebOrderChanges_HTTP`, `TestWebGetOrder_HTTP` (limit 0) | như trên (httpapi) | `ok …/httpapi/web 16.606s` |
| Nhãn khách: bốn nhãn nội bộ → new; còn lại như `Label` | `TestCustomerLabel` | `go test -count=1 -run TestCustomerLabel ./internal/queries/orderview/` | `ok elimaz.com.vn/crm/internal/queries/orderview 0.400s` |
| Hủy đơn new/confirmed chưa vận đơn → cancelled, lý do `customer_changed_mind`, ghi chú khách, giữ hàng held về 0, số dư held 0; gọi lại → `ErrCannotCancel` | `TestCancelFromWeb_Direct` | như dòng 1 (order) | `ok …/order 5.094s` |
| Qua HTTP: 200 `{cancelled:true}`; gửi lại cùng khóa → cùng thân, `Idempotent-Replayed: true`; khóa mới → 422 `cannot_cancel`, không lưu; 1 audit `order.cancel` của khóa dịch vụ, không chép lời nhắn; khóa thiếu `web.orders.cancel` → 403 | `TestWebCancelOrder_HTTP` | như trên (httpapi) | `ok …/httpapi/web 16.606s` |
| [WB04] Đơn có vận đơn awaiting_pickup → `{cancelled:false, requestRef}`, một yêu cầu hủy received có lời nhắn, đơn giữ confirmed; bấm lại → cùng `requestRef`, vẫn 1 yêu cầu; đơn `cancel_requested` → yêu cầu; đơn delivered → `ErrCannotCancel`; đơn tay, mã lạ → `ErrNotFound`; lời nhắn 501 ký tự → `ErrInvalidInput` | `TestCancelFromWeb_Request` | như dòng 1 (order) | `ok …/order 5.094s` |
| Yêu cầu về đơn của Task 195 vẫn đúng sau khi luật hủy nhận `cancel_requested` | `TestRequest_*` (Task 195, không sửa) | như dòng 1 (order) | `ok …/order 5.094s` |
| [WB12] Tranh 30 lần khách hủy (HTTP) với Gửi hãng (`Service.Ship`, lệnh vào jobs): mỗi lần đúng một bên — hoặc đơn cancelled, Ship lỗi, 0 vận đơn chưa hủy, 0 yêu cầu; hoặc 1 vận đơn, đơn confirmed, `{cancelled:false, requestRef}`, 1 yêu cầu mở; không deadlock; thêm một lần chắc chắn hủy trước → Ship từ chối, 0 vận đơn | `TestWebCancelOrder_RaceWithShip` | `go test -race -count=1 -run '^TestWebCancelOrder_RaceWithShip$' -v ./internal/httpapi/web/` | `30 lần tranh: map[gửi hãng:30]` (lần chạy trước: `gửi hãng:29 hủy:1`); `ok …/httpapi/web 4.604s` |
| Mã lỗi: `cannot_cancel` 422, `weborders.ErrBadLimit` 400, `weborders.ErrNotFound` 404 | `TestErrorSnapshot` (3 dòng mới), console `errors.test.ts` | như trên (httpapi); `npx vitest run src/app/errors.test.ts` | `ok …/httpapi 3.930s`; `Tests 17 passed (17)` |
| Luật kiến trúc: `weborders` chỉ nhắc view `_v` của ord, fulfillment; order không nhắc fulfillment; cột `orders_v` khớp `docs/views.md` và database; lọc người bán; mọi lỗi có ánh xạ; sqlc vet | `TestSQLUsageProductCode`, `TestSQLUsageViewRules` (mẫu `weborders/ok.sql` 0, `bad.sql` 2), `TestM2Contract_ViewsDoc`, `TestViewsDocMatchesDatabase`, `TestBoundariesRealCode`, `TestSellerFilterProductCode`, `TestErrorsMappedRealCode`, `TestSqlcVet` | `go test -count=1 -run '^(TestSQLUsageProductCode\|TestSQLUsageViewRules\|TestM2Contract_ViewsDoc\|TestViewsDocMatchesDatabase\|TestBoundariesRealCode\|TestSellerFilterProductCode\|TestErrorsMappedRealCode\|TestSqlcVet)$' ./tests/arch/` | `ok elimaz.com.vn/crm/tests/arch 5.057s` |
| Cổng tĩnh sau khi sửa lần ci-task đầu (đỏ: lint 4, gen-check models, jscpd 3, `TestSchemaViewsAndRules`) | golangci-lint, jscpd | `golangci-lint run ./internal/queries/... ./internal/modules/order/... ./internal/httpapi/... ./tests/arch/...`; `jscpd --config .jscpd.json --similarity --baseline jscpd-baseline.json --fail-on-new-clones=0 .` | `0 issues.`; `Found 0 clones.` |
| Cổng của task (lần 2, sau sửa review lần 1): build, vet, lint, gen-check, sqlc vet, danh mục, jscpd, gitleaks, oasdiff, test-guard, console, test `-race` gói sửa + vùng cao `order` + `tests/arch`, test thường gói phụ thuộc | toàn bộ cổng | `scripts/agent/ci-task.sh` | `CI-TASK XANH (so với điểm tách nhánh f2a3ba6…)`; `ok …/internal/modules/order 83.492s`, `ok …/internal/httpapi 40.934s`, `ok …/internal/httpapi/web 21.255s`, `ok …/internal/queries/orderview 1.687s`, `ok …/tests/arch 79.448s` |
| Commit 1 (cấu trúc): tách `exchangeRemaining` khỏi `checkExchangeQty`, hành vi không đổi | `TestRequest_QtyHeld`, `TestRequest_ConcurrentExchange`, `TestRequest_ExchangeWindow` (Task 195, không sửa) | `go test -race -count=1 -run '^(TestRequest_QtyHeld\|TestRequest_ConcurrentExchange\|TestRequest_ExchangeWindow)$' ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 2.535s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Q8: `GetWebOrder` không trả vận đơn và nhãn gộp; `WebOrderChanges` ở `weborders.Changes` (gói đọc chung), không ở
  `order`; chữ ký có `sellerID` và `now` (như bảng đối chiếu M5-00 dòng M5-13). Q3: luồng thay đổi có `afterId`,
  `nextAfterId`, `hasMore`. Q7: test tranh ở `internal/httpapi/web/cancel_race_test.go`.
- `CancelFromWeb(ctx, tx, sellerID, externalRef, note, a, now)` (kế hoạch: `reason`): lý do cố định
  `customer_changed_mind` (QĐ193), tham số là lời nhắn của khách.
- Migration `0509` (kế hoạch không có migration cho M5-13): `orders_v` + `updated_at` và hai index cho luồng thay đổi.
- Ba test ghim cột `orders_v` thêm `updated_at` (`TestSchemaExtendsM1Orders` 38 → 39 cột, `TestSchemaViewsAndRules`,
  `TestM2Contract_ViewsDoc`) — theo đúng
  cách Task 119 đã làm khi nối `ship_phone_reversed`; không nới điều kiện nào khác. Test kiến trúc `sqlusage_test.go`
  thêm luật cho gói mới (siết, không nới).

## Việc phát hiện thêm

- M5-30 (website gọi CRM): `cancellable` chỉ đúng với đơn new, confirmed; đơn `cancel_requested` vẫn gọi được
  `webCancelOrder` (tạo/trả yêu cầu hủy) — website chọn có hiện nút hay không. Tên hãng cho khách là tên tài khoản hãng
  (Q2): chủ shop nên đặt tên tài khoản hãng dễ hiểu với khách (vd. "Viettel Post").
- Nhân viên tạo được đơn kênh `web` không có mã ngoài qua `createOrder` (kênh tự do, `external_ref` tùy chọn): đơn đó
  không vào luồng thay đổi, website không tra được — có thể cần chặn ở `createOrder` (mốc sau, không thuộc task này).

## Reviewer

Commit 1 (cấu trúc, tách `exchangeRemaining`): **ĐẠT** — không có phát hiện phải sửa; hành vi `checkExchangeQty` giữ
nguyên (cùng lỗi, điều kiện, thứ tự câu đọc; biến thể ngoài đơn vẫn `ErrInvalidInput` trước khi dùng số giữ); ba test
của Task 195 phủ đủ ba nhánh.

Commit 2 (hành vi), lần 1: **CẦN SỬA** — (1) `queries/weborders/weborders.sql`: đơn `channel = 'web'` không có
`external_ref` (nhân viên tạo được qua `createOrder`) làm `Scan` lỗi, luồng thay đổi trả 500 và kẹt con trỏ; (2) bảng
bằng chứng ghi "đơn tay không vào" mà test chưa có ca đó. Đã sửa: lọc `o.external_ref IS NOT NULL` ở cả hai nhánh ứng
viên; `TestWebOrderChanges_HTTP` thêm một đơn tay và một đơn kênh web không mã ngoài (đã chạy bản chưa sửa → 500, bản
sửa → xanh). Ghi chú của reviewer: đơn `cancel_requested` vẫn gửi được yêu cầu hủy dù `cancellable = false` (Q4) — ghi
ở "Việc phát hiện thêm" cho M5-30.

Commit 2, lần 2: **ĐẠT** — không có phát hiện phải sửa; hai điểm lần 1 đã sửa đúng (lọc ở cả hai nhánh ứng viên, test
có ca thật); Ảnh hưởng khớp thực tế, không nơi gọi bị sót; không test bị xóa, tắt, nới. Ghi chú: `updated_at` là giờ
ứng dụng lúc ghi, không phải lúc commit — website lùi 30 s khi `hasMore = false` như kế hoạch (nêu rõ cho M5-30); trả
lại yêu cầu hủy đang mở không ghi audit vì không ghi gì mới.
