# Task 192 — Báo giá cho website: kiểm giá, món ngừng bán, mã giảm, giảm theo chiến dịch web

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md` (Task 192, M5-09; sửa của Task 450 ở đầu mục và ở Task 452). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không đổi ký hiệu nào đã có. Thêm mới trong `order`: `QuoteWeb`, `WebLine`, `WebQuoteInput`, `QuotedLine`, `LineCampaign`, `CouponOutcome`, `WebQuote`, hằng `Problem*`, `MaxWebLines`, `MaxWebQty`; trong `httpapi/web`: `Orders`, `NewOrders`, `(*Orders).Quote`; trong `httpapi`: trường riêng `Deps.webOrders` (dựng ở `prepare`, `server.go`), handler `WebQuote` (`handlers_web_orders.go`). Test: `web_test` thêm `send` (dùng chung cho `get` của `catalog_test.go` và `post` mới; tránh trùng jscpd). Dùng (không đổi chữ ký): `catalog.DefaultSeller`, `catalog.VariantsForSale`, `inventory.DefaultWarehouse`, `inventory.Availability`, `promotion.ActiveForVariants`, `promotion.Check`, `pricing.Compute`, `pricing.Allocate`.
- Gói phụ thuộc ngược (go list): gói nhập `order`: `cmd/crm`, `internal/httpapi`, `internal/modules/cash` (+`internal/refunds`), `internal/modules/fulfillment`, `internal/testkit/{codtest,shiptest}`, `internal/wiring/webcache`. Gói nhập `internal/httpapi/web`: `internal/httpapi`. Chỉ thêm ký hiệu nên không gói nào phải sửa.
- Operation OpenAPI bị chạm, oasdiff: thêm `webQuote` `POST /api/v1/web/quote` (`x-auth: service_key`, `x-permission: web.orders.create`, `x-idempotency: exempt` kèm lý do), schema mới `WebQuoteRequest`, `WebQuoteLineInput`, `WebQuote`, `WebQuotedLine`, `WebQuoteCoupon`. Chỉ thêm → oasdiff không có thay đổi phá vỡ.
- Tài liệu: `docs/views.md` (dòng `catalog.products_v` thêm nơi dùng `order` `ProductCategories`).
- Chỗ dùng trên màn hình: không có (client `console/src/api` sinh lại thêm kiểu, không màn hình nào dùng).
- Câu sqlc, bảng, pha migration: thêm `queries/ord/web_quote.sql` (`ProductCategories`: đọc `catalog.products_v`, view công bố trong `docs/views.md`); không bảng, không migration. Đọc thêm (qua public API) `catalog.variants_v`, `inventory.stock_available_v`/số dư, `promotion.web_campaigns`, `promotion.coupons`.
- Test sẽ chạy: `internal/modules/order` (test mới `TestQuoteWeb_*`, vùng rủi ro cao → cả gói ở ci-task), `internal/httpapi/web` (`TestWebQuote*`), `internal/httpapi` (route, webroute), `tests/arch` (`webexempt`, kiến trúc), gói phụ thuộc theo `ci-task.sh`.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (sha256/fingerprint: chỉ có `observability.Fingerprint` cho lỗi, `files` etag — khác việc); `order/internal/pricing` (`Compute`, `Allocate`, `MaxMoney`); `promotion.CampaignPrice`/`BestCampaign`/`ActiveForVariants` (Task 452, đã kiểm bảng mẫu làm tròn); `promotion.Check`; `catalog.VariantsForSale`; `inventory.Availability`, `inventory.DefaultWarehouse`; `order.ErrTotalOverflow`, `order.ErrInvalidInput` và ánh xạ HTTP có sẵn ở `errors_order.go`; khung test `web_test.newEnv`, `order_test.newOrderEnv`/`sale10`/`coupon`.
- Dùng lại / viết mới vì: dùng lại mọi thứ trên. Không viết `CampaignDiscount` mới: giảm chiến dịch mỗi đơn vị là `Applied.Discount` của `ActiveForVariants` (= niêm yết − `CampaignPrice`), dòng = số lượng × số đó; luật làm tròn chỉ có một chỗ (`promotion`). Tổng tiền và chia mã về dòng dùng `pricing`. Viết mới: `QuoteWeb` (ghép các phần), câu `ProductCategories` (danh mục của mẫu cần cho phạm vi chiến dịch `categories`; `SaleVariant` không có danh mục và sửa `catalog` không cần cho việc này), `web.Orders` (lớp HTTP; tách khỏi `web.Catalog` vì Task 193/194 thêm tạo đơn vào đây).

## Soát thiết kế

5.1 Chủ dữ liệu: báo giá không tạo dữ liệu; tính ở `order` (module điều phối đơn, spec 05). 5.2 Không ghi bảng nào. 5.3 Đọc module khác qua public API (`catalog`, `inventory`, `promotion`) và view `catalog.products_v`. 5.4 Mọi đọc trong một transaction tenant (`db.TxAPI`) để giá, tồn, mã, chiến dịch cùng một lúc. 5.5 Không có việc sau commit. 5.6 Không gọi ra ngoài. 5.7 `tenancy.WithTenant` ở `web.Orders`. 5.8 `order` không nhập HTTP; handler mỏng. 5.9 Route POST không ghi → `x-idempotency: exempt` kèm lý do (luật openapi 3, ADR-47); `webexempt_test` quét handler không gọi `WriteTx`, `RecordAudit`, `Enqueue`, `Emit`. 5.10 Không đọc bộ đệm: giá, tồn, chiến dịch đọc PostgreSQL (test đặt giá cũ vào ReadCache). 5.11 Không thư viện mới. 5.12 Theo mẫu: hàm public nhận `ctx, tx`, lỗi sentinel có sẵn, test `_test` trên PostgreSQL thật, `t.Parallel()`.
6.1 Chỉ đọc, không khóa; kết quả có thể khác lúc tạo đơn → Task 193 tính lại trong transaction tạo đơn (`Fingerprint`, `Changed`). 6.2 Gửi lại cùng yêu cầu cho cùng kết quả (không ghi). 6.3 Không khóa. 6.4 Lỗi tạm của pgx: `tenancy.WithTenant`/`RetryTx` có sẵn chạy lại; thân hàm gán lại biến kết quả. 6.5 READ COMMITTED; các câu đọc trong một transaction ngắn. 6.6–6.8 không áp dụng: không việc nền, không gọi ra ngoài. 6.9 `db.TxAPI` (câu 5 s, khóa 1 s). 6.10 Tiền `int64`; tổng qua `pricing.Compute` (trần 2^53−1 → `ErrTotalOverflow`, 422 `total_overflow`); chia mã về dòng bằng `pricing.Allocate` (Σ = số giảm, mỗi dòng ≤ nền). 6.11 rapid: quy luật tổng (`GrandTotal = Merchandise − giảm chiến dịch − giảm mã + phí = Σ Net + phí`, mỗi dòng `0 ≤ Net`, giảm mã chỉ ở dòng được áp). 6.12 SĐT khách chỉ dùng để so mã gắn SĐT, kiểu `logging.Phone` (tự che); không log, không vào Fingerprint. 6.13 Quyền `web.orders.create` của khóa dịch vụ (route `service_key`); khóa không có quyền → 403, không khóa → 401.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| [WB13] giá CRM 350.000: gửi đúng → `Changed false`; gửi 300.000 → `Changed true`, `UnitPrice 350.000`, `price_changed`; dấu vân tay ổn định, đổi khi dòng đổi | `TestQuoteWeb_PriceChanged` | `go test -race -count=1 -run '^TestQuoteWeb_' ./internal/modules/order/` (trước khi có mã: `undefined: order.WebQuoteInput`) | `ok internal/modules/order 3.517s` (lần cuối, sau tách hàm) |
| [WB09] ngừng bán, mã lạ, biến thể của người bán khác → `unavailable`, giữ đúng biến thể, không vào tổng | `TestQuoteWeb_Unavailable` | như trên | ok |
| Mã 10 % trần 50.000 tối thiểu 300.000, 2 × 350.000 → 50.000; hết hạn → `expired`; gắn SĐT khác/không gửi SĐT → `bound_other`; đúng SĐT → áp; mã lạ → `not_found`; dưới tối thiểu → `min_subtotal`; Σ giảm mã dòng = giảm mã | `TestQuoteWeb_Coupon` (7 ca con) | như trên | ok |
| [WB06] A chiến dịch 20 % cấm mã 1 × 400.000, B 1 × 200.000, mã 10 % → A giảm chiến dịch 80.000, mã chỉ B = 20.000, tổng 500.000; chiến dịch cho mã → mã trên cả hai (52.000); chỉ dòng cấm mã → mã 0 | `TestQuoteWeb_CampaignAndCoupon` | như trên | ok |
| [WB06] chiến dịch 33 % chạy trên CRM giảm đúng mọi dòng 33 % của `web-campaign-rounding.json` (20 dòng; không áp → không chiến dịch) | `TestQuoteWeb_CampaignRoundingTable` | như trên | ok |
| Hết hàng (tồn 0; tồn 1 đặt 2) → `out_of_stock`, không giữ hàng (`Held 0`, 0 dòng `inventory.stock_reservations`) | `TestQuoteWeb_OutOfStock` | như trên | ok |
| 20 × 9.000.000.000.000.000 → `ErrTotalOverflow`; phí 2^53 → `ErrInvalidInput` | `TestQuoteWeb_Overflow` | như trên | ok |
| Đầu vào sai (0/51 dòng, số lượng 0/21, giá âm, phí âm, trùng biến thể, mã 65 ký tự, mã biến thể rỗng) → `ErrInvalidInput`; 50 dòng được | `TestQuoteWeb_InvalidInput` | như trên | ok |
| rapid 200 lượt: tổng = Σ Net + phí = hàng − chiến dịch − mã + phí; Net mỗi dòng đúng, ≥ 0; dòng cấm mã không có giảm mã; Changed ⇔ dòng bán được lệch giá; dòng ngừng bán không vào tổng | `TestQuoteWeb_Invariants` | như trên | ok |
| HTTP: hình dạng câu trả lời; 10 lần không `Idempotency-Key` → 200, `platform.idempotency_keys` không thêm dòng; 403 khóa không quyền; 401 không khóa; mã lạ `not_found` | `TestWebQuote` | `go test -race -count=1 -run '^(TestWebQuote.*\|TestWebAvailability.*\|TestWebCatalog_Auth)$' ./internal/httpapi/web/` | `ok internal/httpapi/web 8.695s` |
| HTTP: 51/0 dòng, số lượng 0/21, giá âm, thiếu phí, trùng biến thể → 400; 50 dòng 200; 20 × 9e15 → 422 `total_overflow` | `TestWebQuote_Limits` | như trên | ok |
| Bộ đệm còn giá cũ (danh mục web vẫn trả 350.000) → báo giá dùng 420.000 của PostgreSQL, 0 lần gọi bộ đệm | `TestWebQuote_IgnoresReadCache` (bộ đệm giả `pinnedCache`; bản đầu dùng Valkey thật đỏ ở ci-task dưới tải: Valkey trả nhánh dự phòng nên không còn "giá cũ") | như trên; `-count=3` riêng test này | ok |
| Route exempt không gọi `WriteTx`, `RecordAudit`, `Enqueue`, `Emit`; luật route `service_key` | `tests/arch` (webexempt, webroute…) | cổng kiểm `ci-task.sh` | `ok tests/arch 56.420s` |
| Cổng kiểm của task: build, vet, lint, gen-check, sqlc-vet, catalog, jscpd, gitleaks, oasdiff, test-guard, console, test -race gói đổi (order 63 s, httpapi 33 s, httpapi/web 14 s, tests/arch), test thường gói phụ thuộc (cash, fulfillment, customer, webcache, e2e…) | — | `ci-task.sh` trên phần đã stage | `CI-TASK XANH (so với điểm tách nhánh 963158c)`. Lần chạy đầu (chưa stage) đỏ: gen-check (mã sinh chưa stage), jscpd 2 bản trùng trong test (đã gộp `send`, viết lại `product`), `TestWebQuote_IgnoresReadCache` (Valkey dưới tải; đã đổi sang bộ đệm giả) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

Theo sửa của Task 450/452: bỏ `WebCampaign`, `WebLine.Campaign`, `campaign_invalid`; `QuoteWeb` gọi `promotion.ActiveForVariants`. Các điểm khác chữ kế hoạch ghi ở "Quyết định thay chủ shop" (1–4).

## Quyết định thay chủ shop

1. **Không viết `order.CampaignDiscount` riêng.** Luật làm tròn đã có ở `promotion.CampaignPrice` (Task 452, kiểm đủ 200 dòng bảng mẫu); giảm dòng = số lượng × `Applied.Discount`. Viết lại ở `order` là hai chỗ giữ cùng một luật. Test bảng mẫu ở task này đi qua `QuoteWeb` với mọi dòng 33 % của `web-campaign-rounding.json` (chiến dịch thật trên CRM). Tệp bảng mẫu đã có từ Task 452, không tạo lại. Phương án khác: hàm bọc mỏng trong `order`.
2. **Tổng vượt 2^53 − 1 dùng lại `order.ErrTotalOverflow` → 422 `total_overflow`**, không khai `ErrAmountTooLarge`/`amount_too_large` cùng nghĩa (luật CLAUDE.md, như QĐ6 của Task 452). Website (M5-29) chưa code nên chưa ai dựa vào tên mã. Dễ đảo: thêm sentinel và dòng ánh xạ.
3. **`QuotedLine` có thêm `Campaign *LineCampaign{ID, Revision, Percent, AllowCoupon}`**: Task 193 cần mã, revision, phần trăm của chiến dịch để ghi lên dòng đơn (QĐ33), website cần biết dòng nào đang giảm. Chỉ thêm trường.
4. **Một dòng một vấn đề, ưu tiên `unavailable` → `out_of_stock` → `price_changed`**; `Changed` tính riêng theo giá của dòng bán được (dòng hết hàng mà lệch giá vẫn làm `Changed = true`). Dòng `unavailable` không có giá CRM nên không vào tổng, không làm `Changed`; dòng `out_of_stock` vẫn tính giá vào tổng (website hiện số tiền đúng nếu khách giảm số lượng). Phương án khác: danh sách vấn đề mỗi dòng (hợp đồng phức tạp hơn).
5. **Trùng biến thể trong một yêu cầu → 400** (`ErrInvalidInput`) thay vì cộng dồn: giỏ website mỗi biến thể một dòng; cộng dồn làm thứ tự dòng trả về khác thứ tự gửi.
6. **Báo giá không kiểm lượt mỗi khách của mã (`customer_limit`)**: báo giá chưa có hồ sơ khách (chỉ SĐT tùy chọn), không tra khách theo SĐT trong route chỉ đọc (dữ liệu cá nhân của `customer`); `promotion.Check` bỏ qua khi `CustomerRootID` rỗng. Tạo đơn (`Redeem`) kiểm và Task 193 báo `CouponInvalidError`. Phương án khác: tra khách theo SĐT bằng `customer` (thêm phụ thuộc, thêm đọc dữ liệu cá nhân).
7. **Lớp HTTP là kiểu mới `web.Orders`** (không gắn vào `web.Catalog`): báo giá, tạo đơn đọc ghi thẳng PostgreSQL, không dùng bộ đệm; Task 193/194 thêm vào đây. Tệp handler `handlers_web_orders.go`.
8. **`Fingerprint`** = hex sha256 của JSON (struct, thứ tự trường cố định) gồm từng dòng (biến thể, số lượng, giá CRM, giảm chiến dịch, giảm mã, vấn đề, mã + revision chiến dịch), phí, tổng, mã (chữ hoa, đã cắt khoảng trắng), mã có áp không. Không có SĐT.
9. `customerPhone` tối đa 32 ký tự (dạng nào cũng được, `promotion` chuẩn hóa khi so); `couponCode` tối đa 64 (như `order.Create`).

## Việc phát hiện thêm

- **Cho Task 193 (M5-10):** `order.Create` của M2 tính nền mã trên mọi dòng (tạm tính − giảm dòng − giảm đơn) và chia mã về mọi dòng. Báo giá theo QĐ29 loại dòng thuộc chiến dịch cấm mã khỏi nền mã. Nếu `CreateFromWeb` đưa giảm chiến dịch vào `LineDiscount` rồi gọi `order.Create` nguyên như cũ, số giảm mã của đơn sẽ khác báo giá (ví dụ [WB06]: báo giá 20.000, đơn 60.000) và mọi đơn có dòng như vậy bị coi là lệch. Task 193 cần cách truyền "dòng không áp mã" vào `Create`/`Redeem` (hoặc ghi rõ khác biệt) — không sửa ở task này (ngoài phạm vi).
- Thêm test báo giá với chiến dịch phạm vi `categories` (đọc `ProductCategories` thật) — reviewer ghi chú; có thể gộp vào test của Task 193.

## Reviewer

Kết luận: ĐẠT (agent `reviewer`, vòng 1, trên phần đã stage sau khi ci-task xanh). Phát hiện phải sửa: không có. Ảnh hưởng dự kiến khớp thực tế (thêm `WebQuoteCampaign` thuộc QĐ3); 13 dòng bằng chứng đều trỏ tới test có kiểm đúng điều nêu. Ghi chú không bắt buộc: (1) chưa có test báo giá với chiến dịch phạm vi `categories` (câu `ProductCategories` chỉ chạy gián tiếp; luật khớp danh mục đã test ở `promotion`) — ghi ở Việc phát hiện thêm; (2) mọi dòng `unavailable` và mã % không tối thiểu → `Applied = true`, giảm 0 (không sai tiền); (3) xác nhận phát hiện cho Task 193 về nền mã của `order.Create`.
