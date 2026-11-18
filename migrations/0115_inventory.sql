-- Inventory lõi (kế hoạch M1, Task M1-28; spec 05 mục 2 "Giữ hàng và sổ tồn", 04 mục 5.5, ADR-46): kho, số dư theo
-- (biến thể, kho), sổ tồn chỉ ghi thêm, giữ hàng theo dòng đơn. View công bố warehouses_v, stock_available_v,
-- stock_ledger_v (cột đúng docs/views.md). Pha mở rộng: chỉ CREATE, GRANT.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 11 (warehouses → … → stock_balances → stock_reservations);
-- stock_ledger ở mục (d): chỉ chèn. Số dư chỉ đổi bằng một câu UPDATE có điều kiện (inventory/movement.go).
-- Có thể bán = on_hand − damaged − held − in_transit; chỉ view stock_available_v và điều kiện "available" của câu
-- ApplyDelta (queries/inventory/balances.sql) viết công thức này. Không có CHECK available ≥ 0: kiểm kho (Task M1-35)
-- được phép làm âm.

-- +goose Up
CREATE TABLE inventory.warehouses (
    id                    uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id             uuid NOT NULL REFERENCES platform.tenants (id),
    operated_by_seller_id uuid NOT NULL,
    code                  text NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9_-]{0,31}$'),
    name                  text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    status                text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'closed')),
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    version               bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT warehouses_code_key UNIQUE (tenant_id, code),
    FOREIGN KEY (tenant_id, operated_by_seller_id) REFERENCES catalog.sellers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('inventory.warehouses');

-- seller_id suy ra từ biến thể (05: "suy ra từ biến thể"); khóa ngoại ghép bảo đảm đúng người bán của biến thể.
CREATE TABLE inventory.stock_balances (
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    variant_id   uuid NOT NULL,
    warehouse_id uuid NOT NULL,
    seller_id    uuid NOT NULL,
    on_hand      int NOT NULL DEFAULT 0 CHECK (on_hand >= 0),
    held         int NOT NULL DEFAULT 0 CHECK (held >= 0),
    in_transit   int NOT NULL DEFAULT 0 CHECK (in_transit >= 0),
    damaged      int NOT NULL DEFAULT 0 CHECK (damaged >= 0),
    version      bigint NOT NULL DEFAULT 1,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, variant_id, warehouse_id),
    FOREIGN KEY (tenant_id, seller_id, variant_id) REFERENCES catalog.variants (tenant_id, seller_id, id),
    FOREIGN KEY (tenant_id, warehouse_id) REFERENCES inventory.warehouses (tenant_id, id)
);
SELECT platform.enable_tenant_rls('inventory.stock_balances');
CREATE INDEX stock_balances_warehouse ON inventory.stock_balances (tenant_id, warehouse_id, variant_id);
CREATE INDEX stock_balances_seller ON inventory.stock_balances (tenant_id, seller_id, variant_id);

-- Sổ tồn chỉ ghi thêm: mỗi dòng là một thay đổi của một ngăn. Một bước của một chứng từ không áp hai lần (unique).
-- Khóa ngoại tới số dư (Q-T28-3): dòng sổ luôn thuộc một dòng số dư có thật.
CREATE TABLE inventory.stock_ledger (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    variant_id   uuid NOT NULL,
    warehouse_id uuid NOT NULL,
    bucket       text NOT NULL CHECK (bucket IN ('on_hand', 'held', 'in_transit', 'damaged')),
    delta        int NOT NULL CHECK (delta <> 0),
    doc_type     text NOT NULL CHECK (doc_type IN ('import', 'export', 'order', 'return', 'stocktake', 'adjust', 'damage')),
    doc_id       uuid NOT NULL,
    step         text NOT NULL CHECK (step ~ '^[a-z][a-z0-9_]{0,31}$'),
    actor_type   text NOT NULL CHECK (actor_type IN ('staff', 'system', 'carrier', 'customer')),
    actor_id     uuid NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT stock_ledger_step_key UNIQUE (tenant_id, doc_type, doc_id, variant_id, warehouse_id, bucket, step),
    FOREIGN KEY (tenant_id, variant_id, warehouse_id)
        REFERENCES inventory.stock_balances (tenant_id, variant_id, warehouse_id)
);
SELECT platform.enable_tenant_rls('inventory.stock_ledger');
CREATE INDEX stock_ledger_balance ON inventory.stock_ledger (tenant_id, variant_id, warehouse_id, created_at);

