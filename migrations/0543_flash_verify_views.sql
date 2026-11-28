-- View chỉ đọc cho lệnh kiểm đợt flash sale `crm verify-flash` (kế hoạch M5, Task 219 / M5-36; gói
-- internal/queries/flashverify). Pha mở rộng: chỉ CREATE VIEW, GRANT SELECT; không bảng nào đổi. Số 0543: số trống kế
-- tiếp sau 0542 (Task 88) trên main và mọi nhánh task-* lúc viết.
-- - inventory.flash_sale_units_v: từng đơn vị suất (trạng thái, dòng đơn đang giữ hay đã bán).
-- - promotion.flash_sale_claims_v: số đã lấy của mỗi khách ở mỗi món.
-- - ord.order_flash_lines_v: dòng đơn flash sale (dòng có flash_sale_item_id), số lượng, đơn của dòng.
-- security_invoker: RLS của bảng gốc áp cho người đọc (crm_app trong tenancy.WithTenant).

-- +goose Up
CREATE VIEW inventory.flash_sale_units_v WITH (security_invoker = true) AS
SELECT id, tenant_id, flash_sale_item_id, status, order_item_id, updated_at
FROM inventory.flash_sale_units;

CREATE VIEW promotion.flash_sale_claims_v WITH (security_invoker = true) AS
SELECT tenant_id, flash_sale_item_id, customer_id, quantity, updated_at
FROM promotion.flash_sale_claims;

CREATE VIEW ord.order_flash_lines_v WITH (security_invoker = true) AS
SELECT id, tenant_id, order_id, flash_sale_item_id, quantity
FROM ord.order_items
WHERE flash_sale_item_id IS NOT NULL;

GRANT SELECT ON inventory.flash_sale_units_v, promotion.flash_sale_claims_v, ord.order_flash_lines_v TO crm_app;
