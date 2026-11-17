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
