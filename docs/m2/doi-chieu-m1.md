# Đối chiếu hợp đồng M0, M1 với mã thật trước khi làm M2

Task 57 (M2-00), 18/11/2026, trên `main` tại `ca62e09` (merge Task 393). Đối chiếu từng giả định G1–G17 của kế hoạch
M2 (`../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md`, mục "Giả định về mốc trước") với **mã đã gộp**.

Cách lấy bằng chứng:

- Go: `go doc -all` cho `internal/modules/{catalog,inventory,customer,order,identity}`, `internal/platform/{files,xlsx}`,
  `internal/testkit`, `internal/modules/inventory/invtest`, `internal/modules/catalog/catalogtest`.
- Database: một database thử tạo bằng `testkit.StartPostgres` trên PostgreSQL chung (cổng 5600), migrate đủ tới 0128,
  đọc `information_schema.columns` theo `ordinal_position`, `pg_constraint`, `pg_sequences`, `platform.file_purposes`,
  `platform.goose_db_version`; database tự xóa khi test xong (tệp test tạm, không commit).
- Tệp: `openapi/openapi.yaml` (`createOrder`, `getOrder`, `searchVariants`, `createUpload`…), `internal/httpapi/errors.go`,
  `internal/httpapi/handlers_order.go`, `tests/arch/boundaries_test.go`, `docs/adr/0046-thu-tu-khoa.md`, `docs/views.md`,
  `docs/quyen.md`, `console/src/app/*`, `console/playwright.config.ts`, `console/e2e/fixtures.ts`, `cmd/crm/main.go`.

Luật áp dụng (kế hoạch, Task M2-00): chỉ khác tên hay chữ ký → task M2 dùng tên thật ghi ở đây; có nhánh "Nếu thiếu"
→ làm theo nhánh; thiếu mà không có nhánh → dừng hỏi. **Kết quả: không có dòng "thiếu" nào; không phải dừng.** Các chỗ
khác chữ ký đã sửa luôn trong kế hoạch M2 (cùng commit), ghi ở cột "Kế hoạch đã sửa".

## Bảng G1–G17

