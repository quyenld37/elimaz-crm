-- Schema của phép đo đường mua flash sale (kế hoạch M0 bản 5, Task 1). Mã đo, không phải migration:
-- mỗi lần `setup` hoặc chạy test thì xóa và tạo lại toàn bộ schema `spike`. Chạy bằng crm_owner.
-- Quy ước như bảng nghiệp vụ thật: tenant_id NOT NULL, UNIQUE (tenant_id, id), khóa ngoại ghép,
-- ENABLE + FORCE ROW LEVEL SECURITY, policy NULLIF.

DROP SCHEMA IF EXISTS spike CASCADE;
CREATE SCHEMA spike;

CREATE TABLE spike.orders (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL,
    customer_id uuid NOT NULL,
    status      text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'cancelled', 'confirmed')),
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id)
);

CREATE TABLE spike.order_items (
    id        uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id uuid NOT NULL,
    order_id  uuid NOT NULL,
    item_id   uuid NOT NULL,
    qty       int  NOT NULL CHECK (qty > 0),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, order_id) REFERENCES spike.orders (tenant_id, id)
);
CREATE INDEX ON spike.order_items (tenant_id, order_id);

CREATE TABLE spike.stock_reservations (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id     uuid NOT NULL,
    order_item_id uuid NOT NULL,
    qty           int  NOT NULL CHECK (qty > 0),
    status        text NOT NULL DEFAULT 'held' CHECK (status IN ('held', 'released', 'consumed')),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, order_item_id) REFERENCES spike.order_items (tenant_id, id)
);
CREATE INDEX ON spike.stock_reservations (tenant_id, order_item_id);

-- Mỗi suất một dòng. Một dòng đơn qty = q gắn đúng q đơn vị, nên order_item_id không unique
-- (kế hoạch ghi UNIQUE; mâu thuẫn với test qty 2 và spec 04 mục 15, xem task-01 "Quyết định thay chủ shop" 4).
CREATE TABLE spike.flash_sale_units (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id     uuid NOT NULL,
    item_id       uuid NOT NULL,
    status        text NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'held', 'sold', 'released')),
    order_item_id uuid NULL,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, order_item_id) REFERENCES spike.order_items (tenant_id, id),
    CHECK ((status IN ('held', 'sold')) = (order_item_id IS NOT NULL))
);
CREATE INDEX ON spike.flash_sale_units (tenant_id, item_id, status, id);
CREATE INDEX ON spike.flash_sale_units (tenant_id, order_item_id);

CREATE TABLE spike.flash_sale_claims (
    tenant_id   uuid NOT NULL,
    item_id     uuid NOT NULL,
    customer_id uuid NOT NULL,
    quantity    int  NOT NULL CHECK (quantity >= 0),
    PRIMARY KEY (tenant_id, item_id, customer_id)
);

CREATE TABLE spike.idempotency_keys (
    tenant_id       uuid  NOT NULL,
    key             text  NOT NULL,
    request_hash    bytea NOT NULL,
    status          text  NOT NULL CHECK (status IN ('running', 'done')),
    locked_until    timestamptz NOT NULL,
    response_status int,
    response_body   jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, key),
    CHECK ((status = 'done') = (response_status IS NOT NULL AND response_body IS NOT NULL))
);

DO $$
DECLARE t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['orders', 'order_items', 'stock_reservations', 'flash_sale_units',
                             'flash_sale_claims', 'idempotency_keys'] LOOP
        EXECUTE format('ALTER TABLE spike.%I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE spike.%I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format($p$CREATE POLICY tenant_isolation ON spike.%I
            USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
            WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)$p$, t);
    END LOOP;
END $$;

-- Nạp `units` suất `available` cho một món. Gọi trong transaction đã đặt app.tenant_id = tenant.
CREATE FUNCTION spike.seed(tenant uuid, item uuid, units int) RETURNS void
LANGUAGE sql AS $$
    INSERT INTO spike.flash_sale_units (tenant_id, item_id, status)
    SELECT tenant, item, 'available' FROM generate_series(1, units);
$$;

GRANT USAGE ON SCHEMA spike TO crm_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA spike TO crm_app;
REVOKE EXECUTE ON FUNCTION spike.seed(uuid, uuid, int) FROM PUBLIC;
