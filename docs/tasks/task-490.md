# Task 490 — Đơn web mang hãng/dịch vụ khách chọn (quote + tạo đơn)

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (grep, cùng kết quả findReferences): **thêm trường** (không đổi tên, không xóa)
  `order.WebQuoteInput.Shipping` (20 chỗ dựng `WebQuoteInput{`: 15 webquote_test, 3 web_property_test, web.go,
  web/quote.go — trường mới tùy chọn, không phải sửa), `order.WebOrderInput.Shipping` (5 chỗ dựng: web.go, web_test 2,
  web_property_test, web/orders.go), `order.Order.WebShipping` (order.Get đọc bằng câu riêng khi kênh web). Mới:
  `order.WebShipChoice`, `order.ShipProblem*`, `order.ErrShipCarrierUnavailable`, `order.ErrShipFeeMismatch`,
  `order.ErrShipUnverified`, `(*web.Shipping).CheckChoice`. **Đổi chữ ký** `(*web.Orders).Quote` thêm tham số
  `ship *order.WebShipChoice` (2 chỗ gọi, đều ở `handlers_web_orders.go`; không test nào gọi thẳng). Lớp HTTP:
  `handlers_web_orders.go` (hàm mới `shipChoice`, nối vào WebQuote, WebCreateOrder; ErrShipUnverified trả lại khóa
  như web_orders_paused), `handlers_order.go` (`orderOut` thêm `webShipping`, hàm `webShippingOut`), `errors_web.go`
  (3 dòng errorMapping, 2 từ chối lưu theo khóa trong `webOrderRefusal`). sqlc sinh lại `models.go` của mọi store
  (bảng ord.orders có cột mới). Mã sinh: `gen.WebShippingChoice`, trường
  `ShippingChoice` của `gen.WebQuoteRequest`/`gen.WebOrderRequest`, `gen.OrderWebShipping`, `Order.webShipping`.
- Gói phụ thuộc ngược (go list, gói nhập `modules/order`): cmd/crm, internal/httpapi, internal/httpapi/web,
  modules/cash, cash/internal/refunds, modules/fulfillment, testkit/codtest, testkit/shiptest, wiring/webcache.
  Chỉ thêm trường nên các gói này biên dịch lại, không đổi hành vi.
- Operation OpenAPI bị chạm, oasdiff: `webQuote`, `webCreateOrder` (thêm thuộc tính tùy chọn `shippingChoice` vào
  thân yêu cầu — không phá client cũ), `getOrder`/thân `Order` (thêm thuộc tính đầu ra tùy chọn `webShipping`),
  schema mới `WebShippingChoice`, `OrderWebShipping`; 2 mã lỗi mới trong `x-error-codes`:
  `shipping_carrier_unavailable`, `shipping_fee_mismatch` (422); chưa kiểm được phí dùng 503 `retry_later` có sẵn.
  oasdiff: chỉ thêm, không breaking.
- Chỗ dùng trên màn hình: `console/src/screens/shipping/ShippingPanel.tsx` (khối Vận chuyển, Task 133/134): dòng
  "Khách chọn: <hãng> · <dịch vụ> · <phí>" khi đơn có `webShipping`; chọn sẵn tài khoản đó trong bảng so phí
  (`picked` khởi đầu = tài khoản khách chọn; `resolvePick` có sẵn rơi về cùng tài khoản rồi rẻ nhất). Không tự gửi.
  Console `src/api/**` sinh lại; `src/app/errors.ts` thêm câu tiếng Việt cho 2 mã.
- Câu sqlc, bảng, pha migration: migration mới `0530_order_web_shipping.sql` chỉ thêm cột `ord.orders.web_shipping
  jsonb NULL` + CHECK object (NOT VALID rồi VALIDATE) + GRANT UPDATE cột (pha mở rộng, như 0503). Câu sửa:
  `SetOrderWebFields` (ghi thêm `web_shipping`); câu mới `OrderWebShipping` (order.Get chỉ gọi với đơn kênh web).
  `GetOrderBase` giữ nguyên: lần ci-task đầu, đọc cột mới trong `GetOrderBase` làm đỏ 3 test dựng lược đồ trước 0530
  (`TestReturnItemsQty_BackfillM2`, `TestMigrateM1CashData`, `TestMigrateM1Data`: đơn tay qua order.Get). Bảng chạm: `ord.orders` (module
  order sở hữu).
