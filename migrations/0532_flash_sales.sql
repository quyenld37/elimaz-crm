-- Bảng flash sale (kế hoạch M5, Task 199 / M5-16; ADR-40; spec 05 ERD flash sale). Kế hoạch cấp số 0507; main đã tới
-- 0531 nên lấy số trống kế tiếp (goose không áp số nhỏ hơn số đã áp). Pha mở rộng: chỉ CREATE, GRANT, VIEW, thêm khóa
-- ngoại NOT VALID rồi VALIDATE trên cột luôn NULL tới nay (ord.order_items.flash_sale_item_id của 0503), thêm index.
-- - promotion.flash_sales: một đợt; task này chỉ soạn ở 'draft' (lên lịch, mở, đóng: Task 200). version tăng mỗi lần sửa.
-- - promotion.flash_sale_items: món của đợt (một biến thể một lần mỗi đợt), giá flash nhỏ hơn giá thường (QĐ24, kiểm ở
--   promotion), số suất, giới hạn mỗi khách, phút giữ đơn nháp. seller_id suy ra từ biến thể: khóa ngoại ghép
--   (tenant_id, seller_id, variant_id) → catalog.variants bảo đảm đúng người bán (ADR-29); unique có seller_id ngay sau
--   tenant_id (luật g; variant_id đã xác định seller_id nên tương đương UNIQUE (tenant_id, flash_sale_id, variant_id)).
-- - promotion.flash_sale_claims: số khách đã lấy của một món (giới hạn mỗi khách nằm trong database, ADR-40).
-- - inventory.flash_sale_units: mỗi đơn vị suất một dòng, lấy bằng SKIP LOCKED (ADR-40); held/sold luôn có dòng đơn.
--   Không unique trên order_item_id (một dòng đơn số lượng 2 giữ hai đơn vị).
-- Thứ tự khóa: ADR-46 nhóm 10 (… → flash_sales → flash_sale_items → flash_sale_claims), nhóm 11 (… →
-- stock_reservations → flash_sale_units → …). Khóa ngoại liên module: docs/views.md mục Khóa ngoại xuyên module.

-- +goose Up
CREATE TABLE promotion.flash_sales (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id     uuid NOT NULL REFERENCES platform.tenants (id),
    name          text NOT NULL CHECK (length(name) BETWEEN 1 AND 120 AND name = btrim(name)),
    starts_at     timestamptz NOT NULL,
    ends_at       timestamptz NOT NULL,
    status        text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'scheduled', 'open', 'closed')),
    closed_reason text CHECK (closed_reason IS NULL OR length(closed_reason) BETWEEN 1 AND 300),
    created_by    uuid NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    version       int NOT NULL DEFAULT 1 CHECK (version >= 1),
    UNIQUE (tenant_id, id),
    CONSTRAINT flash_sales_window_check CHECK (ends_at > starts_at),
    CONSTRAINT flash_sales_length_check CHECK (ends_at - starts_at <= interval '7 days')
);
SELECT platform.enable_tenant_rls('promotion.flash_sales');
CREATE INDEX flash_sales_status ON promotion.flash_sales (tenant_id, status, starts_at);
CREATE INDEX flash_sales_list ON promotion.flash_sales (tenant_id, id DESC);

CREATE TABLE promotion.flash_sale_items (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    flash_sale_id      uuid NOT NULL,
    seller_id          uuid NOT NULL,
    variant_id         uuid NOT NULL,
    warehouse_id       uuid NOT NULL,
    sale_price         bigint NOT NULL CHECK (sale_price > 0),
    quantity           int NOT NULL CHECK (quantity BETWEEN 1 AND 20000),
    per_customer_limit int NOT NULL DEFAULT 1 CHECK (per_customer_limit BETWEEN 1 AND 10),
    hold_minutes       int NOT NULL DEFAULT 10 CHECK (hold_minutes BETWEEN 5 AND 30),
    version            int NOT NULL DEFAULT 1 CHECK (version >= 1),
    UNIQUE (tenant_id, id),
    CONSTRAINT flash_sale_items_variant_key UNIQUE (tenant_id, seller_id, flash_sale_id, variant_id),
    FOREIGN KEY (tenant_id, flash_sale_id) REFERENCES promotion.flash_sales (tenant_id, id),
    FOREIGN KEY (tenant_id, seller_id, variant_id) REFERENCES catalog.variants (tenant_id, seller_id, id),
    FOREIGN KEY (tenant_id, warehouse_id) REFERENCES inventory.warehouses (tenant_id, id)
);
SELECT platform.enable_tenant_rls('promotion.flash_sale_items');
CREATE INDEX flash_sale_items_sale ON promotion.flash_sale_items (tenant_id, flash_sale_id, id);

