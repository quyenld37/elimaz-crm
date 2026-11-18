-- Khách tối thiểu và đơn tối thiểu — lát mỏng (kế hoạch M1, Task M1-30; spec 05 mục 1, 2; Q-X1, Q26). Tên cột đúng
-- ERD 05; M2 mở rộng bằng ALTER (thêm cột, ràng buộc) và CREATE OR REPLACE VIEW chỉ thêm cột vào cuối, không tạo lại.
-- Pha mở rộng: CREATE, GRANT, và khóa ngoại mới cho inventory.stock_reservations (bảng chưa có dữ liệu thật).
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 5 (customer.customers) và nhóm 7 (ord.orders → ord.order_items →
-- ord.order_status_history). Mã đơn lấy từ sequence ord.order_code_seq, không khóa dòng.

-- +goose Up
CREATE TABLE customer.customers (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    root_id    uuid NOT NULL,
    full_name  text NOT NULL CHECK (length(full_name) BETWEEN 1 AND 120),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version    bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, root_id) REFERENCES customer.customers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('customer.customers');

-- Mã đơn duy nhất một chỗ sinh (order.nextCode): 'DH' || lpad(nextval, 7, '0'). Dùng chung mọi tenant.
CREATE SEQUENCE ord.order_code_seq AS bigint START 1;

CREATE TABLE ord.orders (
    id                   uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id            uuid NOT NULL REFERENCES platform.tenants (id),
    seller_id            uuid NOT NULL,
    code                 text NOT NULL CHECK (code ~ '^DH[0-9]{7,}$'),
    customer_id          uuid NOT NULL,
    channel              text NOT NULL CHECK (channel IN ('facebook', 'web', 'manual')),
    idempotency_key      uuid NOT NULL,
    status               text NOT NULL CHECK (status IN
        ('draft', 'new', 'confirmed', 'cancel_requested', 'cancelled', 'delivered', 'returned')),
    current_shipment_id  uuid NULL,
    ship_address         jsonb NOT NULL CHECK (jsonb_typeof(ship_address) = 'object'),
    subtotal             bigint NOT NULL CHECK (subtotal BETWEEN 0 AND 9007199254740991),
    discount_total       bigint NOT NULL DEFAULT 0 CHECK (discount_total BETWEEN 0 AND 9007199254740991),
    shipping_fee_charged bigint NOT NULL DEFAULT 0 CHECK (shipping_fee_charged BETWEEN 0 AND 9007199254740991),
    cod_amount           bigint NOT NULL CHECK (cod_amount BETWEEN 0 AND 9007199254740991),
    grand_total          bigint NOT NULL CHECK (grand_total BETWEEN 0 AND 9007199254740991),
    created_by           uuid NULL,
    cancel_reason        text NULL CHECK (length(cancel_reason) BETWEEN 1 AND 500),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    version              bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT orders_code_key UNIQUE (tenant_id, code),
    CONSTRAINT orders_idempotency_key_key UNIQUE (tenant_id, idempotency_key),
    CONSTRAINT orders_seller_id_key UNIQUE (tenant_id, seller_id, id),
    CHECK (grand_total = subtotal - discount_total + shipping_fee_charged),
    CHECK (cod_amount BETWEEN 0 AND grand_total),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customer.customers (tenant_id, id),
    FOREIGN KEY (tenant_id, seller_id) REFERENCES catalog.sellers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('ord.orders');
COMMENT ON INDEX ord.orders_code_key IS 'seller-exempt: mã đơn duy nhất trên cả shop (05, Q26)';
COMMENT ON INDEX ord.orders_idempotency_key_key IS 'seller-exempt: khóa nghiệp vụ của lần tạo đơn, duy nhất trên cả shop (05, 04 mục 5.5)';
CREATE INDEX orders_customer ON ord.orders (tenant_id, customer_id);

-- Một đơn một người bán (KH14, ADR-29): dòng đơn trỏ đơn và biến thể bằng khóa ghép (tenant_id, seller_id, …).
CREATE TABLE ord.order_items (
    id                       uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id                uuid NOT NULL REFERENCES platform.tenants (id),
    seller_id                uuid NOT NULL,
    order_id                 uuid NOT NULL,
    position                 int NOT NULL CHECK (position >= 1),
    variant_id               uuid NOT NULL,
    sku_snapshot             text NOT NULL,
    name_snapshot            text NOT NULL,
    quantity                 int NOT NULL CHECK (quantity > 0),
    unit_price               bigint NOT NULL CHECK (unit_price BETWEEN 0 AND 9007199254740991),
    line_discount            bigint NOT NULL DEFAULT 0 CHECK (line_discount BETWEEN 0 AND 9007199254740991),
    allocated_order_discount bigint NOT NULL DEFAULT 0
        CHECK (allocated_order_discount BETWEEN 0 AND 9007199254740991),
    net_amount               bigint NOT NULL CHECK (net_amount BETWEEN 0 AND 9007199254740991),
    created_at               timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT order_items_position_key UNIQUE (tenant_id, seller_id, order_id, position),
    CHECK (net_amount = quantity::bigint * unit_price - line_discount - allocated_order_discount),
    FOREIGN KEY (tenant_id, seller_id, order_id) REFERENCES ord.orders (tenant_id, seller_id, id),
    FOREIGN KEY (tenant_id, seller_id, variant_id) REFERENCES catalog.variants (tenant_id, seller_id, id)
);
SELECT platform.enable_tenant_rls('ord.order_items');
CREATE INDEX order_items_variant ON ord.order_items (tenant_id, variant_id);

-- Lịch sử trạng thái chỉ ghi thêm. from_status NULL ở dòng tạo đơn.
CREATE TABLE ord.order_status_history (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    order_id    uuid NOT NULL,
    from_status text NULL,
    to_status   text NOT NULL,
    actor_type  text NOT NULL CHECK (actor_type IN ('staff', 'carrier', 'system', 'customer')),
    actor_id    uuid NULL,
    reason      text NULL CHECK (reason ~ '^[a-z][a-z0-9_]{0,63}$'),
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, order_id) REFERENCES ord.orders (tenant_id, id)
);
SELECT platform.enable_tenant_rls('ord.order_status_history');
CREATE INDEX order_status_history_order ON ord.order_status_history (tenant_id, order_id, recorded_at);

