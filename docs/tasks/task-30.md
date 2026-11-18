# Task 30 — Khách tối thiểu, đơn tối thiểu (lát mỏng)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m1-san-pham-kho.md` (Task 30 (M1-30)). Loại việc (quy trình mục 3): A. Vùng rủi ro cao (`internal/modules/order`, `inventory`).

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không ký hiệu có sẵn nào đổi chữ ký. Thêm mới: gói
  `internal/modules/customer` (`Create`), gói `internal/modules/order` (`Writer`, `CreateManual`, `Get`, `Order`,
  `Item`, `Actor`, `ItemInput`, `CreateManualInput`, `ShipAddress`, `OutOfStockError`, `AuditFields`, sentinel lỗi).
  `AttachShipment`, `ApplyShipmentProgress` thuộc Task 333 (M1-30b), không làm ở đây. `internal/httpapi`: thêm handler `CreateOrder`, `GetOrder` (gen.Handler đòi), thêm
  nhánh `orderError` trong `errors.go`, `errorBody` thêm hai trường tùy chọn (`variantId`, `available`; chỉ ghi khi
  `out_of_stock`) — `writeError` có 1 chỗ định nghĩa, mọi chỗ gọi giữ nguyên.
- Gói phụ thuộc ngược (go list): `internal/httpapi` ← `cmd/crm`, `tests/arch`, `tests/e2e`. `order`, `customer` mới,
  chưa ai nhập. `inventory` không đổi mã; `inventory_test` đổi helper `line` (dòng đơn thật vì có khóa ngoại mới).
- Operation OpenAPI bị chạm, oasdiff: thêm `createOrder` (POST /api/v1/orders, `order.create`, IdempotencyKey),
  `getOrder` (GET /api/v1/orders/{id}, `order.read`); schema mới `CreateOrderRequest`, `Order`, `OrderItem`;
  `Error` thêm hai thuộc tính tùy chọn. Chỉ thêm mới, oasdiff không báo phá vỡ.
- Chỗ dùng trên màn hình: chưa có (màn đơn ở M2). `console/src/api/**` sinh lại nếu `make gen` sinh client.
- Câu sqlc, bảng, pha migration: `migrations/0117_customer_ord.sql` (pha mở rộng: CREATE bảng, sequence, view, GRANT;
  thêm khóa ngoại `inventory.stock_reservations(tenant_id, order_item_id) → ord.order_items(tenant_id, id)` — thêm ràng
  buộc trên bảng chưa có dữ liệu thật, không phải lệnh thu hẹp trong danh sách). Bảng `customer.customers`,
  `ord.orders`, `ord.order_items`, `ord.order_status_history`, sequence `ord.order_code_seq`, view
  `customer.customers_v`, `ord.orders_v`. Câu sqlc: `queries/customer/customers.sql`, `queries/ord/{orders,items,
  history,code}.sql`; khối sqlc mới cho `customer` và `order`.
- Test sẽ chạy: `go test -race ./internal/modules/order/... ./internal/modules/customer/...
  ./internal/modules/inventory/... ./internal/httpapi/... ./tests/arch/...` (inventory, order là vùng cao: chạy cả bộ);
  `make ci` một lần trước khi stage.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (idempotency, events,
  queue, tenancy, logging), `go doc` của `catalog`, `inventory`; tìm "nextval", "Reserve", "VariantsForSale",
  "logging.Phone", "WriteTx", "RecordingBus".
- Dùng lại / viết mới vì: dùng lại `catalog.VariantsForSale` + `catalog.VariantForSale` (phân biệt ngừng bán / khác
  người bán), `catalog.DefaultSeller`, `inventory.DefaultWarehouse`, `inventory.Writer.Reserve` (tự ghi sổ, sự kiện
  `stock.changed`),
  `events.Registry.Emit`, `identity.RecordAudit`, `httpapi.WriteTx`, `logging.Phone` (che SĐT), `testkit.OpenStaff`,
  `testkit.RecordingBus`, `invtest.SeedStock`, `catalog.MaxMoney`. Viết mới: phép cộng/nhân tiền có kiểm tràn trong
  `order` (chỉ order tính tiền đơn; `catalog.validMoney` chỉ kiểm khoảng, không cộng nhân) — giữ riêng vì khác lý do
  thay đổi.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1 Chủ dữ liệu: khách → `customer` (schema `customer`); đơn, dòng, lịch sử → `order` (schema `ord`), 04 mục 3.
