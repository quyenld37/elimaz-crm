-- Danh mục đơn vị hành chính hai cấp (kế hoạch M2, Task M2-04). Bảng không có tenant_id; chạy được trong transaction
-- tenant của bên gọi.

-- name: ListAdminProvinces :many
SELECT code, name
FROM customer.admin_units
WHERE level = 'province'
ORDER BY code;

-- name: ListAdminWards :many
-- q đã bỏ dấu và thoát \ % _ (dùng ESCAPE '\').
SELECT code, name
FROM customer.admin_units
WHERE level = 'ward'
  AND parent_code = @province_code::text
  AND (@q::text = '' OR name_search LIKE '%' || @q::text || '%' ESCAPE '\')
ORDER BY name_search, code
LIMIT @max_rows::int;

-- name: GetAdminUnits :many
SELECT code, level, parent_code, name
FROM customer.admin_units
WHERE code = ANY (@codes::text[]);

-- name: ListAllAdminWards :many
-- Mọi xã của mọi tỉnh (danh mục địa giới cho website, kế hoạch M5 Task 191), theo tỉnh rồi tên.
SELECT code, parent_code::text AS parent_code, name
FROM customer.admin_units
WHERE level = 'ward'
ORDER BY parent_code, name_search, code;

-- name: InsertFixtureAdminUnit :execrows
-- Đơn vị hành chính thử của e2e website (lệnh seed-web-fixture, Task 218): chỉ chủ bảng ghi được (crm_app chỉ SELECT);
-- mã đã có thì không đổi gì (bên gọi đọc lại để kiểm tên, cấp, cha).
INSERT INTO customer.admin_units (code, level, parent_code, name, name_search, effective_from, source)
VALUES (@code, @level, sqlc.narg(parent_code), @name, @name_search, DATE '2025-07-01', 'web-fixture')
ON CONFLICT (code) DO NOTHING;
