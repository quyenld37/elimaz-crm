# View chỉ đọc công bố giữa module

Viết trước ở Task M1-25 (kế hoạch M1, Q37; A16, A17, B11). Một module chỉ đọc dữ liệu của module khác qua view ở
đây; gói `internal/queries` chỉ đọc view ở đây. Luật đi kèm:

- Tên `<schema>.<tên>_v`, `WITH (security_invoker = true)` (RLS của bảng gốc áp cho người đọc), `GRANT SELECT … TO
  crm_app`. `tenancy.CheckSchemaRules` luật (f) kiểm tên và `security_invoker`.
- Module `A` được nhắc view của module `B` khi `A` được gọi `B` theo cột "Được gọi" (04 mục 3; `moduleCalls()` trong
  `tests/arch/boundaries_test.go`) và view có dòng ở bảng dưới. View `platform.*_v` có ở bảng dưới thì mọi module và
  `internal/queries` được nhắc. Kiểm bằng `tests/arch/sqlusage_test.go`.
- Cột ghi đúng thứ tự trong view. `tests/arch/viewsdoc_test.go` so mọi view `_v` có trong database với dòng ở đây
  (dòng của view chưa tạo được bỏ qua; view có trong database mà thiếu dòng là lỗi).
- Mốc chủ của module mở rộng view bằng `CREATE OR REPLACE VIEW`, **chỉ thêm cột vào cuối**, giữ tên, kiểu, thứ tự cột
  đã có (Q-X2); sửa dòng ở đây trong cùng commit. Cột khóa của view lát mỏng (`ord.orders_v`,
  `fulfillment.shipments_v`) tên là `id` như bảng.

## View công bố

