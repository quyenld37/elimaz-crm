-- Bộ phối (kế hoạch M5, Task 454). Mọi câu chạy trong tenancy.WithTenant (RLS). Module khác chỉ đọc qua view công bố
-- (docs/views.md): mẫu, biến thể, người bán, ảnh của catalog. Còn hàng (view inventory) do lớp HTTP đọc qua webcatalog.

-- name: GetLookSet :one
-- revision của cả danh sách, không khóa (đường đọc). Chưa có dòng → pgx.ErrNoRows (= rỗng, revision 0).
SELECT revision, updated_by, updated_at
FROM content.look_sets;

-- name: EnsureLookSet :exec
-- Chèn dòng revision 0 nếu tenant chưa có; RLS (WITH CHECK) từ chối tenant_id khác ngữ cảnh.
INSERT INTO content.look_sets (tenant_id)
VALUES (@tenant_id)
ON CONFLICT (tenant_id) DO NOTHING;

-- name: LockLookSet :one
-- Khóa dòng của tenant (ADR-46 nhóm 12d, sau bảng banner) trước khi so revision và thay danh sách.
SELECT revision
FROM content.look_sets
FOR UPDATE;

-- name: BumpLookSet :one
UPDATE content.look_sets
SET revision   = revision + 1,
    updated_by = @updated_by::uuid,
    updated_at = @updated_at::timestamptz
WHERE tenant_id = @tenant_id
RETURNING revision, updated_by, updated_at;

-- name: ListLooks :many
SELECT id, on_home, name, title, headline, story, photo_file_id, category_ids, model_height_cm, model_size, starts_at,
       ends_at
FROM content.looks
ORDER BY position;

-- name: ListLookItems :many
SELECT look_id, product_id, color, x_pct::float8 AS x_pct, y_pct::float8 AS y_pct
FROM content.look_items
ORDER BY look_id, position;

-- name: DeleteLooks :exec
-- Xóa cả danh sách của tenant; look_items xóa dây chuyền (khóa looks rồi look_items, đúng thứ tự 12d).
DELETE FROM content.looks
WHERE tenant_id = @tenant_id;

-- name: InsertLook :exec
INSERT INTO content.looks (id, tenant_id, position, on_home, name, title, headline, story, photo_file_id, category_ids,
                           model_height_cm, model_size, starts_at, ends_at)
VALUES (@id, @tenant_id, @position, @on_home, @name, @title, @headline, @story, @photo_file_id, @category_ids::uuid[],
        sqlc.narg(model_height_cm), @model_size, sqlc.narg(starts_at), sqlc.narg(ends_at));

-- name: InsertLookItem :exec
INSERT INTO content.look_items (tenant_id, look_id, product_id, color, x_pct, y_pct, position)
VALUES (@tenant_id, @look_id, @product_id, @color, @x_pct::float8, @y_pct::float8, @position);

-- name: LookProducts :many
-- Mẫu (chưa xóa) theo mã kèm "đang bán": status active, người bán hoạt động, có biến thể bán được giá > 0 (như mẫu có
-- trong danh mục đang bán của website; 0 ₫ không bán). Mã không có, đã xóa → không có dòng.
SELECT p.product_id, p.code, p.name, p.category_id,
       (p.status = 'active'
        AND EXISTS (SELECT 1 FROM catalog.sellers_v AS s WHERE s.seller_id = p.seller_id AND s.status = 'active')
        AND EXISTS (SELECT 1 FROM catalog.variants_v AS v
                    WHERE v.product_id = p.product_id AND v.sellable AND v.price > 0))::boolean AS on_sale
FROM catalog.products_v AS p
WHERE p.product_id = ANY(@ids::uuid[]) AND p.deleted_at IS NULL
ORDER BY p.product_id;

-- name: LookProductImages :many
-- Ảnh đang hiện (chưa xóa) của các mẫu theo thứ tự hiện: ảnh người mẫu của bộ chọn trong số này.
SELECT i.product_id, i.file_id, i.color, i.position
FROM catalog.product_images_v AS i
WHERE i.product_id = ANY(@product_ids::uuid[])
ORDER BY i.product_id, i.position, i.image_id;
