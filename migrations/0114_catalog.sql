-- Catalog lõi (kế hoạch M1 bản 2, Task M1-27; spec 05 mục 2, ADR-29): người bán, mẫu, biến thể màu/size, giá, bộ đếm
-- barcode nội bộ; view công bố sellers_v, products_v, variants_v (cột đúng docs/views.md). Pha mở rộng: chỉ CREATE.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 4 (sellers → products → variants → barcode_counters).
-- Luật (g) ADR-29: mọi unique của bảng có seller_id có seller_id ngay sau tenant_id (unique màu/size cũng vậy: mẫu
-- chỉ thuộc một người bán nên cùng ý nghĩa với (tenant_id, product_id, color, size) của kế hoạch).
-- Tiền là bigint đồng trong [0, 2^53 - 1] (số nguyên lớn nhất JavaScript đọc đúng).

-- +goose Up
CREATE TABLE catalog.sellers (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    code       text NOT NULL CHECK (code <> ''),
    name       text NOT NULL CHECK (name <> ''),
    kind       text NOT NULL CHECK (kind IN ('own', 'partner')),
    status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version    bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT sellers_code_key UNIQUE (tenant_id, code)
);
SELECT platform.enable_tenant_rls('catalog.sellers');

-- description: mô tả ngắn, chữ thường (Q34). article: bài viết dạng khối (catalog.DecodeArticle kiểm lược đồ; không
-- lưu HTML). name_search: textnorm.Fold(name). category_id: khóa ngoại thêm ở Task M1-34 (bảng categories).
CREATE TABLE catalog.products (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    seller_id   uuid NOT NULL,
    code        text NOT NULL CHECK (code <> ''),
    name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    name_search text NOT NULL,
    description text NOT NULL DEFAULT '' CHECK (length(description) <= 1000),
    article     jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(article) = 'array'),
    status      text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'discontinued')),
    category_id uuid NULL,
    deleted_at  timestamptz NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    version     bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT products_code_key UNIQUE (tenant_id, seller_id, code),
    UNIQUE (tenant_id, seller_id, id),
    FOREIGN KEY (tenant_id, seller_id) REFERENCES catalog.sellers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('catalog.products');

CREATE TABLE catalog.variants (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    seller_id  uuid NOT NULL,
    product_id uuid NOT NULL,
    sku        text NOT NULL CHECK (sku ~ '^[A-Z0-9][A-Z0-9._-]{0,63}$'),
    barcode    text NOT NULL CHECK (barcode ~ '^[0-9A-Za-z-]{4,32}$'),
    color      text NOT NULL CHECK (length(color) BETWEEN 1 AND 50),
    color_hex  text NULL CHECK (color_hex ~ '^#[0-9A-F]{6}$'),
    size       text NOT NULL CHECK (length(size) BETWEEN 1 AND 50),
    price      bigint NOT NULL CHECK (price BETWEEN 0 AND 9007199254740991),
    cost_price bigint NULL CHECK (cost_price BETWEEN 0 AND 9007199254740991),
    status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'discontinued')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version    bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT variants_sku_key UNIQUE (tenant_id, seller_id, sku),
    CONSTRAINT variants_barcode_key UNIQUE (tenant_id, seller_id, barcode),
    CONSTRAINT variants_option_key UNIQUE (tenant_id, seller_id, product_id, color, size),
    UNIQUE (tenant_id, seller_id, id),
    FOREIGN KEY (tenant_id, seller_id, product_id) REFERENCES catalog.products (tenant_id, seller_id, id)
);
SELECT platform.enable_tenant_rls('catalog.variants');

-- Bộ đếm barcode EAN-13 nội bộ (tiền tố 2) theo tenant: catalog/internal/barcode.NextEAN13 tăng trong một câu.
CREATE TABLE catalog.barcode_counters (
    tenant_id uuid PRIMARY KEY REFERENCES platform.tenants (id),
    next      bigint NOT NULL CHECK (next >= 1)
);
SELECT platform.enable_tenant_rls('catalog.barcode_counters');

CREATE VIEW catalog.sellers_v WITH (security_invoker = true) AS
SELECT id AS seller_id, code, name, kind, status
FROM catalog.sellers;

CREATE VIEW catalog.products_v WITH (security_invoker = true) AS
SELECT id AS product_id, seller_id, code, name, name_search, description, article, status, category_id, deleted_at,
       updated_at
FROM catalog.products;

-- sellable: bán được khi mẫu và biến thể đều active và mẫu chưa xóa (KH12).
CREATE VIEW catalog.variants_v WITH (security_invoker = true) AS
SELECT v.id AS variant_id, v.seller_id, v.product_id, v.sku, v.barcode, p.code AS product_code, p.name AS product_name,
       v.color, v.color_hex, v.size, v.price, v.cost_price, v.status AS variant_status, p.status AS product_status,
       (p.status = 'active' AND v.status = 'active' AND p.deleted_at IS NULL) AS sellable, v.updated_at
FROM catalog.variants AS v
JOIN catalog.products AS p ON p.tenant_id = v.tenant_id AND p.seller_id = v.seller_id AND p.id = v.product_id;

-- crm_app: đọc mọi bảng; ghi mẫu, biến thể, bộ đếm (không DELETE). Người bán chỉ đọc (seed-dev ghi bằng chủ bảng;
-- Task M1-37 mở ghi khi cần).
GRANT SELECT ON catalog.sellers, catalog.products, catalog.variants, catalog.barcode_counters TO crm_app;
GRANT INSERT ON catalog.products, catalog.variants, catalog.barcode_counters TO crm_app;
GRANT UPDATE (name, name_search, description, article, status, version, updated_at) ON catalog.products TO crm_app;
GRANT UPDATE (sku, barcode, color, color_hex, size, price, cost_price, status, version, updated_at)
    ON catalog.variants TO crm_app;
GRANT UPDATE (next) ON catalog.barcode_counters TO crm_app;
GRANT SELECT ON catalog.sellers_v, catalog.products_v, catalog.variants_v TO crm_app;
