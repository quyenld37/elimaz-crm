# ADR-46 — Bảng thứ tự khóa

Trạng thái: đã duyệt (spec 07-adr ADR-46, 18/11/2026)
Ngày: 18/11/2026
Kiểm bằng: retry_test, txusage_test (Task 4); review của mỗi task có migration. M2 (Task M2-32): hai đường song song
không deadlock — `order`: `TestCreateCoupon_NoDeadlockWithCancel` (nhóm 7 → 10 → 11), `TestEdit_NoDeadlock`,
`TestEdit_CouponSwapNoDeadlock`, `TestCancelReturn_ExchangeRaceNoDeadlock`, `TestExpireDrafts_ManyOrdersNoDeadlock`;
`customer`: `TestMergeVsForOrderNoDeadlock`, `TestFindOrCreateForOrderVsAddIdentityNoDeadlock` (nhóm 5);
`inventory`: `TestReserve_NoDeadlockOnCrossOrder`, `TestCore_NoDeadlockOnCrossOrder` (nhóm 11, biến thể tăng dần)

Tệp này là **chỗ duy nhất** giữ bảng thứ tự khóa của ADR-46 điểm 4. Các điểm 1–3 (một helper `db.RetryTx`, bên gọi
nhận `ErrRetryLater`, hàm trong transaction chạy lại được), điểm 5 (READ COMMITTED) và điểm 6 (`lock_timeout`,
`statement_timeout` theo transaction) đọc ở spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, ADR-46; không chép
lại ở đây.

## Phạm vi

Áp cho mọi dòng **đã có** bị khóa trong transaction: `SELECT … FOR UPDATE` hoặc `FOR SHARE`, `UPDATE`, `DELETE`,
`INSERT … ON CONFLICT DO UPDATE` chạm dòng đã có. Dòng mới chèn không tính (chưa transaction nào khác thấy nó). Một
transaction chạm nhiều bảng thì khóa theo thứ tự nhóm dưới đây, nhóm có số nhỏ trước; trong một nhóm theo thứ tự
bảng ghi trong ô.

## Bảng thứ tự khóa