| # | Kết quả | Tên thật, chữ ký thật (chỗ khác kế hoạch) | Kế hoạch đã sửa |
|---|---|---|---|
| G1 | khớp | `catalog.sellers(id, tenant_id, code, name, kind, status, created_at, updated_at, version)`; `catalog.DefaultSeller(ctx, tx) (uuid.UUID, error)`; thêm `EnsureDefaultSeller(ctx, tx, tenantID)`, hằng `DefaultSellerCode = "ELIMAZ"`, view `catalog.sellers_v(seller_id, code, name, kind, status)` | — |
| G2 | khác (thêm cột) | `catalog.variants` có thêm `color_hex`, `cost_price` (không `deleted_at`, đúng). `catalog.variants_v` theo thứ tự thật: `variant_id, seller_id, product_id, sku, barcode, product_code, product_name, color, color_hex, size, price, cost_price, variant_status, product_status, sellable, updated_at` | Dòng G2 |
| G3 | khớp | `catalog.VariantForSale(ctx, tx, sellerID, variantID) (SaleVariant, error)`; `catalog.VariantsForSale(ctx, tx, sellerID, ids) (map[uuid.UUID]SaleVariant, error)` **đã có**; `SaleVariant{ID, SellerID, ProductID; SKU, Name string; Price int64}`. Nhánh "thiếu" của Task M2-16 không cần | — |
| G4 | khớp | `GET /api/v1/catalog/variants/search` `searchVariants` (`catalog.read`), tham số `q` (1–100 ký tự), `limit` (1–100, mặc định 20), không `sellerId` | — |
| G5 | khác (thêm cột) | Bảng và khóa ngoại `stock_reservations (tenant_id, order_item_id) → ord.order_items` đúng. Thêm: `stock_reservations` có `release_reason`, `expires_at`, `state_changed_at`, unique `(tenant_id, order_item_id, variant_id, warehouse_id)`; `stock_available_v` có thêm `updated_at` ở cuối | Dòng G5 |
| G6 | khác (chữ ký) | Đổi tồn là **phương thức** của `inventory.Writer{Events *events.Registry; Queue queue.JobQueue; Realtime realtime.Bus}`, không phải hàm gói: `w.Reserve(ctx, tx, []ReserveLine, a Actor) ([]uuid.UUID, error)`, `w.Release(ctx, tx, ids, reason, a)`, `w.StartTransit(ctx, tx, ids, a)`, `w.Consume(ctx, tx, ids, a)`. Bốn hàm theo dòng đơn **đã có**: `w.ReleaseHeldByItems(ctx, tx, orderItemIDs, reason, a)`, `w.StartTransitByItems`, `w.ConsumeByItems` (phương thức của `Writer`) và hàm gói `inventory.DefaultWarehouse(ctx, tx) (uuid.UUID, error)`; thêm hàm đọc `ReservationStatesByItems(ctx, tx, orderItemIDs) (map[string]int, error)`. `Availability`, `EnsureDefaultWarehouse` là hàm gói; `ReserveLine`, `Actor{Type string; ID uuid.UUID}`, `*InsufficientError{VariantID, WarehouseID; Requested, Available int32}`, `ErrInsufficient`, `ErrStateConflict` khớp. Nhánh "thiếu" của Task M2-16 (`byitems.go`) không cần; hàm M2 tự viết ở Task M2-22 (`ReleaseInTransitByItems`, `ReceiveCustomerReturn`, `MoveToDamaged`) cũng là phương thức của `inventory.Writer` | Dòng G6, Task M2-16, 18, 21, 22 |
| G7 | khác (nhỏ) | `customer.customers(id, tenant_id, root_id!, full_name! (1–120), created_at, updated_at, version)`, khóa ngoại `(tenant_id, root_id) → customers`: khớp. Sequence `ord.order_code_seq`, `order.nextCode` trong `order/code.go`, không có `ord.order_counters`: khớp. `ord.orders` đúng cột kế hoạch (thêm `created_at, updated_at`); `CHECK status` **đã có đủ bảy trạng thái** `draft, new, confirmed, cancel_requested, cancelled, delivered, returned`; `CHECK channel IN ('facebook','web','manual')`; `CHECK grand_total = subtotal − discount_total + shipping_fee_charged`, `cod_amount ≤ grand_total`; unique `(tenant_id, code)`, `(tenant_id, idempotency_key)`, `(tenant_id, seller_id, id)`. `ord.order_items`: có `position` (≥ 1), `CHECK net_amount = quantity·unit_price − line_discount − allocated_order_discount`; unique theo vị trí là **`(tenant_id, seller_id, order_id, position)`** (kế hoạch ghi thiếu `seller_id`); hai khóa ngoại ghép đúng. `ord.order_status_history` đúng; `reason` phải khớp `^[a-z][a-z0-9_]{0,63}$`, `actor_type IN (staff, carrier, system, customer)`. View: `customer.customers_v(id, root_id, full_name, created_at)`, `ord.orders_v(id, tenant_id, code, seller_id, customer_id, status, channel, grand_total, cod_amount, current_shipment_id, created_at)` — **đúng cột, đúng thứ tự** (R3) | Dòng G7; Task M2-05 không thêm lại giá trị `status` |
| G8 | khác (chữ ký) | `customer.Create(ctx, tx, fullName) (uuid.UUID, error)`, `customer.ErrInvalidInput`: khớp. `CreateManual` là **phương thức** `order.Writer.CreateManual(ctx, tx, in CreateManualInput, actor order.Actor) (Order, error)`, với `order.Actor{TenantID, UserID uuid.UUID}` (không phải `identity.AuthUser`: `order` không nhập `identity`). `CreateManualInput{IdempotencyKey uuid.UUID; SellerID, CustomerID, WarehouseID uuid.UUID; Items []ItemInput{VariantID; Qty int32}; ShipAddress; ShippingFee int64}`. `order.AttachShipment(ctx, tx, sellerID, orderID, shipmentID) error` (có `sellerID`, ADR-29); `order.Writer.ApplyShipmentProgress(ctx, tx, sellerID, orderID, shipmentID, step) (applied bool, err error)` (phương thức, có `sellerID`); thêm `order.Lock(ctx, tx, sellerID, id) error`. `order.Get(ctx, tx, sellerID, id)`: khớp. `ShipAddress{Name string; Phone logging.Phone; Line logging.Address}` (`Line` là `logging.Address`, không phải `string`). Sentinel có sẵn: `ErrOutOfStock` (chi tiết `*OutOfStockError{VariantID; Available int32}`), `ErrNotForSale`, `ErrMixedSellers`, `ErrTotalOverflow`, `ErrInvalidInput`, `ErrDuplicateOrder`, `ErrNotFound`, `ErrShipmentAlreadyAttached`, `ErrStatusConflict`. Hằng `StatusConfirmed`, `StatusDelivered`; sự kiện `order.created`, `order.confirmed`, `order.delivered` (payload `Changed{orderId, sellerId, version}`). Tệp: `order/{order.go,create.go,code.go,shipment_progress.go,errors.go,events.go}`, `queries/ord/{code,history,items,orders}.sql`. API: thân `createOrder` là `{customer: {fullName} (bắt buộc), warehouseId?, items[]{variantId, qty}, shipAddress{name, phone, line}, shippingFee}` — **không có `customerId`, không có `idempotencyKey` trong thân**: khóa nghiệp vụ là `orderBusinessKey(userID, Idempotency-Key)` (UUID v5) dựng ở handler. `Order` trả `id, code, status, customerId, subtotal, shippingFee, grandTotal, codAmount, createdAt, items[]{id, position, variantId, sku, name, qty, unitPrice, netAmount}` (chưa có `version`, `discountTotal`, `channel`). Mã lỗi: `out_of_stock` 409 (kèm `variantId`, `available`), `variant_not_for_sale` 422, `mixed_sellers` 422, `total_overflow` 422, `ErrDuplicateOrder` → 422 `idempotency_key_reused`, `ErrInvalidInput` → 400 `invalid_input`, `ErrNotFound` → 404 `not_found`. `ErrShipmentAlreadyAttached`, `ErrStatusConflict` **chưa** được ánh xạ ở `errors.go` | Dòng G8; Task M2-02, 16, 18 |
| G9 | khớp | `tests/arch/boundaries_test.go` `moduleCalls`: `order → customer, catalog, inventory, promotion`; `promotion → catalog, customer, inventory`; `customer → identity` (đã có cả dòng M2). Còn thiếu đúng phần kế hoạch giao Task M2-02: ngoại lệ "chỉ đọc view" `customer` → `ord.*_v` | — |
| G10 | khớp | Gói `internal/platform/xlsx` có sẵn (nhánh "tách gói" của Task M2-13 không cần): `Read(r io.ReaderAt, size int64, lim Limits, header []string) (rows []Row, cellErrs []CellError, err error)`, `Limits{MaxBytes, MaxRows, MaxUnzipped, MaxCellChars}`, `DefaultLimits()` (5 MiB, 20.000 dòng, 50 MiB, 2.000 ký tự), `Sanitize`, `Write`, lỗi `ErrNotXlsx`, `ErrTooLarge`, `ErrTooManyRows`, `ErrHeaderMismatch`, mã ô `formula_not_allowed`. Ô có công thức thành lỗi ô (không tính); ô chữ bắt đầu bằng `=` được đọc như chữ | Task M2-13 |
| G11 | khác (nhỏ) | `files.FileStore` có đủ `PresignPut`, `PresignPutAnySize`, `PresignGet`, `Head`, `Open(ctx, b, key, maxBytes)`, `CopyIfAbsent`, `Put`, `Delete`. Bảng `platform.file_purposes(key, bucket, mode)` — có thêm cột **`mode`** (`standard` mặc định, `private` cho ảnh riêng tư); mục đích M1: `product_image` (originals), `catalog_import` (private). `files.RegisterPurpose(files.Purpose{Key, Bucket, AllowedTypes, MaxBytes, Image, Upload, Private})`; test `TestPurposes_GoMatchesTable` có; khi khởi động `files.CheckPurposes(ctx, d)`. `CreateUpload(ctx, tx, fs, UploadInput{TenantID, Purpose, FileName, ContentType, SizeBytes}, actor)`, `CompleteUpload(ctx, d, fs, q, …)`. Hàm dùng lại cho tệp nhập: `files.ReadContent(ctx, d, fs, tenantID, fileID, purpose, maxBytes) ([]byte, error)` — đọc dòng tệp trong transaction ngắn rồi đọc kho **ngoài** transaction (ADR-46), đúng việc Task M2-13 cần. Operation `createUpload`, `completeUpload`, `getFile`: khớp. M1 nối kiểm tệp nhập catalog bằng **lệnh** `commands` (`catalog.NewImportValidator`, `cmd/crm/main.go` `newCommands`), không phải `queue.Handler` | Task M2-13 |
| G12 | khác (thêm) | `testkit.SeedSellers(t, d, tenantID) (elimaz, partner)`, `testkit.ForEachSeller(t, sellers, fn)`, `invtest.SeedStock(t, tx, variantID, warehouseID, onHand int32)`: khớp. Có thêm bộ dựng sẵn nên dùng lại khi viết `testkit.SeedCatalog`: `invtest.NewShop(t, tx, tenantID) *Shop{Tenant, Seller, Partner, Warehouse}` và `(*Shop).Variant(t, tx, seller, price int64, onHand int32) uuid.UUID`; `catalogtest.{Sellers, Seller, Product, Variant}` | Task M2-02 |
| G13 | khác (cấu trúc) | `console/src/app/{router.tsx, queryClient.ts, errors.ts, idempotency.ts}` có; `useWriteAction<TReq, TRes>(fn: (key, body) => Promise<TRes>, opts?)` đúng. **Khác:** từ Task 393, màn hình **tự đăng ký**: mỗi màn là thư mục `console/src/screens/<tên>/route.tsx` (khai `screens: ScreenRoute[]`), `src/app/screens.ts` tự nạp bằng `import.meta.glob`; không sửa `router.tsx` khi thêm màn. Có `console/src/shared/` (thành phần dùng chung). Playwright: dự án `chromium` (mọi `*.spec.ts`) và `webkit` iPhone 13 (`*.phone.spec.ts`); khung `DESKTOP` 1512×767, `PHONE` 390×844 ở `console/e2e/fixtures.ts`. Test: `npm test` (vitest), `npx tsc --noEmit`, `npx eslint .`, `npx playwright test` | Task M2-27–30, bảng Phụ thuộc |
| G14 | khớp | `docs/mockups/m1/` (trang `.html` + `khung.js` + `m1.css` + thư mục `png/`) | — |
| G15 | khác (nhỏ) | `docs/adr/0046-thu-tu-khoa.md` có nhóm 2, 4, 5, 7, 8, 9, 10, 11, 12a, 12b, 13 như kế hoạch. Nhóm 5 **đã có** `customer.customers → customer.customer_identities` (spec); nhóm 7 **đã có** `ord.orders → ord.order_items → ord.order_status_history → ord.returns → ord.return_items` (spec); nhóm 10 đã có `promotion.coupons → flash_sales → flash_sale_items → flash_sale_claims`. Task M2-02 chỉ chèn tên chưa có, không dời `returns`, `return_items` | Task M2-02 |
| G16 | khác (cơ chế) | `identity.RoleDef{Key, Name string; Permissions []Permission}`, `var DefaultRoles`, `EnsureDefaultRoles(ctx, tx, tenantID)` (chỉ thêm; vai trò `customized` không nhận thêm trừ `owner`): khớp. Vai trò `manager` **tự tính** = mọi quyền trừ `identity.users.manage`, `identity.roles.manage`, `platform.*` (quyền M2 tự vào, không phải liệt kê). **Thêm:** `identity.SyncDefaultRoles(ctx, owner *db.DB)` chạy **sau mỗi lần migrate** (vai trò `migrate`, Task 315) cho mọi tenant — tenant cũ nhận quyền mới mà không cần migration riêng. Quyền M1 có: `catalog.read/manage/import`, `order.create`, `order.read`, `files.upload`, `platform.files.manage`, `inventory.suppliers.manage`, `identity.roles.manage`, `inventory.read`, `inventory.documents.create/post`, `inventory.stocktake`, `inventory.stocktake.apply`, `fulfillment.read`, `fulfillment.ship`. Vai trò: `owner` Chủ, `manager` Quản lý, `sale` Bán hàng, `warehouse` Kho, `accountant` Kế toán. `docs/quyen.md` và `permissions_doc_test.go` có | Task M2-01 |
| G17 | khớp | Migration cuối của M1: **0128** (`0128_identity_custom_roles.sql`; dải M1 0112–0128, bỏ trống 0126) < 0200 | — |

