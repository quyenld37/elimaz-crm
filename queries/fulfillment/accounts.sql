-- Tài khoản hãng vận chuyển (kế hoạch M1, Task M1-31). Chạy trong tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 8.

-- name: InsertCarrierAccount :one
-- Chèn tài khoản (seed, test: chủ bảng); đã có cùng (carrier_key, external_account_ref) thì giữ nguyên, trả mã cũ.
INSERT INTO fulfillment.carrier_accounts (tenant_id, seller_id, carrier_key, name, external_account_ref, warehouse_id)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @seller_id, @carrier_key, @name,
        @external_account_ref, @warehouse_id)
ON CONFLICT (tenant_id, carrier_key, external_account_ref) DO UPDATE SET carrier_key = EXCLUDED.carrier_key
RETURNING id;

-- name: GetCarrierAccount :one
SELECT id, seller_id, carrier_key, external_account_ref, warehouse_id, status
FROM fulfillment.carrier_accounts
WHERE id = @id;

-- Tài khoản hãng M3 (kế hoạch M3, Task M3-09). Mọi câu trong tenancy.WithTenant (crm_app). Khóa: nhóm 8 của ADR-46,
-- tuyến platform.tenant_routes (nhóm 1) ghi trước bằng tenancy.PutRoute.

-- name: InsertAccount :one
INSERT INTO fulfillment.carrier_accounts (id, tenant_id, seller_id, carrier_key, mode, environment, name,
    external_account_ref, warehouse_id, route_key, default_service, default_options, is_default_for_warehouse,
    show_on_web, web_sort, credential_enc, credential_key_id, credential_updated_at, status)
VALUES (@id, @tenant_id, @seller_id, @carrier_key, @mode, @environment, @name, @external_account_ref, @warehouse_id,
    @route_key, @default_service, @default_options, @is_default_for_warehouse, @show_on_web, @web_sort,
    sqlc.narg('credential_enc'), sqlc.narg('credential_key_id'),
    CASE WHEN sqlc.narg('credential_enc')::bytea IS NULL THEN NULL ELSE now() END, @status)
RETURNING created_at;

-- name: GetAccount :one
SELECT id, tenant_id, seller_id, carrier_key, mode, environment, name, external_account_ref, warehouse_id, route_key,
       default_service, default_options, is_default_for_warehouse, show_on_web, web_sort, credential_enc,
       credential_key_id, credential_updated_at, status, last_check_at, last_check_result, version
FROM fulfillment.carrier_accounts
WHERE id = @id;

-- name: LockAccount :one
SELECT id, tenant_id, seller_id, carrier_key, mode, environment, name, external_account_ref, warehouse_id, route_key,
       default_service, default_options, is_default_for_warehouse, show_on_web, web_sort, credential_enc,
       credential_key_id, credential_updated_at, status, last_check_at, last_check_result, version
FROM fulfillment.carrier_accounts
WHERE id = @id
FOR UPDATE;

-- name: ListAccounts :many
-- Trang theo id tăng dần (con trỏ: id của dòng cuối trang trước).
SELECT id, tenant_id, seller_id, carrier_key, mode, environment, name, external_account_ref, warehouse_id, route_key,
       default_service, default_options, is_default_for_warehouse, show_on_web, web_sort, credential_enc,
       credential_key_id, credential_updated_at, status, last_check_at, last_check_result, version
