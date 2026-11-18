-- Cài đặt đơn và nguồn đơn mặc định (kế hoạch M2, Task M2-05). Chạy trong tenancy.WithTenant; RLS (WITH CHECK) từ
-- chối tenant_id khác ngữ cảnh.

-- name: EnsureOrderSettings :exec
INSERT INTO ord.order_settings (tenant_id)
VALUES (@tenant_id)
ON CONFLICT (tenant_id) DO NOTHING;

-- name: InsertTrafficSourceIfMissing :exec
-- Nguồn hệ thống có khóa cố định; đã có (cùng key, hay cùng tên do nhân viên tạo trước) thì bỏ qua.
INSERT INTO ord.traffic_sources (tenant_id, key, name, position)
VALUES (@tenant_id, @key::text, @name::text, @position::int)
ON CONFLICT DO NOTHING;

-- name: TrafficSourceByKey :one
SELECT id
FROM ord.traffic_sources
WHERE key = @key::text;