## Kiểm riêng (khác thì dừng, hỏi) — đều đạt

| Điểm | Kết quả |
|---|---|
| `customers` có `full_name`, `root_id` | Có, cả hai `NOT NULL` |
| `order_items` có `position` | Có, `NOT NULL`, ≥ 1 |
| Cột và thứ tự `customer.customers_v` | `id, root_id, full_name, created_at` — đúng |
| Cột và thứ tự `ord.orders_v` | `id, tenant_id, code, seller_id, customer_id, status, channel, grand_total, cod_amount, current_shipment_id, created_at` — đúng |
| Sequence `ord.order_code_seq`, `order.nextCode` trong `code.go`, không `ord.order_counters` | Đúng |
| `platform.file_purposes`, `files.RegisterPurpose` | Có (bảng có thêm cột `mode`) |
| `docs/quyen.md`, `permissions_doc_test` | Có |
| `VariantsForSale` (G3), bốn hàm theo dòng đơn (G6) | Có cả năm |
| Gói đọc Excel (G10) | `internal/platform/xlsx` |

## Chỗ lệch kéo theo quyết định cho task M2

Các quyết định dưới đây đã ghi vào kế hoạch M2 (mục "Quyết định thay chủ shop", mã Q-T57-x) và sửa vào task tương ứng.

