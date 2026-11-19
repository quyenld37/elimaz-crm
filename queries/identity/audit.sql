-- Nhật ký thao tác chỉ ghi thêm (kế hoạch M0, Task 8). Chạy trong transaction của bên gọi, trong tenancy.WithTenant.
-- Chuỗi rỗng của cột tùy chọn lưu NULL; before/after nil lưu NULL.

-- name: InsertAuditLog :exec
INSERT INTO identity.audit_logs
    (tenant_id, actor_type, actor_id, action, entity_type, entity_id, changed_fields, before, after, ip, request_id)
VALUES (
    @tenant_id,
    @actor_type,
    NULLIF(@actor_id::text, ''),
    @action,
    @entity_type,
    NULLIF(@entity_id::text, ''),
    @changed_fields::text[],
    sqlc.narg(before)::jsonb,
    sqlc.narg(after)::jsonb,
    NULLIF(@ip::text, '')::inet,
    NULLIF(@request_id::text, '')
);

-- name: ListEntityAudit :many
-- Dòng audit của một thực thể (kế hoạch M2, Task M2-24: lịch sử thao tác của đơn), mới nhất trước, tối đa 500 dòng mới
-- nhất (Task 425: thực thể có hơn 500 dòng vẫn có các lần sửa mới nhất). Chỉ tên trường bên ghi liệt kê ở after.fields
-- (mảng chuỗi; không phải mảng → rỗng); không đọc giá trị before/after.
SELECT id, actor_type, COALESCE(actor_id, '')::text AS actor_id, action,
       (CASE WHEN jsonb_typeof(after -> 'fields') = 'array'
             THEN ARRAY(SELECT jsonb_array_elements_text(after -> 'fields'))
             ELSE '{}'::text[] END)::text[] AS listed_fields,
       created_at
FROM identity.audit_logs
WHERE entity_type = @entity_type AND entity_id = @entity_id::text
ORDER BY created_at DESC, id DESC
LIMIT 500;
