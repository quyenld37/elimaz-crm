-- Kiểm kho (kế hoạch M1, Task M1-35; spec 05 mục 2 "Kiểm kho", KH05, KH07). Pha mở rộng: chỉ CREATE, GRANT.
-- Phiên kiểm theo kho (một phiên mở mỗi kho); mỗi dòng chụp expected_on_shelf = on_hand − in_transit lúc ghi số đếm,
-- delta = counted − expected_on_shelf. Áp dụng (inventory/stocktake.go) cộng delta vào on_hand qua câu ApplyDelta có
-- sẵn; không đụng held, in_transit, giữ hàng.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 11 (… → stocktakes → stocktake_lines → stock_balances → …).

-- +goose Up
-- Mã phiên KK-<số> lấy từ sequence (không khóa dòng), chung mọi tenant.
CREATE SEQUENCE inventory.stocktake_code_seq;

CREATE TABLE inventory.stocktakes (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    warehouse_id uuid NOT NULL,
    code         text NOT NULL CHECK (code ~ '^KK-[0-9]{6,}$'),
    status       text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'applied', 'cancelled')),
    started_by   uuid NULL,
    applied_by   uuid NULL,
    applied_at   timestamptz NULL,
    cancelled_by uuid NULL,
    cancelled_at timestamptz NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    version      bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT stocktakes_code_key UNIQUE (tenant_id, code),
    CHECK ((status = 'applied') = (applied_at IS NOT NULL)),
    CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL)),
    FOREIGN KEY (tenant_id, warehouse_id) REFERENCES inventory.warehouses (tenant_id, id)
);
SELECT platform.enable_tenant_rls('inventory.stocktakes');
-- Một phiên mở mỗi kho (hai yêu cầu mở cùng lúc → một bên vi phạm unique → stocktake_open).
CREATE UNIQUE INDEX stocktakes_one_open ON inventory.stocktakes (tenant_id, warehouse_id) WHERE status = 'open';
CREATE INDEX stocktakes_warehouse ON inventory.stocktakes (tenant_id, warehouse_id, id);

-- Dòng đếm: một dòng mỗi biến thể. seller_id suy ra từ biến thể (khóa ngoại ghép như stock_balances).
CREATE TABLE inventory.stocktake_lines (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id         uuid NOT NULL REFERENCES platform.tenants (id),
    stocktake_id      uuid NOT NULL,
    variant_id        uuid NOT NULL,
    seller_id         uuid NOT NULL,
    expected_on_shelf int NOT NULL CHECK (expected_on_shelf >= 0),
    counted           int NOT NULL CHECK (counted BETWEEN 0 AND 1000000),
    delta             int GENERATED ALWAYS AS (counted - expected_on_shelf) STORED,
    counted_at        timestamptz NOT NULL,
    counted_by        uuid NULL,
    version           bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT stocktake_lines_variant_key UNIQUE (tenant_id, seller_id, stocktake_id, variant_id),
    FOREIGN KEY (tenant_id, stocktake_id) REFERENCES inventory.stocktakes (tenant_id, id),
    FOREIGN KEY (tenant_id, seller_id, variant_id) REFERENCES catalog.variants (tenant_id, seller_id, id)
);
SELECT platform.enable_tenant_rls('inventory.stocktake_lines');
-- Tra dòng theo phiên (ghi số đếm, xem trước, áp dụng theo variant_id tăng dần).
CREATE INDEX stocktake_lines_stocktake ON inventory.stocktake_lines (tenant_id, stocktake_id, variant_id);

-- Lớp thứ hai sau kiểm trong Go: dòng của phiên không còn mở không chèn, sửa được (mọi role). Hàm chạy bằng quyền
-- người gọi (RLS áp dụng); FOR SHARE giữ phiên không đổi trạng thái tới hết transaction.
-- +goose StatementBegin
CREATE FUNCTION inventory.stocktake_lines_open_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
DECLARE
    st_status text;
BEGIN
    SELECT s.status INTO st_status
    FROM inventory.stocktakes AS s
    WHERE s.tenant_id = NEW.tenant_id AND s.id = NEW.stocktake_id
    FOR SHARE;
    IF st_status IS DISTINCT FROM 'open' THEN
        RAISE EXCEPTION 'stocktake_lines: phiên không còn mở (% bị chặn)', TG_OP
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER stocktake_lines_open_only
    BEFORE INSERT OR UPDATE ON inventory.stocktake_lines
    FOR EACH ROW EXECUTE FUNCTION inventory.stocktake_lines_open_only();

-- crm_app: phiên đọc, chèn, sửa trạng thái; dòng đọc, chèn, sửa số đếm. Không DELETE.
GRANT SELECT, INSERT ON inventory.stocktakes, inventory.stocktake_lines TO crm_app;
GRANT UPDATE (status, applied_by, applied_at, cancelled_by, cancelled_at, updated_at, version)
    ON inventory.stocktakes TO crm_app;
GRANT UPDATE (expected_on_shelf, counted, counted_at, counted_by, version) ON inventory.stocktake_lines TO crm_app;
GRANT USAGE ON SEQUENCE inventory.stocktake_code_seq TO crm_app;