- Test sẽ chạy: order `-run` `TestCreateFromWeb_ShippingChoiceStored`, `…SyncRefused`, `…Queued`,
  `TestQuoteWeb_ShippingChoice`; httpapi/web `TestWebQuote_ShippingChoice`, `TestWebCreateOrder_ShippingChoice`,
  `TestWebCreateOrder_ShippingChoiceQueuedAndFallback`; httpapi `TestErrorSnapshot`, `TestWebOrderErrors`; console
  vitest `ShippingPanel.test.tsx`, `errors.test.ts`; cả gói ở ci-task. Không e2e/Playwright (lệnh chủ shop).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (không có hàm kiểm lựa
  chọn giao hàng); `web.Shipping.Quote` (báo phí qua bộ đệm, Task 198), `fulfillment.(*Service).WebCarriers`,
  `fulfillment.GetWebCarrierSettings`, `order.webOrder.attempt` (luật sync/queued, QĐ15), `SetOrderWebFields`,
  `QuoteTable.resolvePick` (console).
- Dùng lại / viết mới vì: dùng lại `Shipping.Quote` để lấy đúng các lựa chọn CRM đã báo cho website (cùng khóa đệm,
  cùng làm tròn) thay vì gọi hãng lần nữa; dùng lại `WebCarriers` để kiểm hãng còn hiện trên web; dùng lại luật
  sync → từ chối / queued → `price_review` của `attempt`; dùng lại `resolvePick` (chọn sẵn theo khóa, rơi về cùng tài
  khoản). Viết mới `CheckChoice` (chưa có hàm đối chiếu lựa chọn với báo phí) ở `httpapi/web` vì order không được nhập
  fulfillment (tests/arch: fulfillment → order).

## Soát thiết kế

- 5.1 Chủ dữ liệu: lựa chọn của khách là thuộc tính của đơn → cột `ord.orders.web_shipping` (module order).
- 5.2 Không ghi bảng module khác: chỉ ghi ord.orders; fulfillment chỉ được đọc qua public API.
- 5.3 Đọc module khác: `fulfillment.GetWebCarrierSettings`, `(*Service).WebCarriers` (public API, như Task 198), báo
  phí qua `web.Shipping.Quote`.
- 5.4 Nguyên tử: lựa chọn ghi cùng transaction tạo đơn (`writeWebFields` trong savepoint của CreateFromWeb).
- 5.5 Không áp dụng: không có việc sau commit mới.
- 5.6 Gọi ra ngoài: kiểm phí có thể gọi hãng khi bộ đệm báo phí hết hạn — chạy **trước** WriteTx, ngoài mọi
  transaction (như webShippingQuote: chỉ báo phí, không tạo gì ở hãng, không cần `in_doubt`). Không gửi hãng.
- 5.7 Tenant: đọc cài đặt, hãng trong `tenancy.WithTenant`; cột mới trên bảng đã có RLS.
- 5.8 order không nhập net/http, ogen; dịch gen → order ở httpapi/web.
- 5.9 Đường ghi: webCreateOrder vẫn Idempotency-Key bắt buộc; webQuote vẫn exempt (chỉ đọc).
- 5.10 Valkey: bộ đệm báo phí chỉ là bản chụp phí hãng CRM vừa báo; Valkey mất thì `GetOrLoad` hỏi lại hãng. Quyết
  định "phí khớp" dựa trên số CRM báo cho website, không dựa trên việc Valkey có hay không (xem Quyết định 3).
- 5.11 Không thư viện mới. 5.12 Theo mẫu: lỗi sentinel trong order, đổi mã ở errors_web.go; test trên PostgreSQL thật.
- 6.1 Đồng thời: không sửa bản ghi đã có; hãng bị ẩn giữa lúc kiểm và lúc ghi đơn → đơn vẫn mang lựa chọn (nhân viên
  thấy khi gửi; khoảng hở vài ms, chấp nhận, ghi ở Quyết định).
- 6.2 Gửi lại: cùng Idempotency-Key → câu trả lời đã lưu (từ chối 422 mới cũng lưu như quote_changed); cùng
  externalRef khóa khác → trả đơn đã có **trước** khi xét lỗi lựa chọn (kiểm trước tx chỉ gắn Problem, CreateFromWeb
  xét sau bước tìm đơn theo externalRef), nên phí đổi sau đó không biến đơn đã tạo thành 422.
