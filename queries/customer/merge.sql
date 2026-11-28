-- Gộp khách và hoàn tác (kế hoạch M2, Task M2-11; 05 mục 1, Q14). Gộp chỉ đổi root_id, không dời dữ liệu. Chạy trong
-- tenancy.WithTenant (RLS lọc tenant). Thứ tự khóa ADR-46 nhóm 5: customers (theo id tăng dần) → customer_merges.

-- name: LockCustomers :many
-- Khóa cả tập hồ sơ theo id tăng dần (LockRows chạy trên kết quả đã sắp) và trả gốc đọc dưới khóa.
SELECT id, tenant_id, root_id, do_not_contact, do_not_contact_at
FROM customer.customers
WHERE id = ANY (@ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: SetRootID :exec
UPDATE customer.customers
SET root_id    = @root_id,
    updated_at = @now,
    version    = version + 1
WHERE id = ANY (@ids::uuid[]);

-- name: InsertMerge :one
INSERT INTO customer.customer_merges (tenant_id, survivor_id, merged_id, moved_ids, matched_by, actor_id, created_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @survivor_id, @merged_id, @moved_ids::uuid[],
        @matched_by, @actor_id, @now)
RETURNING id, tenant_id, created_at;

-- name: GetMerge :one
SELECT id, tenant_id, survivor_id, merged_id, moved_ids, matched_by, actor_id, created_at, undone_at
FROM customer.customer_merges
WHERE id = @id;

-- name: LockMerge :one
-- Sau khi đã khóa các hồ sơ (ADR-46: customers trước customer_merges).
SELECT undone_at
FROM customer.customer_merges
WHERE id = @id
FOR UPDATE;

-- name: BlockingMerge :one
-- Lần gộp chưa hoàn tác khác đã dời survivor_id của lần này (nên tạo sau lần này: gốc giữ lại chỉ bị dời sau khi đã
-- là gốc). Có nhiều lần thì lấy lần đã đưa survivor vào gốc hiện tại của nó (lần phải hoàn tác trước tiên).
SELECT m.id
FROM customer.customer_merges m
JOIN customer.customers c ON c.tenant_id = m.tenant_id AND c.id = @survivor_id::uuid
WHERE m.undone_at IS NULL
  AND m.id <> @merge_id
  AND @survivor_id::uuid = ANY (m.moved_ids)
ORDER BY (m.survivor_id = c.root_id) DESC, m.created_at DESC, m.id DESC
LIMIT 1;

-- name: MarkMergeUndone :exec
UPDATE customer.customer_merges
SET undone_at = @now::timestamptz,
    undone_by = sqlc.narg(undone_by)::uuid
WHERE id = @id;
