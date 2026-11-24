# Task 196 — Tra đơn web, luồng thay đổi đơn web, khách hủy từ website

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 196, M5-13, làn L1 sau M5-12).
Loại việc (quy trình mục 3): A

Đối chiếu trước khi viết (Step 0): kế hoạch gọi `order.Lock`, `order.Cancel`, `CreateShipment`; mã thật là
`order.LockSnapshot(ctx, tx, sellerID, id)`, `Writer.Cancel(ctx, tx, sellerID, id, version, reason, note, a, now)`,
`(*fulfillment.Service).Ship` (bảng đối chiếu M5-00, dòng M5-13). `Ship` gọi `lockShippable` → khóa đơn (nhóm 7) trước
khi chèn vận đơn, rồi `AttachShipmentBy` (đơn phải confirmed) — khớp dòng C1: hai đường cùng khóa dòng đơn trước nên
tranh hủy/gửi hãng tuần tự hóa. `CreateCancelRequest` đã có từ Task 195 (`requests_web.go`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký có sẵn.
  - `order` (tệp mới `webview.go`): kiểu `WebOrderView`, `WebViewLine`, `WebShipment`, `WebOrderChange`,
    `WebChangesCursor`, `CancelOutcome`; hàm `GetWebOrder`, `WebOrderChanges`, `WebDisplayStatus`; phương thức
    `Writer.CancelFromWeb`; sentinel `ErrCannotCancel`; hằng `MaxWebChanges`.
  - `order` (sửa hành vi): `checkOrderForRequest` (không xuất) cho yêu cầu hủy nhận thêm đơn `cancel_requested`
    (QĐ thay chủ shop Q4) — ảnh hưởng `CreateRequestFromWeb` loại cancel và `CreateCancelRequest` (0 chỗ gọi ngoài
    order ngoài handler `WebCreateRequest`). Tách `exchangeRemaining` khỏi `checkExchangeQty` (không xuất; commit cấu
    trúc riêng).
  - `httpapi`: 3 handler mới do ogen sinh interface (`WebGetOrder`, `WebOrderChanges`, `WebCancelOrder` trong
    `handlers_web_orders.go`); `errors_web.go` thêm dòng `order.ErrCannotCancel` → 422 `cannot_cancel`;
    `errors_snapshot_test.go` một dòng. `httpapi/web/orders.go`: hàm đổi `WebOrderView` → `gen.WebOrderView`.
- Gói phụ thuộc ngược (go list): `order` ← `cmd/crm`, `httpapi`, `httpapi/web`, `cash`, `cash/internal/refunds`,
  `fulfillment`, `testkit/codtest`, `testkit/shiptest`, `wiring/webcache`. `order` mới nhập
  `internal/queries/orderview` (chỉ thư viện chuẩn; test kiến trúc cho phép "module order và gói internal/queries cùng
  nhập"; không vòng).
- Operation OpenAPI bị chạm, oasdiff: chỉ thêm. `webGetOrder` `GET /api/v1/web/orders/{externalRef}`
  (`web.orders.read`), `webOrderChanges` `GET /api/v1/web/orders/changes?since=&afterId=&limit=` (`web.orders.read`),
  `webCancelOrder` `POST /api/v1/web/orders/{externalRef}/cancel` (`web.orders.cancel`, `Idempotency-Key` bắt buộc,
  `x-auth: service_key`). Schema mới `WebOrderView`, `WebOrderLine`, `WebOrderShipment`, `WebOrderChanges`,
  `WebOrderChange`, `WebCancelOrderBody`, `WebCancelOutcome`. Mã lỗi mới `cannot_cancel` trong `x-error-codes` (câu
  tiếng Việt ở `console/src/app/errors.ts`).
- Chỗ dùng trên màn hình: không có màn hình mới (website là bên gọi, M5-30). `console/src/api/**` sinh lại bằng
  `make gen`; thêm câu lỗi `cannot_cancel` vào `errors.ts` (test `errors.test.ts` kiểm đủ mã).
- Câu sqlc, bảng, pha migration: câu mới `queries/ord/web_view.sql` (khối sqlc `order` có sẵn): `WebShipmentOf` (đọc
  view `fulfillment.shipments_v` ⋈ `fulfillment.carrier_accounts_v`, không bảng của fulfillment), `WebOrderChanges`
  (keyset), `OpenCancelRequestRef`. Dùng lại `WebOrderByExternalRef`, `OrderDeliveredAt`, `OrderItemQuantities`,
  `HeldRequestLines`, `GetOrderBase`/`Get`. Migration mới `0509_web_order_changes_index.sql` (pha mở rộng: chỉ hai
  index — `ord.orders (tenant_id, seller_id, updated_at, id) WHERE channel = 'web'`,
  `fulfillment.shipments (tenant_id, last_status_at)`), số sửa chữa đầu tiên trong dải 0509–0519 của kế hoạch.
- Test sẽ chạy: `order -run 'WebOrder|CancelFromWeb|WebDisplay|Request'` (mới + yêu cầu của 195 vì sửa
  `checkOrderForRequest`); `httpapi/web -run 'WebOrderView|WebCancel|WebOrderChanges'` (mới, gồm tranh 30 lần);
  `httpapi -run 'ErrorSnapshot|ErrorCodes|Route|Contract'`; `tests/arch`; console `errors.test.ts`; cả gói, vùng rủi
  ro cao (`order`) ở `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `orderview.Label` (nhãn gộp 17 khóa từ ba trục,
  dùng ở danh sách đơn), `fulfillment.StatusLabel`; `order.LockSnapshot`, `Writer.Cancel`, `Writer.CreateCancelRequest`;
  câu `WebOrderByExternalRef`, `OrderDeliveredAt`, `OrderItemQuantities`, `HeldRequestLines`; `order.Get`;
  `refusableWrite`, `webRequestWrite`, `webActor`, `auditService`, `catalogRead`; `shiptest`, `fulfillment.New`
  (test tranh); `env`, `serviceKey` của test `httpapi/web`.
- Dùng lại / viết mới vì: dùng lại tất cả các mục trên. Viết mới: `WebDisplayStatus` (gộp bốn nhãn chỉ nội bộ —
  nháp, chờ duyệt giảm, chờ khách, hết hàng — về `new`; còn lại dùng nguyên `orderview.Label`); câu đọc vận đơn cho
  khách (`WebShipmentOf`, chỉ cột khách được thấy) vì `orders_dynsql.go` là danh sách nhân viên (lọc, COD, bút toán) —
  khác mục đích; câu luồng thay đổi (keyset `(changed_at, id)` với `changed_at = greatest(đơn, vận đơn)`) không có
  sẵn. `exchangeRemaining` tách từ `checkExchangeQty` để hai chỗ (kiểm khi tạo yêu cầu, số còn đổi trên trang đơn)
  cùng một cách tính.

## Soát thiết kế

- 5.1 Chủ dữ liệu: đơn, yêu cầu thuộc `order`; vận đơn thuộc `fulfillment` (chỉ đọc qua view công bố).
- 5.2 Không ghi bảng module khác: hủy ghi qua `Writer.Cancel` của order (nhả hàng qua `inventory.Writer`, trả lượt mã
  qua `promotion.Release` — đường có sẵn); yêu cầu qua `CreateCancelRequest`; audit ở lớp HTTP.
- 5.3 Đọc module khác: `fulfillment.shipments_v`, `fulfillment.carrier_accounts_v` bằng SQL (luật view M1,
  `docs/views.md`); order không nhập `fulfillment`.
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
- **Q7 — Test tranh đặt ở `internal/httpapi/web/cancel_race_test.go`** thay vì `tests/e2e/web_cancel_race_test.go`:
  `tests/e2e` dựng tiến trình thật và không có hãng giả trong tiến trình; test cần `fulfillment.Service.Ship` (lệnh
  ghi vào `jobs`) chạy song song với `webCancelOrder` qua HTTP trong cùng tiến trình.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Commit 1 (cấu trúc): tách `exchangeRemaining` khỏi `checkExchangeQty`, hành vi không đổi | `TestRequest_QtyHeld`, `TestRequest_ConcurrentExchange`, `TestRequest_ExchangeWindow` (Task 195, không sửa) | `go test -race -count=1 -run '^(TestRequest_QtyHeld\|TestRequest_ConcurrentExchange\|TestRequest_ExchangeWindow)$' ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 2.535s` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Commit 1 (cấu trúc, tách `exchangeRemaining`): **ĐẠT** — không có phát hiện phải sửa; hành vi `checkExchangeQty` giữ
nguyên (cùng lỗi, điều kiện, thứ tự câu đọc; biến thể ngoài đơn vẫn `ErrInvalidInput` trước khi dùng số giữ); ba test
của Task 195 phủ đủ ba nhánh.

Commit 2 (hành vi): CHƯA REVIEW