| Thứ tự | Nhóm (module) | Bảng, theo thứ tự | Mốc thêm |
|---|---|---|---|
| 1 | `platform`: quản trị tenant | `platform.tenants` → `platform.tenant_routes` | spec |
| 2 | `identity` | `identity.users` → `identity.roles` → `identity.role_permissions` → `identity.user_role_grants` → `identity.user_credentials` → `identity.user_second_factors` → `identity.user_sessions` → `identity.service_keys` | spec; `identity.role_permissions`: M0 Task 7; M5 (Task M5-00 viết trước, M5-06 tạo ở 0501): `service_keys` |
| 3 | `channel` | `channel.channel_accounts` → `channel.send_buckets` → `channel.oauth_sessions` | spec; M4 (Task M2-33 viết khung, M4-03 đối chiếu): `send_buckets`, `oauth_sessions` |
| 4 | `catalog` | `catalog.imports` → `catalog.sellers` → `catalog.categories` → `catalog.attributes` → `catalog.attribute_values` → `catalog.tags` → `catalog.products` → `catalog.product_attribute_values` → `catalog.product_tags` → `catalog.product_images` → `catalog.variants` → `catalog.barcode_counters` | spec; M1 (Task M1-25 viết trước): `sellers`, `variants`, `barcode_counters` M1-27; `categories`, `attributes`, `attribute_values`, `tags`, `product_attribute_values`, `product_tags` M1-34; `product_images` M1-44; `imports` M1-47 |
| 5 | `customer` | `customer.customers` → `customer.customer_identities` → `customer.customer_addresses` → `customer.customer_tag_links` → `customer.customer_tag_rules` → `customer.customer_merges` → `customer.customer_imports` → `customer.feedback` → `customer.birthday_profiles` | spec; M2 (Task M2-02 viết trước, M2-03 tạo): `customer_addresses`, `customer_tag_links`, `customer_tag_rules`, `customer_merges`, `customer_imports`; M5 (Task 450 viết trước, M5-43 tạo ở 0523, M5-56 ở 0525): `feedback`, `birthday_profiles` |
| 6 | `conversation` | `conversation.tags` → `conversation.reply_templates` → `conversation.page_settings` → `conversation.posts` → `conversation.conversations` → `conversation.conversation_tag_links` → `conversation.conversation_assignments` → `conversation.reminders` → `conversation.live_sessions` → `conversation.live_comment_claims` → `conversation.messages` → `conversation.message_revisions` → `conversation.automation_rules` → `conversation.automation_runs` → `conversation.call_logs` → `conversation.presence` | spec; M4 (Task M2-33 viết khung, M4-03 đối chiếu): `tags`, `reply_templates`, `page_settings`, `posts`, `conversation_tag_links`, `conversation_assignments`, `reminders`, `live_comment_claims`, `message_revisions`, `automation_rules`, `call_logs`, `presence` |
| 7 | `order` (schema `ord`) | `ord.orders` → `ord.order_items` → `ord.order_duplicate_links` → `ord.order_status_history` → `ord.returns` → `ord.return_items` → `ord.traffic_sources` → `ord.order_settings` → `ord.order_requests` → `ord.order_request_messages` → `ord.order_request_files` → `ord.order_request_events` → `ord.web_intake` | spec; `ord.order_status_history`: M1 (Task M1-25 viết trước, M1-30 tạo; chỉ chèn). Mã đơn lấy từ sequence `ord.order_code_seq`, không khóa dòng; M2 (Task M2-02 viết trước, M2-05 tạo): `order_duplicate_links`, `traffic_sources`, `order_settings`; `returns`, `return_items` M2-05. Mã phiếu trả lấy từ sequence `ord.return_code_seq`, không khóa dòng; M5 (Task M5-00 viết trước, M5-12 tạo ở 0504): `order_requests`; `order_request_messages`, `order_request_files` chỉ chèn, không khóa; Task 486 (0528): `order_request_events` (lịch sử trạng thái) chỉ chèn, không khóa; M5 (Task 450 viết trước, M5-44 tạo ở 0524): `web_intake` (một dòng mỗi tenant; tạo đơn web chỉ đọc không khóa) |
| 8 | `fulfillment` | `fulfillment.carrier_accounts` → `fulfillment.shipments` → `fulfillment.shipment_events` → `fulfillment.callback_tasks` → `fulfillment.address_change_requests` → `fulfillment.shipment_anomalies` → `fulfillment.label_batches` → `fulfillment.shipping_quotes` → `fulfillment.carrier_locations` → `fulfillment.channel_carrier_settings` | spec; `carrier_accounts`, `shipment_events`: M1 (Task M1-25 viết trước, M1-31 tạo); M3 (Task M2-33 viết khung, M3-01 tạo ở migration 0300, M3-04 đối chiếu 20/11): `callback_tasks`, `address_change_requests`, `shipment_anomalies`, `label_batches`, `shipping_quotes`, `carrier_locations`. `shipment_events` chỉ chèn, không khóa. Đường bắt đầu từ vận đơn mà đổi đơn (nhóm 7) luôn `order.Lock` trước rồi mới khóa `shipments`; M5 (Task M5-00 viết trước, M5-15 tạo ở 0506): `channel_carrier_settings` (nếu M5-15 chọn khóa `platform.settings` thay bảng mới thì bảng này không tạo, khóa thuộc nhóm 12c) |
| 9 | `cash` | `cash.cod_statements` → `cash.cod_statement_lines` → `cash.cod_transfers` → `cash.cod_ledger_entries` → `cash.payments` → `cash.refund_obligations` → `cash.reconcile_runs` | spec; `cash.cod_ledger_entries`: M1 (Task M1-25 viết trước, M1-32 tạo); M3 (Task M2-33 viết khung, M3-02 tạo ở migration 0303, M3-04 đối chiếu 20/11): `cod_statements`, `cod_statement_lines`, `payments`, `refund_obligations`, `reconcile_runs` (khác `inventory.reconcile_runs` ở nhóm 11). `cod_ledger_entries` chỉ chèn, không khóa |
| 10 | `promotion` | `promotion.coupon_batches` → `promotion.coupons` → `promotion.coupon_customer_uses` → `promotion.coupon_redemptions` → `promotion.web_campaigns` → `promotion.web_campaign_targets` → `promotion.birthday_settings` → `promotion.birthday_runs` → `promotion.birthday_gifts` → `promotion.flash_sales` → `promotion.flash_sale_items` → `promotion.flash_sale_claims` | spec; M2 (Task M2-02 viết trước, M2-06 tạo; C16): `coupon_batches`, `coupons`, `coupon_customer_uses`, `coupon_redemptions`; M5 (Task M5-00 đối chiếu 21/11, M5-16 tạo ở 0507): `flash_sales`, `flash_sale_items`, `flash_sale_claims`; lô mã của M5 ghi bằng `INSERT … ON CONFLICT DO NOTHING`, không khóa dòng lô có sẵn; M5 (Task 450 viết trước, M5-40 tạo ở 0520, M5-56 ở 0526): `web_campaigns`, `web_campaign_targets` (chỉ chèn/xóa theo chiến dịch đã khóa), `birthday_settings`, `birthday_runs`; `birthday_gifts` (M5-56 ở 0526, Task 468: chỉ chèn, dưới khóa tư vấn của lượt phát) |
| 11 | `inventory` | `inventory.warehouses` → `inventory.suppliers` → `inventory.stock_documents` → `inventory.stock_document_lines` → `inventory.stocktakes` → `inventory.stocktake_lines` → `inventory.stock_balances` → `inventory.stock_reservations` → `inventory.flash_sale_units` → `inventory.reconcile_runs` | spec; M1 (Task M1-25 viết trước): `warehouses` M1-28; `suppliers`, `stock_documents`, `stock_document_lines` M1-29; `stocktakes`, `stocktake_lines` M1-35; `reconcile_runs` M1-36 (khác `cash.reconcile_runs` của M3); `flash_sale_units`: M5 (M5-16 tạo ở 0507; M5-00 đối chiếu 21/11, không đổi vị trí) |
| 12 | `affiliate` | `affiliate.affiliates` → `affiliate.commissions` → `affiliate.payouts` → `affiliate.payout_items` | spec |
| 12a | `reporting` | để trống (M6, M7 thêm bảng vào nhóm này; C14, R4) | M1 (Task M1-25 giữ chỗ) |
| 12b | `platform`: tệp | `platform.files` → `platform.file_variants` | M1 (Task M1-25 viết trước, M1-41 tạo `files`, M1-43 ghi `file_variants`) |
| 12c | `platform`: cấu hình | `platform.settings` | M3 (Task M2-33 viết khung, M3-04 tạo ở migration 0304 và đối chiếu 20/11; chỉ sửa một mình trong transaction của `PUT` cài đặt, kể cả `PUT /api/v1/fulfillment-settings`; đọc không khóa) |
| 12d | `content` | `content.home_banner_sets` → `content.home_banners` → `content.look_sets` → `content.looks` → `content.look_items` | M5 (Task 453 chèn nhóm, tạo hai bảng banner ở migration 0521; bộ phối ở Task 454). Lưu banner khóa `home_banner_sets` rồi thay `home_banners`; chỉ đọc `platform.files` qua `files.Status`, không khóa 12b. Lưu bộ phối (Task 454, migration 0522) khóa `look_sets`, xóa `looks` (xóa dây chuyền `look_items`) rồi chèn `looks`, `look_items`; không chạm bảng banner |
| 13 | `platform`: dấu "đã xong", luôn cuối transaction | `platform.data_deletion_requests` → `platform.idempotency_keys` → `platform.jobs` | spec; `platform.jobs`: M0 Task 11 (migration 0007; `OnTerminal` của hàng đợi chạy trước câu đổi trạng thái `jobs`); `data_deletion_requests`: M4 (Task M2-33 viết khung, M4-03 đối chiếu; đứng đầu nhóm vì transaction xử lý yêu cầu xóa chạm `channel.oauth_sessions` (nhóm 3) trước rồi mới đánh dấu yêu cầu xong, phần C mục 7c) |