- 5.2 Không ghi bảng module khác: handler gọi `customer.Create` rồi `order.Writer.CreateManual` trong cùng tx; giữ
  hàng và đổi tồn chỉ qua `inventory.Writer`; biến thể đọc qua `catalog.VariantsForSale`/`VariantForSale`. Ngoại lệ có
  chủ ý: migration 0117 thêm khóa ngoại lên `inventory.stock_reservations` (kế hoạch đòi, ADR-27 mục 4).
- 5.3 Đọc qua view: `order` không đọc bảng `catalog.*`, `inventory.*`, `customer.*` (chỉ gọi public API). Công bố
  `customer.customers_v`, `ord.orders_v` đúng cột `docs/views.md`.
- 5.4 Nguyên tử: tạo đơn + dòng + lịch sử + giữ hàng + sự kiện + audit + khóa chống trùng trong một transaction
  (`WriteTx`). Thiếu hàng → cả transaction lùi (0 đơn, 0 giữ hàng).
- 5.5 Không nguyên tử: `order.created`, `order.confirmed` qua `events.Emit` (outbox trong tx).
- 5.6 Gọi ra ngoài: không áp dụng (không gọi mạng; vận chuyển ở Task 31).
- 5.7 Ngữ cảnh shop: mọi truy vấn trong `tenancy.WithTenant`; bảng mới RLS bằng `platform.enable_tenant_rls`,
  `UNIQUE (tenant_id, id)`, khóa ngoại ghép `tenant_id`; sequence dùng chung mọi tenant (mã đơn duy nhất toàn hệ, Q26).
- 5.8 Nghiệp vụ không biết HTTP: `order`, `customer` không nhập `net/http`, `gen`; handler mỏng.
- 5.9 Đường ghi: `createOrder` có `x-permission: order.create` và `IdempotencyKey`; `getOrder` `order.read`.
- 5.10 Valkey: không áp dụng.
- 5.11 Thư viện mới: không.
- 5.12 Module mẫu: theo `catalog` (Writer tự ghi sự kiện, audit; hàm đọc nhận `sellerID`), store sqlc riêng.
- 6.1 Đồng thời: tồn trừ bằng câu điều kiện của `inventory.Reserve` (không đọc ra Go rồi ghi); mã đơn từ sequence
  (không khóa).
- 6.2 Gửi lại: `Idempotency-Key` (WriteTx); khóa nghiệp vụ `ord.orders.idempotency_key` unique — trùng → lỗi, lùi
  (không tạo đơn thứ hai).
- 6.3 Thứ tự khóa: nhóm 5 `customer` (dòng mới) → nhóm 7 `ord.orders` → `order_items` → `order_status_history` (dòng
  mới) → nhóm 11 `stock_balances` theo `(variant_id, warehouse_id)` tăng dần (Reserve tự sắp) → nhóm 13 khóa chống trùng.
  Khớp `docs/adr/0046-thu-tu-khoa.md` nhóm 5, 7 (không sửa tệp).
- 6.4 Lỗi tạm: `WriteTx`/`RetryTx` chạy lại tối đa 3 lần; fn gán lại kết quả mỗi lần; `Writer` không giữ trạng thái.
- 6.5 Cách ly: READ COMMITTED + câu điều kiện + khóa dòng.
- 6.6–6.8 Việc nền: không áp dụng (không thêm handler việc nền; consumer sự kiện ở mốc sau).
- 6.9 Treo lâu: giới hạn của `db.TxAPI` (M0).
- 6.10 Tiền: `int64` đồng, cộng nhân có kiểm tràn (`ErrTotalOverflow`), CHECK 0..9007199254740991 mọi cột tiền, CHECK
  `grand_total = subtotal − discount_total + shipping_fee_charged`, `net_amount = quantity × unit_price − …`.
- 6.11 Quy luật: rapid 200 lượt — tổng số lượng các đơn tạo được không vượt tồn; mỗi đơn có đủ dòng giữ hàng hoặc không
  tồn tại.
- 6.12 Dữ liệu cá nhân: `ShipAddress.Phone` là `logging.Phone`, `Line` là `logging.Address`, cả `ShipAddress` có
  `LogValue` che toàn bộ; audit (ghi ở handler) chỉ ghi mã đơn, số tiền, số dòng; không ghi `ship_address`, tên khách vào log, audit, sự kiện (payload chỉ mã và số).
- 6.13 Quyền: route khai `x-permission`; mặc định từ chối (guard M0).

## Quyết định thay chủ shop

1. **Đường ghi là phương thức của `order.Writer`** (`Events`, `Queue`, `Realtime`, `RequestID`) thay cho hàm gói
   `order.CreateManual(ctx, tx, in, actor)` như kế hoạch ghi: theo module mẫu Task 27 (CLAUDE.md), vì hàm cần
   `inventory.Writer` và `events.Registry`. Tên, tham số, kết quả giữ đúng kế hoạch. Khác: hàm gói nhận thêm tham số
   phụ thuộc — dài, dễ quên. Đảo ngược dễ.
