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
| `platform.file_variants_v` (A2-12; chỉ tệp `ready`, chưa xóa; đường dẫn = `FILES_PUBLIC_BASE_URL` + `/` + `key`; ghép với `catalog.product_images_v` theo `file_id`) | `file_id, preset_version, width, format, key` | 44 | M5 |
| `inventory.warehouses_v` (M3 thêm cột địa chỉ, SĐT vào cuối) | `warehouse_id, operated_by_seller_id, code, name, status` | 28 | M2–M7 |
| `inventory.stock_available_v` (view số dư duy nhất; B11: không có `stock_balances_v`, `availability_v`) | `variant_id, warehouse_id, seller_id, on_hand, held, in_transit, damaged, available, updated_at` | 28 | M2–M7 |
| `inventory.stock_ledger_v` | `ledger_id, variant_id, warehouse_id, bucket, delta, doc_type, doc_id, step, actor_type, actor_id, created_at` | 28 | M6, M7 |
| `inventory.reconcile_runs_v` | `run_id, ran_at, ok, checked, mismatch_count` | 36 | M6, M7 |
| `customer.customers_v` (tối thiểu; M2 mở rộng) | `id, root_id, full_name, created_at` | 30 | M2 trở đi |
| `ord.orders_v` (tối thiểu; M2 mở rộng) | `id, tenant_id, code, seller_id, customer_id, status, channel, grand_total, cod_amount, current_shipment_id, created_at` | 30 | M2 trở đi |
| `fulfillment.shipments_v` (tối thiểu; M3 mở rộng) | `id, order_id, code, carrier_key, carrier_account_id, tracking_code, status, cod_amount, fee` | 31 | M3 trở đi |
| `cash.cod_balances_v` (tối thiểu; M3 mở rộng) | `shipment_id, account, balance` | 32 | M3 trở đi |

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

## Ngoại lệ luật (g)

Luật (g) của `tenancy.CheckSchemaRules` (ADR-29): bảng có cột `seller_id` thì mọi unique khác khóa chính và khác
`(tenant_id, id)` có `seller_id` ngay sau `tenant_id` (mã hàng duy nhất theo người bán). Một unique cố ý duy nhất trên
cả shop được miễn khi index có `COMMENT ON INDEX <schema>.<index> IS 'seller-exempt: <lý do>'` **và** có dòng ở bảng
dưới; `viewsdoc_test` đòi hai tập bằng nhau.

| Index | Lý do | Task |
|---|---|---|
| `ord.orders_code_key` | mã đơn duy nhất trên cả shop (05, Q26: một sequence sinh mã) | 30 |
| `ord.orders_idempotency_key_key` | khóa nghiệp vụ của lần tạo đơn duy nhất trên cả shop (05, 04 mục 5.5) | 30 |
