-- Banner trang chủ (kế hoạch M5, Task 453 / M5-41): chuyển từ website (commerce.sqlite home_banners(id=1, revision,
-- payload)) về module mới content (Nội dung website), từ đây CRM là nguồn sự thật. Pha mở rộng: chỉ CREATE, GRANT,
-- INSERT dòng mục đích tệp.
-- - home_banner_sets: một dòng mỗi tenant giữ revision của cả danh sách (như home_banners(id=1, revision) của
--   website); lưu là thay cả danh sách khi revision khớp (khóa dòng này trước). Chưa có dòng = danh sách rỗng,
--   revision 0.
-- - home_banners: tối đa 8 banner, position 0–7 theo thứ tự hiện. Luật theo loại ở CHECK (chốt cuối; Go kiểm trước và
--   trả mã lỗi riêng). Ảnh là mã tệp của platform.files mục đích web_banner (không khóa ngoại: tệp có thể bị xóa mềm,
--   banner khi đó ẩn với lý do image-missing).
-- - Thứ tự khóa: ADR-46 nhóm 12d content (home_banner_sets → home_banners → look_sets → looks → look_items).
-- - Mục đích tệp web_banner (R5): ảnh công khai, bucket originals như ảnh sản phẩm; Go đăng ký cùng khóa bằng
--   content.RegisterFilePurposes() lúc khởi động (bộ kích thước riêng files.PresetBannerV1, rộng ≥ 600 px).

-- +goose Up
CREATE SCHEMA content;
GRANT USAGE ON SCHEMA content TO crm_app;

CREATE TABLE content.home_banner_sets (
    tenant_id  uuid PRIMARY KEY REFERENCES platform.tenants (id),
    revision   int NOT NULL DEFAULT 0 CHECK (revision >= 0),
    updated_by uuid,
    updated_at timestamptz
);
SELECT platform.enable_tenant_rls('content.home_banner_sets');

CREATE TABLE content.home_banners (
    id            uuid NOT NULL,
    tenant_id     uuid NOT NULL REFERENCES platform.tenants (id),
    position      int NOT NULL CHECK (position BETWEEN 0 AND 7),
    enabled       boolean NOT NULL,
    kind          text NOT NULL CHECK (kind IN ('campaign', 'trending', 'new', 'category', 'image')),
    campaign_id   uuid,
    category_id   uuid,
    kicker        text NOT NULL DEFAULT '' CHECK (char_length(kicker) <= 40),
    title         text NOT NULL DEFAULT '' CHECK (char_length(title) <= 60),
    cta           text NOT NULL DEFAULT '' CHECK (char_length(cta) <= 30),
    theme         text NOT NULL DEFAULT 'auto' CHECK (theme IN ('auto', 'red', 'black', 'light')),
    starts_at     timestamptz,
    ends_at       timestamptz,
    product_ids   uuid[] NOT NULL DEFAULT '{}' CHECK (cardinality(product_ids) <= 3),
    image_desktop uuid,
    image_mobile  uuid,
    alt           text NOT NULL DEFAULT '' CHECK (char_length(alt) <= 120),
    link_kind     text CHECK (link_kind IN ('campaign', 'category', 'product', 'offers')),
    link_id       uuid,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, position),
    FOREIGN KEY (tenant_id) REFERENCES content.home_banner_sets (tenant_id),
    CONSTRAINT home_banners_window CHECK (starts_at IS NULL OR ends_at IS NULL OR ends_at > starts_at),
    CONSTRAINT home_banners_campaign CHECK (kind <> 'campaign' OR campaign_id IS NOT NULL),
    CONSTRAINT home_banners_category CHECK (kind <> 'category' OR category_id IS NOT NULL),
    CONSTRAINT home_banners_image CHECK (
        kind <> 'image'
        OR (image_desktop IS NOT NULL AND char_length(alt) >= 1 AND link_kind IS NOT NULL
            AND (link_kind = 'offers' OR link_id IS NOT NULL))
    )
);
SELECT platform.enable_tenant_rls('content.home_banners');

GRANT SELECT, INSERT ON content.home_banner_sets TO crm_app;
GRANT UPDATE (revision, updated_by, updated_at) ON content.home_banner_sets TO crm_app;
GRANT SELECT, INSERT, DELETE ON content.home_banners TO crm_app;

INSERT INTO platform.file_purposes (key, bucket)
VALUES ('web_banner', 'originals')
ON CONFLICT DO NOTHING;