CREATE TABLE promotion.flash_sale_claims (
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    flash_sale_item_id uuid NOT NULL,
    customer_id        uuid NOT NULL,
    quantity           int NOT NULL CHECK (quantity >= 0),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, flash_sale_item_id, customer_id),
    FOREIGN KEY (tenant_id, flash_sale_item_id) REFERENCES promotion.flash_sale_items (tenant_id, id),
    FOREIGN KEY (tenant_id, customer_id) REFERENCES customer.customers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('promotion.flash_sale_claims');
CREATE INDEX flash_sale_claims_customer ON promotion.flash_sale_claims (tenant_id, customer_id);

CREATE TABLE inventory.flash_sale_units (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    flash_sale_item_id uuid NOT NULL,
    status             text NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'held', 'sold', 'released')),
    order_item_id      uuid,
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT flash_sale_units_order_item_check CHECK ((status IN ('held', 'sold')) = (order_item_id IS NOT NULL)),
    FOREIGN KEY (tenant_id, flash_sale_item_id) REFERENCES promotion.flash_sale_items (tenant_id, id),
    FOREIGN KEY (tenant_id, order_item_id) REFERENCES ord.order_items (tenant_id, id)
);
SELECT platform.enable_tenant_rls('inventory.flash_sale_units');
CREATE INDEX flash_sale_units_pick ON inventory.flash_sale_units (tenant_id, flash_sale_item_id, status, id);
CREATE INDEX flash_sale_units_order_item ON inventory.flash_sale_units (tenant_id, order_item_id)
    WHERE order_item_id IS NOT NULL;

ALTER TABLE ord.order_items
    ADD CONSTRAINT order_items_flash_sale_item_fk FOREIGN KEY (tenant_id, flash_sale_item_id)
        REFERENCES promotion.flash_sale_items (tenant_id, id) NOT VALID;
ALTER TABLE ord.order_items VALIDATE CONSTRAINT order_items_flash_sale_item_fk;
-- Xóa món nháp kiểm khóa ngoại phía order_items: index để không quét cả bảng dòng đơn.
CREATE INDEX order_items_flash_sale_item ON ord.order_items (tenant_id, flash_sale_item_id)
    WHERE flash_sale_item_id IS NOT NULL;

-- View công bố (docs/views.md): đợt, món (cho màn và website ở task sau), số đơn vị theo trạng thái của từng món.
CREATE VIEW promotion.flash_sales_v WITH (security_invoker = true) AS
SELECT id, tenant_id, name, starts_at, ends_at, status, closed_reason, version, updated_at
FROM promotion.flash_sales;

CREATE VIEW promotion.flash_sale_items_v WITH (security_invoker = true) AS
SELECT id, tenant_id, flash_sale_id, seller_id, variant_id, warehouse_id, sale_price, quantity, per_customer_limit,
       hold_minutes, version
FROM promotion.flash_sale_items;

CREATE VIEW inventory.flash_units_summary_v WITH (security_invoker = true) AS
SELECT tenant_id, flash_sale_item_id,
       count(*) FILTER (WHERE status = 'available') AS available,
       count(*) FILTER (WHERE status = 'held') AS held,
       count(*) FILTER (WHERE status = 'sold') AS sold,
       count(*) FILTER (WHERE status = 'released') AS released
FROM inventory.flash_sale_units
GROUP BY tenant_id, flash_sale_item_id;

GRANT SELECT, INSERT ON promotion.flash_sales TO crm_app;
GRANT UPDATE (name, starts_at, ends_at, status, closed_reason, updated_at, version) ON promotion.flash_sales TO crm_app;
GRANT SELECT, INSERT, DELETE ON promotion.flash_sale_items TO crm_app;
GRANT UPDATE (sale_price, quantity, per_customer_limit, hold_minutes, version) ON promotion.flash_sale_items TO crm_app;
GRANT SELECT, INSERT ON promotion.flash_sale_claims TO crm_app;
GRANT UPDATE (quantity, updated_at) ON promotion.flash_sale_claims TO crm_app;
GRANT SELECT, INSERT ON inventory.flash_sale_units TO crm_app;
GRANT UPDATE (status, order_item_id, updated_at) ON inventory.flash_sale_units TO crm_app;
GRANT SELECT ON promotion.flash_sales_v, promotion.flash_sale_items_v, inventory.flash_units_summary_v TO crm_app;