-- Trigger theo câu lệnh như identity.audit_logs (0004): chặn UPDATE, DELETE, TRUNCATE của mọi role, kể cả chủ bảng và
-- cả khi RLS lọc hết dòng.
-- +goose StatementBegin
CREATE FUNCTION inventory.stock_ledger_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION 'stock_ledger is append-only (% bị chặn)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER stock_ledger_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON inventory.stock_ledger
    FOR EACH STATEMENT EXECUTE FUNCTION inventory.stock_ledger_append_only();

-- Giữ hàng của từng dòng đơn. Khóa ngoại tới ord.order_items thêm ở Task M1-30. release_reason (Q-T28-2) chỉ có khi
-- released. Trạng thái chỉ đổi bằng UPDATE … WHERE state = <cũ> (B2).
CREATE TABLE inventory.stock_reservations (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        uuid NOT NULL REFERENCES platform.tenants (id),
    order_item_id    uuid NOT NULL,
    variant_id       uuid NOT NULL,
    warehouse_id     uuid NOT NULL,
    quantity         int NOT NULL CHECK (quantity > 0),
    state            text NOT NULL DEFAULT 'held' CHECK (state IN ('held', 'in_transit', 'consumed', 'released')),
    release_reason   text NULL CHECK (release_reason ~ '^[a-z][a-z0-9_]{0,63}$'),
    expires_at       timestamptz NULL,
    state_changed_at timestamptz NOT NULL DEFAULT now(),
    created_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT stock_reservations_item_key UNIQUE (tenant_id, order_item_id, variant_id, warehouse_id),
    CHECK (release_reason IS NULL OR state = 'released'),
    FOREIGN KEY (tenant_id, variant_id, warehouse_id)
        REFERENCES inventory.stock_balances (tenant_id, variant_id, warehouse_id)
);
SELECT platform.enable_tenant_rls('inventory.stock_reservations');
CREATE INDEX stock_reservations_balance_state
    ON inventory.stock_reservations (tenant_id, variant_id, warehouse_id, state);

CREATE VIEW inventory.warehouses_v WITH (security_invoker = true) AS
SELECT id AS warehouse_id, operated_by_seller_id, code, name, status
FROM inventory.warehouses;

CREATE VIEW inventory.stock_available_v WITH (security_invoker = true) AS
SELECT variant_id, warehouse_id, seller_id, on_hand, held, in_transit, damaged,
       on_hand - damaged - held - in_transit AS available, updated_at
FROM inventory.stock_balances;

CREATE VIEW inventory.stock_ledger_v WITH (security_invoker = true) AS
SELECT id AS ledger_id, variant_id, warehouse_id, bucket, delta, doc_type, doc_id, step, actor_type, actor_id, created_at
FROM inventory.stock_ledger;

-- crm_app: kho chỉ đọc (seed-dev tạo bằng chủ bảng; màn hình quản lý kho ở mốc sau); số dư chèn và sửa đúng các cột
-- số; sổ chỉ đọc và chèn; giữ hàng chèn và sửa trạng thái. Không DELETE.
REVOKE ALL ON inventory.stock_ledger FROM PUBLIC;
GRANT SELECT ON inventory.warehouses, inventory.stock_balances, inventory.stock_ledger, inventory.stock_reservations
    TO crm_app;
GRANT INSERT ON inventory.stock_balances, inventory.stock_ledger, inventory.stock_reservations TO crm_app;
GRANT UPDATE (on_hand, held, in_transit, damaged, version, updated_at) ON inventory.stock_balances TO crm_app;
GRANT UPDATE (state, release_reason, state_changed_at) ON inventory.stock_reservations TO crm_app;
GRANT SELECT ON inventory.warehouses_v, inventory.stock_available_v, inventory.stock_ledger_v TO crm_app;
