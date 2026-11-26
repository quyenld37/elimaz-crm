-- Cờ "không liên hệ" và tên từ kênh (kế hoạch M4, Task 152; 02 mục 3, CH08). Chạy trong tenancy.WithTenant.

-- name: SetDoNotContact :one
-- Chỉ đổi khi khác giá trị hiện tại (bật lại khi đã bật: 0 dòng, pgx.ErrNoRows). Bên gọi đã khóa dòng gốc.
UPDATE customer.customers
SET do_not_contact    = @enabled::boolean,
    do_not_contact_at = sqlc.narg(at)::timestamptz,
    updated_at        = @now,
    version           = version + 1
WHERE id = @id
  AND do_not_contact <> @enabled::boolean
RETURNING version;

-- name: GetDoNotContactOfRoot :one
-- Cờ của hồ sơ đại diện (gốc) của id.
SELECT r.do_not_contact
FROM customer.customers c
JOIN customer.customers r ON r.tenant_id = c.tenant_id AND r.id = c.root_id
WHERE c.id = @id;

-- name: SetNameFromChannel :one
-- Một câu có điều kiện (không đọc ra rồi ghi lại): tên nhân viên (staff) không bao giờ bị đè; cùng tên thì không đổi.
-- 0 dòng → pgx.ErrNoRows.
UPDATE customer.customers
SET full_name   = @full_name,
    name_search = @name_search,
    name_source = 'channel',
    updated_at  = now(),
    version     = version + 1
WHERE id = @id
  AND name_source <> 'staff'
  AND full_name <> @full_name
RETURNING tenant_id, root_id, version;
