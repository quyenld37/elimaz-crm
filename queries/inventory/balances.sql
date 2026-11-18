-- Số dư tồn (kế hoạch M1, Task M1-28; ADR-46 điểm 4, 5). Chạy trong tenancy.WithTenant: RLS giới hạn trong tenant.

-- name: EnsureBalance :exec
-- Chèn dòng số dư 0 nếu chưa có; người bán lấy từ biến thể (view công bố của catalog). Không khóa dòng đã có.
INSERT INTO inventory.stock_balances (tenant_id, variant_id, warehouse_id, seller_id)
SELECT w.tenant_id, v.variant_id, w.id, v.seller_id
FROM inventory.warehouses AS w
JOIN catalog.variants_v AS v ON v.variant_id = @variant_id::uuid
WHERE w.id = @warehouse_id::uuid
ON CONFLICT (tenant_id, variant_id, warehouse_id) DO NOTHING;

-- name: ApplyDelta :one
-- Câu đổi số dư duy nhất: cộng delta vào đúng một ngăn khi điều kiện cố định đúng trên bản mới nhất của dòng
-- (READ COMMITTED đánh giá lại WHERE sau khi chờ khóa). Không dòng nào → thiếu (hoặc chưa có dòng số dư).
-- cond: none | available (on_hand − damaged − held − in_transit ≥ q) | damaged | held | in_transit (ngăn ≥ q).
UPDATE inventory.stock_balances
SET on_hand    = on_hand + CASE WHEN @bucket::text = 'on_hand' THEN @delta::int ELSE 0 END,
    held       = held + CASE WHEN @bucket::text = 'held' THEN @delta::int ELSE 0 END,
    in_transit = in_transit + CASE WHEN @bucket::text = 'in_transit' THEN @delta::int ELSE 0 END,
    damaged    = damaged + CASE WHEN @bucket::text = 'damaged' THEN @delta::int ELSE 0 END,
    version    = version + 1,
    updated_at = now()
WHERE variant_id = @variant_id::uuid AND warehouse_id = @warehouse_id::uuid
  AND CASE @cond::text
        WHEN 'none' THEN true
        WHEN 'available' THEN on_hand - damaged - held - in_transit >= @q::int
        WHEN 'damaged' THEN damaged >= @q::int
        WHEN 'held' THEN held >= @q::int
        WHEN 'in_transit' THEN in_transit >= @q::int
        ELSE false
      END
RETURNING tenant_id, seller_id, version;

-- name: LockBalancesOfReservations :many
-- Khóa trước các dòng số dư của những dòng giữ hàng ids, theo (variant_id, warehouse_id) tăng dần (ADR-46: số dư
-- trước giữ hàng). Truy vấn con không bị khóa (FOR UPDATE chỉ áp bảng trong FROM).
SELECT b.variant_id, b.warehouse_id
FROM inventory.stock_balances AS b
WHERE (b.variant_id, b.warehouse_id) IN (
    SELECT r.variant_id, r.warehouse_id FROM inventory.stock_reservations AS r WHERE r.id = ANY (@ids::uuid[])
)
ORDER BY b.variant_id, b.warehouse_id
FOR UPDATE;

-- name: ListAvailability :many
SELECT variant_id, warehouse_id, on_hand, held, in_transit, damaged, available
FROM inventory.stock_available_v
WHERE warehouse_id = @warehouse_id::uuid AND variant_id = ANY (@variant_ids::uuid[])
ORDER BY variant_id;