2. **Khóa nghiệp vụ `idempotency_key` (uuid) của đơn nhập tay** = UUID v5 (SHA-1) của `"<mã nhân viên>:<Idempotency-
   Key>"`, vì `Idempotency-Key` của M0 là chuỗi `^[A-Za-z0-9_-]{1,128}$`, không phải uuid. Trùng khóa nghiệp vụ (cùng
   nhân viên, cùng khóa, khóa yêu cầu đã hết hạn) → `ErrDuplicateOrder` → 422 `idempotency_key_reused` (mã có sẵn),
   transaction lùi nên không có khách mồ côi. Khác: thêm trường `idempotencyKey` vào thân (M2 có thể thêm, tùy chọn).
3. **Biến thể không thuộc người bán của đơn** (của người bán khác **hoặc** không có) → `mixed_sellers` 422: `order`
   không đọc biến thể không lọc `seller_id` (ADR-29), nên không phân biệt hai trường hợp; có mà ngừng bán →
   `variant_not_for_sale` 422. Khác: thêm mã `variant_not_found` (M2 có thể thêm).
4. **Sự kiện và version của đơn:** đơn chèn với `version = 2`; `order.created` seq 1, `order.confirmed` seq 2 (bảng
   `domain_events` unique theo `(aggregate, seq)`). Payload `{orderId, sellerId, version}` (A16). Thêm hai dòng vào
   "Sự kiện công bố" của `docs/views.md`. Migration cấp sẵn `UPDATE (status, current_shipment_id, cancel_reason,
   updated_at, version)` trên `ord.orders` cho `crm_app` vì Task 333 không có migration.
5. **`UNIQUE (tenant_id, seller_id, order_id, position)`** thay cho `(tenant_id, order_id, position)`: cùng nghĩa (một
   đơn một người bán), đúng luật (g) mà không cần miễn. `ord.orders`: `UNIQUE (tenant_id, code)` và
   `(tenant_id, idempotency_key)` giữ đúng spec 05, có ghi chú `seller-exempt` và dòng ở `docs/views.md`.
6. **Chi tiết `out_of_stock`:** thân lỗi thêm `variantId`, `available` (kế hoạch đòi) — hai trường tùy chọn của
   `errorBody` trong `errors.go`, chỉ có ở mã này; không có vết lỗi.
7. **Test inventory dùng dòng đơn thật:** khóa ngoại mới làm `uuid.New()` làm mã dòng đơn không còn hợp lệ; helper
   `line` của `inventory_test` chèn khách, đơn, dòng đơn tối thiểu bằng chủ bảng (SQL trong test, không nhập `order`).
9. **`order` không nhập `identity`** (bảng "Được gọi" của 04 mục 3; `tests/arch` `TestBoundariesRealCode` chặn), dù
   Global Constraints của kế hoạch ghi "mọi module được nhập kiểu công khai của identity" và chữ ký kế hoạch dùng
   `identity.AuthUser`: spec đứng trên kế hoạch. Chọn: `order.Actor{TenantID, UserID}` (như `inventory.Actor`), audit
   `order.create` ghi ở handler bằng `identity.RecordAudit` trong cùng tx với `Order.AuditFields()` (không dữ liệu cá
   nhân). Khác: thêm `identity` vào danh sách của `order` trong test kiến trúc (nới test — không làm).
8. **`ShipAddress`**: `Name` (1–120), `Phone` (`logging.Phone`, 8–15 chữ số, có thể `+` đầu), `Line` (`logging.Address`,
   1–500); sai → `ErrInvalidInput` 400 `invalid_input` (mã có sẵn). Lưu jsonb `{name, phone, line}`.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| 2 × 250000 + ship 30000 → 500000 / 530000 / COD 530000, confirmed, held 2, mã `^DH[0-9]{7,}$`, khách root_id = id, lịch sử, order.created/confirmed | `TestCreateManual_Totals` | `go test -race -count=1 ./internal/modules/order/...` | `--- PASS: TestCreateManual_Totals`; `ok …/modules/order` |
