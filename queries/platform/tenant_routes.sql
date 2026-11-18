-- Định tuyến tenant (kế hoạch M0, Task 5): tìm tenant từ khóa ngoài (Page ID, email nhân viên…) trước khi có ngữ
-- cảnh tenant. platform.tenant_routes và platform.tenants không RLS; crm_app chỉ đọc.

-- name: ResolveTenantRoute :one
SELECT tenant_id
FROM platform.tenant_routes
WHERE kind = @kind AND external_key = @external_key;

-- name: ListTenantIDs :many
SELECT id
FROM platform.tenants
ORDER BY id;

-- name: AddTenantRoute :one
-- Thêm định tuyến; đã có thì giữ dòng cũ. Trả tenant mà khóa đang trỏ tới (bên gọi so với tenant mong muốn).
WITH ins AS (
    INSERT INTO platform.tenant_routes (kind, external_key, tenant_id)
    VALUES (@kind, @external_key, @tenant_id)
    ON CONFLICT (kind, external_key) DO NOTHING
    RETURNING tenant_id
)
SELECT tenant_id FROM ins
UNION ALL
SELECT r.tenant_id FROM platform.tenant_routes AS r WHERE r.kind = @kind AND r.external_key = @external_key
LIMIT 1;

-- Ghi tuyến kind/key cho tenant trong ngữ cảnh (app.tenant_id) qua hàm SECURITY DEFINER của 0290 (crm_app không có
-- INSERT trên bảng). Khóa đã thuộc tenant khác → SQLSTATE 23505.
-- name: PutTenantRoute :exec
SELECT platform.put_tenant_route(@kind::text, @external_key::text);
