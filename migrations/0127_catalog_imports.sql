-- Bản nhập sản phẩm bằng Excel (kế hoạch M1, Task M1-47; KH01; spec 05 mục 2). Pha mở rộng: một bảng, index, quyền.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 4 (catalog.imports đứng đầu nhóm, trước mẫu và biến thể).
--
-- Một dòng = một lần nhập một tệp: validating (lệnh catalog.import.validate đang kiểm) → invalid (errors) | ready
-- (plan, summary) | duplicate (tệp cùng nội dung đã được áp: duplicate_of) | failed (không đọc được tệp); ready →
-- applying → applied | failed ở Task M1-47b. file_sha256 chỉ biết khi lệnh đã đọc tệp nên được rỗng ở validating,
-- failed. plan chỉ có dữ liệu sản phẩm (không dữ liệu cá nhân). Khóa ngoại tới platform.files là ngoại lệ xuyên module
-- của ADR-27 mục 4 như product_images (tệp không bị xóa dòng).

-- +goose Up
CREATE TABLE catalog.imports (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    file_id      uuid NOT NULL,
    file_sha256  bytea NULL CHECK (length(file_sha256) = 32),
    status       text NOT NULL
        CHECK (status IN ('validating', 'invalid', 'ready', 'applying', 'applied', 'duplicate', 'failed')),
    seller_id    uuid NOT NULL,
    summary      jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(summary) = 'object'),
    errors       jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(errors) = 'array'),
    plan         jsonb NULL CHECK (jsonb_typeof(plan) = 'object'),
    duplicate_of uuid NULL,
    created_by   uuid NOT NULL,
    applied_by   uuid NULL,
    applied_at   timestamptz NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    version      bigint NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    CHECK (file_sha256 IS NOT NULL OR status IN ('validating', 'failed')),
    CHECK ((status = 'duplicate') = (duplicate_of IS NOT NULL)),
    CHECK (plan IS NULL OR status IN ('ready', 'applying', 'applied', 'failed')),
    CHECK ((status = 'applied') = (applied_at IS NOT NULL AND applied_by IS NOT NULL)),
    CONSTRAINT imports_file_fkey FOREIGN KEY (tenant_id, file_id) REFERENCES platform.files (tenant_id, id),
    CONSTRAINT imports_seller_fkey FOREIGN KEY (tenant_id, seller_id) REFERENCES catalog.sellers (tenant_id, id),
    CONSTRAINT imports_duplicate_fkey FOREIGN KEY (tenant_id, duplicate_of) REFERENCES catalog.imports (tenant_id, id)
);
SELECT platform.enable_tenant_rls('catalog.imports');
-- Một tệp (theo nội dung) chỉ được áp một lần cho một người bán (KH01; luật (g) ADR-29: seller_id ngay sau
-- tenant_id); cũng là index tìm "đã áp chưa".
CREATE UNIQUE INDEX imports_applied_sha_key ON catalog.imports (tenant_id, seller_id, file_sha256)
    WHERE status = 'applied';

GRANT SELECT, INSERT ON catalog.imports TO crm_app;
GRANT UPDATE (file_sha256, status, summary, errors, plan, duplicate_of, applied_by, applied_at, updated_at, version)
    ON catalog.imports TO crm_app;
