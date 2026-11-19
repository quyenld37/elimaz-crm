-- Quy tắc tự gắn nhãn theo chỉ số khách (kế hoạch M2, Task M2-25). Chạy trong tenancy.WithTenant. Thứ tự khóa ADR-46
-- nhóm 5: customers → customer_tag_links → customer_tag_rules; đường áp quy tắc chỉ đọc customer_tag_rules.

-- name: GetTagRule :one
SELECT tag_id, conditions, enabled, version
FROM customer.customer_tag_rules
WHERE tag_id = @tag_id;

-- name: InsertTagRule :one
-- Nhãn chưa có quy tắc; đã có (PUT song song vừa commit) → không dòng.
INSERT INTO customer.customer_tag_rules (tenant_id, tag_id, conditions, enabled, updated_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @tag_id, @conditions, @enabled, @now)
ON CONFLICT (tenant_id, tag_id) DO NOTHING
RETURNING tenant_id, tag_id, conditions, enabled, version;

-- name: UpdateTagRule :one
-- Sửa khi version còn khớp; không dòng → chưa có quy tắc hoặc version lệch.
UPDATE customer.customer_tag_rules
SET conditions = @conditions, enabled = @enabled, updated_at = @now, version = version + 1
WHERE tag_id = @tag_id AND version = @version
RETURNING tenant_id, tag_id, conditions, enabled, version;

-- name: ListTagRules :many
SELECT tag_id, conditions, enabled
FROM customer.customer_tag_rules
ORDER BY tag_id;

-- name: ListRuleTagLinks :many
-- Dòng nhãn do quy tắc gắn trên mọi hồ sơ của nhóm.
SELECT customer_id, tag_id
FROM customer.customer_tag_links
WHERE customer_id = ANY (@ids::uuid[])
  AND source = 'rule'
ORDER BY customer_id, tag_id;

-- name: DeleteRuleTagLink :exec
-- Gỡ đúng một dòng rule; dòng staff không bao giờ bị gỡ ở đây.
DELETE FROM customer.customer_tag_links
WHERE customer_id = @customer_id
  AND tag_id = @tag_id
  AND source = 'rule';

-- name: LockRootsAfter :many
-- Lô gốc của lịch đêm: hồ sơ gốc có id > @after, theo id tăng dần, khóa dòng (ADR-46 nhóm 5); kèm chỉ số.
SELECT id, tenant_id, order_count, delivered_count, returned_count, total_spent, last_order_at
FROM customer.customers
WHERE id = root_id
  AND id > @after
ORDER BY id
LIMIT @max_rows
FOR UPDATE;
