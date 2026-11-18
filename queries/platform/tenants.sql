-- Bảng tenant (kế hoạch M0, Task 4). crm_app chỉ đọc; tạo tenant do chủ bảng làm.

-- name: GetTenant :one
SELECT id, name, created_at
FROM platform.tenants
WHERE id = @id;

-- name: CreateTenant :one
INSERT INTO platform.tenants (name)
VALUES (@name)
RETURNING id, name, created_at;

-- name: FindTenantByName :one
-- Lệnh crm seed-dev: tenant cũ nhất có tên này (tên không unique; seed chạy lại không tạo thêm).
SELECT id
FROM platform.tenants
WHERE name = @name
ORDER BY id
LIMIT 1;
