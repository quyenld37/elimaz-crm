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

-- name: GetOrderSettings :one
SELECT draft_ttl_minutes, duplicate_window_minutes, discount_approval_threshold_bp, return_warning_days, version
FROM ord.order_settings;

-- name: LockOrderSettings :one
-- Khóa dòng cài đặt của tenant (nhóm 7 ADR-46, bảng cuối) trước khi so version (order.Writer.UpdateSettings).
SELECT draft_ttl_minutes, duplicate_window_minutes, discount_approval_threshold_bp, return_warning_days, version
FROM ord.order_settings
FOR UPDATE;

-- name: UpdateOrderSettings :one
UPDATE ord.order_settings
SET draft_ttl_minutes              = @draft_ttl_minutes::int,
    duplicate_window_minutes       = @duplicate_window_minutes::int,
    discount_approval_threshold_bp = @discount_approval_threshold_bp::int,
    return_warning_days            = @return_warning_days::int,
    version                        = version + 1,
    updated_at                     = now(),
    updated_by                     = @updated_by::uuid
WHERE version = @version::bigint
RETURNING draft_ttl_minutes, duplicate_window_minutes, discount_approval_threshold_bp, return_warning_days, version;
