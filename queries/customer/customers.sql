-- Khách tối thiểu (kế hoạch M1, Task M1-30; 05 mục 1). Chạy trong tenancy.WithTenant. M2 mở rộng.

-- name: InsertCustomer :exec
-- Hồ sơ chưa gộp: root_id = id. tenant_id lấy từ ngữ cảnh của tenancy.WithTenant (RLS kiểm lại).
INSERT INTO customer.customers (id, tenant_id, root_id, full_name)
VALUES (@id, NULLIF(current_setting('app.tenant_id', true), '')::uuid, @id, @full_name);
