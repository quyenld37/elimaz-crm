-- Kho tệp dùng chung của mọi mốc (kế hoạch M1, Task M1-41; spec 05 mục 7.3, ADR-36; Q15, Q38, R10). Pha mở rộng:
-- ba bảng mới, quyền, index. Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 12b (platform.files → platform.file_variants);
-- platform.file_purposes là danh mục tĩnh không RLS, chỉ migration ghi.
--
-- Mốc sau thêm mục đích của mình bằng MỘT dòng trong migration của mốc đó (cùng một lời gọi files.RegisterPurpose):
--   INSERT INTO platform.file_purposes (key, bucket[, mode]) VALUES ('<khóa>', '<bucket>'[, 'private']) ON CONFLICT DO NOTHING;
-- không sửa CHECK, không sửa bảng files.

-- +goose Up
CREATE TABLE platform.file_purposes (
    key    text PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9_]{2,40}$'),
    bucket text NOT NULL CHECK (bucket IN ('tmp', 'public', 'originals', 'private')),
    -- private (R10): ảnh riêng tư của khách, chỉ lệnh file.process_private xử lý, biến thể chỉ ở crm-private.
    mode   text NOT NULL DEFAULT 'standard' CHECK (mode IN ('standard', 'private')),
    CHECK (mode = 'standard' OR bucket = 'private')
);
GRANT SELECT ON platform.file_purposes TO crm_app;

INSERT INTO platform.file_purposes (key, bucket) VALUES ('product_image', 'originals') ON CONFLICT DO NOTHING;
INSERT INTO platform.file_purposes (key, bucket) VALUES ('catalog_import', 'private') ON CONFLICT DO NOTHING;

-- Một tệp. bucket, key là chỗ của đối tượng trên kho lúc này (tệp vừa tải lên: tmp, tmp/<tenant>/<id>); khóa do hệ
-- thống sinh, không bao giờ có tên tệp của người dùng (original_name chỉ để hiện và đặt tên khi tải về).
-- awaiting_upload (Q15): đã phát đường dẫn tải lên, chưa tải xong. expires_at: tệp tạm do máy chủ ghi (tệp xuất).
CREATE TABLE platform.files (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      uuid NOT NULL REFERENCES platform.tenants (id),
    purpose        text NOT NULL REFERENCES platform.file_purposes (key),
    bucket         text NOT NULL CHECK (bucket IN ('tmp', 'public', 'originals', 'private')),
    key            text NOT NULL CHECK (key <> ''),
    original_name  text NOT NULL CHECK (length(original_name) <= 255),
    declared_type  text NOT NULL,
    detected_type  text NULL,
    size_bytes     bigint NOT NULL CHECK (size_bytes > 0),
    content_sha256 bytea NULL CHECK (content_sha256 IS NULL OR length(content_sha256) = 32),
    width          int NULL CHECK (width > 0),
    height         int NULL CHECK (height > 0),
    status         text NOT NULL
        CHECK (status IN ('awaiting_upload', 'uploaded', 'processing', 'ready', 'rejected', 'lost')),
    reject_reason  text NULL,
    preset_version int NULL,
    created_by     uuid NULL,
    expires_at     timestamptz NULL,
    lost_at        timestamptz NULL,
    deleted_at     timestamptz NULL,
    deleted_by     uuid NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    version        bigint NOT NULL DEFAULT 1,
    CHECK ((status = 'rejected') = (reject_reason IS NOT NULL)),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, bucket, key)
);
SELECT platform.enable_tenant_rls('platform.files');
CREATE INDEX files_content_sha256 ON platform.files (tenant_id, content_sha256);

-- Một biến thể ảnh (Task M1-43 ghi). bucket public (ảnh sản phẩm, khóa img/<sha>/…) hoặc private (R10).
CREATE TABLE platform.file_variants (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      uuid NOT NULL,
    file_id        uuid NOT NULL,
    preset_version int NOT NULL,
    width          int NOT NULL CHECK (width > 0),
    format         text NOT NULL CHECK (format IN ('avif', 'webp', 'jpeg')),
    bucket         text NOT NULL CHECK (bucket IN ('public', 'private')),
    key            text NOT NULL CHECK (key <> ''),
    size_bytes     bigint NOT NULL CHECK (size_bytes > 0),
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, file_id, preset_version, width, format),
    FOREIGN KEY (tenant_id, file_id) REFERENCES platform.files (tenant_id, id)
);
SELECT platform.enable_tenant_rls('platform.file_variants');

-- crm_app: tạo tệp, đổi trạng thái (không đổi tenant, purpose, tên gốc, loại khai, kích thước khai); không xóa dòng
-- (xóa là đặt deleted_at). Biến thể: chỉ thêm (Task M1-43).
GRANT SELECT, INSERT ON platform.files TO crm_app;
GRANT UPDATE (bucket, key, detected_type, content_sha256, width, height, status, reject_reason, preset_version,
    lost_at, deleted_at, deleted_by, updated_at, version) ON platform.files TO crm_app;
GRANT SELECT, INSERT ON platform.file_variants TO crm_app;