Mã nhóm đã cấp (R4, A2-4): `12a` `reporting` (M1 giữ chỗ, M6 và M7 dùng), `12b` `platform` tệp (M1), `12c`
`platform.settings` (M3; dòng do Task M2-33 chèn), `12d` `content` (M5, Task 453). Thứ tự 12 → 12a → 12b → 12c → 12d →
13; mã kế tiếp chưa dùng là `12e`. Trong
`inventory.stock_balances`: theo `(variant_id, warehouse_id)` tăng dần. Đường bắt đầu từ nhóm sau mà phải sửa nhóm
trước (sự kiện vận đơn đổi đơn và tồn) khóa dòng nhóm trước trước (luật đi kèm dưới đây).

## Luật đi kèm (chép nguyên ADR-46 điểm 4)

- **Trong cùng một bảng:** theo khóa chính tăng dần (`… ORDER BY id FOR UPDATE`). Bảng có khóa chính ghép thì theo
  thứ tự các cột của khóa; với `stock_balances` trong một kho, đây chính là luật `variant_id` tăng dần của 04 mục 5.5.
- **Sửa nhiều dòng một lần:** `UPDATE … WHERE id = ANY($1)` không bảo đảm thứ tự lấy khóa. Phải khóa trước bằng
  `SELECT … WHERE id = ANY($1) ORDER BY id FOR UPDATE` rồi mới sửa, hoặc chạy từng câu theo khóa tăng dần (như câu trừ
  có điều kiện cho từng biến thể khi tạo đơn).