| 100 đơn song song → 100 mã khác nhau | `TestCreateManual_CodesUnique` | như trên | `--- PASS` |
| Hai đơn tranh chiếc cuối → một 201, một 409 `out_of_stock` (kèm `variantId`, `available 0`), 1 đơn, held 1 | `TestOrderAPI_LastUnitRace` | `go test -race -count=1 ./internal/httpapi/ -run TestOrderAPI` | `ok …/httpapi` |
| Dòng 2 thiếu → 409; 0 đơn, 0 dòng, 0 giữ hàng, 0 khách; số dư dòng 1 không đổi | `TestCreateManual_SecondLineShort` | module order | `--- PASS` |
| KH14: biến thể người bán P → `mixed_sellers`; chèn thẳng dòng đơn bằng crm_app → lỗi khóa ngoại 23503 | `TestOrderItems_SellerFK`, `TestOrderAPI_Refusals` | module order, httpapi | `--- PASS` |
| KH12: ngừng bán → 422 `variant_not_for_sale`; đơn cũ GET được, `name_snapshot` giữ tên cũ | `TestCreateOrder_DiscontinuedVariant`, `TestOrderAPI_Refusals` | module order, httpapi | `--- PASS` |
| 50 lần cùng `Idempotency-Key` → 1 đơn, 1 khách; 201 cùng thân hoặc 409 `request_in_progress` | `TestOrderAPI_SameKey50` | httpapi | `ok` |
| Giá 9007199254740991 × 2 (và + ship, hai dòng) → `ErrTotalOverflow` / 422 `total_overflow`, không lưu | `TestCreateManual_TotalOverflow`, `TestOrderAPI_Refusals` | module order, httpapi | `--- PASS` |
| Đúng một operation `createOrder` cho POST /api/v1/orders; view đúng cột `docs/views.md`; miễn luật (g) khớp | `TestOrderAPI_Contract`, `tests/arch` `TestViewsDocMatchesDatabase` | `go test ./tests/arch/` | `ok …/tests/arch` |
| rapid 200 lượt: không bán vượt, mỗi đơn đủ giữ hàng hoặc không tồn tại | `TestCreateManual_NoOversell` | module order | `--- PASS: TestCreateManual_NoOversell` |
| Lịch sử chỉ ghi thêm; audit không có SĐT, địa chỉ | `TestOrderStatusHistory_AppendOnly`, `TestOrderAPI_CreateGet` | module order, httpapi | `--- PASS` |
| Inventory vẫn đúng với khóa ngoại mới | bộ `internal/modules/inventory` | `go test -race ./internal/modules/inventory/...` | `ok` |
| Cổng đầy đủ | — | `make ci` sau lần gộp `main` thứ hai (e863734), đã stage | build, vet, golangci-lint (0), gen-check, sqlc-vet, `go test -race ./...`: mọi gói `ok` trừ `TestInventoryAPI_InsufficientBody` (test của Task 29 dùng mã dòng đơn giả, đã sửa → `--- PASS`, chạy lại `-run 'TestInventoryAPI\|TestOrderAPI\|TestErrorCodes\|TestCatalog'` → `ok`); govulncheck "No vulnerabilities found"; apicheck, catalog-check, test-guard, gitleaks, console-check qua; jscpd: 2 đoạn trùng mới đều trong mã Task 19 trên `main` (không thuộc task này; xem "Việc phát hiện thêm"). Lần chạy đầu (máy tải ~22) có imager, queue, e2e boot lỗi do quá thời gian; chạy lại đều `ok`. |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- `CreateManual` là phương thức của `order.Writer` (Quyết định 1), nhận `order.Actor` thay cho `identity.AuthUser`
  (Quyết định 9).
- Kế hoạch được chia lại trong ngày 18/11 khi task đang chạy: gắn vận đơn và áp bước vận đơn sang Task 333 (M1-30b).
  Bản đầu của task này đã viết cả phần đó (reviewer lần 1 soát, test qua); đã gỡ khỏi commit để đúng phạm vi. Bản
  nháp (`shipment_progress.go`, test, câu `LockOrder`/`SetCurrentShipment`/`SetOrderStatus`/`OrderItemIDs`) lưu ở
  scratchpad của phiên (có thể bị dọn), chỉ để tham khảo; Task 333 làm lại đúng quy trình.
- Thêm tệp `internal/modules/order/events.go` (sự kiện, `AuditFields`), `env_test.go` ngoài danh sách tệp của kế hoạch; khối sqlc mới cho `customer`, `order` trong `sqlc.yaml`.
- `ShipAddress.Line` kiểu `logging.Address` (kế hoạch ghi `string`) để log tự che địa chỉ.
- Unique vị trí dòng đơn có `seller_id` (Quyết định 5).
- Thêm lỗi `ErrInvalidInput`, `ErrDuplicateOrder`, `ErrNotFound`, `customer.ErrInvalidInput`
  (đổi sang mã có sẵn `invalid_input`, `idempotency_key_reused`, `not_found`).
