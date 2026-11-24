-- Góp ý của khách (kế hoạch M5, Task 455). Chạy trong tenancy.WithTenant. Khóa: chỉ một dòng customer.feedback (nhóm 5
-- ADR-46), không khóa bảng khác.

-- name: InsertFeedback :one
INSERT INTO customer.feedback (tenant_id, topic, mood, body, name, contact, order_ref, page, device, client_hash,
                               customer_id, created_at, updated_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @topic, sqlc.narg(mood)::int, @body,
        sqlc.narg(name)::text, sqlc.narg(contact)::text, sqlc.narg(order_ref)::text, sqlc.narg(page)::text,
        sqlc.narg(device)::text, sqlc.narg(client_hash)::text, sqlc.narg(customer_id)::uuid, @now, @now)
RETURNING tenant_id, id;

-- name: UpdateFeedback :many
-- Đổi trạng thái (state NULL: giữ), ghi chú (set_note false: giữ; note NULL: xóa) khi version còn khớp. 0 dòng: không có
-- hay version cũ (bên gọi đọc FeedbackExists để phân biệt).
UPDATE customer.feedback
SET state      = coalesce(sqlc.narg(state)::text, state),
    note       = CASE WHEN @set_note::bool THEN sqlc.narg(note)::text ELSE note END,
    updated_at = @now,
    updated_by = sqlc.narg(updated_by)::uuid,
    version    = version + 1
WHERE id = @id
  AND version = @version
RETURNING tenant_id, id;

-- name: FeedbackExists :one
SELECT EXISTS (SELECT 1 FROM customer.feedback WHERE id = @id);

-- name: ListFeedback :many
-- Mới nhất trước, trang theo (created_at, id) giảm dần. root_id: chỉ góp ý gắn với một hồ sơ trong nhóm của gốc đó (khách
-- đã gộp vẫn thấy góp ý gắn lúc trước). id: chỉ một góp ý (đọc lại sau khi sửa). Kèm gốc hiện tại, tên và mã KH của gốc.
SELECT f.id, f.topic, f.mood, f.body, f.name, f.contact, f.order_ref, f.page, f.device, f.state, f.note, f.created_at,
       f.updated_at, f.updated_by, f.version, r.id AS root_id, r.full_name AS root_name, r.code AS root_code
FROM customer.feedback AS f
LEFT JOIN customer.customers AS c ON c.tenant_id = f.tenant_id AND c.id = f.customer_id
LEFT JOIN customer.customers AS r ON r.tenant_id = c.tenant_id AND r.id = c.root_id
WHERE (sqlc.narg(id)::uuid IS NULL OR f.id = sqlc.narg(id)::uuid)
  AND (sqlc.narg(state)::text IS NULL OR f.state = sqlc.narg(state)::text)
  AND (sqlc.narg(topic)::text IS NULL OR f.topic = sqlc.narg(topic)::text)
  AND (sqlc.narg(root_id)::uuid IS NULL
       OR f.customer_id IN (SELECT m.id FROM customer.customers AS m WHERE m.root_id = sqlc.narg(root_id)::uuid))
  AND (sqlc.narg(after_created)::timestamptz IS NULL
       OR (f.created_at, f.id) < (sqlc.narg(after_created)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY f.created_at DESC, f.id DESC
LIMIT @lim::int;

-- name: CountFeedback :many
-- Số góp ý theo trạng thái của tenant (Task 486; index feedback_state_created).
SELECT state, count(*)::int AS n
FROM customer.feedback
GROUP BY state;
