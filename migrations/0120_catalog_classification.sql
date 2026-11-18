-- Phân loại sản phẩm (kế hoạch M1 bản 2, Task M1-34; Q27; spec 05 dòng "Danh mục, thuộc tính": không có seller_id,
-- dùng chung cả cửa hàng): danh mục dạng cây tối đa 3 cấp, thuộc tính và giá trị, nhãn kệ hàng; bảng nối với mẫu;
-- view công bố đúng cột docs/views.md. Pha mở rộng: chỉ CREATE, thêm khóa ngoại cho cột products.category_id đã có
-- (mọi dòng hiện có là NULL).
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 4 (categories → attributes → attribute_values → tags → products →
-- product_attribute_values → product_tags).

-- +goose Up
-- depth: 1 là gốc; catalog.UpdateCategory giữ depth = depth của cha + 1 cho cả cây con.
CREATE TABLE catalog.categories (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    parent_id  uuid NULL,
    name       text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    slug       text NOT NULL CHECK (slug ~ '^[a-z0-9-]{1,80}$'),
    depth      smallint NOT NULL CHECK (depth BETWEEN 1 AND 3),
    position   int NOT NULL DEFAULT 0,
    status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    version    bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CONSTRAINT categories_slug_key UNIQUE (tenant_id, slug),
    CONSTRAINT categories_name_key UNIQUE NULLS NOT DISTINCT (tenant_id, parent_id, name),
    CONSTRAINT categories_parent_fkey FOREIGN KEY (tenant_id, parent_id) REFERENCES catalog.categories (tenant_id, id),
    CHECK (parent_id IS DISTINCT FROM id)
);
SELECT platform.enable_tenant_rls('catalog.categories');

CREATE TABLE catalog.attributes (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    code       text NOT NULL CHECK (code ~ '^[a-z0-9][a-z0-9_-]{0,39}$'),
    name       text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    position   int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT attributes_code_key UNIQUE (tenant_id, code)
);
SELECT platform.enable_tenant_rls('catalog.attributes');

CREATE TABLE catalog.attribute_values (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    attribute_id uuid NOT NULL,
    value        text NOT NULL CHECK (length(value) BETWEEN 1 AND 100),
    position     int NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT attribute_values_value_key UNIQUE (tenant_id, attribute_id, value),
    CONSTRAINT attribute_values_attribute_fkey FOREIGN KEY (tenant_id, attribute_id)
        REFERENCES catalog.attributes (tenant_id, id)
);
SELECT platform.enable_tenant_rls('catalog.attribute_values');

CREATE TABLE catalog.tags (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    code       text NOT NULL CHECK (code ~ '^[a-z0-9][a-z0-9_-]{0,39}$'),
    name       text NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT tags_code_key UNIQUE (tenant_id, code)
);
SELECT platform.enable_tenant_rls('catalog.tags');

CREATE TABLE catalog.product_attribute_values (
    tenant_id          uuid NOT NULL REFERENCES platform.tenants (id),
    product_id         uuid NOT NULL,
    attribute_value_id uuid NOT NULL,
    PRIMARY KEY (tenant_id, product_id, attribute_value_id),
    CONSTRAINT product_attribute_values_product_fkey FOREIGN KEY (tenant_id, product_id)
        REFERENCES catalog.products (tenant_id, id),
    CONSTRAINT product_attribute_values_value_fkey FOREIGN KEY (tenant_id, attribute_value_id)
        REFERENCES catalog.attribute_values (tenant_id, id)
);
SELECT platform.enable_tenant_rls('catalog.product_attribute_values');
-- Website (M5) lọc mẫu theo giá trị thuộc tính.
CREATE INDEX product_attribute_values_value_idx ON catalog.product_attribute_values (tenant_id, attribute_value_id);

CREATE TABLE catalog.product_tags (
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    product_id uuid NOT NULL,
    tag_id     uuid NOT NULL,
    PRIMARY KEY (tenant_id, product_id, tag_id),
    CONSTRAINT product_tags_product_fkey FOREIGN KEY (tenant_id, product_id) REFERENCES catalog.products (tenant_id, id),
    CONSTRAINT product_tags_tag_fkey FOREIGN KEY (tenant_id, tag_id) REFERENCES catalog.tags (tenant_id, id)
);
SELECT platform.enable_tenant_rls('catalog.product_tags');
-- Kệ hàng trên website (M5) đọc mẫu theo nhãn.
CREATE INDEX product_tags_tag_idx ON catalog.product_tags (tenant_id, tag_id);

ALTER TABLE catalog.products
    ADD CONSTRAINT products_category_fkey FOREIGN KEY (tenant_id, category_id)
        REFERENCES catalog.categories (tenant_id, id);
-- Lưu trữ danh mục đếm mẫu active trong danh mục.
CREATE INDEX products_category_idx ON catalog.products (tenant_id, category_id) WHERE category_id IS NOT NULL;

CREATE VIEW catalog.categories_v WITH (security_invoker = true) AS
SELECT id AS category_id, parent_id, name, slug, depth, position, status
FROM catalog.categories;

CREATE VIEW catalog.attributes_v WITH (security_invoker = true) AS
SELECT id AS attribute_id, code, name, position
FROM catalog.attributes;

CREATE VIEW catalog.attribute_values_v WITH (security_invoker = true) AS
SELECT id AS attribute_value_id, attribute_id, value, position
FROM catalog.attribute_values;

CREATE VIEW catalog.tags_v WITH (security_invoker = true) AS
SELECT id AS tag_id, code, name
FROM catalog.tags;

CREATE VIEW catalog.product_tags_v WITH (security_invoker = true) AS
SELECT product_id, tag_id
FROM catalog.product_tags;

CREATE VIEW catalog.product_attribute_values_v WITH (security_invoker = true) AS
SELECT product_id, attribute_value_id
FROM catalog.product_attribute_values;

-- crm_app: đọc mọi bảng; tạo danh mục, thuộc tính, giá trị, nhãn (không DELETE); sửa danh mục; thay bảng nối (DELETE
-- rồi INSERT, SetClassification); gán danh mục cho mẫu.
GRANT SELECT, INSERT ON catalog.categories, catalog.attributes, catalog.attribute_values, catalog.tags TO crm_app;
GRANT UPDATE (parent_id, name, slug, depth, position, status, version, updated_at) ON catalog.categories TO crm_app;
GRANT SELECT, INSERT, DELETE ON catalog.product_attribute_values, catalog.product_tags TO crm_app;
GRANT UPDATE (category_id) ON catalog.products TO crm_app;
GRANT SELECT ON catalog.categories_v, catalog.attributes_v, catalog.attribute_values_v, catalog.tags_v,
    catalog.product_tags_v, catalog.product_attribute_values_v TO crm_app;
