# ADR-46 — Bảng thứ tự khóa

Trạng thái: đã duyệt (spec 07-adr ADR-46, 18/11/2026)
Ngày: 18/11/2026
Kiểm bằng: retry_test, txusage_test (Task 4); review của mỗi task có migration

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
| 2 | `identity` | `identity.users` → `identity.roles` → `identity.role_permissions` → `identity.user_role_grants` → `identity.user_credentials` → `identity.user_second_factors` → `identity.user_sessions` | spec; `identity.role_permissions`: M0 Task 7 |
| 3 | `channel` | `channel.channel_accounts` | spec |
| 4 | `catalog` | `catalog.products` → `catalog.variants` | spec |
| 5 | `customer` | `customer.customers` → `customer.customer_identities` | spec |
| 6 | `conversation` | `conversation.conversations` → `conversation.live_sessions` → `conversation.messages` → `conversation.automation_runs` | spec |
| 7 | `order` (schema `ord`) | `ord.orders` → `ord.order_items` → `ord.returns` → `ord.return_items` | spec |
| 8 | `fulfillment` | `fulfillment.shipments` | spec |
| 9 | `cash` | `cash.cod_statements` → `cash.cod_transfers` → `cash.payments` | spec |
| 10 | `promotion` | `promotion.coupons` → `promotion.flash_sales` → `promotion.flash_sale_items` → `promotion.flash_sale_claims` | spec |
| 11 | `inventory` | `inventory.stock_balances` → `inventory.stock_reservations` → `inventory.flash_sale_units` | spec |
| 12 | `affiliate` | `affiliate.affiliates` → `affiliate.commissions` → `affiliate.payouts` → `affiliate.payout_items` | spec |
| 13 | `platform`: dấu "đã xong", luôn cuối transaction | `platform.idempotency_keys` → `platform.jobs` | spec; `platform.jobs`: M0 Task 11 (migration 0007; `OnTerminal` của hàng đợi chạy trước câu đổi trạng thái `jobs`) |

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
- Hai đường chính đã khớp bảng: lượt mua flash sale của ADR-40 khóa `flash_sale_claims` (10) → `flash_sale_units`
  bằng `SKIP LOCKED` (11) → tạo đơn (dòng mới) → `idempotency_keys` (13); tạo đơn bán thường tạo đơn (dòng mới) →
  `coupons` (10) → `stock_balances` (11) → `idempotency_keys` (13).
- **Bảng mới** được xếp vào bảng này trong cùng commit với migration tạo ra nó. Review của task có migration kiểm việc
  này.

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

Bảng phiên bản của goose `platform.goose_db_version` (M0 Task 4) chỉ do goose ghi trong lần chạy migrate, dưới cùng
khóa advisory; không mã ứng dụng nào chạm.