1. **Đường ghi của `order`, `inventory` là phương thức của `Writer`** (mẫu M1 Task 27). Hàm ghi mới của M2 trong `order`
   (`Create`, `AttachShipmentBy`, `DetachShipment`, `OnShipmentEvent`, `ApplyPaymentTotals`, `Edit`, `Cancel`…) là phương
   thức của `order.Writer`; gọi tồn bằng `inventory.Writer{Events, Queue, Realtime}` dựng từ cùng trường. Hàm đọc vẫn là
   hàm gói.
2. **`order.Actor` của M1 là `{TenantID, UserID}`**. M2 thêm trường `Type string` (rỗng = `staff`; `system`, `carrier`,
   `customer`) vào chính kiểu này, không khai kiểu `Actor` thứ hai; chỗ kế hoạch viết `Actor{Type, ID}` đọc là
   `Actor{TenantID, UserID, Type}`.
3. **Hàm đơn nhận `sellerID`** (ADR-29) như M1: `AttachShipmentBy`, `OnShipmentEvent`, `DetachShipment` thêm tham số
   `sellerID` ngay sau `tx`, như `AttachShipment`, `ApplyShipmentProgress` của M1.
4. **Khóa nghiệp vụ của `createOrder`**: giữ cách M1 (`orderBusinessKey(userID, Idempotency-Key)` ở handler); thân yêu cầu
   **không** thêm `idempotencyKey`. Gửi lại cùng khóa nhưng khác nội dung dùng lại sentinel M1 `ErrDuplicateOrder` → 422
   `idempotency_key_reused` (không khai `ErrOrderKeyReused`, mã `order_key_reused`).
