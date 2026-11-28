-- Kiểm kho mở sổ (kế hoạch M7, Task 262 = M7-17; MO02). Pha mở rộng: chỉ thêm cột có mặc định, index, view, GRANT.
-- - inventory.stocktakes.kind: 'regular' (kiểm kho của M1, mặc định — dòng cũ và câu chèn cũ giữ nguyên nghĩa) hay
--   'opening' (phiên mở sổ trên kho trống; inventory/opening.go đặt sau StartStocktake trong cùng transaction).
-- - Mỗi kho mở sổ một lần: index duy nhất một phần bỏ qua phiên đã hủy (hủy rồi mở lại được). Ràng buộc "một phiên
--   open mỗi kho" của M1 (stocktakes_one_open) giữ nguyên.
-- - inventory.opening_status_v (docs/views.md): một dòng mỗi phiên mở sổ. lines_expected = số biến thể active của mẫu
--   draft hoặc active chưa xóa, mọi người bán của tenant (tính lúc đọc; cùng điều kiện với câu CountOpeningUncounted
--   của queries/inventory/opening.sql); lines_counted, units_counted = số dòng đếm và tổng số đếm của phiên.

-- +goose Up
ALTER TABLE inventory.stocktakes
    ADD COLUMN kind text NOT NULL DEFAULT 'regular' CHECK (kind IN ('regular', 'opening'));
GRANT UPDATE (kind) ON inventory.stocktakes TO crm_app;

CREATE UNIQUE INDEX stocktakes_one_opening ON inventory.stocktakes (tenant_id, warehouse_id)
    WHERE kind = 'opening' AND status <> 'cancelled';

CREATE VIEW inventory.opening_status_v WITH (security_invoker = true) AS
SELECT s.warehouse_id, s.id AS stocktake_id, s.status,
       (SELECT count(*)
        FROM catalog.variants_v AS v
        JOIN catalog.products_v AS p ON p.seller_id = v.seller_id AND p.product_id = v.product_id
        WHERE v.variant_status = 'active' AND v.product_status IN ('draft', 'active') AND p.deleted_at IS NULL)::int
           AS lines_expected,
       (SELECT count(*) FROM inventory.stocktake_lines AS l
        WHERE l.tenant_id = s.tenant_id AND l.stocktake_id = s.id)::int AS lines_counted,
       (SELECT coalesce(sum(l.counted), 0) FROM inventory.stocktake_lines AS l
        WHERE l.tenant_id = s.tenant_id AND l.stocktake_id = s.id)::bigint AS units_counted,
       s.applied_at
FROM inventory.stocktakes AS s
WHERE s.kind = 'opening';
GRANT SELECT ON inventory.opening_status_v TO crm_app;
