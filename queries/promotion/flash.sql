-- Đợt flash sale và món của đợt (kế hoạch M5, Task 199). Mọi câu chạy trong tx tenant của bên gọi (RLS lọc theo
-- tenant). Đọc catalog chỉ qua view variants_v. Ghi món chỉ sau khi khóa dòng đợt (ADR-46 nhóm 10: flash_sales →
-- flash_sale_items).

-- name: InsertFlashSale :one
INSERT INTO promotion.flash_sales (tenant_id, name, starts_at, ends_at, created_by)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @name, @starts_at, @ends_at, @created_by)
RETURNING id, name, starts_at, ends_at, status, closed_reason, created_by, created_at, updated_at, version;

-- name: FlashSaleByID :one
SELECT id, name, starts_at, ends_at, status, closed_reason, created_by, created_at, updated_at, version
FROM promotion.flash_sales
WHERE id = @id;

-- name: FlashSaleByIDForUpdate :one
-- Mọi lệnh ghi đợt hay món: khóa dòng đợt trước món của nó (ADR-46 nhóm 10).
SELECT id, name, starts_at, ends_at, status, closed_reason, created_by, created_at, updated_at, version
FROM promotion.flash_sales
WHERE id = @id
FOR UPDATE;

-- name: UpdateFlashSale :one
-- Bên gọi đã khóa dòng, so version và kiểm còn nháp; version + 1.
UPDATE promotion.flash_sales
SET name = @name, starts_at = @starts_at, ends_at = @ends_at, version = version + 1, updated_at = now()
WHERE id = @id AND version = @version AND status = 'draft'
RETURNING id, name, starts_at, ends_at, status, closed_reason, created_by, created_at, updated_at, version;

-- name: ListFlashSales :many
-- Mới nhất trước (id uuidv7 giảm dần), con trỏ là id của dòng cuối trang trước; status rỗng = mọi trạng thái. Kèm số
-- món, tổng suất, số đơn vị đã bán (view inventory.flash_units_summary_v; Task 528).
SELECT s.id, s.name, s.starts_at, s.ends_at, s.status, s.closed_reason, s.created_by, s.created_at, s.updated_at,
       s.version, t.item_count, t.quantity, t.sold
FROM promotion.flash_sales AS s
CROSS JOIN LATERAL (
    SELECT count(*)::int AS item_count, coalesce(sum(i.quantity), 0)::int AS quantity,
           coalesce(sum(u.sold), 0)::int AS sold
    FROM promotion.flash_sale_items AS i
    LEFT JOIN LATERAL (SELECT v.sold FROM inventory.flash_units_summary_v AS v WHERE v.flash_sale_item_id = i.id) AS u
        ON true
    WHERE i.flash_sale_id = s.id
) AS t
WHERE (sqlc.narg(status)::text IS NULL OR s.status = sqlc.narg(status)::text)
  AND (sqlc.narg(after)::uuid IS NULL OR s.id < sqlc.narg(after)::uuid)
ORDER BY s.id DESC
LIMIT @row_limit;

-- name: VariantSeller :one
-- Người bán của biến thể (món flash sale lấy seller_id từ biến thể, spec 05).
SELECT seller_id FROM catalog.variants_v WHERE variant_id = @variant_id;

-- name: InsertFlashItem :one
INSERT INTO promotion.flash_sale_items (tenant_id, flash_sale_id, seller_id, variant_id, warehouse_id, sale_price,
                                        quantity, per_customer_limit, hold_minutes)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @flash_sale_id, @seller_id, @variant_id,
        @warehouse_id, @sale_price, @quantity, @per_customer_limit, @hold_minutes)
RETURNING id, flash_sale_id, seller_id, variant_id, warehouse_id, sale_price, quantity, per_customer_limit,
          hold_minutes, version;

-- name: FlashItemForUpdate :one
-- Bên gọi đã khóa dòng đợt.
SELECT id, flash_sale_id, seller_id, variant_id, warehouse_id, sale_price, quantity, per_customer_limit, hold_minutes,
       version
FROM promotion.flash_sale_items
WHERE id = @id AND flash_sale_id = @flash_sale_id
FOR UPDATE;

-- name: UpdateFlashItem :one
UPDATE promotion.flash_sale_items
SET sale_price = @sale_price, quantity = @quantity, per_customer_limit = @per_customer_limit,
    hold_minutes = @hold_minutes, version = version + 1
WHERE id = @id AND version = @version
RETURNING id, flash_sale_id, seller_id, variant_id, warehouse_id, sale_price, quantity, per_customer_limit,
          hold_minutes, version;

-- name: DeleteFlashItem :execrows
DELETE FROM promotion.flash_sale_items
WHERE id = @id AND flash_sale_id = @flash_sale_id;

-- name: FlashItemsBySale :many
-- Món của đợt theo thứ tự thêm (id uuidv7), kèm mã, tên, giá thường hiện tại của biến thể.
SELECT i.id, i.flash_sale_id, i.seller_id, i.variant_id, i.warehouse_id, i.sale_price, i.quantity,
       i.per_customer_limit, i.hold_minutes, i.version, v.sku, v.product_name, v.color, v.size,
       v.price AS list_price
FROM promotion.flash_sale_items AS i
JOIN catalog.variants_v AS v ON v.variant_id = i.variant_id AND v.seller_id = i.seller_id
WHERE i.flash_sale_id = @flash_sale_id
ORDER BY i.id;

-- name: FlashSaleSold :one
-- Số đơn vị suất đã bán của mọi món trong đợt (view inventory.flash_units_summary_v; đợt chưa trải đơn vị: 0; Task 528).
SELECT coalesce(sum(u.sold), 0)::int AS sold
FROM promotion.flash_sale_items AS i
JOIN inventory.flash_units_summary_v AS u ON u.flash_sale_item_id = i.id
WHERE i.flash_sale_id = @flash_sale_id;