5. **Thân `createOrder`**: M1 có `customer{fullName}` bắt buộc, không có `customerId`. M2 nới `customer` thành tùy chọn
   (nới bắt buộc của yêu cầu: oasdiff không báo phá vỡ), thêm `customerId` tùy chọn và `customer.phone`,
   `customer.phoneIsCustomers`; phải có đúng một trong hai (`customerId` hoặc `customer`), sai → 400 `invalid_input`.
6. **Vai trò mẫu**: không viết `migrations/0200_identity_m2.sql`; quyền M2 vào tenant đã có qua `SyncDefaultRoles` (chạy
   sau mỗi migrate, cùng luật "chỉ thêm, `customized` không nhận, `owner` luôn đủ"). Số 0200 bỏ trống.
7. **Ánh xạ lỗi M1 còn thiếu**: Task M2-02 thêm `ErrShipmentAlreadyAttached` → 409 `shipment_already_attached`. Lỗi
   `ErrStatusConflict` của M1 (bất biến tồn lệch trạng thái đơn) giữ nghĩa cũ; từ chối chuyển trạng thái của M2 là
   `ErrInvalidTransition` mới (422 `invalid_transition`).
8. **Nhập khách Excel (Task M2-13)**: đọc tệp bằng `files.ReadContent` (đã đọc kho ngoài transaction) rồi
   `xlsx.Read(bytes.NewReader(b), int64(len(b)), lim, header)`; migration 0208 chèn `('customer_import', 'private')`
   (cột `mode` để mặc định `standard`). Việc chạy nền giữ là `queue.Handler` như kế hoạch (đọc kho của chính CRM, chạy lại
   được, không cần trạng thái `in_doubt` của `commands`); ghi lý do khác mẫu `catalog` vào mục Tái dùng của task.
