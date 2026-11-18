-- Danh mục đơn vị hành chính hai cấp (kế hoạch M2, Task M2-04, Q15): tỉnh/thành và xã/phường hiện hành (từ 01/07/2025).
-- Dữ liệu tham chiếu chung cho mọi shop: không tenant_id, không RLS (tenancy.CheckSchemaRules chỉ xét bảng có
-- tenant_id). crm_app chỉ đọc; chỉ migration ghi (dữ liệu sinh bằng tools/adminunits ở migration riêng). ADR-46: bảng
-- này không bao giờ bị khóa.

-- +goose Up

CREATE TABLE customer.admin_units (
    code           text PRIMARY KEY,
    level          text NOT NULL CHECK (level IN ('province', 'ward')),
    parent_code    text REFERENCES customer.admin_units (code),
    name           text NOT NULL,
    name_search    text NOT NULL,
    effective_from date NOT NULL,
    source         text NOT NULL,
    -- Tỉnh không có cha, xã luôn có cha (cha là tỉnh: tools/adminunits kiểm trước khi sinh dữ liệu).
    CONSTRAINT admin_units_parent_check CHECK ((level = 'province') = (parent_code IS NULL))
);

CREATE INDEX admin_units_parent_idx ON customer.admin_units (parent_code);
CREATE INDEX admin_units_name_search_idx ON customer.admin_units USING gin (name_search gin_trgm_ops);

REVOKE ALL ON customer.admin_units FROM PUBLIC;
GRANT SELECT ON customer.admin_units TO crm_app;
