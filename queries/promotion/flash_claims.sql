-- Lượt mua flash sale, phần của promotion (kế hoạch M5, Task 201 / M5-18; ADR-40). Chạy trong tx tenant của bên gọi
-- (RLS lọc theo tenant). Giới hạn mỗi khách nằm trong database: một câu upsert có điều kiện trên dòng claim (ADR-46
-- nhóm 10: flash_sale_claims là khóa đầu tiên của lượt mua). Không khóa flash_sales, flash_sale_items.

-- name: FlashItemForBuy :one
-- Món và đợt của món (đọc, không khóa).
SELECT i.id, i.flash_sale_id, i.variant_id, i.seller_id, i.warehouse_id, i.sale_price, i.per_customer_limit,
       i.hold_minutes, s.status, s.starts_at, s.ends_at
FROM promotion.flash_sale_items AS i
JOIN promotion.flash_sales AS s ON s.id = i.flash_sale_id
WHERE i.id = @item_id;

-- name: ClaimFlash :one
-- Cộng qty vào số khách đã lấy của món khi tổng mới ≤ limit (nhánh chèn mới: qty ≤ limit). Không dòng trả về: vượt giới
-- hạn. Hai lượt cùng khách cùng món chờ nhau trên dòng này (READ COMMITTED: điều kiện đánh giá lại trên bản mới nhất).
INSERT INTO promotion.flash_sale_claims AS c (tenant_id, flash_sale_item_id, customer_id, quantity)
SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid, @item_id::uuid, @customer_id::uuid, @qty::int
WHERE @qty::int <= @claim_limit::int
ON CONFLICT (tenant_id, flash_sale_item_id, customer_id) DO UPDATE
SET quantity = c.quantity + EXCLUDED.quantity, updated_at = now()
WHERE c.quantity + EXCLUDED.quantity <= @claim_limit::int
RETURNING c.quantity;

-- name: UnclaimFlash :execrows
-- Trả qty về số khách đã lấy của món khi còn đủ (0 dòng: không có claim hay claim nhỏ hơn qty).
UPDATE promotion.flash_sale_claims
SET quantity = quantity - @qty::int, updated_at = now()
WHERE flash_sale_item_id = @item_id AND customer_id = @customer_id AND quantity >= @qty::int;

-- name: FlashSaleStatusForShare :one
-- Trạng thái đợt, khóa chia sẻ tới hết tx (M5 Task 202): đường hủy đơn flash quyết định "đợt còn mở hay đã đóng" và
-- đóng đợt (FOR UPDATE) chờ nhau, nên quyết định không đổi trong tx. ADR-46 nhóm 10: flash_sales trước claims.
SELECT status
FROM promotion.flash_sales
WHERE id = @id
FOR SHARE;