9. **Màn hình M2-27–30**: thêm màn bằng thư mục `console/src/screens/<tên>/`; không sửa `router.tsx`. Bốn task chỉ còn
   đụng chung `console/src/app/errors.ts` (bảng câu lỗi), nên vẫn chạy nối tiếp như bảng Phụ thuộc.

## Task M2 nào chạy được ngay

Phần M1 chưa gộp vào `main` (18/11): 45 (chép kho ngoài), 46 (diễn tập kho tệp), 52 (màn Nhập Excel), 53, 330, 331
(màn Sức khỏe, Sự kiện lỗi, Tệp mất), 323 (nhập Excel — áp dụng), 325–329 (màn Sản phẩm ảnh, bài viết; màn Kho nhà cung
cấp, phiếu, kiểm kho), 334 (danh sách tồn có lọc), M1-55 (đóng mốc M1; bảng số toàn cục ghi 56 cho M1-55), và M0-23
(CI, mutation testing, sổ tay, staging). Không task M2 nào dùng hàm, bảng hay màn của các task này.

| Task M2 | Chạy được ngay? | Lý do |
|---|---|---|
| 01–25 (máy chủ) | Có, theo thứ tự trong bảng Phụ thuộc | Chỉ dùng phần M1 đã gộp (G1–G12, G15–G17 đã kiểm ở trên) |
| 26 (mockup) | Có | `docs/mockups/m1/` và màn M1 mẫu (Sản phẩm, Kho, Nhân viên, Vai trò) đã có |
| 27–30 (màn hình) | Có, khi các task máy chủ ghi trong bảng Phụ thuộc xong và mockup đã chốt | Công cụ màn hình M1-48, khung, `useWriteAction`, màn tự đăng ký đã gộp |
| 31 (thử tải) | Có, sau 10, 16–24 | Không cần phần M1 còn mở |
| 33 (chung trước M3/M4) | Có, sau 02, gộp sau 13 | `0290` trong dải riêng; migration lệch thứ tự đã có (Task 318) |
| 32 (đóng mốc M2) | **Phải chờ** | Cần M1 đóng mốc (M1-55) và M0-23 (mutation testing `.gremlins.yaml`, review độc lập, staging): chưa có trong `main` |

## Mốc nền khác

- `make ci` trên `main` (`ca62e09`): kết quả ghi ở tệp task 57, mục Bằng chứng.
- Role `crm_search` (chủ hàm tìm `SECURITY DEFINER`, BYPASSRLS) đã có trong `docker/postgres/init/01-roles.sql`; mẫu hàm
  tìm tự lọc `tenant_id` là `migrations/0123_catalog_search.sql` của M1 — Task M2-10 dùng lại cách viết này, không đổi
  hạ tầng.
- Module `customer` của M1 chưa có tệp test riêng (`customer.Create` được kiểm qua test của `order`); Task M2-03, M2-09
  là nơi đầu tiên có `internal/modules/customer/*_test.go`.
- Module của M1 **không** có `module.go`: handler việc nền, lệnh, lịch nối thẳng trong `cmd/crm/main.go`. `module.go`
  của Task M2-02 là cách mới cho ba module M2 (kế hoạch giữ), không dời phần nối của M1.
- jscpd: `jscpd-baseline.json` có **0** dấu vân tay (không đoạn trùng nào được miễn); `make jscpd` chặn mọi đoạn trùng mới.
- Lệnh test của console: `cd console && npm test && npx tsc --noEmit && npx eslint .`; e2e `npx playwright test`
  (máy chủ thử và cách chụp: `console/e2e/fixtures.ts`).
