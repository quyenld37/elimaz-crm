-- Vòng đời đợt flash sale (kế hoạch M5, Task 200 / M5-17): lên lịch, mở, đóng, thêm suất, số liệu đang chạy. Mọi câu
-- chạy trong tx tenant của bên gọi. Ghi đợt hay món chỉ sau khi khóa dòng đợt (ADR-46 nhóm 10: flash_sales →
-- flash_sale_items). Đơn vị suất đọc qua view inventory.flash_units_summary_v.

-- name: SetFlashSaleStatus :one
-- Bên gọi đã khóa dòng và kiểm trạng thái cũ; version + 1 (Seq của sự kiện đợt). closed_reason chỉ đặt khi đóng.
UPDATE promotion.flash_sales
SET status = @status, closed_reason = sqlc.narg(closed_reason), version = version + 1, updated_at = now()
WHERE id = @id AND status = @from_status
RETURNING tenant_id, id, name, starts_at, ends_at, status, closed_reason, created_by, created_at, updated_at, version;

-- name: BumpFlashSaleVersion :one
-- Thêm suất: đợt đổi số suất (bộ đệm website, màn hình làm mới theo sự kiện); bên gọi đã khóa dòng.
UPDATE promotion.flash_sales
SET version = version + 1, updated_at = now()
WHERE id = @id
RETURNING tenant_id, version;

-- name: FlashOverlaps :many
-- Biến thể của đợt sale_id đang nằm trong đợt khác scheduled/open có thời gian chồng [starts_at, ends_at).
SELECT DISTINCT o.variant_id
FROM promotion.flash_sale_items AS o
JOIN promotion.flash_sales AS s ON s.id = o.flash_sale_id
WHERE s.id <> @sale_id
  AND s.status IN ('scheduled', 'open')
  AND s.starts_at < @ends_at AND s.ends_at > @starts_at
  AND o.variant_id IN (SELECT i.variant_id FROM promotion.flash_sale_items AS i WHERE i.flash_sale_id = @sale_id)
ORDER BY o.variant_id;

-- name: DueFlashSales :many
-- Nhịp: khóa các đợt đến hạn mở (scheduled, starts_at ≤ now) hay đến hạn đóng (scheduled/open, ends_at ≤ now) theo id
-- tăng dần (ADR-46). Đợt vừa bị transaction khác đổi trạng thái được đánh giá lại sau khi chờ khóa.
SELECT id, name, starts_at, ends_at, status, closed_reason, created_by, created_at, updated_at, version
FROM promotion.flash_sales
WHERE (status = 'scheduled' AND starts_at <= @now::timestamptz)
   OR (status IN ('scheduled', 'open') AND ends_at <= @now::timestamptz)
ORDER BY id
FOR UPDATE;

-- name: AddFlashItemQuantity :one
-- Thêm suất: bên gọi đã khóa đợt và món; CHECK quantity ≤ 20000 của bảng giữ trần.
UPDATE promotion.flash_sale_items
SET quantity = quantity + @add::int, version = version + 1
WHERE id = @id
RETURNING id, flash_sale_id, seller_id, variant_id, warehouse_id, sale_price, quantity, per_customer_limit,
          hold_minutes, version;

-- name: FlashLiveItems :many
-- Số liệu đang chạy của từng món: suất, đơn vị theo trạng thái, số khách đã lấy (quantity > 0).
SELECT i.id, i.quantity,
       coalesce(u.available, 0)::int AS available, coalesce(u.held, 0)::int AS held,
       coalesce(u.sold, 0)::int AS sold, coalesce(u.released, 0)::int AS released,
       (SELECT count(*) FROM promotion.flash_sale_claims AS c
        WHERE c.flash_sale_item_id = i.id AND c.quantity > 0)::int AS buyers
FROM promotion.flash_sale_items AS i
LEFT JOIN inventory.flash_units_summary_v AS u ON u.flash_sale_item_id = i.id
WHERE i.flash_sale_id = @flash_sale_id
ORDER BY i.id;