- **Bắt đầu từ dòng ở nhóm sau:** đường nào khởi đầu từ một dòng ở nhóm sau mà phải sửa dòng ở nhóm trước (sự kiện
  vận đơn đổi trạng thái đơn; đơn nháp hết hạn nhả suất và trừ lại `flash_sale_claims`) thì khóa dòng nhóm trước
  trước (`SELECT … FROM orders WHERE id = $1 FOR UPDATE`), rồi mới khóa dòng nhóm sau.
- **Tin vào:** tìm hoặc ghi khách (nhóm 5) trước, rồi mới cập nhật hội thoại (nhóm 6).
- **Khách trước hội thoại (C11, M4):** mọi đường của M4 khóa khách (nhóm 5) trước hội thoại (nhóm 6), kể cả đường chốt
  đơn từ comment live (khách gốc → hội thoại theo `id` tăng dần → đơn).
- Hai đường chính đã khớp bảng: lượt mua flash sale của ADR-40 khóa `flash_sale_claims` (10) → `flash_sale_units`
  bằng `SKIP LOCKED` (11) → tạo đơn (dòng mới) → `idempotency_keys` (13); tạo đơn bán thường tạo đơn (dòng mới) →
  `coupons` (10) → `stock_balances` (11) → `idempotency_keys` (13).
- Đơn web (Task 193, `order.Writer.CreateFromWeb`): báo giá lại không khóa → đường tạo đơn bán thường trong một
  savepoint (khách (5) → đơn mới (7) → `coupons` (10) → `stock_balances` (11)) → ghi cột web lên đơn, dòng vừa chèn →
  danh tính `web_account` (khóa lại dòng khách đã giữ ở nhóm 5, không chờ mới) → `idempotency_keys` (13).
- **Bảng mới** được xếp vào bảng này trong cùng commit với migration tạo ra nó. Review của task có migration kiểm việc
  này.

## Khóa tư vấn (advisory)

- `identity` tên vai trò (M1 Task M1-56): tạo vai trò và sửa vai trò lấy `pg_advisory_xact_lock` theo tenant
  (`queries/identity/roles.sql` `LockRoleNames`) **trước mọi khóa dòng**, rồi mới khóa `identity.roles`. Chỉ hai đường
  này lấy khóa đó, nên không tạo vòng chờ với đường khóa dòng.
- `catalog` áp bản nhập Excel (M1 Task M1-47b): việc `catalog.import.apply` lấy `pg_advisory_xact_lock` theo (tenant,
  người bán) (`queries/catalog/imports.sql` `LockImportApplySeller`) **trước mọi khóa dòng**, rồi khóa `catalog.imports`
  → danh mục (chia sẻ, id tăng dần) → mẫu có sẵn của plan (id tăng dần, `LockProductsByIDs`) → biến thể. Chỉ đường này
  lấy khóa đó: hai lần áp của cùng người bán chạy lần lượt, lần sau kiểm lại trên dữ liệu lần trước vừa ghi.
- `cash` sổ COD theo vận đơn (M3 Task M3-29): đường ghi chọn bút toán theo số dư hiện tại của một vận đơn (consumer
  `cash` theo `shipment.status_changed`; chốt bảng kê M3-32, điều chỉnh M3-33 khi làm phải lấy cùng khóa) lấy
  `pg_advisory_xact_lock` theo vận đơn (`queries/cash/cod.sql` `LockShipmentCOD`, khóa `cash.cod:<shipment>`) **trước
  mọi khóa dòng của nhóm 9**, rồi mới đọc số dư và chèn bút toán (`cod_transfers`, `cod_ledger_entries` chỉ chèn). Hai
  đường cùng vận đơn chạy lần lượt; đường nhiều vận đơn lấy khóa theo mã vận đơn tăng dần. Chốt bảng kê (M3-32,
  Task 123): đường HTTP chỉ đổi trạng thái bảng kê theo version; consumer `cash_statements` ghi tiền theo lô ≤ 200 dòng
  có vận đơn mỗi transaction (`statement.PostBatch`): khóa tư vấn các vận đơn của lô theo mã tăng dần → khóa
  `cod_statements` (tăng version) → khóa `cod_statement_lines` của lô theo `line_no` → chèn bút toán. Lô nhỏ để số khóa
  tư vấn mỗi transaction không vượt bảng khóa (`max_locks_per_transaction` × `max_connections`).