- Thêm gói `internal/modules/catalog/catalogtest` (`Seller`, `Product`, `Variant`, như `invtest`) để test của `order`
  và `httpapi` không chép SQL `catalog.*` (jscpd báo trùng); câu `InsertSellerIfMissing` nhận thêm `kind` (một chỗ
  gọi: `catalog.EnsureDefaultSeller`, truyền `own`). Thêm `invtest.Shop` (`NewShop`, `Variant`, `Product`: người bán
  ELIMAZ, P, kho MAIN, biến thể có tồn) dùng chung cho test `order` và `httpapi`.

- Hai đổi cấu trúc nhỏ trong cùng commit (không tách commit riêng vì chỉ cần sau khi gộp và để qua cổng):
  `readAsDefaultSeller` trong `handlers_catalog.go` (jscpd: `GetOrder` trùng `GetProduct`), và `writeDomainError` +
  `stockShortage` trong `errors.go` (funlen sau khi gộp nhánh kho của Task 29 với nhánh đơn). Hành vi giữ nguyên: test
  catalog, kho, đơn của httpapi qua.
- Gộp `main` hai lần (sau Task 398/399 và sau Task 29/19/319): `errors.go` giữ cả `insufficient_stock` (Task 29) và
  `out_of_stock`; `errorBody.VariantID` dùng kiểu `*string` của Task 29; `openapi.yaml` lấy bản `main` rồi thêm phần
  đơn; mã sinh lấy bản `main` rồi `make gen`.

- `internal/httpapi/handlers_inventory_test.go` (Task 29, `TestInventoryAPI_InsufficientBody`): giữ 3 cái bằng một
  đơn nhập tay thật thay cho `Reserve` với mã dòng đơn ngẫu nhiên — khóa ngoại mới của task này chặn mã giả. Điều test
  kiểm (409 `insufficient_stock` kèm `variantId`, `available 2`) giữ nguyên.

## Việc phát hiện thêm

- `main` (e863734) không qua `make jscpd`: 2 đoạn trùng mới do Task 19 thêm
  (`internal/platform/metrics/flusher_test.go` ~ `internal/platform/observability/helpers_test.go`;
  `internal/platform/observability/alerter.go` ~ `scripts/external-probe/probe.go`). Không sửa ở đây (ngoài phạm vi).
- Cho M2 (reviewer): `createOrder` nhận `warehouseId` mà chưa kiểm phạm vi kho của nhân viên.
- Trigger B4 (tổng `allocated_order_discount` bằng giảm cấp đơn) và `UNIQUE (tenant_id, channel, external_ref)` của 05
  chưa có: M1 không có giảm giá, chưa có cột `external_ref` — M2 thêm.
- Khóa ngoại `ord.orders.current_shipment_id → fulfillment.shipments` chưa có (bảng ở Task 31).
- Cho Task 333 (từ reviewer): trong `ApplyShipmentProgress`, đừng nuốt mọi `inventory.ErrStateConflict` của
  `StartTransitByItems` — `transition` có thể trả sentinel đó sau khi đã đổi trạng thái giữ hàng (bất biến hỏng);
  nên kiểm còn dòng `held` trước. Cho M2: `saleLines` đọc biến thể không khóa, biến thể vừa ngừng bán đồng thời vẫn
  lên đơn được (spec không đòi khóa).

## Reviewer

Kết luận: ĐẠT

- Lần 1 (trước khi tách Task 333, trước lần gộp main thứ hai): ĐẠT, không phát hiện phải sửa. Ghi chú: phần gắn và
  áp bước vận đơn thuộc Task 333 → đã gỡ khỏi commit; `ApplyShipmentProgress` không nên nuốt mọi
  `inventory.ErrStateConflict` → chuyển cho Task 333 ("Việc phát hiện thêm").
- Lần 2 (bản cuối đã stage, sau gộp e863734): ĐẠT, không phát hiện phải sửa. Đã kiểm: ánh xạ lỗi sau gộp giữ hành
  vi catalog, kho, tệp, identity (`stockShortage` xét trước không chồng lấn); phạm vi chỉ còn Task 30; giữ hàng cùng
  transaction và lùi hết khi từ chối; tiền kiểm tràn và CHECK; RLS, khóa ngoại ghép theo người bán; dữ liệu cá nhân
  không vào audit, sự kiện, log; `order` không nhập `identity`.
- Ghi chú không bắt buộc (chưa làm, ghi cho M2): `warehouseId` của đơn chưa so với phạm vi kho của nhân viên (kế
  hoạch M1 không đòi); lỗi không thuộc nhóm "từ chối" giữ khóa chống trùng tới hết 60 s (cách chung của M0).
