-- Địa chỉ lấy hàng của kho (kế hoạch M3, Task M3-01; A13, A17). Bảng của module inventory (M1, 0115); mốc M3 thêm cột
-- theo luật mở rộng, chủ dữ liệu vẫn là inventory (hàm ghi inventory.SetWarehousePickup ở task sau). pickup_address theo
-- mã địa giới của CRM {name, line, province_code, ward_code}; pickup_phone là SĐT của shop (không phải của khách).
-- Pha mở rộng: thêm cột có DEFAULT, nối cột vào cuối view.

-- +goose Up
ALTER TABLE inventory.warehouses
    ADD COLUMN pickup_address jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(pickup_address) = 'object'),
    ADD COLUMN pickup_phone text NOT NULL DEFAULT '' CHECK (length(pickup_phone) <= 20);

CREATE OR REPLACE VIEW inventory.warehouses_v WITH (security_invoker = true) AS
SELECT id AS warehouse_id, operated_by_seller_id, code, name, status, pickup_address, pickup_phone
FROM inventory.warehouses;

GRANT UPDATE (pickup_address, pickup_phone) ON inventory.warehouses TO crm_app;