- `promotion` lượt phát quà sinh nhật (M5 Task 468): `promotion.Writer.RunBirthdayGifts` (lịch `promotion.birthday_run`
  và "Tạo mã ngay") lấy `pg_try_advisory_xact_lock` theo tenant (`queries/promotion/birthday.sql` `TryBirthdayRunLock`,
  khóa `promotion.birthday_run:<tenant>`) **trước mọi khóa dòng**; không lấy được → `ErrRunInProgress` ngay (không chờ),
  nên không tạo vòng chờ. Sau đó: đọc cài đặt không khóa → chèn `birthday_runs` → mỗi nhóm hạn một savepoint
  (`EnsureWebBatch`: `coupon_batches` → `coupons`, rồi chèn `birthday_gifts`) → cập nhật dòng lượt vừa chèn.

## Luật cho mốc sau (Q-X4, C14)

- Thêm bảng vào nhóm có sẵn: **chèn** tên bảng vào đúng chỗ trong ô, không viết lại cả nhóm; ghi mốc và task ở cột
  "Mốc thêm".
- Nhóm mới đặt giữa hai nhóm dùng số kèm chữ (`12a`, `12b`…). Mỗi mã nhóm chỉ dùng một lần; mốc sau lấy chữ kế tiếp
  chưa dùng.
- Chỗ spec chưa nói: nhóm của mốc trước đứng trước, mốc sau xếp theo.
- Không chép lại hay ghi đè dòng đã có; không tạo tệp thứ tự khóa khác.

## Bảng M0 ngoài thứ tự nhóm

(a) **Chỉ chèn, không khóa dòng đã có:** `identity.audit_logs` (M0 Task 8), `platform.domain_events` (M0 Task 13),
`platform.webhook_deliveries` (M0 Task 14; `ON CONFLICT DO NOTHING`), `platform.alert_events` (M0 Task 19),
`platform.backup_runs` (M0 Task 19).

(b) **Chỉ sửa trong transaction riêng, không chung transaction với bảng nghiệp vụ:** `platform.schedules` (M0 Task 13;
Ticker khóa `schedules` rồi chỉ chèn `jobs`), `platform.metrics_minute` (M0 Task 10), `platform.process_heartbeats`
(M0 Task 10), `platform.error_events` (M0 Task 20), `platform.migration_checksums` (M0 Task 4; chỉ vai trò `migrate`,
dưới `pg_advisory_lock(727001)`).

(c) **Ngoại lệ đã biết:** hai đường `POST /api/v1/platform/jobs/{id}/retry` và `…/abandon` (M0 Task 12) sửa dòng
`platform.jobs` như dòng nghiệp vụ rồi mới `Claim.Complete` sửa `platform.idempotency_keys`. Không đường nào khóa
`idempotency_keys` rồi mới sửa dòng `jobs` đã có (bước ghi nhận khóa của Task 9 chạy trong transaction riêng), nên hai
thứ tự không gặp nhau.

(d) **Bảng M1 ngoài thứ tự nhóm** (Task M1-25 viết trước): `platform.file_sync_runs` (M1-45; chỉ `INSERT` bằng role
`crm_filesync`, không RLS), `platform.file_purposes` (M1-41; danh mục tĩnh, chỉ migration ghi, không RLS),
`inventory.stock_ledger` (M1-28; sổ chỉ ghi thêm, trigger chặn `UPDATE`/`DELETE`, không khóa dòng đã có).

(e) **Bảng M2 ngoài thứ tự nhóm** (Task M2-02 viết trước): chỉ chèn, không khóa dòng đã có — `customer.customer_notes`
(M2-03; trigger chặn `UPDATE`/`DELETE`), `ord.order_notes` (M2-05; chỉ ghi thêm); danh mục tĩnh không theo tenant,
chỉ migration ghi — `customer.admin_units` (M2-04); chỉ sửa trong transaction riêng (đổi tên, màu nhãn), không chung
transaction với bảng khác — `customer.customer_tags` (M2-03; gắn nhãn cho khách khóa `customer_tag_links`, không khóa
`customer_tags`).

(f) **Bảng M3 ngoài thứ tự nhóm** (Task M2-33 viết khung, M3-04 đối chiếu 20/11 với migration 0300–0306): chỉ chèn,
không khóa dòng đã có — `fulfillment.callback_task_logs`, `cash.reconcile_findings`. Test `TestLockOrder_M3TablesListed`
đòi mọi bảng `CREATE TABLE` của `migrations/03*.sql` có tên trong tệp này.

Bảng phiên bản của goose `platform.goose_db_version` (M0 Task 4) chỉ do goose ghi trong lần chạy migrate, dưới cùng
khóa advisory; không mã ứng dụng nào chạm.
