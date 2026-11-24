-- Bộ phối "Hôm nay mặc gì?" (kế hoạch M5, Task 454 / M5-42): chuyển từ website (commerce.sqlite home_looks(id=1,
-- revision, payload)) về module content, từ đây CRM là nguồn sự thật. Pha mở rộng: chỉ CREATE, GRANT.
-- - look_sets: một dòng mỗi tenant giữ revision của cả danh sách (như home_looks(id=1, revision) của website); lưu là
--   thay cả danh sách khi revision khớp (khóa dòng này trước). Chưa có dòng = danh sách rỗng, revision 0.
-- - looks: tối đa 500 bộ, position 0–499 theo thứ tự; on_home = hiện trên trang chủ (tối đa 6, Go kiểm sau khi khóa
--   look_sets vì là luật của cả danh sách); bộ không lên trang chủ vẫn mở bằng link /bo-phoi/<slug>-<8 hex đầu của id>.
--   Ảnh người mẫu là mã tệp của một ảnh sản phẩm (catalog.product_images_v) của các món (không khóa ngoại: ảnh có thể
--   bị xóa sau, bộ khi đó ẩn với lý do photo-missing).
-- - look_items: 1–3 món khác nhau mỗi bộ (Go kiểm số món; khóa chính chặn trùng), chấm trên ảnh x, y theo % (0–100).
-- - Thứ tự khóa: ADR-46 nhóm 12d content (home_banner_sets → home_banners → look_sets → looks → look_items).

-- +goose Up
CREATE TABLE content.look_sets (
    tenant_id  uuid PRIMARY KEY REFERENCES platform.tenants (id),
    revision   int NOT NULL DEFAULT 0 CHECK (revision >= 0),
    updated_by uuid,
    updated_at timestamptz
);
SELECT platform.enable_tenant_rls('content.look_sets');

CREATE TABLE content.looks (
    id              uuid NOT NULL,
    tenant_id       uuid NOT NULL REFERENCES platform.tenants (id),
    position        int NOT NULL CHECK (position BETWEEN 0 AND 499),
    on_home         boolean NOT NULL DEFAULT false,
    name            text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 28),
    title           text NOT NULL DEFAULT '' CHECK (char_length(title) <= 60),
    headline        text NOT NULL DEFAULT '' CHECK (char_length(headline) <= 90),
    story           text NOT NULL DEFAULT '' CHECK (char_length(story) <= 220),
    photo_file_id   uuid NOT NULL,
    category_ids    uuid[] NOT NULL DEFAULT '{}' CHECK (cardinality(category_ids) <= 6),
    model_height_cm int CHECK (model_height_cm BETWEEN 130 AND 200),
    model_size      text NOT NULL DEFAULT '' CHECK (char_length(model_size) <= 6),
    starts_at       timestamptz,
    ends_at         timestamptz,
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, position),
    FOREIGN KEY (tenant_id) REFERENCES content.look_sets (tenant_id),
    CONSTRAINT looks_window CHECK (starts_at IS NULL OR ends_at IS NULL OR ends_at > starts_at)
);
SELECT platform.enable_tenant_rls('content.looks');

CREATE TABLE content.look_items (
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    look_id    uuid NOT NULL,
    product_id uuid NOT NULL,
    color      text NOT NULL DEFAULT '' CHECK (char_length(color) <= 60),
    x_pct      numeric(5, 2) NOT NULL CHECK (x_pct BETWEEN 0 AND 100),
    y_pct      numeric(5, 2) NOT NULL CHECK (y_pct BETWEEN 0 AND 100),
    position   int NOT NULL CHECK (position BETWEEN 0 AND 2),
    PRIMARY KEY (tenant_id, look_id, product_id),
    UNIQUE (tenant_id, look_id, position),
    FOREIGN KEY (tenant_id, look_id) REFERENCES content.looks (tenant_id, id) ON DELETE CASCADE
);
SELECT platform.enable_tenant_rls('content.look_items');

GRANT SELECT, INSERT ON content.look_sets TO crm_app;
GRANT UPDATE (revision, updated_by, updated_at) ON content.look_sets TO crm_app;
GRANT SELECT, INSERT, DELETE ON content.looks TO crm_app;
GRANT SELECT, INSERT, DELETE ON content.look_items TO crm_app;