FROM fulfillment.carrier_accounts
WHERE (sqlc.narg('warehouse_id')::uuid IS NULL OR warehouse_id = sqlc.narg('warehouse_id'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('after')::uuid IS NULL OR id > sqlc.narg('after'))
ORDER BY id
LIMIT @lim;

-- name: ActiveAccounts :many
-- Tài khoản active (worker, báo phí trên web): lọc kho, cờ hiện trên web; xếp theo web_sort rồi id.
SELECT id, tenant_id, seller_id, carrier_key, mode, environment, name, external_account_ref, warehouse_id, route_key,
       default_service, default_options, is_default_for_warehouse, show_on_web, web_sort, credential_enc,
       credential_key_id, credential_updated_at, status, last_check_at, last_check_result, version
FROM fulfillment.carrier_accounts
WHERE status = 'active'
  AND (sqlc.narg('warehouse_id')::uuid IS NULL OR warehouse_id = sqlc.narg('warehouse_id'))
  AND (sqlc.narg('show_on_web')::boolean IS NULL OR show_on_web = sqlc.narg('show_on_web'))
ORDER BY web_sort, id;

-- name: UpdateAccount :one
-- Bên gọi đã khóa dòng (LockAccount) và so version.
UPDATE fulfillment.carrier_accounts
SET name = @name, environment = @environment, warehouse_id = @warehouse_id, default_service = @default_service,
    default_options = @default_options, is_default_for_warehouse = @is_default_for_warehouse,
    show_on_web = @show_on_web, web_sort = @web_sort, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING version;

-- name: SetAccountCredentials :one
-- Ghi khóa API đã mã hóa, tài khoản sang checking (bên gọi ghi lệnh kiểm); tài khoản paused giữ paused (ghi khóa không
-- bật lại tài khoản chủ shop đã tạm dừng).
UPDATE fulfillment.carrier_accounts
SET credential_enc = @credential_enc, credential_key_id = @credential_key_id, credential_updated_at = now(),
    status = CASE WHEN status = 'paused' THEN 'paused' ELSE 'checking' END, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING version;

-- name: SetAccountStatus :one
UPDATE fulfillment.carrier_accounts
SET status = @status, version = version + 1, updated_at = now()
WHERE id = @id
RETURNING version;

-- name: ApplyCheckResult :one
-- Kết quả lệnh kiểm: chỉ khi khóa chưa đổi từ lúc kiểm (credential_updated_at); trạng thái chỉ đổi khi tài khoản đang
-- draft, checking, active, auth_failed (paused giữ nguyên, chỉ ghi kết quả). Trả trạng thái trước để biết có vừa sang
-- auth_failed không. Không dòng nào: khóa đã đổi hoặc tài khoản đã revoked.
UPDATE fulfillment.carrier_accounts a
SET status = CASE WHEN a.status IN ('draft', 'checking', 'active', 'auth_failed') THEN @status ELSE a.status END,
    last_check_at = now(), last_check_result = @result, version = a.version + 1, updated_at = now()
FROM (SELECT c.id AS prev_id, c.status AS prev_status FROM fulfillment.carrier_accounts c WHERE c.id = @id FOR UPDATE) prev
WHERE a.id = prev.prev_id AND a.status <> 'revoked'
  AND a.credential_updated_at IS NOT DISTINCT FROM sqlc.narg('credential_updated_at')::timestamptz
RETURNING prev.prev_status, a.status, a.tenant_id, a.carrier_key;

-- name: MarkAuthFailed :one
-- Hãng từ chối khóa ở một lệnh bất kỳ của tài khoản: active, checking, draft → auth_failed. Trả trạng thái trước.
UPDATE fulfillment.carrier_accounts a
SET status = 'auth_failed', last_check_at = now(), last_check_result = @result, version = a.version + 1,
    updated_at = now()
FROM (SELECT c.id AS prev_id, c.status AS prev_status FROM fulfillment.carrier_accounts c WHERE c.id = @id FOR UPDATE) prev
WHERE a.id = prev.prev_id AND a.status IN ('active', 'checking', 'draft', 'auth_failed')
RETURNING prev.prev_status, a.tenant_id, a.carrier_key;

-- name: WarehousePickup :one
-- Địa chỉ lấy hàng của kho (view công bố của inventory, docs/views.md).
SELECT pickup_address, pickup_phone FROM inventory.warehouses_v WHERE warehouse_id = @warehouse_id;