| View | Cột (theo thứ tự) | Task | Mốc đọc |
|---|---|---|---|
| `identity.users_v` (B2-11: bảng của M0, M1 công bố vì Task 26 đã `ALTER` `identity`; không có `email`) | `id, display_name, status` | 26 | M6, M7 |
| `catalog.sellers_v` | `seller_id, code, name, kind, status` | 27 | M2–M7 |
| `catalog.products_v` | `product_id, seller_id, code, name, name_search, description, article, status, category_id, deleted_at, updated_at` | 27 | M2, M5–M7 |
| `catalog.variants_v` | `variant_id, seller_id, product_id, sku, barcode, product_code, product_name, color, color_hex, size, price, cost_price, variant_status, product_status, sellable, updated_at` | 27 | M2–M7 |
| `catalog.categories_v` | `category_id, parent_id, name, slug, depth, position, status` | 34 | M5–M7 |
| `catalog.attributes_v` | `attribute_id, code, name, position` | 34 | M5–M7 |
| `catalog.attribute_values_v` | `attribute_value_id, attribute_id, value, position` | 34 | M5–M7 |
| `catalog.tags_v` | `tag_id, code, name` | 34 | M5–M7 |
| `catalog.product_tags_v` | `product_id, tag_id` | 34 | M5–M7 |
| `catalog.product_attribute_values_v` | `product_id, attribute_value_id` | 34 | M5–M7 |
| `catalog.product_images_v` (chỉ ảnh chưa xóa) | `image_id, product_id, file_id, color, position` | 44 | M5, M7 |
| `platform.file_variants_v` (A2-12; chỉ tệp `ready`, chưa xóa, mục đích chế độ `standard`, biến thể `public` — R10; đường dẫn = `FILES_PUBLIC_BASE_URL` + `/` + `key`; ghép với `catalog.product_images_v` theo `file_id`) | `file_id, preset_version, width, format, key` | 44 | M5 |
| `inventory.warehouses_v` (M3 thêm cột địa chỉ, SĐT vào cuối) | `warehouse_id, operated_by_seller_id, code, name, status` | 28 | M2–M7 |
| `inventory.stock_available_v` (view số dư duy nhất; B11: không có `stock_balances_v`, `availability_v`) | `variant_id, warehouse_id, seller_id, on_hand, held, in_transit, damaged, available, updated_at` | 28 | M2–M7 |
| `inventory.stock_ledger_v` | `ledger_id, variant_id, warehouse_id, bucket, delta, doc_type, doc_id, step, actor_type, actor_id, created_at` | 28 | M6, M7 |
| `inventory.reconcile_runs_v` | `run_id, ran_at, ok, checked, mismatch_count` | 36 | M6, M7 |
| `customer.customers_v` (M1 Task 30; M2 Task 60 nối 11 cột sau `created_at`; chỉ số chỉ đúng ở dòng `id = root_id`) | `id, root_id, full_name, created_at, tenant_id, code, lifecycle, birthday, order_count, delivered_count, returned_count, total_spent, last_order_at, updated_at, version` | 30, 60 | M2 trở đi |
| `ord.orders_v` (M1 Task 30; M2 Task 62 nối 26 cột sau `created_at`; mốc sau nối sau `version`) | `id, tenant_id, code, seller_id, customer_id, status, channel, grand_total, cod_amount, current_shipment_id, created_at, pending_reason, packing_status, payment_status, channel_account_id, traffic_source_id, conversation_id, external_ref, order_type, warehouse_id, ship_phone, address_complete, subtotal, line_discount_total, order_discount, coupon_discount, discount_total, shipping_fee_charged, prepaid_total, created_by, care_by, confirmed_at, cancelled_at, cancel_reason, draft_expires_at, duplicate_suspect, version` | 30, 62 | M2 trở đi (`customer` đọc: ngoại lệ chỉ đọc view, Q19) |
| `customer.customer_identities_v` (M2, viết trước ở Task 59) | `tenant_id, id, customer_id, kind, scope, value, verified_at, source, created_at` | 60 | M3–M6 |
| `customer.customer_addresses_v` (chỉ địa chỉ chưa xóa) | `tenant_id, id, customer_id, recipient_name, phone, province_code, ward_code, street, is_default` | 60 | M3, M5 |
| `customer.customer_tags_v` | `tenant_id, id, name, color` | 60 | M4, M6 |
| `customer.customer_tag_links_v` | `tenant_id, customer_id, tag_id, source` | 60 | M4, M6 |
| `ord.order_items_v` (M2, viết trước ở Task 59) | `id, tenant_id, seller_id, order_id, variant_id, position, sku_snapshot, name_snapshot, quantity, unit_price, line_discount, allocated_order_discount, net_amount` | 62 | M2 (`customer` đọc: ngoại lệ chỉ đọc view, Q19), M3, M5, M6 |
| `ord.order_status_history_v` (`from_status` của dòng tạo đơn là `''`, kể cả dòng M1 ghi `NULL`) | `id, tenant_id, order_id, from_status, to_status, from_pending, to_pending, actor_type, actor_id, reason, occurred_at, recorded_at` | 62 | M2 (`customer` đọc: ngoại lệ chỉ đọc view, Q19), M6 |
| `ord.returns_v` | `id, tenant_id, code, order_id, shipment_id, kind, status, refund_amount, replacement_order_id, received_at, created_at` | 62 | M2 (`customer` đọc: ngoại lệ chỉ đọc view, Q19), M3, M6 |
| `ord.return_items_v` (M3 nối ba cột số lượng vào cuối, R11) | `id, tenant_id, return_id, order_item_id, quantity, condition, refund_amount` | 62 | M3, M6, P2 |
| `ord.traffic_sources_v` | `id, tenant_id, key, name, channel_account_id, active, position` | 62 | M4, M5, M6 |
| `promotion.coupons_v` (B2-11; không có `bound_phone`, `bound_customer_id`) | `id, tenant_id, code, batch_id, name, discount_kind, value, max_discount, min_subtotal, channels, seller_id, max_uses, used_count, per_customer_limit, valid_from, valid_to, status, created_at, updated_at` | 63 | M5, M7 |
| `fulfillment.shipments_v` (tối thiểu; M3 mở rộng) | `id, order_id, code, carrier_key, carrier_account_id, tracking_code, status, cod_amount, fee` | 31 | M3 trở đi |
| `cash.cod_balances_v` (tối thiểu; M3 mở rộng) | `shipment_id, account, balance` | 32 | M3 trở đi |

## Khóa ngoại xuyên module

Đánh đổi có chủ ý (ADR-27 mục 4): bảng của một module có khóa ngoại tới bảng của module khác khi dữ liệu không được
phép lệch nhau. Module chủ của bảng bị trỏ không đổi khóa (cột, unique) mà khóa ngoại dựa vào khi chưa sửa dòng ở đây.

| Khóa ngoại | Vì sao | Task |
|---|---|---|
| `inventory.stock_reservations (tenant_id, order_item_id) → ord.order_items (tenant_id, id)` `ON DELETE SET NULL (order_item_id)` | giữ hàng luôn thuộc một dòng đơn có thật (M1); từ M2 sửa đơn bỏ được dòng đã nhả hàng: dòng giữ hàng `released` và sổ tồn còn, chỉ mất liên kết (`order_item_id` cho phép rỗng; `0204` của `ord` `ALTER` bảng của `inventory`) | 30, 62 |
| `ord.orders (tenant_id, warehouse_id) → inventory.warehouses (tenant_id, id)` | kho giữ hàng của đơn | 62 |
| `ord.order_items (tenant_id, seller_id, variant_id) → catalog.variants (tenant_id, seller_id, id)`; `ord.orders (tenant_id, seller_id) → catalog.sellers`; `ord.orders (tenant_id, customer_id) → customer.customers` | một đơn một người bán (ADR-29, KH14); đơn trỏ khách có thật | 30 |

