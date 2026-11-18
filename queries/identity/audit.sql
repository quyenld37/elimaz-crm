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
