# M5 — Đối chiếu giả định của kế hoạch với mã thật M0–M3 (Task 183, M5-00)

Đọc ngày 21/11/2026 trên nhánh `task-183` tách từ `main` ở `a613197` (đã gộp M0–M2, M3 phần máy chủ Task 91–126, 108,
119, 448, 449 và màn M3 133, 136; trước khi review đã gộp `main` `19ef392` — Task 132 màn kết nối vận chuyển, không đổi dòng nào dưới đây). **M4 chưa làm** (chỉ có mockup Task 141; không có `migrations/04*.sql`, không có
module `channel`, `conversation`): dòng nào cần M4 ghi "M4 chưa có" và cách M5 chạy không cần M4.

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m5-website-flash-sale.md`, mục "Giả định về mốc trước" và bảng
"Đối chiếu với mã thật (Task 439, 21/11)". Tệp này kiểm lại **từng dòng** của cả hai bảng trên mã; bảng Task 439 đúng ở
mọi chỗ trừ các dòng đánh dấu **(sửa Task 439)** dưới đây. Thứ tự ưu tiên khi đọc chữ ký: tệp này → bảng Task 439 →
thân task trong kế hoạch.

Cách đọc đã dùng: `grep -n "^func" internal/modules/<module>/*.go`, `grep -n "CREATE TABLE\|CHECK\|UNIQUE" migrations/*.sql`,
`docs/views.md`, `grep -rn "Topic"` cho sự kiện, `tools/routegen/main.go`, `.claude/rules/openapi.md`,
`internal/adapters/carrier/registry.go`, `cmd/crm/wire_m3.go`; phía website `apps/storefront/src/server/nhanh/`.

Kết quả: **khớp** = tên và ý nghĩa đúng như kế hoạch; **khác tên** = có chức năng, tên hay chữ ký khác (ghi tên thật);
**thiếu** = chưa có chức năng.

## 1. Bảng giả định (A1–A6, B1–B3, C1–C2, D1, M0, M0b, M1r)

| # | Giả định (rút gọn) | Kết quả | Tên thật trong mã (tệp:dòng) | Task M5 phải sửa Interfaces |
|---|---|---|---|---|
| A1 | `catalog.sellers`, `DefaultSeller(ctx, tx)`, view `sellers_v` | khớp | `catalog.DefaultSeller` (`catalog/catalog.go:142`); `catalog.sellers_v(seller_id, code, name, kind, status)` (`docs/views.md:22`) | — |
| A2 | `variants_v`, `products_v` (cột theo M1 bản 2); M5-07 thêm `updated_at` của biến thể | khác (đã hơn) | `variants_v` có thêm `color_hex, cost_price, updated_at` (`docs/views.md:24`); `stock_available_v` có `updated_at` (`:34`). Migration 0502 chỉ thêm `weight_grams` (QĐ30) | M5-07 (bỏ phần thêm `updated_at`) |
| A2b | `categories_v`, `attributes_v` (có `position`), `tags_v` — "chờ M1" | khớp (đã có) | `categories_v(category_id, parent_id, name, slug, depth, position, status)`, `attributes_v(attribute_id, code, name, position)`, `tags_v(tag_id, code, name)`, thêm `product_tags_v(product_id, tag_id)` (`docs/views.md:25–29`) | M5-07 (dùng `product_tags_v` để gắn nhãn) |
| A2c | `product_images_v(image_id, product_id, file_id, color, position)`; `files.PublicVariants(ctx, tx, fileIDs, baseURL)` | khớp | `files.PublicVariants` (`platform/files/download.go:130`); view không có `updated_at` — mọi lần gắn, xếp, đổi màu, xóa ảnh gọi `imagesChanged` → `bumpProduct` (`catalog/images.go:370`), nên `products_v.updated_at` và `product.updated` đổi theo | M5-07: xem dòng "ảnh xử lý xong sau khi gắn" ở mục 3 |
| A2d | `catalog.ArticleHTML(ctx, tx, raw, baseURL string)` | khác tên | `catalog.ArticleHTML(ctx, tx, raw json.RawMessage, view catalog.ImageView)` (`catalog/article_html.go:56`); `ImageView{BaseURL; Preset}` (`catalog/images.go:47`); có bước gắn ảnh `ResolveArticleImages` (`:71`, R12) và `TestArticleHTML_WithImages` (`article_html_images_test.go:98`) | M5-07, M5-28 |
| A3 | `catalog.VariantsForSale(ctx, tx, sellerID, ids)` — "chờ M1" | khớp (đã có) | `catalog/variants.go:137`; `VariantForSale` (`:121`) giữ nguyên | — |
| A4 | Sự kiện `product.updated`, `price.changed`, `stock.changed`; payload `variantIds` là mảng; aggregate của tồn `balance:<variant>:<warehouse>` | khác (nhỏ) | Hằng `catalog.TopicProductUpdated`, `catalog.TopicPriceChanged` (`catalog/events.go:17–18`), `inventory.TopicStockChanged` (`inventory/movement.go:95`). Payload khớp (`ProductUpdated{productId, sellerId, version}`, `PriceChanged{productId, sellerId, variantIds, price}`, `StockChanged{variantIds, warehouseId, sellerId}`). Tồn: `AggregateType = "balance"`, `AggregateID = "<variant>:<warehouse>"` (`movement.go:116–118`), không có tiền tố `balance:` trong mã | M5-02 (consumer `webcache` dùng hằng của module, không viết chuỗi), M5-08 |
| A5 | `stock_balances`, `stock_ledger` (CHECK `doc_type`), `stock_reservations`, `stock_available_v`, `ErrInsufficient`, `*InsufficientError`, `inventory.reconcile` | khớp | `inventory/errors.go:14, 40`; `CheckLedgerVsBalance`, `CheckHeldVsReservations` (`inventory/reconcile.go:35–36`); CHECK `doc_type` 7 giá trị (`0115_inventory.sql:56`) | M5-17 mở rộng CHECK `doc_type` (QĐ31) |
| A5b | `inventory.DefaultWarehouse(ctx, tx)` — "chờ M1" | khớp (đã có) | `inventory/inventory.go:131` | — |
| A6 | Kho tệp: `NewFile`, `Register`, `RegisterPurpose`, `file_purposes`, `RegisterPrivateImage`, `file.process_private`, `Status`, `DownloadURL`, `testkit.MemFileStore` | khác tên (một chỗ) **(sửa Task 439)** | `files.NewFile` không có `Bucket` (`register.go:19`); `files.Register` (`:37`) từ chối mục đích ảnh/private bằng `ErrPurposeMode`; `files.Purpose{Key; Bucket; AllowedTypes; MaxBytes; Image; Upload; Private}` (`purpose.go:35`); `RegisterPrivateImage(ctx, tx, q, NewPrivateImage{ID; Purpose; Size; ContentType, FileName; CreatedBy})` (`process_private.go:18–31`); lệnh private đọc ảnh thô ở `BucketTmp` khóa `files.TmpKey(tenant, id)` (`process.go:244`) → M5-12 phải `FileStore.Put(BucketTmp, TmpKey…)` **ngoài** transaction rồi mới `RegisterPrivateImage`; `Status`, `DownloadURL` (`download.go:36, 61`), `ErrNotReady`, `ErrRejected` (`upload.go:50–52`). Bộ nhớ giả là **`filestest.MemFileStore(t) *files.MemStore`** (`platform/files/filestest/store.go:32`), không nằm trong `testkit` | M5-12 |
| B1 | `customer.NormalizePhone`, `FindOrCreateForOrder`, `AddIdentity`, `*IdentityTakenError` | khác tên (đã ghi ở Task 439) | Phương thức `customer.Writer` (`fororder.go:49`, `identity.go:52`); `NormalizePhone` (`phone.go:12`); `IdentityWebAccount` (`customer.go:47`), `SourceWeb`, `ActorService` (`write.go:39, 47`). **Tên rỗng:** khi SĐT đã có khách thì không tạo mới (dùng hồ sơ cũ, tên rỗng được); khi phải tạo khách, `validName` đòi 1–120 ký tự → `ErrInvalidInput` (`write.go:384–391`) | M5-10, M5-20: khách mới không tên đặt "Khách website" (QĐ-183-3) |
| B2 | `ord.orders` (kênh, `external_ref` unique có `seller_id`, trạng thái, `cancel_reason`), `order.Create/Cancel/Confirm/ExpireDrafts`, `SourceByKey`, lỗi M1 | khác tên (đã ghi ở Task 439) | `orders_external_ref_key UNIQUE (tenant_id, seller_id, channel, external_ref)` (`0204_order.sql:193`); `Writer.Create` (`create_m2.go:122`), `Writer.Cancel` (`cancel.go:55`), `Writer.Confirm` (`confirm.go:33`), `Writer.ExpireDrafts` (`drafts.go:33`), `order.SourceByKey` (`settings_defaults.go:69`), `SourceKeyWeb = "web"` (`:18`), `GetDetail` (`read.go:56`); lỗi `ErrOutOfStock`, `ErrNotForSale`, `ErrMixedSellers` (`errors.go:13–17`), `*OutOfStockError` (`:67`). **Thêm:** `pending_reason` CHECK chỉ `'', awaiting_customer, out_of_stock, discount_approval` (`0204_order.sql:151`) — cờ `price_review` của QĐ15 **chưa có** | M5-10 (mở rộng CHECK `pending_reason` thêm `price_review` ở 0503, hoặc ghi cờ ở cột riêng — QĐ-183-2) |
| B2b | `order.Lock(ctx, tx, orderID) (Snapshot, error)` | khác tên | `order.LockSnapshot(ctx, tx, sellerID, id) (Snapshot, error)` (`order/shipping.go:41`); `order.Lock(ctx, tx, sellerID, id) error` (`shipment_progress.go:55`) | M5-13 |
| B3 | `promotion.coupons`, `coupon_batches`, `Check`, `Redeem`, `Release`, `CreateBatch`, sinh mã, lý do từ chối | khác tên (đã ghi ở Task 439) | `promotion.Check` (`redeem.go:283`), `Writer.Redeem(…, o OrderRef, in)` (`:73`), `promotion.Release` (`:168`, hàm gói), `Writer.CreateBatch(ctx, tx, a, in)` (`batch.go:75`), `GetBatch` (`:218`), `NewCode`, `CodeAlphabet` (`codegen.go:10, 22`); lý do khớp 9 giá trị + `not_found` (`errors.go:37–46`); `EvalInput` có `CustomerID` (`evaluate.go:21`). `created_by uuid NOT NULL` không khóa ngoại ở cả hai bảng (`0206_promotion.sql:22, 52`) | M5-14 (ghi `ServiceKeyID` vào `created_by`) |
| C1 | `ActiveAccounts`, `QuoteNow`, `AccountFilter`, `FeeQuote`, `show_on_web`, `web_sort`; `CreateShipment` gọi `order.Lock` | khác tên (đã ghi ở Task 439); **thêm hiện trạng** | `(*Service).ActiveAccounts` (`fulfillment/accounts.go:468`), `(*Service).QuoteNow` (`quote.go:55`), `FeeQuote{…; RetryAfter; AuthCode}` (`quote.go:41`), `(*Service).Ship` (`area_create.go:114`). **Hiện trạng 21/11:** `cmd/crm/wire_m3.go:57–59` chỉ nối `carrier.All` khi `carrier.FakeOn` (ngoài production, có `FAKECARRIER_URL`); production có bộ adapter **rỗng**, tài khoản hãng là `mode = manual` (Task 448). `ActiveAccounts` lọc theo khả năng bỏ mọi tài khoản không có adapter (`accounts.go:490–495`) → báo phí web trên production luôn rỗng, chỉ còn phí dự phòng | M5-15 (QĐ-183-4) |
| C2 | `customer.ListProvinces`, `ListWards` | khớp | `customer/adminunits.go:41, 56` | — |
| D1 | Đơn nháp có hạn: `draft_expires_at`, `ExpireDrafts`, lịch `order.expire_drafts` (M2) | khớp | `IntentDraft` + `DraftTTL` (`create_m2.go:33, 115`); `ExpireDraftsSchedule = "order.expire_drafts"` (`drafts.go:24`). **M4 chưa có** nhưng không cần: flash sale dùng thẳng phần của M2 | M5-19 |
| M0 | Interface nền: `RetryTx`, `TxFlashBuy`, idempotency, `WriteTx`, events, schedules, realtime, testkit, `routegen` `IdempotencyMode` | khác tên (đã ghi ở Task 439) **(sửa Task 439 một chỗ)** | `db.TxFlashBuy{200 ms, 2 s}` (`platform/db/tx.go:36`); `idempotency.Acquire` (`idempotency.go:80`); `httpapi.WriteTx(ctx, d, tenantID, fn)` (`httpapi/idempotency.go:205`) không nhận tùy chọn transaction; `(*events.Registry).Emit/Register`, `(*schedules.Registry).Define`; `realtime.Signal{TenantID, Topic, Requires, Scope, Entity}` (`realtime/bus.go:50`) chưa có `System`; `tenancy.Resolve`, `ResolveTx`, `ListTenantIDs` (`tenancy/routes.go:17–39`). Ký JWT Access trong test là **`cfaccesstest.AccessKeyServer(t)`** (`internal/adapters/cfaccess/cfaccesstest/keys.go:57`), không có `testkit.AccessKeyServer`. `identity.AuthUser{UserID, TenantID, Email, Grants}` (`identity/permissions.go:127`) chưa có `Kind`, `ServiceKeyID` | M5-02 (`Signal.System`), M5-05, M5-06 (`AuthUser`), M5-20 (`WriteTx` với `TxFlashBuy`) |
| M0 (luật hợp đồng) | `x-idempotency` gồm `no-replay`; ba khóa M5 `x-auth`, `x-admission`, `x-body-limit` chưa có trong `.claude/rules/openapi.md` (C2-16, C3-4) | khớp; **C2-16 đã xong** | `no-replay` có ở `tools/routegen/main.go:259–263`; `.claude/rules/openapi.md` dòng 13 đã có luật 6 về `x-auth: service_key`, `x-admission`, `x-body-limit`. `routegen` **chưa đọc** ba khóa này (chỉ `x-permission`, `x-public`, `x-authenticated`, `main.go:155–157`); `x-authenticated: true` (Task 315) là khóa khác, chỉ cho route đọc của nhân viên — M5-05 không dùng lại tên đó | M5-05 (điều kiện "điều phối viên đã thêm ba khóa" đã đạt) |
| M0b | `platform.put_tenant_route` nhận `service_key` (R6); `tenancy.PutRoute`, `ErrRouteTaken` | khớp (đã có) | `0290_platform_tenant_route_fn.sql:25`; `tenancy/put_route.go:15, 22` | — |
| M1r | `identity.DefaultRoles`/`EnsureDefaultRoles`; `docs/quyen.md` + `permissions_doc_test` | khớp; **thêm** | `identity/roles.go:33, 86`; thêm `SyncDefaultRoles` (`:112`) chạy sau mỗi migrate (như Task 91 G-M1-9: không cần migration gán quyền); `identity/permissions_doc_test.go` | M5-06, 12, 16 (thêm quyền vào `DefaultRoles` + dòng `docs/quyen.md`, không viết migration gán quyền) |

Không có dòng nào **thiếu** chạm vùng rủi ro cao (mục 4).

## 2. Các điểm M5 phải chọn nhánh

| Điểm | Mã thật | Chọn (QĐ ở `docs/tasks/task-183.md`) | Task |
|---|---|---|---|
| `cancel_reason` khi khách hủy trên web | CHECK `orders_cancel_reason_q18_check`: `customer_changed_mind, out_of_stock, unreachable, wrong_info, other, draft_expired, duplicate` (`0204_order.sql:166–167`) | Dùng lại `customer_changed_mind`; nguồn "khách tự hủy trên web" ghi ở người làm (`order.Actor{Type: "service"}`) và audit, không mở rộng CHECK (QĐ-183-1) | M5-10, M5-13 |
| `updated_at` trên `variants_v`, `stock_available_v` | Cả hai đã có (`docs/views.md:24, 34`) | Không thêm; `changes?since=` lấy `greatest(products_v.updated_at, variants_v.updated_at)`; tồn đọc điểm cuối riêng | M5-07, M5-08 |
| `created_by` của mã và lô có khóa ngoại không | Không (`uuid NOT NULL`, `0206_promotion.sql:22, 52`) | Ghi `ServiceKeyID`, bỏ nhánh `created_by_service_key` (như Task 439) | M5-14 |
| Khóa JSON mã tỉnh trong `fulfillment.shipments.ship_to` | `provinceCode` (camelCase; `0331_fulfillment_shipments_v_province.sql:12`; `order.ShipAddress.ProvinceCode` `json:"provinceCode"` ở `order/order.go:78`); view `shipments_v.province_code` đã đọc đúng | Đọc cột `province_code` của `shipments_v`, không đọc JSON | M5-15 |
| `FindOrCreateForOrder` nhận tên rỗng không | Chỉ khi không phải tạo khách mới (xem B1) | Web và flash sale luôn gửi tên; tên khách để trống trên website → "Khách website" (QĐ-183-3) | M5-10, M5-20 |
| Cờ "giá đổi khi gửi từ hàng đợi" (QĐ15) | Không có `price_review` trong CHECK `pending_reason` | Thêm giá trị `price_review` vào CHECK `pending_reason` ở 0503 (pha mở rộng: thêm `NOT VALID` rồi `VALIDATE`), vì đơn web vào ở `new` và CHECK `orders_pending_status_check` đã cho `pending_reason` khác rỗng ở `new` (QĐ-183-2) | M5-10 |
| Báo phí ship cho web khi chưa có adapter hãng | Production không có adapter (C1) | Giữ QĐ19 (dự phòng mặc định tắt → `503 retry_later`); thêm việc cho chủ shop: nhập phí dự phòng trước khi bật nguồn CRM (QĐ-183-4) | M5-15, M5-31 |

## 3. Việc nhỏ phát hiện thêm (giao cho task sau, không làm ở đây)

- **Ảnh xử lý xong sau khi gắn (M5-07):** gắn ảnh làm tăng version mẫu, nhưng lệnh `file.process` chuyển tệp sang `ready`
  sau đó không đổi `products_v.updated_at`. Website có thể lưu bản sản phẩm lúc ảnh chưa có biến thể. M5-07 kiểm: hoặc
  `changes?since=` gộp cả thời điểm tệp ảnh của mẫu đổi trạng thái, hoặc chỉ trả ảnh `ready` và để vòng tải lại 60 s
  của bộ đệm lấy lại; ghi lựa chọn trong tệp task M5-07.
- **`order.cancelled` không có hằng xuất** (đã ghi ở Task 439, dòng M5-19): consumer của M5 so `"order." +
  string(StatusCancelled)` qua hằng của gói `order` nếu M5-19 thêm hằng xuất; không viết chuỗi rời.
- **Gói `internal/queries` phẳng** (đã ghi ở Task 439): `webcatalog_dynsql.go` đặt trong `internal/queries`; không có
  `internal/wiring` — nối ở `cmd/crm` và `Background(...)` của module.

## 4. "Đã bán 30 ngày" mà website đang hiện (cho M5-08)

Đọc ở `apps/storefront/src/server/nhanh/sync.ts:488–515` (`syncSales`), `mapping.ts:464–488` (`unitsSold`),
`source.ts:186–197`, `features/catalog/sold.ts:5–8` (tệp `state.ts` kế hoạch nhắc không có; khóa `sales30` nằm trong bảng trạng thái của `NhanhStore`, ghi ở `sync.ts`):

- Đếm **số đơn vị** (cộng `quantity`, làm tròn) theo **biến thể**, của đơn **tạo** trong 30 ngày gần nhất
  (`createdAtFrom = now − 30 ngày`), **mọi kênh bán** (không lọc kho, không lọc kênh; chủ shop duyệt 24/10/2026).
- Bỏ đơn có trạng thái Nhanh thuộc `NOT_SOLD = {58, 63, 64, 68, 71, 72, 74}` (đơn hủy, hoàn, thất bại) và đơn thử của
  website (ghi chú bắt đầu bằng `testOrderNote`).
- Website cộng số của mọi biến thể đang hiện của một mẫu → `soldRecently` của mẫu; chỉ hiện khi ≥ 100, dạng
  "Đã bán {làm tròn xuống bội 50}+ trong 30 ngày"; cũng dùng để xếp "bán chạy" (giỏ hàng, `commerce/service.ts:1824`).
- Làm mới mỗi ngày một lần.

Nghĩa tương ứng trong CRM cho M5-08: tổng `ord.order_items_v.quantity` theo `variant_id`, của đơn có `created_at ≥
now() − 30 ngày`, mọi `channel`, trạng thái **không** thuộc `draft, cancelled, returned` (đơn nháp chưa thành đơn; hủy
và hoàn tương ứng `NOT_SOLD`). CRM không có cờ "đơn thử": đơn thử của bộ e2e chạy trên CRM cục bộ, không tới CRM thật;
nếu sau này cần thì thêm cờ ở task riêng. Trả theo biến thể (website tự cộng theo mẫu như hiện nay), bộ đệm 1 ngày.

## 5. Thứ tự khóa (ADR-46) sau khi chèn bảng M5

Đã chèn vào `docs/adr/0046-thu-tu-khoa.md` đúng vị trí Global Constraints: nhóm 2 `identity.service_keys` sau
`user_sessions`; nhóm 7 `ord.order_requests` sau `order_settings`, `order_request_messages`, `order_request_files` chỉ
chèn; nhóm 8 `fulfillment.channel_carrier_settings` sau `carrier_locations` (nếu M5-15 chọn khóa `platform.settings`
thay bảng mới thì bảng này bỏ trống, khóa rơi vào nhóm 12c đã có). Nhóm 10 (`flash_sales → flash_sale_items →
flash_sale_claims`) và nhóm 11 (`flash_sale_units`) đã có sẵn từ trước, không đổi. Năm đường của M5 (lượt mua, nhả
suất, lên lịch, đóng đợt, đơn web) đi theo nhóm tăng dần trên bảng sau khi chèn: 5 → 7 (dòng mới) → 10 → 11 → 13;
đường nhả suất bắt đầu từ đơn (nhóm 7) rồi mới nhóm 10, 11 — đúng luật "bắt đầu từ dòng ở nhóm sau".

## 6. Điểm dừng A

Không có dòng **thiếu** chạm vùng rủi ro cao: đơn nháp có hạn và việc hết hạn có (D1); `Service.Ship` khóa đơn bằng
`order.LockSnapshot` trước (C1); `stock.changed` có (A4). Ba chỗ "khác" lớn nhất đã có cách đi trong phạm vi task M5
(mở rộng `WriteTx` cho `TxFlashBuy` ở M5-20; `Signal.System` ở M5-02; `AuthUser.Kind` ở M5-06). Không dừng.