## Hàm tìm công bố

Tìm dưới RLS mà không Seq Scan (spec 04 mục 7, đo ở Task M0-05): hàm `SECURITY DEFINER` chủ `crm_search`, tự lọc
`tenant_id` theo `app.tenant_id` (ngoài ngữ cảnh: 0 dòng) và theo người bán gửi vào; chỉ `crm_app` được gọi. `q` đã qua
`textnorm.Fold`; `raw` là chuỗi gốc bỏ khoảng trắng đầu cuối (so barcode). Gọi trong `tenancy.WithTenant`; chỉ `internal/queries` gọi (M2, M4 dùng qua `searchVariants`).

| Hàm | Trả về | Task |
|---|---|---|
| `catalog.search_variants(p_seller uuid, q text, raw text, lim int)` (≤ 100 dòng; barcode đúng, rồi SKU, mã mẫu theo tiền tố, rồi tên — các từ theo thứ tự; sắp theo hạng rồi SKU. Nhóm khớp quá `lim` dòng chỉ góp `lim` dòng: SKU nhỏ nhất, mã mẫu nhỏ nhất (theo thứ tự byte của index, có thể khác thứ tự collation của kết quả), tên không dấu ngắn nhất — Task 390) | `variant_id, product_id, sku, barcode, product_code, product_name, color, size, price, sellable` | 39, 390 |
| `catalog.search_products(p_seller uuid, q text, raw text, lim int)` (≤ 1.000 mẫu, cùng cách khớp) | `SETOF uuid` (mã mẫu) | 39 |

## Sự kiện công bố

Sự kiện nội bộ (`events.Registry.Emit`, spec 04 mục 5.4) mà mốc sau nghe. `Seq` là `version` mới của đối tượng.
Payload chỉ mang mã và số (A16): consumer đọc dữ liệu qua view ở trên. Mốc chủ chỉ thêm khóa vào payload, không đổi
hay bỏ khóa đã có.

| Topic | Aggregate | Payload | Phát khi | Task |
|---|---|---|---|---|
| `product.updated` | `product` (mã mẫu) | `{productId, sellerId, version}` | tạo, sửa mẫu; thêm, sửa biến thể (version mẫu tăng) | 27 |
| `price.changed` | `variant` (mã biến thể) | `{productId, sellerId, variantIds: [<id>], price}` | giá bán của biến thể đổi | 27 |
| `stock.changed` | `balance:<variant>:<warehouse>` | `{variantIds: [<variant>], warehouseId, sellerId}` | số dư tồn đổi | 28 |
| `order.created` | `order` (mã đơn) | `{orderId, sellerId, version}` | tạo đơn (seq 1) | 30 |
| `order.confirmed` | `order` (mã đơn) | `{orderId, sellerId, version}` | đơn sang Đã xác nhận (đơn nhập tay: ngay khi tạo, seq 2) | 30 |
| `order.delivered` | `order` (mã đơn) | `{orderId, sellerId, version}` | vận đơn hiện hành báo giao xong, đơn sang Đã giao | 333 |
| `customer.created` | `customer` (mã hồ sơ) | `{customerId, version}` | tạo khách (seq 1; kể cả lớp bọc `customer.Create` của M1) | 66 |
| `shipment.status_changed` | `shipment` (mã vận đơn) | `{shipmentId, status, codAmount}` | vận đơn tiến trạng thái (`awaiting_pickup`, `shipping`, `delivered`; chỉ tiến) | 31 |

## Ngoại lệ luật (g)

Luật (g) của `tenancy.CheckSchemaRules` (ADR-29): bảng có cột `seller_id` thì mọi unique khác khóa chính và khác
`(tenant_id, id)` có `seller_id` ngay sau `tenant_id` (mã hàng duy nhất theo người bán). Một unique cố ý duy nhất trên
cả shop được miễn khi index có `COMMENT ON INDEX <schema>.<index> IS 'seller-exempt: <lý do>'` **và** có dòng ở bảng
dưới; `viewsdoc_test` đòi hai tập bằng nhau.

| Index | Lý do | Task |
|---|---|---|
| `ord.orders_code_key` | mã đơn duy nhất trên cả shop (05, Q26: một sequence sinh mã) | 30 |
| `ord.orders_idempotency_key_key` | khóa nghiệp vụ của lần tạo đơn duy nhất trên cả shop (05, 04 mục 5.5) | 30 |
| `fulfillment.carrier_accounts_ref_key` | một tài khoản ở hãng (`carrier_key`, `external_account_ref`) chỉ thuộc một người bán trong shop | 31 |
| `promotion.coupons_code_key` | khách gõ mã ở một ô chung của shop: mã duy nhất trên cả shop, kể cả mã của người bán (05 mục 2) | 63 |
