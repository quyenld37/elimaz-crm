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
| `identity.users_v` (B2-11: bảng của M0, M1 công bố vì Task 26 đã `ALTER` `identity`; không có `email`) | `id, display_name, status` | 26 | M4 (`conversation`: tên người đang xem hội thoại, Task 163), M6, M7 |
| `catalog.sellers_v` | `seller_id, code, name, kind, status` | 27 | M2–M7 |
| `catalog.products_v` (M5 Task 190 nối `version`, Task 489 nối `created_at` vào cuối) | `product_id, seller_id, code, name, name_search, description, article, status, category_id, deleted_at, updated_at, version, created_at` | 27, 190, 489 | M2, M5–M7 (`internal/queries/webcatalog`: danh mục cho website; `order` `ProductCategories`: danh mục của mẫu cho báo giá web, Task 192) |
| `catalog.variants_v` (M5 Task 190 nối `weight_grams` vào cuối, QĐ30) | `variant_id, seller_id, product_id, sku, barcode, product_code, product_name, color, color_hex, size, price, cost_price, variant_status, product_status, sellable, updated_at, weight_grams` | 27, 190 | M2–M7 |
| `catalog.categories_v` | `category_id, parent_id, name, slug, depth, position, status` | 34 | M5–M7 |
| `catalog.attributes_v` | `attribute_id, code, name, position` | 34 | M5–M7 |
| `catalog.attribute_values_v` | `attribute_value_id, attribute_id, value, position` | 34 | M5–M7 |
| `catalog.tags_v` | `tag_id, code, name` | 34 | M5–M7 |
| `catalog.product_tags_v` | `product_id, tag_id` | 34 | M5–M7 |
| `catalog.product_attribute_values_v` | `product_id, attribute_value_id` | 34 | M5–M7 |
| `catalog.product_images_v` (chỉ ảnh chưa xóa) | `image_id, product_id, file_id, color, position` | 44 | M5, M7 |
| `platform.file_variants_v` (A2-12; chỉ tệp `ready`, chưa xóa, mục đích chế độ `standard`, biến thể `public` — R10; đường dẫn = `FILES_PUBLIC_BASE_URL` + `/` + `key`; ghép với `catalog.product_images_v` theo `file_id`) | `file_id, preset_version, width, format, key` | 44 | M5 |
| `inventory.warehouses_v` (M3 Task 92 nối địa chỉ lấy hàng, SĐT của shop vào cuối) | `warehouse_id, operated_by_seller_id, code, name, status, pickup_address, pickup_phone` | 28, 92 | M2–M7 (M3: fulfillment đọc địa chỉ lấy hàng, ngoại lệ "chỉ đọc view" — Task 100) |
| `inventory.stock_available_v` (view số dư duy nhất; B11: không có `stock_balances_v`, `availability_v`) | `variant_id, warehouse_id, seller_id, on_hand, held, in_transit, damaged, available, updated_at` | 28 | M2–M7 |
| `inventory.stock_ledger_v` | `ledger_id, variant_id, warehouse_id, bucket, delta, doc_type, doc_id, step, actor_type, actor_id, created_at` | 28 | M6, M7 |
| `inventory.reconcile_runs_v` | `run_id, ran_at, ok, checked, mismatch_count` | 36 | M6, M7 |
| `customer.customers_v` (M1 Task 30; M2 Task 60 nối 11 cột sau `created_at`; M4 Task 152 nối `do_not_contact`, `name_source` (cờ chỉ đúng ở gốc); chỉ số chỉ đúng ở dòng `id = root_id`) | `id, root_id, full_name, created_at, tenant_id, code, lifecycle, birthday, order_count, delivered_count, returned_count, total_spent, last_order_at, updated_at, version, do_not_contact, name_source` | 30, 60, 152 | M2 trở đi |
| `ord.orders_v` (M1 Task 30; M2 Task 62 nối 26 cột sau `created_at`; mốc sau nối sau `version`; M3 Task 119 nối `ship_phone_reversed`; M5 Task 196 nối `updated_at`) | `id, tenant_id, code, seller_id, customer_id, status, channel, grand_total, cod_amount, current_shipment_id, created_at, pending_reason, packing_status, payment_status, channel_account_id, traffic_source_id, conversation_id, external_ref, order_type, warehouse_id, ship_phone, address_complete, subtotal, line_discount_total, order_discount, coupon_discount, discount_total, shipping_fee_charged, prepaid_total, created_by, care_by, confirmed_at, cancelled_at, cancel_reason, draft_expires_at, duplicate_suspect, version, ship_phone_reversed, updated_at` | 30, 62, 119, 196 | M2 trở đi (`customer` đọc: ngoại lệ chỉ đọc view, Q19) |
| `customer.customer_identities_v` (M2, viết trước ở Task 59) | `tenant_id, id, customer_id, kind, scope, value, verified_at, source, created_at` | 60 | M3–M6 |
| `customer.customer_addresses_v` (chỉ địa chỉ chưa xóa) | `tenant_id, id, customer_id, recipient_name, phone, province_code, ward_code, street, is_default` | 60 | M3, M5 |
| `customer.customer_tags_v` | `tenant_id, id, name, color` | 60 | M4, M6 |
| `customer.customer_tag_links_v` | `tenant_id, customer_id, tag_id, source` | 60 | M4, M6 |
| `customer.birthday_profiles_v` (M5 Task 468; hồ sơ sinh nhật kèm tên, gốc của khách và `delivered_count` của gốc; `phone`, `email`, `stop_token` là dữ liệu cá nhân, không vào log) | `tenant_id, customer_id, root_id, full_name, phone, email, day, month, consent_zalo, consent_mail, stop_token, stopped_at, legacy_delivered, source, created_at, delivered_count` | 468 | M5 (`promotion` chọn khách nhận quà sinh nhật, outbox, danh sách) |
| `customer.admin_units_v` (danh mục dùng chung, không tenant_id) | `code, level, parent_code, name` | 109 | M3 (fulfillment khớp địa giới theo hãng, ngoại lệ "chỉ đọc view" — Task 109) |
| `ord.order_items_v` (M2, viết trước ở Task 59) | `id, tenant_id, seller_id, order_id, variant_id, position, sku_snapshot, name_snapshot, quantity, unit_price, line_discount, allocated_order_discount, net_amount` | 62 | M2 (`customer` đọc: ngoại lệ chỉ đọc view, Q19), M3, M5, M6 |
| `ord.order_status_history_v` (`from_status` của dòng tạo đơn là `''`, kể cả dòng M1 ghi `NULL`) | `id, tenant_id, order_id, from_status, to_status, from_pending, to_pending, actor_type, actor_id, reason, occurred_at, recorded_at` | 62 | M2 (`customer` đọc: ngoại lệ chỉ đọc view, Q19), M6 |
| `ord.returns_v` | `id, tenant_id, code, order_id, shipment_id, kind, status, refund_amount, replacement_order_id, received_at, created_at` | 62 | M2 (`customer` đọc: ngoại lệ chỉ đọc view, Q19), M3, M6 |
| `ord.return_items_v` (M3 Task 92 nối ba cột số lượng vào cuối, R11; cộng lại là 0 khi chưa nhận hoặc đúng `quantity`) | `id, tenant_id, return_id, order_item_id, quantity, condition, refund_amount, sellable_qty, damaged_qty, missing_qty` | 62, 92 | M3, M6, P2 |
| `ord.traffic_sources_v` | `id, tenant_id, key, name, channel_account_id, active, position` | 62 | M4, M5, M6 |
| `promotion.coupons_v` (B2-11; không có `bound_phone`, `bound_customer_id`) | `id, tenant_id, code, batch_id, name, discount_kind, value, max_discount, min_subtotal, channels, seller_id, max_uses, used_count, per_customer_limit, valid_from, valid_to, status, created_at, updated_at` | 63 | M5, M7 |
| `promotion.web_campaigns_v` (chiến dịch giảm giá web chưa xóa; `status` tính theo giờ database: `draft`/`paused` theo `state`, còn lại `scheduled`/`active`/`ended`, `ends_at` đúng giờ là `ended`) | `id, tenant_id, seller_id, name, percent, scope, allow_coupon, clearance, starts_at, ends_at, state, status, link_key, revision, updated_at` | 452 | M5 (`content`: banner chiến dịch và đích của banner ảnh, Task 453; `internal/queries`) |
| `promotion.web_campaign_targets_v` (đích của chiến dịch chưa xóa; `target_kind` `product`/`category`) | `tenant_id, campaign_id, target_kind, target_id` | 452 | M5 (`content`, Task 453) |
| `fulfillment.shipments_v` (M1 chín cột; M3 Task 92 nối sau `fee`; không `ship_to`, `options`, `create_job_id`; `province_code` = `ship_to->>'provinceCode'` (sửa ở 0331, Task 442)) | `id, order_id, code, carrier_key, carrier_account_id, tracking_code, status, cod_amount, fee, tenant_id, seller_id, mode, service, sub_status, cancel_kind, create_state, cod_collected_amount, declared_value, fee_actual, weight_grams, partial, picked_up_at, delivered_at, carrier_reported_returned_at, returned_at, last_status_at, last_carrier_event_at, last_synced_at, last_sync_error, stuck_since, needs_review, label_file_id, label_printed_at, created_at, province_code` | 31, 92 | M3 trở đi |
| `fulfillment.carrier_accounts_v` (không `credential_enc`, `credential_key_id`, `route_key`, `external_account_ref`) | `tenant_id, id, seller_id, carrier_key, mode, environment, name, warehouse_id, status, is_default_for_warehouse, show_on_web, web_sort, last_check_at, last_check_result, created_at, updated_at` | 92 | M3 (cash: khóa hãng của bảng kê, Task 122), M5–M7 |
| `fulfillment.shipment_events_v` (không `raw`, `event_key`) | `tenant_id, id, shipment_id, source, raw_status, canonical, status, sub_status, occurred_at, recorded_at, applied, ignored_reason, actor_id` | 92, 119 | M3 (`internal/queries`: dòng thời gian đơn), M6, M7 |
| `fulfillment.locations_v` | `tenant_id, carrier_key, level, our_code, carrier_code, carrier_name, synced_at` | 92 | M5 |
| `channel.channel_accounts_v` (không `credential_enc`, `credential_kid`) | `id, tenant_id, seller_id, channel, external_id, name, token_kind, status, status_reason, send_mode, human_agent_enabled, subscribed_fields, token_checked_at, last_webhook_at, last_feed_at, blocked_until, version, created_at, updated_at` | 145 | M4 (`conversation`), M6, M7 (Q-X2) |
| `conversation.conversations_v` (M4 Task 151; Q-X2) | `tenant_id, id, channel_account_id, customer_id, kind, status, assignee_id, post_id, last_message_at, last_customer_message_at, created_at` | 151 | M6, M7, dòng thời gian khách |
| `conversation.tags_v` (chỉ nhãn chưa xóa; B2-11) | `tenant_id, id, name, color, sort` | 151 | M7 (báo cáo sẵn sàng) |
| `conversation.call_logs_v` (Q14; không có `note` — dữ liệu cá nhân) | `tenant_id, id, customer_id, conversation_id, order_id, outcome, callback_at, created_by, created_at` | 169 | hồ sơ khách, dòng thời gian khách (C7), M6, M7 |
| `cash.cod_balances_v` (tối thiểu; M3 mở rộng) | `shipment_id, account, balance` | 32 | M3 trở đi |
| `cash.shipment_cod_v` (số dư từng ngăn theo vận đơn, dạng ngang; vận đơn chưa có dòng sổ thì không có dòng) | `tenant_id, shipment_id, to_collect, carrier_holding, shop_received, carrier_fee, sales_clearing` | 93 | M3 trở đi |
| `cash.cod_transfers_v` (không `idempotency_key`, `actor_id`) | `tenant_id, id, shipment_id, kind, statement_line_id, reverses_transfer_id, reason, actor_type, occurred_at` | 93 | M3, M6, M7 |
| `cash.payments_v` | `tenant_id, id, order_id, method, amount, received_at, actor_id` | 93, 119 | M3, M6, M7 |
| `cash.refund_obligations_v` | `tenant_id, id, order_id, return_id, source, amount, paid_amount, status` | 93 | M3, M6, M7 |
| `cash.reconcile_runs_v` | `tenant_id, id, ran_at, ok, findings` | 93 | M7 (A22) |

