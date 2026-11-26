-- Ghi hồ sơ khách (kế hoạch M2, Task M2-09; 05 mục 1). Chạy trong tenancy.WithTenant (RLS lọc tenant). Thứ tự khóa
-- ADR-46 nhóm 5: customers → customer_identities → customer_addresses.

-- name: InsertCustomerProfile :one
-- code lấy DEFAULT từ customer.customer_code_seq ('KH' || lpad(nextval, 6, '0')); root_id do trigger đặt bằng id.
INSERT INTO customer.customers (id, tenant_id, full_name, name_search, lifecycle, birthday, created_by, created_at,
                                updated_at)
VALUES (@id, NULLIF(current_setting('app.tenant_id', true), '')::uuid, @full_name, @name_search, @lifecycle,
        sqlc.narg(birthday)::date, sqlc.narg(created_by)::uuid, @now, @now)
RETURNING tenant_id, root_id, code, version, created_at;

-- name: LockCustomer :one
SELECT tenant_id, root_id, full_name, birthday, version
FROM customer.customers
WHERE id = @id
FOR UPDATE;

-- name: UpdateCustomerProfile :one
UPDATE customer.customers
SET name_source = CASE WHEN full_name <> @full_name THEN 'staff' ELSE name_source END, -- nhân viên đổi tên (Task 152)
    full_name   = @full_name,
    name_search = @name_search,
    birthday    = sqlc.narg(birthday)::date,
    updated_at  = @now,
    version     = version + 1
WHERE id = @id
  AND version = @version
RETURNING version;

-- name: PromoteCustomer :one
-- lifecycle lead → customer ở đơn đầu tiên; không dòng nào (đã là customer) → pgx.ErrNoRows.
UPDATE customer.customers
SET lifecycle  = 'customer',
    updated_at = @now,
    version    = version + 1
WHERE id = @id
  AND lifecycle = 'lead'
RETURNING tenant_id, version;

-- name: TxNow :one
-- Giờ của transaction (lớp bọc M1 customer.Create, Q30: không gọi time.Now() trong module).
SELECT now()::timestamptz AS now;
