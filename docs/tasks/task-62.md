# Task 62 — M2-05: Schema đơn hàng (mở rộng bảng M1), phiếu trả

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md`, Task 62 (M2-05): mở rộng đơn của M1, phiếu
trả, nguồn đơn có khóa, view `ord`, mã đơn một chỗ; ràng buộc tổng tiền kiểm lúc commit. Loại việc (quy trình mục 3): A

Cột và thứ tự hiện hành của `ord.orders_v` (R3, chép từ `docs/m2/doi-chieu-m1.md` dòng G7 và mục kiểm):
`id, tenant_id, code, seller_id, customer_id, status, channel, grand_total, cod_amount, current_shipment_id, created_at`
— kiểu `uuid, uuid, text, uuid, uuid, text, text, bigint, bigint, uuid, timestamptz`.

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): chỉ **thêm**, không đổi chữ ký có sẵn.
  - `order.Order`: thêm trường M2 (`WarehouseID`, `PendingReason`, `PackingStatus`, `PaymentStatus`, `OrderType`,
    `LineDiscountTotal`, `OrderDiscount`, `CouponDiscount`, `PrepaidTotal`, `ConfirmedAt`, `DraftExpiresAt`, …).
    Chỗ dựng `Order{…}` bằng tên trường: `order/create.go` (insertOrder), `order/order.go` (Get); chỗ đọc:
    `internal/httpapi/handlers_order.go`, `order/*_test.go`, `testkit/shiptest` — thêm trường không làm hỏng chỗ nào.
  - `order.Item`: thêm `LineDiscount`, `AllocatedOrderDiscount`.
  - `order.ShipAddress`: thêm `ProvinceCode`, `WardCode` (JSON `provinceCode`, `wardCode`, `omitempty` để JSON của đơn
    M1 không đổi).
  - Mới: `order.EnsureDefaults` (gọi trong `cmd/crm/seed.go` seed-dev), `order.SourceByKey`, `order.ErrTrafficSourceNotFound`, hằng khóa nguồn
    (`SourceKeyManual`…); hàm không xuất `nextReturnCode`.
  - `order.Get` (M1) đọc bằng câu mới `GetOrderBase` (`queries/ord/orders_base.sql`) để điền đủ trường M2; câu M1
    `GetOrder` bị xóa (chỉ `Get` dùng; luật "chuyển cách mới thì xóa cách cũ").
  - `Writer.CreateManual` (M1, chữ ký giữ): ghi thêm `warehouse_id` (cột mới `NOT NULL`, kho giữ hàng đã chọn sẵn
    trong `draft`), `ship_phone`, `ship_phone_reversed` (qua `customer.NormalizePhone`, `ReverseDigits`; không chuẩn
    hóa được → `''`), `confirmed_at`, `confirmed_by` (đơn M1 tạo thẳng `confirmed`). Câu `InsertOrder` thêm tham số.
    Dòng nhập cùng biến thể gộp làm một (`mergeLines`, QĐ1) vì unique một biến thể một dòng. Chỗ gọi `CreateManual`:
    `internal/httpapi/handlers_order.go`, `internal/testkit/shiptest`, test của `order`, `fulfillment`, `cash`.
  - testkit (mới, commit cấu trúc riêng): `MigrateBefore`, `OpenDB`, `InTenant` (`migrate.go`), `Column`, `Columns`,
    `CheckViews`, `CheckSchemaRulesClean`, `Staff.CheckOwnerAppendOnly` (`schemacheck.go`) — dời từ
    `customer/schema_test.go` (Task 60) để test schema của order dùng lại thay vì chép (jscpd báo 6 đoạn trùng).
- Gói phụ thuộc ngược (go list): `order` ← `cmd/crm`, `internal/httpapi`, `internal/modules/fulfillment`,
  `internal/testkit/shiptest`, `order/internal/*`. `inventory` (câu `InsertReservation` thêm `::uuid`, mã sinh đổi
  kiểu `models.StockReservation.OrderItemID`) ← mọi module gọi tồn. testkit ← mọi gói test.
- Operation OpenAPI bị chạm, oasdiff: không có (task không chạm `openapi.yaml`).
- Chỗ dùng trên màn hình: không có.
- Câu sqlc, bảng, pha migration:
  - `0204_order.sql` (pha **mở rộng**): `ALTER ord.orders` thêm 30 cột (có `DEFAULT` hoặc cho phép rỗng), điền dòng có
    sẵn theo từng tenant (`set_config('app.tenant_id')` trong vòng lặp, không tắt RLS), `SET NOT NULL warehouse_id` (dòng
    `-- contract:`), ràng buộc mới `NOT VALID` → `VALIDATE`; thay `CHECK cod_amount` của M1 (thêm mới, validate rồi bỏ
    cũ); `ALTER ord.order_items` thêm `updated_at`, `CHECK`, unique một biến thể một dòng; `ALTER
    ord.order_status_history` thêm `from_pending`, `to_pending`, mở `actor_type` thêm `service`; `ALTER
    inventory.stock_reservations`: `order_item_id` bỏ `NOT NULL`, khóa ngoại tới `order_items` thành `ON DELETE SET NULL
    (order_item_id)` (bảng của inventory, ghi mục đánh đổi trong `docs/views.md`); trigger ràng buộc
    `ord.check_order_totals` (`DEFERRABLE INITIALLY DEFERRED`); 6 bảng mới (`order_notes`, `traffic_sources`,
    `order_settings`, `order_duplicate_links`, `returns`, `return_items`); sequence `ord.return_code_seq`.
  - `0205_order_views.sql`: `CREATE OR REPLACE VIEW ord.orders_v` (11 cột M1 giữ vị trí 1–11, nối 26 cột), 5 view mới.
  - Câu sqlc: mới `queries/ord/orders_base.sql` (`GetOrderBase`), `queries/ord/settings.sql` (cài đặt, nguồn đơn);
    `queries/ord/code.sql` thêm `NextReturnCode`; `queries/ord/orders.sql` `InsertOrder` thêm cột, xóa `GetOrder`;
    `queries/inventory/reservations.sql` `InsertReservation` ép `@order_item_id::uuid` (giữ kiểu Go `uuid.UUID` sau khi
    cột cho phép rỗng). Mã sinh đổi theo `make gen` (`models.go` mọi store).
  - `docs/views.md`: dòng `ord.orders_v` 37 cột (R13) cùng commit với `0205`; mục đánh đổi khóa ngoại xuyên module.
  - `tests/arch/m2contract_test.go`: dòng `ord.orders_v` mong 37 cột (Task 59 ghi rõ sửa ở M2-05).
- Test sẽ chạy: trong lúc làm `go test -race ./internal/modules/order/...` (gồm test M1 Task 30, 333),
  `./internal/modules/customer/...`, `./internal/modules/inventory/...`, `./tests/arch/...`, `./cmd/crm/...`;
  script cổng kiểm của task một lần trước review (migration, testkit đổi → bộ toàn phần).

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` (testkit: `OpenStaff`,
  `CreateDatabase`, `SeedCatalog`; `tenancy.CheckSchemaRules`); grep `nextval('ord.order_code_seq')`, `NextOrderCode`,
  `NormalizePhone`, `ReverseDigits`, `migrateBefore`, `EnsureDefault`.
- Dùng lại / viết mới vì:
  - Mã đơn: dùng `order.nextCode` của M1 (R2), không viết hàm thứ hai; `nextReturnCode` viết cạnh nó cùng mẫu.
  - SĐT giao: dùng `customer.NormalizePhone`, `customer.ReverseDigits` (Task 60; `order` được gọi `customer`). Điền dòng
    có sẵn trong migration là bản SQL chạy một lần của cùng luật (như `name_search` của Task 60); test so với Go.
  - Helper test schema của Task 60 (`migrateBefore`, `open`, `inTenant`, `viewColumns`, vòng kiểm view,
    `CheckSchemaRules`, kiểm chỉ ghi thêm): dời sang testkit và dùng lại ở cả hai test (tránh bản thứ hai).
  - Mẫu bảng chỉ ghi thêm (`order_notes`): cùng mẫu trigger theo câu lệnh của `order_status_history` (M1).
  - `EnsureDefaults` theo mẫu `catalog.EnsureDefaultSeller`/`inventory.EnsureDefaultWarehouse` (chạy lại được).

## Soát thiết kế

Task dựng schema, không có đường ghi API mới; các mục liên quan:

- 5.1 Đúng chủ dữ liệu: mọi bảng mới trong schema `ord` của module `order` (04 mục 3; `returns`, `return_items` của M2
  là chủ duy nhất, Q-X2, B7). `inventory.stock_reservations` chỉ bị `ALTER` khóa ngoại tới `ord.order_items` (đánh
  đổi có chủ ý, ghi `docs/views.md`).
- 5.2 Không ghi bảng module khác: migration chỉ đọc `inventory.stock_reservations`, `inventory.warehouses` để điền
  `warehouse_id` (chạy bằng owner, một lần). Mã Go `order` không đọc bảng `inventory`.
- 5.3 Module khác đọc `ord` qua 6 view `security_invoker`, cột đúng `docs/views.md`.
- 5.7 Ngữ cảnh shop: bảng mới có `tenant_id`, `enable_tenant_rls`, `UNIQUE (tenant_id, id)` (bảng có `id`), unique và
  khóa ngoại ghép `tenant_id`; luật (g) với bảng có `seller_id`. Test tenant B không thấy dữ liệu A; `CheckSchemaRules`.
- 5.12 Theo module mẫu: SQL tĩnh qua sqlc, test gói `_test` trên PostgreSQL thật.
- 6.1 Sửa đồng thời: bảng mới có `version`; không có đường ghi mới.
- 6.3 Thứ tự khóa: dòng nhóm 7 của ADR-46 đã có từ Task 59 (`orders → order_items → order_duplicate_links →
  order_status_history → returns → return_items → traffic_sources → order_settings`); trigger tổng tiền chỉ đọc dòng
  của đơn bị chạm (không khóa).
- 6.10 Tiền: `bigint` đồng, `CHECK BETWEEN 0 AND 9007199254740991`; tổng đơn = Σ dòng kiểm ở database lúc commit
  (trigger ràng buộc trễ, B4), `cod_amount = max(grand_total − prepaid_total, 0)`.
- 6.12 Dữ liệu cá nhân: `ship_phone` chỉ ở bảng và view; không log. Thông báo lỗi trigger chỉ có mã đơn, tên cột, số.
- Không áp dụng: 5.4–5.6, 5.8–5.11, 6.2, 6.4–6.9, 6.11, 6.13 — không có đường ghi API, hàng đợi, gọi ra ngoài, quyền mới.

## Quyết định thay chủ shop

- **QĐ1 — `CreateManual` (M1) gộp dòng nhập cùng biến thể.** Kế hoạch thêm unique "một biến thể một dòng" và đòi test
  M1 vẫn đạt, nhưng `CreateManual` của M1 nhận hai dòng cùng biến thể (test rapid `TestCreateManual_NoOversell` sinh ca
  này) → hai yêu cầu mâu thuẫn. Chọn: `CreateManual` gộp dòng cùng biến thể (cộng số lượng, giữ vị trí lần đầu;
  tổng vượt int32 → `ErrInvalidInput`). Vì sao: API M1 vẫn nhận đúng các yêu cầu cũ (không thêm lỗi 400 mới), không
  đụng màn hình; dữ liệu đúng ràng buộc. Phương án khác: trả `invalid_input` khi trùng (đổi hành vi API). Đổi lại:
  bỏ `mergeLines`, trả lỗi.
- **QĐ2 — `from_status` của dòng tạo đơn.** Kế hoạch ghi "`from_status = ''` lúc tạo, như M1", nhưng M1 ghi `NULL`
  (cột cho phép rỗng) và bảng lịch sử chỉ ghi thêm (không sửa dòng cũ được). Chọn: bảng giữ nguyên;
  `ord.order_status_history_v` trả `COALESCE(from_status, '')` (cùng kiểu `text`) → người đọc luôn thấy `''`. Phương án
  khác: trigger đổi NULL → '' khi chèn (dòng cũ vẫn NULL). Đổi lại: `CREATE OR REPLACE VIEW` bỏ `COALESCE`.
- **QĐ3 — Unique dòng đơn có `seller_id`.** Kế hoạch ghi `UNIQUE (tenant_id, order_id, variant_id)`; luật (g) của
  `CheckSchemaRules` (ADR-29) đòi `seller_id` ngay sau `tenant_id` ở bảng có `seller_id`. Chọn
  `UNIQUE (tenant_id, seller_id, order_id, variant_id)` — tương đương vì một đơn một người bán (như M1 đã làm với
  unique vị trí, G7).
- **QĐ4 — `ErrTrafficSourceNotFound` là lỗi nội bộ** (không ánh xạ HTTP, ra 500 có log): `SourceByKey` chỉ được gọi với
  khóa hệ thống cố định; thiếu nguồn nghĩa là tenant chưa chạy `EnsureDefaults`. Ghi trong `internalSentinels` của
  `tests/arch/errors_mapped_test.go`. Đổi lại: thêm một dòng ánh xạ.
- **QĐ5 — Quyền `crm_app` trên bảng mới** (kế hoạch không ghi): `SELECT, INSERT` mọi bảng mới; `UPDATE` theo cột cho
  các cột đường ghi M2 đổi (trạng thái, tiền, địa chỉ, kho, chăm sóc, duyệt, hủy, nghi trùng; nguồn đơn; cài đặt;
  phiếu trả); không `DELETE` (Task sửa đơn M2-20 tự thêm `DELETE` dòng đơn khi cần); `order_notes` không sửa, không
  xóa (REVOKE + trigger). Không cấp `UPDATE` cho `customer_id`, `channel`, `code`, `external_ref`, `order_type`.
- **QĐ6 — Điền dòng có sẵn theo từng tenant** bằng `set_config('app.tenant_id', …, true)` trong vòng lặp `DO` (không tắt
  `FORCE ROW LEVEL SECURITY` của bảng `inventory`, `ord`). `confirmed_by = created_by` cùng `confirmed_at = created_at`
  cho đơn M1 (người tạo cũng là người xác nhận); `line_discount_total`, `order_discount` lấy từ dòng đơn; migration tự
  kiểm `subtotal = Σ dòng` và dừng nếu lệch.
- **QĐ7 — `order.Get` (M1) đọc bằng câu mới `GetOrderBase`** để điền đủ trường M2 của `Order`; câu `GetOrder` của M1
  bị xóa (chỉ `Get` dùng; "chuyển cách mới thì xóa cách cũ").

## Bằng chứng

Lệnh chung: `go test -race -count=1 ./internal/modules/order/ -run <test>` (bộ Docker chung crm-shared, mỗi test một
database riêng). Trước khi có mã: gói `order` không biên dịch (`got.WarehouseID undefined`, `undefined:
nextReturnCode`…); có mã Go nhưng bỏ tạm `0204`, `0205` → 12 test schema đỏ (`--- FAIL: TestSchemaExtendsM1Orders`,
`TestSchemaOrderMoneyChecks`, … `TestEnsureDefaults`). Sau khi viết: tất cả xanh.

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Đơn 680.000 − 20.000 + 30.000 = 690.000, COD 690.000 chèn được; `grand_total 690001` → 23514; COD ≠ max(grand − prepaid, 0) → 23514; dòng `net 480001` → 23514; giảm dòng vượt tiền dòng → 23514; `line_discount_total`, `subtotal` lệch tổng dòng → lỗi lúc commit nêu tên cột | `TestSchemaOrderMoneyChecks` | `-run TestSchemaOrderMoneyChecks` | `ok` |
| Giảm đơn 100.001 chia 50.001/18.000/32.000 commit được; 50.000/18.000/32.000 → commit lỗi 23514 chứa `allocated_order_discount` | `TestSchemaAllocatedDiscountAtCommit` | `-run TestSchemaAllocatedDiscountAtCommit` | `ok` |
| Dòng của đơn người bán S1 trỏ W1 (của S2) → 23503; hai dòng cùng biến thể → 23505 | `TestSchemaOrderItemRules` | `-run TestSchemaOrderItemRules` | `ok` |
| `draft` không `draft_expires_at` → 23514; `draft` có hạn + `discount_approval` được; `confirmed` có hạn nháp → 23514; `confirmed` + `awaiting_customer` → 23514 | `TestSchemaStatusRules` | `-run TestSchemaStatusRules` | `ok` |
| `UPDATE order_status_history`, `UPDATE`/`DELETE order_notes` (chủ bảng) → 42501 `append-only`; `actor_type 'service'` chèn được | `TestSchemaAppendOnly` | `-run TestSchemaAppendOnly` | `ok` |
| Phiếu hoàn hãng: một phiếu còn hiệu lực mỗi (đơn, vận đơn), phiếu `cancelled` không tính; mã sai mẫu → 23514 | `TestSchemaReturns` | `-run TestSchemaReturns` | `ok` |
| Tenant B không thấy dòng của A qua 5 view và `order_notes`, `order_settings`, `returns`; A thấy | `TestSchemaTenantIsolation` | `-run TestSchemaTenantIsolation` | `ok` |
| Sáu view có `security_invoker`, cột đúng dòng `docs/views.md`; `CheckSchemaRules` → `[]` (gồm luật (f), (g)) | `TestSchemaViewsAndRules` | `-run TestSchemaViewsAndRules` | `ok` |
| Mở rộng bảng M1: 2 đơn kiểu M1 trước 0204 (`DH0000001`, `DH0000002`) → sau migrate mã giữ, `position` giữ, kho = kho của dòng giữ hàng (W2) / `MAIN` khi không có, `pending_reason ''`, SĐT = `customer.NormalizePhone` (không chuẩn hóa được → ''), `confirmed_at = created_at`; chạm mọi đơn và dòng rồi commit (trigger đạt); không còn ràng buộc `NOT VALID`; `orders_v` 11 cột đầu cùng tên, kiểu, tổng 37; `order_code_seq` vẫn 2; `CreateManual` sau migrate ra `DH0000003`, ghi kho, SĐT, `confirmed_at`; `order.Get` đọc trường M2 | `TestSchemaExtendsM1Orders` | `-run TestSchemaExtendsM1Orders` | `ok` |
| `order_status_history_v` của đơn vừa tạo có `from_status ''` | `TestSchemaHistoryViewCreateRow`, `TestSchemaExtendsM1Orders` | `-run TestSchemaHistory` | `ok` |
| `nextReturnCode` hai lần → `TH0000001`, `TH0000002` | `TestNextReturnCode` | `-run TestNextReturnCode` | `ok` |
| Không mã ứng dụng nào ngoài `queries/ord/code.sql` (và mã sinh, chú thích `code.go`) gọi `nextval` của hai sequence mã | `TestCodeSequenceSingleSource` | `-run TestCodeSequenceSingleSource` | `ok` |
| `EnsureDefaults` ba lần → 1 dòng cài đặt (1440, 1440, 1000, 7), 10 nguồn; `SourceByKey("web")` → "Website"; đổi tên vẫn ra; khóa không có → `ErrTrafficSourceNotFound` | `TestEnsureDefaults` | `-run TestEnsureDefaults` | `ok` |
| Dòng nhập cùng biến thể gộp (1 + 3 → 4, vị trí 1), giữ hàng 4 | `TestCreateManual_MergesSameVariant` | `-run TestCreateManual_MergesSameVariant` | `ok` |
| Test M1 Task 30, 333 (gồm `ApplyShipmentProgress`, rapid không bán vượt) vẫn đạt | cả gói `order` | `go test -race -count=1 ./internal/modules/order/...` | `ok …/order 36.707s` |
| `viewsdoc_test` (dòng `ord.orders_v` 37 cột khớp view sau `0205`), `TestM2Contract_ViewsDoc`, lỗi được ánh xạ | `tests/arch` | `go test -race -count=1 ./tests/arch/` | `ok …/tests/arch 20.375s` |
| Cổng kiểm của task (build, vet, lint, gen-check, sqlc-vet, catalog, jscpd, gitleaks, oasdiff, test-guard; test toàn bộ vì đổi migration, testkit) trên phần đã stage | toàn bộ | script cổng kiểm của task (lần 2; lần 1 chạy khi chưa stage nên gen-check đỏ, jscpd báo trùng đã gom vào testkit) | mọi cổng tĩnh qua; test: mọi gói `ok` (gồm `httpapi`, `fulfillment`, `cash`, `order`, `inventory`, `customer`, `tests/arch`, `tests/e2e`) trừ `tests/deploy` `TestMakeMutation_SingleSourceAndFileEntries` — đỏ sẵn trên `main` sau Task 400, ghi ở "Việc phát hiện thêm" |
| Commit cấu trúc: test khách vẫn xanh sau khi dời helper sang testkit | `internal/modules/customer` | `go test -race -count=1 ./internal/modules/customer/ ./internal/testkit/` | `ok` cả hai |

## Test bị xóa hoặc tắt

Không xóa, không tắt test nào. Đổi theo hành vi mới (không nới quy luật):
- `internal/modules/order/property_test.go` `TestCreateManual_NoOversell` quy luật (2): số dòng giữ hàng mong bằng số
  **biến thể khác nhau** của đơn (trước: số dòng nhập), vì dòng cùng biến thể nay gộp (QĐ1). Quy luật (1), (3) giữ.
- `tests/arch/m2contract_test.go` `TestM2Contract_ViewsDoc`: dòng `ord.orders_v` mong 37 cột (Task 59 ghi rõ sửa ở M2-05).
- `tests/arch/errors_mapped_test.go`: thêm `order.ErrTrafficSourceNotFound` vào danh sách lỗi nội bộ (QĐ4).

## Khác kế hoạch

- Kế hoạch chỉ ghi sửa `order.go`, `code.go`; phải sửa thêm mã M1 vì cột mới `warehouse_id NOT NULL`: câu `InsertOrder`
  và `create.go` ghi `warehouse_id` (kho giữ hàng đã chọn), `ship_phone`, `ship_phone_reversed`, `confirmed_at`,
  `confirmed_by`; `queries/inventory/reservations.sql` ép `@order_item_id::uuid` (cột thành cho phép rỗng, giữ kiểu Go);
  helper test `inventory/env_test.go` chèn đơn thử có `warehouse_id`.
- Test "2 đơn tạo bằng `order.CreateManual` của M1 trước 0204": `CreateManual` của bản này ghi `warehouse_id` nên không
  chạy trên schema M1; test ghi đơn bằng đúng các cột của câu M1 (`InsertOrder`, `InsertOrderItem`,
  `InsertStatusHistory` tại `db91441`), mã từ cùng sequence → `DH0000001`, `DH0000002`; sau migrate `CreateManual` thật
  ra `DH0000003`.
- `CHECK unit_price BETWEEN …`, `allocated_order_discount >= 0`, `net_amount >= 0`, `sku/name NOT NULL` đã có từ M1 (G7)
  → không thêm lại; chỉ thêm `order_items_line_discount_within_check` (tên `order_items_line_discount_check` đã dùng).
  `actor_type` thêm `service` bằng ràng buộc mới rồi bỏ ràng buộc 4 giá trị (nới ra); thêm `CHECK` giá trị cho
  `from_pending`, `to_pending`. Index `(tenant_id, order_id, recorded_at)` của lịch sử đã có từ M1.
- Thêm ngoài danh sách, cùng hướng spec: `CHECK` mẫu mã phiếu trả `^TH[0-9]{7,}$`; `returns.status DEFAULT 'requested'`;
  `order_duplicate_links`: `CHECK ((resolved_at IS NULL) = (resolution IS NULL))`; `traffic_sources`: `CHECK` dạng
  `key`, tên 1–100 ký tự, `position >= 0`; index phụ `returns (tenant_id, order_id)`,
  `return_items (tenant_id, order_item_id)`, `order_notes (tenant_id, order_id, created_at)`,
  `order_duplicate_links (tenant_id, other_order_id)`.
- Câu SQL của cài đặt, nguồn đơn đặt ở tệp mới `queries/ord/settings.sql` (kế hoạch chỉ liệt `orders_base.sql`).
- `testkit.MigrateBefore` (dời từ test Task 60) là commit cấu trúc riêng trước commit hành vi.
- `docs/views.md`: thêm mục "Khóa ngoại xuyên module" (kế hoạch ghi "mục đánh đổi khóa ngoại xuyên module" nhưng tệp
  chưa có mục này).

## Việc phát hiện thêm

- `tests/deploy` `TestMakeMutation_SingleSourceAndFileEntries` đỏ trên `main` sau Task 400 (`8d398a8` sửa `Makefile`):
  "Makefile chép lại mục internal/platform/queue của HIGH_RISK_DIRS; phải đọc từ lib.sh". Task này không chạm
  `Makefile`, `tests/deploy`; không sửa (ngoài phạm vi).

## Reviewer

Commit cấu trúc (testkit + test khách): **ĐẠT** lượt 1. Không có phát hiện phải sửa; ghi chú: `CheckOwnerAppendOnly` chặt
hơn bản cũ (đòi thêm mã 42501); so `pe.Message` chứa `append-only` là cần thiết (trigger chỉ ghi thêm và thiếu quyền
cùng mã 42501).

Commit hành vi (migration 0204/0205, order, view, mã sinh): **ĐẠT** lượt 1. Không có phát hiện phải sửa. Ghi chú không
bắt buộc (không sửa, ghi lại cho task sau):
1. Ca "giảm dòng vượt tiền dòng" và "`grand_total 690001`" bị nhiều ràng buộc cùng bắt (test không chỉ ra ràng buộc
   nào); hành vi đúng không đổi.
2. `InsertTrafficSourceIfMissing` bỏ qua cả xung đột trùng tên: nguồn nhân viên tạo trùng tên trước khi seed thì nguồn
   hệ thống không có và `SourceByKey` trả `ErrTrafficSourceNotFound` (M4, M5 cần nhớ).
3. `SET NOT NULL warehouse_id` và unique một biến thể một dòng phát hành cùng mã mới — bản M1 cũ không được chạy song
   song khi deploy.
4. Chưa có test cho `ON DELETE SET NULL (order_item_id)` của `inventory.stock_reservations` — Task M2-20 (xóa dòng đơn)
   nên thêm.
Reviewer nêu: mục Ảnh hưởng chưa ghi `cmd/crm/seed.go` là chỗ bị sửa (gọi `order.EnsureDefaults`) — đã bổ sung.
