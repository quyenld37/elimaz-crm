-- Chuyển tối đa 50 sản phẩm mới và tồn dự kiến từ Nhanh (kế hoạch M7, Task 541 = M7-41; ADR-48). Pha mở rộng: chỉ
-- CREATE TABLE, index, RLS, GRANT; không chạm bảng cũ.
-- - catalog.legacy_refs: mã cũ (Nhanh) của một thực thể CRM, để chạy lại lệnh nhập không tạo trùng (ảnh, biến thể,
--   mẫu). kind: product, variant, image; legacy_id là chữ (mã số Nhanh, hay địa chỉ ảnh đã băm); entity_id là mã
--   thực thể của CRM (mẫu, biến thể hay tệp ảnh). Không có khóa ngoại (một cột trỏ tới ba bảng); chỉ thêm.
-- - inventory.opening_expected: tồn Nhanh lúc chụp, làm SỐ DỰ KIẾN của phiên kiểm kho mở sổ (Task 262): CRM đếm thật,
--   cột này chỉ để thấy chỗ lệch. Không phải tồn: không có sổ, không số dư. Ghi lại được (chụp lại thì thay số).
-- - Thứ tự khóa (ADR-46): hai bảng lá, chỉ ghi ở cuối transaction của lệnh nhập; không bảng nào khóa bảng khác.

-- +goose Up
CREATE TABLE catalog.legacy_refs (
    id         uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid        NOT NULL REFERENCES platform.tenants (id),
    source     text        NOT NULL CHECK (source IN ('nhanh')),
    kind       text        NOT NULL CHECK (kind IN ('product', 'variant', 'image')),
    legacy_id  text        NOT NULL CHECK (length(legacy_id) BETWEEN 1 AND 128),
    entity_id  uuid        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, source, kind, legacy_id)
);
CREATE INDEX legacy_refs_entity ON catalog.legacy_refs (tenant_id, kind, entity_id);
SELECT platform.enable_tenant_rls('catalog.legacy_refs');
GRANT SELECT, INSERT ON catalog.legacy_refs TO crm_app;

CREATE TABLE inventory.opening_expected (
    tenant_id    uuid        NOT NULL REFERENCES platform.tenants (id),
    warehouse_id uuid        NOT NULL,
    variant_id   uuid        NOT NULL,
    seller_id    uuid        NOT NULL,
    expected     int         NOT NULL CHECK (expected BETWEEN 0 AND 1000000),
    source       text        NOT NULL CHECK (source IN ('nhanh')),
    taken_at     timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, warehouse_id, variant_id),
    FOREIGN KEY (tenant_id, warehouse_id) REFERENCES inventory.warehouses (tenant_id, id),
    FOREIGN KEY (tenant_id, seller_id, variant_id) REFERENCES catalog.variants (tenant_id, seller_id, id)
);
SELECT platform.enable_tenant_rls('inventory.opening_expected');
GRANT SELECT, INSERT ON inventory.opening_expected TO crm_app;
GRANT UPDATE (expected, source, taken_at, updated_at) ON inventory.opening_expected TO crm_app;