- 6.3 Khóa: không khóa thêm dòng nào (UPDATE đơn vừa chèn trong cùng tx).
- 6.4 Lỗi tạm: chưa kiểm được phí (ErrNoQuote, cache.ErrBusy) → sync 503 retry_later không lưu; queued nhận đơn,
  price_review.
- 6.5–6.9 Không áp dụng: không đổi mức cách ly, không việc nền, không gọi hãng trong tx.
- 6.10 Tiền: int64 đồng; so phí đúng bằng đồng; phí khách trả (shippingFee) ≤ phí lựa chọn (cho miễn phí ship của
  website); tổng đơn vẫn do CRM tính.
- 6.11 Quy luật: test bảng ca tiền (khớp, lệch, hãng ẩn, phí dự phòng có/không/lệch, queued, idempotency).
- 6.12 Dữ liệu cá nhân: lựa chọn chỉ có mã tài khoản, khóa, tên hãng, dịch vụ, phí, mã tỉnh/phường không lưu.
- 6.13 Quyền: webQuote/webCreateOrder giữ `web.orders.create`; Order.webShipping đọc theo quyền getOrder có sẵn.

## Quyết định thay chủ shop

1. **Website gửi lại cả thân báo phí** (`shippingChoice.quote` = đúng thân đã gửi webShippingQuote) cùng
   `accountId`/`service`/`fee` của lựa chọn: CRM không tự biết khối lượng website đã dùng (website mặc định 500 g cho
   biến thể không cân). Khác: ký số (HMAC) lựa chọn — cần khóa bí mật mới, khó đảo ngược hơn.
2. **`shippingFee` được nhỏ hơn phí lựa chọn** (miễn phí ship theo ngưỡng của website, Task 214 gửi 0): CRM chỉ từ chối
   khi `shippingFee > fee`; phí của hãng (`fee`) phải khớp báo phí. Khác: bắt bằng nhau — phá miễn phí ship.
3. **"Khớp báo phí" = khớp lựa chọn CRM đang báo cho cùng thân** (đọc qua bộ đệm báo phí 10 phút như webShippingQuote;
   hết hạn thì hỏi hãng lại, ngoài transaction). Phí dự phòng (không accountId) khớp khi bằng phí dự phòng đang lưu.
   Mã tỉnh/phường của thân báo phí phải trùng địa chỉ giao của đơn.
4. **Mã lỗi**: hãng không còn hiện trên web / phí dự phòng đã bỏ → 422 `shipping_carrier_unavailable`; phí, dịch vụ
   hay địa chỉ lệch báo phí, `shippingFee > fee` → 422 `shipping_fee_mismatch`; chưa kiểm được (hãng lỗi, bộ đệm bận)
   → 503 `retry_later` không lưu. Hai lỗi 422 lưu theo khóa như `quote_changed` (gửi thẳng). Đơn từ hàng đợi
   (`queued`) vẫn tạo, `priceReview = true`, lựa chọn lưu kèm vấn đề để nhân viên thấy (QĐ15).
5. **Lưu một cột jsonb** `web_shipping` (như `web_quote`) thay vì 6 cột; dễ đảo ngược.
6. **Màn đơn**: dòng "Khách chọn: …" ở đầu khối Vận chuyển khi chưa gửi hãng; chọn sẵn tài khoản đó nếu có trong bảng so
   phí, không có thì giữ cách cũ (rẻ nhất). Không mockup mới (luật mockup DESIGN.md là của website, không áp ở CRM);
   dùng lớp `box box-info small` có sẵn. Luật console 1 (mockup màn nội bộ do điều phối viên duyệt, Q-X8): chưa có
   mockup cho dòng này; điều phối viên xác nhận khi gộp (đảo ngược: bỏ `CustomerChoice` và `customerPick` trong
   ShippingPanel.tsx, máy chủ không đổi).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đơn mang lựa chọn đã kiểm lưu đúng hãng/dịch vụ/phí, không price_review; miễn phí ship (trả 0); phí dự phòng; client cũ không lựa chọn → nil; gửi lại cùng externalRef khi lựa chọn nay sai → Replayed, không ghi đè | `TestCreateFromWeb_ShippingChoiceStored` | `go test -race -count=1 -run '^(TestCreateFromWeb_ShippingChoice.*\|TestQuoteWeb_ShippingChoice)$' ./internal/modules/order/` | `ok elimaz.com.vn/crm/internal/modules/order 1.965s` |
