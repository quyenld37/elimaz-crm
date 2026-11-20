-- Số lượng tốt / hỏng / thiếu trên dòng phiếu trả (kế hoạch M3, Task M3-01; Q-34, Q-T91-3, R11). Bảng của module order
-- (M2, 0204); mốc M3 thêm cột theo luật mở rộng, chủ dữ liệu vẫn là order. Ba cột cộng lại là 0 (chưa nhận) hoặc đúng
-- quantity; condition thêm 'mixed' (một dòng có cả hàng tốt và hỏng/thiếu).
-- Pha mở rộng: thêm cột có DEFAULT, nới CHECK, điền dòng có sẵn, nối cột vào cuối view.

-- +goose Up
ALTER TABLE ord.return_items
    ADD COLUMN sellable_qty int NOT NULL DEFAULT 0 CHECK (sellable_qty >= 0),
    ADD COLUMN damaged_qty int NOT NULL DEFAULT 0 CHECK (damaged_qty >= 0),
    ADD COLUMN missing_qty int NOT NULL DEFAULT 0 CHECK (missing_qty >= 0),
    DROP CONSTRAINT return_items_condition_check,
    ADD CONSTRAINT return_items_condition_check CHECK (condition IN ('sellable', 'damaged', 'missing', 'mixed'));

-- Dòng của phiếu đã nhận (received, restocked, closed) điền theo condition, từng tenant trong ngữ cảnh của tenant đó
-- (RLS ép cả chủ bảng). Phiếu chưa nhận hay đã hủy giữ 0/0/0.
-- +goose StatementBegin
DO $backfill$
DECLARE
    t uuid;
BEGIN
    FOR t IN SELECT id FROM platform.tenants LOOP
        PERFORM set_config('app.tenant_id', t::text, true);
        UPDATE ord.return_items i
        SET sellable_qty = CASE WHEN i.condition = 'sellable' THEN i.quantity ELSE 0 END,
            damaged_qty = CASE WHEN i.condition = 'damaged' THEN i.quantity ELSE 0 END,
            missing_qty = CASE WHEN i.condition = 'missing' THEN i.quantity ELSE 0 END
        FROM ord.returns r
        WHERE r.tenant_id = i.tenant_id AND r.id = i.return_id AND r.status IN ('received', 'restocked', 'closed');
    END LOOP;
    PERFORM set_config('app.tenant_id', '', true);
END
$backfill$;
-- +goose StatementEnd

ALTER TABLE ord.return_items
    ADD CONSTRAINT return_items_qty_split CHECK (sellable_qty + damaged_qty + missing_qty IN (0, quantity));

CREATE OR REPLACE VIEW ord.return_items_v WITH (security_invoker = true) AS
SELECT id, tenant_id, return_id, order_item_id, quantity, condition, refund_amount, sellable_qty, damaged_qty,
       missing_qty
FROM ord.return_items;

GRANT UPDATE (sellable_qty, damaged_qty, missing_qty) ON ord.return_items TO crm_app;
