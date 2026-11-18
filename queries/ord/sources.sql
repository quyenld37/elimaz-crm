-- Nguồn đơn do shop cấu hình (kế hoạch M2, Task M2-15). Chạy trong tenancy.WithTenant; RLS lọc theo tenant.

-- name: ListTrafficSources :many
SELECT id, coalesce(key, '')::text AS key, name, active, position, version
FROM ord.traffic_sources
ORDER BY position, name, id;

-- name: InsertTrafficSource :one
-- Nguồn nhân viên tạo: không có key, đứng cuối danh sách (vị trí lớn nhất + 1; tập rỗng → 1). tenant_id lấy từ ngữ
-- cảnh tenancy.WithTenant (thiếu → NULL, NOT NULL chặn).
INSERT INTO ord.traffic_sources (tenant_id, name, position)
SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid, @name::text, coalesce(max(position), 0) + 1
FROM ord.traffic_sources
RETURNING id, coalesce(key, '')::text AS key, name, active, position, version;

-- name: LockTrafficSource :one
SELECT id, coalesce(key, '')::text AS key, name, active, position, version
FROM ord.traffic_sources
WHERE id = @id
FOR UPDATE;

-- name: UpdateTrafficSource :one
UPDATE ord.traffic_sources
SET name       = @name::text,
    active     = @active::boolean,
    position   = @position::int,
    version    = version + 1,
    updated_at = now()
WHERE id = @id AND version = @version::bigint
RETURNING id, coalesce(key, '')::text AS key, name, active, position, version;

-- name: TrafficSourceActive :one
SELECT active
FROM ord.traffic_sources
WHERE id = @id;