| Gửi thẳng: hãng ẩn/phí lệch/chưa kiểm/khách trả hơn phí hãng → đúng sentinel; 0 đơn, 0 giữ hàng, 0 khách | `TestCreateFromWeb_ShippingChoiceSyncRefused` | như trên | ok |
| Từ hàng đợi: vẫn tạo, price_review, lựa chọn lưu kèm vấn đề (3 ca) | `TestCreateFromWeb_ShippingChoiceQueued` | như trên | ok |
| Báo giá: dấu vân tay gồm lựa chọn (không lựa chọn giữ dấu cũ), lỗi theo vấn đề, phí âm → invalid | `TestQuoteWeb_ShippingChoice` | như trên | ok |
| HTTP /web/quote: khớp → 200; phí lệch, dịch vụ lạ, trả hơn phí hãng → 422 shipping_fee_mismatch; hãng lạ, chưa có dự phòng → 422 shipping_carrier_unavailable; miễn phí ship 200 | `TestWebQuote_ShippingChoice` (httpapi/web) | `go test -race -count=1 -run '^(TestWebQuote_ShippingChoice\|TestWebCreateOrder_ShippingChoice.*)$' ./internal/httpapi/web/` | `ok elimaz.com.vn/crm/internal/httpapi/web 3.059s` |
| HTTP tạo đơn: khớp 201 lưu hãng (tên lấy từ CRM); hãng ẩn → 422 lưu theo khóa (gửi lại Idempotent-Replayed, 1 khóa, 0 đơn); đơn đã có gửi lại khóa khác → 201; địa chỉ khác thân báo phí → 422 fee_mismatch | `TestWebCreateOrder_ShippingChoice` | như trên | ok |
| HTTP: queued phí lệch → 201 priceReview + problem; phí dự phòng chưa đặt/lệch/khớp; mọi hãng lỗi → 503 retry_later, 0 khóa lưu, 0 đơn | `TestWebCreateOrder_ShippingChoiceQueuedAndFallback` | như trên | ok |
| Mã lỗi mới: 422/422/503 qua bảng đổi mã; 2 từ chối được lưu, unverified không | `TestErrorSnapshot` (dòng Task 490), `TestWebOrderErrors` | `go test -count=1 -run '^(TestWebOrderErrors\|TestErrorSnapshot.*)$' ./internal/httpapi/` | ok |
| Màn đơn: "Khách chọn: <hãng> · <dịch vụ> · <phí>", chọn sẵn hãng khách chọn (không phải rẻ nhất), không tự gửi; hãng không trong bảng → rẻ nhất; phí dự phòng + vấn đề có câu riêng; không lựa chọn → không dòng | `ShippingPanel.test.tsx` › "Hãng khách chọn trên website (Task 490)" (4 ca) | `cd console && npx vitest run src/screens/shipping/ShippingPanel.test.tsx` | `Tests 33 passed (33)` |
| Câu tiếng Việt cho mã lỗi mới | `src/app/errors.test.ts` | `npx vitest run src/app/errors.test.ts` | `Tests 17 passed (17)` |
| Cổng tĩnh, test gói đổi + phụ thuộc | ci-task | `ci-task.sh` (lần 3) | `CI-TASK XANH (so với điểm tách nhánh aeba0e5)`; `ok internal/httpapi 39.864s`, `ok internal/httpapi/web 24.032s`, `ok internal/modules/order 85.324s`, `ok internal/modules/cash 44.455s`, `ok internal/modules/fulfillment 52.612s`; oasdiff (apicheck) qua |

## E2E viết sẵn, chưa chạy

(không có: kế hoạch không có e2e cho task này; đủ ca ở test gói order và httpapi/web)

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (agent reviewer, lần 1, trên phần đã stage sau ci-task xanh). Phát hiện phải sửa: không có. Ghi chú
không bắt buộc đã xử lý: tên test ở mục Ảnh hưởng sửa cho khớp mã thật; luật console 1 (mockup) ghi vào Quyết định 6
để điều phối viên xác nhận. Ghi chú còn lại (lỗi cứng của báo phí trả trước khi tìm đơn theo externalRef — thân sai
thì lần đầu cũng sai; dịch vụ của phí dự phòng không đối chiếu; khối lượng do website khai) là đánh đổi đã biết, không
sửa.