## Khóa ngoại xuyên module

Đánh đổi có chủ ý (ADR-27 mục 4): bảng của một module có khóa ngoại tới bảng của module khác khi dữ liệu không được
phép lệch nhau. Module chủ của bảng bị trỏ không đổi khóa (cột, unique) mà khóa ngoại dựa vào khi chưa sửa dòng ở đây.

| Khóa ngoại | Vì sao | Task |
|---|---|---|
| `inventory.stock_reservations (tenant_id, order_item_id) → ord.order_items (tenant_id, id)` `ON DELETE SET NULL (order_item_id)` | giữ hàng luôn thuộc một dòng đơn có thật (M1); từ M2 sửa đơn bỏ được dòng đã nhả hàng: dòng giữ hàng `released` và sổ tồn còn, chỉ mất liên kết (`order_item_id` cho phép rỗng; `0204` của `ord` `ALTER` bảng của `inventory`) | 30, 62 |
| `ord.orders (tenant_id, warehouse_id) → inventory.warehouses (tenant_id, id)` | kho giữ hàng của đơn | 62 |
| `ord.order_items (tenant_id, seller_id, variant_id) → catalog.variants (tenant_id, seller_id, id)`; `ord.orders (tenant_id, seller_id) → catalog.sellers`; `ord.orders (tenant_id, customer_id) → customer.customers` | một đơn một người bán (ADR-29, KH14); đơn trỏ khách có thật | 30 |
| `fulfillment.shipments (tenant_id, seller_id, order_id) → ord.orders (tenant_id, seller_id, id)`; `fulfillment.callback_tasks`, `fulfillment.shipping_quotes (tenant_id, order_id) → ord.orders (tenant_id, id)` | vận đơn cùng người bán với đơn và tài khoản hãng (KH14 cho vận đơn, ADR-29); việc gọi lại, báo giá thuộc đơn có thật | 31, 92 |
| `ord.orders (tenant_id, current_shipment_id) → fulfillment.shipments (tenant_id, id)` (`orders_current_shipment_fk`; M2 Task 05 để lại cho M3) | vận đơn hiện hành của đơn là vận đơn có thật; đường tạo chèn vận đơn trước rồi gắn | 92 |
| `fulfillment.shipments (tenant_id, label_file_id)`, `fulfillment.label_batches (tenant_id, file_id) → platform.files (tenant_id, id)` | tem in là tệp có thật | 92 |
| `cash.cod_statements (tenant_id, carrier_account_id) → fulfillment.carrier_accounts (tenant_id, id)` | bảng kê COD thuộc một tài khoản hãng có thật | 93 |
| `cash.payments`, `cash.refund_obligations (tenant_id, order_id) → ord.orders (tenant_id, id)`; `cash.refund_obligations (tenant_id, return_id) → ord.returns (tenant_id, id)` | phiếu thu, nghĩa vụ hoàn thuộc đơn (và phiếu trả) có thật | 93 |

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
`Seq` của một đối tượng chỉ tăng nhưng **có thể nhảy** (không liền nhau): có lần đổi tăng `version` mà không phát sự kiện
(ví dụ lớp bọc M1 `order.AttachShipment`; review M1 D-06, Task 415). Consumer chỉ dựa vào thứ tự (Seq lớn hơn là mới hơn,
như hàng đợi `OrderStrict`), không coi Seq nhảy là mất sự kiện.
Payload chỉ mang mã và số (A16): consumer đọc dữ liệu qua view ở trên. Mốc chủ chỉ thêm khóa vào payload, không đổi
hay bỏ khóa đã có.

