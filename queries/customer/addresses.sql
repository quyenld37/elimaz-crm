-- Địa chỉ của khách (kế hoạch M2, Task M2-09). Xóa mềm (deleted_at). Một địa chỉ mặc định mỗi hồ sơ
-- (customer_addresses_one_default): bên gọi khóa dòng khách trước rồi mới đổi địa chỉ (ADR-46 nhóm 5).

-- name: InsertAddress :one
INSERT INTO customer.customer_addresses (tenant_id, customer_id, recipient_name, phone, province_code, ward_code,
                                         street, is_default, created_at, updated_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @customer_id, @recipient_name, @phone,
        @province_code, @ward_code, @street, @is_default, @now, @now)
RETURNING id, version;

-- name: ClearDefaultAddress :exec
UPDATE customer.customer_addresses
SET is_default = false,
    updated_at = @now,
    version    = version + 1
WHERE customer_id = @customer_id
  AND is_default
  AND deleted_at IS NULL
  AND id <> @keep_id;

-- name: GetAddressOwner :one
SELECT customer_id
FROM customer.customer_addresses
WHERE id = @id
  AND deleted_at IS NULL;

-- name: LockAddress :one
SELECT recipient_name, phone, province_code, ward_code, street, is_default, version
FROM customer.customer_addresses
WHERE id = @id
  AND deleted_at IS NULL
FOR UPDATE;

-- name: UpdateAddress :one
UPDATE customer.customer_addresses
SET recipient_name = @recipient_name,
    phone          = @phone,
    province_code  = @province_code,
    ward_code      = @ward_code,
    street         = @street,
    is_default     = @is_default,
    updated_at     = @now,
    version        = version + 1
WHERE id = @id
  AND version = @version
  AND deleted_at IS NULL
RETURNING version;

-- name: SoftDeleteAddress :execrows
UPDATE customer.customer_addresses
SET deleted_at = @now::timestamptz,
    is_default = false,
    updated_at = @now,
    version    = version + 1
WHERE id = @id
  AND deleted_at IS NULL;

-- name: ListGroupAddresses :many
SELECT a.id, a.customer_id, a.recipient_name, a.phone, a.province_code, a.ward_code, a.street, a.is_default, a.version,
       coalesce(p.name, '') AS province_name, coalesce(w.name, '') AS ward_name
FROM customer.customer_addresses a
LEFT JOIN customer.admin_units p ON p.code = a.province_code
LEFT JOIN customer.admin_units w ON w.code = a.ward_code
WHERE a.customer_id = ANY (@customer_ids::uuid[])
  AND a.deleted_at IS NULL
ORDER BY a.is_default DESC, a.created_at, a.id;
