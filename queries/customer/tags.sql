-- Nhãn khách gắn tay (kế hoạch M2, Task M2-12; nhãn theo quy tắc: Task M2-25). Chạy trong tenancy.WithTenant.
-- Thứ tự khóa (ADR-46 nhóm 5, mục e): gắn/gỡ chỉ chạm customer_tag_links (sau customers); customer_tags chỉ sửa trong
-- transaction riêng, không bị khóa khi gắn nhãn.

-- name: ListTags :many
SELECT id, name, color, version
FROM customer.customer_tags
ORDER BY name, id;

-- name: GetTag :one
SELECT id, name, color, version
FROM customer.customer_tags
WHERE id = @id;

-- name: InsertTag :one
-- Tên trùng: vi phạm customer_tags_name_key (bên gọi đổi thành ErrTagNameTaken).
INSERT INTO customer.customer_tags (tenant_id, name, color, created_at, updated_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @name, @color, @now, @now)
RETURNING tenant_id, id, name, color, version;

-- name: UpdateTag :one
-- Sửa khi version còn khớp; không dòng → nhãn không có hoặc version lệch.
UPDATE customer.customer_tags
SET name = @name, color = @color, updated_at = @now, version = version + 1
WHERE id = @id AND version = @version
RETURNING tenant_id, id, name, color, version;

-- name: InsertTagLink :one
-- Gắn nhãn; đã có (cùng hồ sơ, cùng nhãn, nguồn nào cũng vậy) thì không làm gì (không dòng trả về).
INSERT INTO customer.customer_tag_links (tenant_id, customer_id, tag_id, source, created_by, created_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @customer_id, @tag_id, @source, @created_by, @now)
ON CONFLICT (tenant_id, customer_id, tag_id) DO NOTHING
RETURNING tenant_id;

-- name: DeleteStaffTagLinks :many
-- Gỡ nhãn gắn tay khỏi mọi hồ sơ của nhóm; dòng rule giữ nguyên (quy tắc quản).
DELETE FROM customer.customer_tag_links
WHERE customer_id = ANY (@ids::uuid[])
  AND tag_id = @tag_id
  AND source = 'staff'
RETURNING tenant_id;

-- name: CountRuleTagLinks :one
SELECT count(*)
FROM customer.customer_tag_links
WHERE customer_id = ANY (@ids::uuid[])
  AND tag_id = @tag_id
  AND source = 'rule';

-- name: ListGroupTags :many
-- Nhãn của cả nhóm (mỗi nhãn một dòng): nguồn staff nếu có ít nhất một dòng gắn tay, không thì rule.
SELECT t.id, t.name, t.color, t.version, max(l.source)::text AS source
FROM customer.customer_tag_links AS l
JOIN customer.customer_tags AS t ON t.tenant_id = l.tenant_id AND t.id = l.tag_id
WHERE l.customer_id = ANY (@ids::uuid[])
GROUP BY t.id, t.name, t.color, t.version
ORDER BY t.name, t.id;
