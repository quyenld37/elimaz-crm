-- Ảnh sản phẩm (kế hoạch M1, Task M1-44; spec 05 mục 2, 7.3; KH02, TP06, TP12) và view biến thể ảnh của kho tệp
-- (A2-12). Pha mở rộng: một bảng, index, hai view, quyền. Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 4
-- (catalog.products → catalog.product_images); platform.files chỉ đọc, không khóa.
--
-- Một dòng = một tệp ảnh gắn vào một mẫu. Xóa là đặt deleted_at (khôi phục được trong 30 ngày, TP06); gắn lại tệp đã
-- xóa hồi lại chính dòng đó (UNIQUE (tenant_id, product_id, file_id)). color: một màu của biến thể trong mẫu (NULL: ảnh
-- chung). position: thứ tự hiện, đọc theo (position, id). Khóa ngoại tới platform.files là ngoại lệ xuyên module của
-- ADR-27 mục 4 (tệp không bị xóa dòng; SQL của catalog không đọc bảng của kho tệp, chỉ gọi files.Status).

-- +goose Up
CREATE TABLE catalog.product_images (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    product_id uuid NOT NULL,
    file_id    uuid NOT NULL,
    color      text NULL CHECK (length(color) BETWEEN 1 AND 50),
    position   int NOT NULL CHECK (position > 0),
    created_by uuid NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz NULL,
    deleted_by uuid NULL,
    UNIQUE (tenant_id, id),
    CONSTRAINT product_images_file_key UNIQUE (tenant_id, product_id, file_id),
    CONSTRAINT product_images_product_fkey FOREIGN KEY (tenant_id, product_id) REFERENCES catalog.products (tenant_id, id),
    CONSTRAINT product_images_file_fkey FOREIGN KEY (tenant_id, file_id) REFERENCES platform.files (tenant_id, id)
);
SELECT platform.enable_tenant_rls('catalog.product_images');
-- Ảnh đang hiện của một mẫu theo thứ tự (getProduct, ảnh bìa của danh sách).
CREATE INDEX product_images_product_idx ON catalog.product_images (tenant_id, product_id, position, id)
    WHERE deleted_at IS NULL;
-- Tệp đang được mẫu nào dùng (dọn tệp Q16, mốc sau).
CREATE INDEX product_images_file_idx ON catalog.product_images (tenant_id, file_id);

CREATE VIEW catalog.product_images_v WITH (security_invoker = true) AS
    SELECT id AS image_id, product_id, file_id, color, position
    FROM catalog.product_images
    WHERE deleted_at IS NULL;

-- Biến thể ảnh công khai cho gói queries của mốc sau (M5 cần chiều rộng, định dạng). R10: ảnh riêng tư không bao giờ có
-- ở đây (lọc mode standard và bucket public). Đường dẫn = FILES_PUBLIC_BASE_URL + '/' + key.
CREATE VIEW platform.file_variants_v WITH (security_invoker = true) AS
    SELECT v.file_id, v.preset_version, v.width, v.format, v.key
    FROM platform.file_variants v
    JOIN platform.files f ON f.tenant_id = v.tenant_id AND f.id = v.file_id
    JOIN platform.file_purposes p ON p.key = f.purpose
    WHERE f.status = 'ready' AND f.deleted_at IS NULL AND p.mode = 'standard' AND v.bucket = 'public';

GRANT SELECT, INSERT ON catalog.product_images TO crm_app;
GRANT UPDATE (color, position, deleted_at, deleted_by) ON catalog.product_images TO crm_app;
GRANT SELECT ON catalog.product_images_v, platform.file_variants_v TO crm_app;