-- Trigger theo câu lệnh như inventory.stock_ledger (0115): chặn UPDATE, DELETE, TRUNCATE của mọi role.
-- +goose StatementBegin
CREATE FUNCTION ord.order_status_history_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION 'order_status_history is append-only (% bị chặn)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER order_status_history_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON ord.order_status_history
    FOR EACH STATEMENT EXECUTE FUNCTION ord.order_status_history_append_only();

-- Khóa ngoại xuyên module (ADR-27 mục 4, đánh đổi có chủ ý): giữ hàng luôn thuộc một dòng đơn có thật.
ALTER TABLE inventory.stock_reservations
    ADD CONSTRAINT stock_reservations_order_item_fkey
    FOREIGN KEY (tenant_id, order_item_id) REFERENCES ord.order_items (tenant_id, id);

CREATE VIEW customer.customers_v WITH (security_invoker = true) AS
SELECT id, root_id, full_name, created_at
FROM customer.customers;

CREATE VIEW ord.orders_v WITH (security_invoker = true) AS
SELECT id, tenant_id, code, seller_id, customer_id, status, channel, grand_total, cod_amount, current_shipment_id,
       created_at
FROM ord.orders;

-- crm_app: khách chèn, đọc; đơn chèn, đọc, sửa đúng các cột trạng thái; dòng đơn chèn, đọc; lịch sử chèn, đọc.
-- Không DELETE.
REVOKE ALL ON ord.order_status_history FROM PUBLIC;
GRANT SELECT, INSERT ON customer.customers, ord.orders, ord.order_items, ord.order_status_history TO crm_app;
GRANT UPDATE (status, current_shipment_id, cancel_reason, updated_at, version) ON ord.orders TO crm_app;
GRANT USAGE ON SEQUENCE ord.order_code_seq TO crm_app;
GRANT SELECT ON customer.customers_v, ord.orders_v TO crm_app;
