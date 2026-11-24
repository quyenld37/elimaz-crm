-- Danh sách nhật ký thao tác (kế hoạch M5, Task 457 / M5-45). Chạy trong tenancy.WithTenant (RLS). Không đọc before,
-- after, ip (có thể có dữ liệu cá nhân): chỉ người làm, hành động, thực thể, tên trường đổi.

-- name: ListAuditLogs :many
-- Mới nhất trước, trang theo (created_at, id) giảm dần; bộ lọc NULL là không lọc; [from_at, to_at). Index:
-- audit_logs_created, audit_logs_action, audit_logs_actor (0527), audit_logs_entity (0004). LIMIT là hằng 51
-- (identity.AuditPageSize + 1, đọc thừa một dòng để biết còn trang sau): LIMIT theo tham số làm kế hoạch chung (pgx
-- dùng lại câu đã chuẩn bị) ước 10% bảng và chọn quét cả bảng rồi xếp (TestListAuditLogs_Plan).
SELECT id, actor_type, COALESCE(actor_id, '')::text AS actor_id, action, entity_type,
       COALESCE(entity_id, '')::text AS entity_id, changed_fields, created_at
FROM identity.audit_logs
WHERE (sqlc.narg(action)::text IS NULL OR action = sqlc.narg(action)::text)
  AND (sqlc.narg(actor_id)::text IS NULL OR actor_id = sqlc.narg(actor_id)::text)
  AND (sqlc.narg(entity_type)::text IS NULL OR entity_type = sqlc.narg(entity_type)::text)
  AND (sqlc.narg(entity_id)::text IS NULL OR entity_id = sqlc.narg(entity_id)::text)
  AND (sqlc.narg(from_at)::timestamptz IS NULL OR created_at >= sqlc.narg(from_at)::timestamptz)
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR created_at < sqlc.narg(to_at)::timestamptz)
  AND (sqlc.narg(after_created)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(after_created)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT 51;

-- name: AuditActorNames :many
-- Tên người làm của một trang audit: nhân viên theo mã (display_name), khóa dịch vụ theo mã (tên khóa, cả khóa đã thu
-- hồi). kind là actor_type của dòng audit tương ứng.
SELECT 'staff'::text AS kind, u.id, u.display_name AS name
FROM identity.users AS u
WHERE u.id = ANY(@staff_ids::uuid[])
UNION ALL
SELECT 'service'::text AS kind, k.id, k.name
FROM identity.service_keys AS k
WHERE k.id = ANY(@key_ids::uuid[]);

-- name: AuditActors :many
-- Người làm cho ô lọc của màn nhật ký (Task 488): mọi nhân viên (kể cả đã khóa) và mọi khóa dịch vụ (kể cả đã thu hồi)
-- của tenant, chỉ tên; active là nhân viên chưa khóa, khóa chưa thu hồi. Xếp theo tên rồi mã.
SELECT 'staff'::text AS actor_type, u.id, u.display_name AS name, (u.status = 'active') AS active
FROM identity.users AS u
UNION ALL
SELECT 'service'::text AS actor_type, k.id, k.name, (k.revoked_at IS NULL) AS active
FROM identity.service_keys AS k
ORDER BY name, id;