| Topic | Aggregate | Payload | Phát khi | Task |
|---|---|---|---|---|
| `product.updated` | `product` (mã mẫu) | `{productId, sellerId, version}` | tạo, sửa mẫu; thêm, sửa biến thể (version mẫu tăng) | 27 |
| `price.changed` | `variant` (mã biến thể) | `{productId, sellerId, variantIds: [<id>], price}` | giá bán của biến thể đổi | 27 |
| `stock.changed` | `balance:<variant>:<warehouse>` | `{variantIds: [<variant>], warehouseId, sellerId}` | số dư tồn đổi | 28 |
| `order.created` | `order` (mã đơn) | `{orderId, sellerId, version, customerId, status}` | tạo đơn (seq 1; Task 73 thêm `customerId`, `status`) | 30, 73 |
| `order.confirmed` | `order` (mã đơn) | `{orderId, sellerId, version, customerId, status}` | đơn sang Đã xác nhận (tạo thẳng confirmed: ngay khi tạo, seq 2) | 30, 73 |
| `order.delivered` | `order` (mã đơn) | `{orderId, sellerId, version, customerId, status, partial?}` | vận đơn hiện hành báo giao xong, đơn sang Đã giao; `partial: true` chỉ khi giao một phần (`Writer.MarkDeliveredPartial`, M3-06), giao đủ không có khóa này | 333, 97 |
| `order.<trạng thái>` (`order.new`, `order.cancelled`, `order.cancel_requested`, `order.returned`, `order.draft`) | `order` (mã đơn) | `{orderId, sellerId, version, customerId, status}` | mọi lần đổi trạng thái qua `order.applyTransition` (seq = version mới); hủy: `Writer.Cancel` (`order.cancelled`, hay `order.cancel_requested` khi đơn đã có vận đơn hiện hành — M3 nghe để gửi lệnh hủy cho hãng), `Writer.ExpireDrafts`, hãng trả lời hủy `Writer.OnShipmentCancelConfirmed` (`order.cancelled`) / `Writer.OnShipmentCancelRejected` (`order.confirmed`); kho nhận kiện hoàn của hãng `Writer.ReceiveReturn` (`order.returned`). Consumer `order_duplicates` nghe `order.cancelled` (mọi đường hủy) để gỡ cờ nghi trùng (Task 432) | 73, 78, 79, 432 |
| `order.partially_returned`, `order.returned` (kho nhận phiếu khách trả / đổi) | `order` (mã đơn) | `{orderId, sellerId, version, customerId, status, returnId, kind, refundAmount}` | `Writer.ReceiveReturn` phiếu `customer_return`/`exchange`: đơn gốc giữ trạng thái, version + 1 (seq); `order.returned` khi mọi dòng đơn đã trả đủ qua các phiếu đã nhận, không thì `order.partially_returned`. Phiếu hoàn của hãng: `order.returned` là sự kiện đổi trạng thái `confirmed → returned` (dòng trên, payload không có `returnId`) | 79 |
| `order.updated` | `order` (mã đơn) | `{orderId, sellerId, version, customerId, status}` | đơn đổi trường mà không đổi trạng thái: gắn, gỡ vận đơn hiện hành (`Writer.AttachShipmentBy`, `Writer.DetachShipment`, `Writer.OnShipmentCancelConfirmed` trên đơn confirmed — trên đơn cancel_requested hai hàm này hủy đơn, phát `order.cancelled`; lớp bọc M1 `AttachShipment` không phát), tổng đã thanh toán (`Writer.ApplyPaymentTotals`); M3-06: trạng thái đóng gói (`Writer.SetPackingStatus`), địa chỉ giao khi hãng đã nhận (`Writer.ApplyAddressChange`) | 75, 97 |
| `order.delivery_reversed` | `order` (mã đơn) | `{orderId, sellerId, version, customerId, status}` | `Writer.ReverseDelivery` (M3-06, TD10, Q-1): hãng sửa "đã giao" của vận đơn hiện hành, đơn `delivered → confirmed` (seq = version mới, `status` = `confirmed`; không phát thêm `order.confirmed`). Consumer `customer_stats` của M2 nghe tên này (`customer.TopicOrderDeliveryReversed`) | 82 (nghe), 97 (phát) |
| `customer.created` | `customer` (mã hồ sơ) | `{customerId, version}` | tạo khách (seq 1; kể cả lớp bọc `customer.Create` của M1) | 66 |
| `customer.merged` | `customer_merge` (mã lần gộp) | `{mergeId, survivorRootId, mergedRootId}` | gộp nhóm khách (seq 1; chỉ đổi `root_id`) | 68 |
| `customer.merge_undone` | `customer_merge` (mã lần gộp) | `{mergeId, survivorRootId, mergedRootId}` | hoàn tác lần gộp (seq 2) | 68 |
| `coupon.used` | `order_coupon` (mã đơn) | `{couponId, orderId, customerId, discount}` (`customerId`: hồ sơ gốc) | một đơn dùng mã giảm giá (`promotion.Writer.Redeem`; seq = `version` của đơn ở bên gọi) | 74 |
| `promotion.web_campaign.changed` | `web_campaign` (mã chiến dịch) | `{campaignId, sellerId, revision, deleted?}` | tạo, lưu, bật, tạm dừng, xóa chiến dịch giảm giá web (`promotion.Writer`; seq = `revision` mới; xem trước không phát; `deleted: true` chỉ khi xóa). Consumer `webcache` làm mới vùng `campaigns` của bộ đệm website | 452 |
| `order.web_intake.changed` | `web_intake` (mã tenant) | `{paused, version}` | tạm dừng hay mở lại nhận đơn web (`order.Writer.SetWebIntake`; seq = `version` mới; không lý do, không người). Consumer `webcache` làm mới vùng `web_intake` của bộ đệm website | 456 |
| `content.banners.changed` | `home_banners` (mã tenant) | `{revision}` | lưu danh sách banner trang chủ (`content.Writer.SaveBanners`; seq = `revision` mới). Consumer `webcache` làm mới vùng `banners` | 453 |
| `content.looks.changed` | `home_looks` (mã tenant) | `{revision}` | lưu danh sách bộ phối (`content.Writer.SaveLooks`; seq = `revision` mới). Consumer `webcache` làm mới vùng `looks` | 454 |
| `shipment.status_changed` | `shipment` (mã vận đơn) | `{shipmentId, status, codAmount}`; M3-08 thêm (bỏ trống khi không có) `{orderId, from, effects, codCollected, partial, occurredAt}` | vận đơn tiến trạng thái (`awaiting_pickup`, `shipping`, `delivered`; chỉ tiến), hoặc `creating` → `cancelled` khi lệnh tạo kết thúc mà hãng chắc chắn chưa tạo (Task 408); M3-08 `ApplyTracking`: mỗi bước đổi trạng thái một sự kiện (Seq = version sau bước, `effects` của đúng bước theo thứ tự của máy trạng thái), `AttachTrackingCode`: `creating` → `awaiting_pickup`; M3-14 `RequestCancel`: `awaiting_pickup` → `cancel_requested` (hủy đơn, đổi hãng; kết quả của hãng đi qua `ApplyTracking`) | 31, 408, 99, 105 |
| `shipment.created` | `shipment` (mã vận đơn) | `{shipmentId, orderId, trackingCode, fee?}` | hãng nhận vận đơn: `AttachTrackingCode` gắn mã hãng (Seq = version − 1, `shipment.status_changed` cùng lần mang Seq = version) | 99 |
| `cod_statement.parsed`, `cod_statement.confirmed`, `cod_statement.posting` (nội bộ của `cash`) | `cod_statement` (mã bảng kê) | `{statementId, afterLineNo?}` | bảng kê vừa đọc xong tệp (lệnh đọc tệp `OnDone`) → consumer `cash_statements` khớp dòng; vừa chốt (`Service.ConfirmStatement`) → lô ghi tiền đầu; lô kế tiếp sau `afterLineNo` (Seq = version của bảng kê, tăng mỗi lô) | 123 |
| `cod.settled` | `cod_statement_line` (mã dòng bảng kê) | `{shipmentId, statementId, statementLineId}` | `carrier_holding` của vận đơn về 0 nhờ dòng bảng kê khi ghi tiền (Seq 1: mỗi dòng một lần) | 123 |

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
| `fulfillment.carrier_accounts_route_key` | khóa tuyến webhook duy nhất trên cả shop (định tuyến tới đúng một tài khoản) | 92 |
| `fulfillment.shipments_code_key` | mã vận đơn của CRM (`NewShipmentCode`) duy nhất trên cả shop | 31, 92 |
| `fulfillment.shipments_tracking_key` | mã hãng duy nhất theo hãng trên cả shop (VC13; tín hiệu webhook tra theo mã hãng) | 31, 92 |
| `fulfillment.shipments_active_order` | một đơn một vận đơn chưa hủy (B1); đơn đã thuộc đúng một người bán | 31, 92 |
| `promotion.coupons_code_key` | khách gõ mã ở một ô chung của shop: mã duy nhất trên cả shop, kể cả mã của người bán (05 mục 2) | 63 |
| `promotion.web_campaigns_link_key` | liên kết `/uu-dai/<tên>-<link_key>` của website là đường dẫn chung của cả shop | 452 |
| `channel.channel_accounts_external_key` | một Page (kênh, mã Page) chỉ nối một lần trong shop, thuộc đúng một người bán | 145 |
