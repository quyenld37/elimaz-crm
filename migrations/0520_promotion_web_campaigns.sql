-- Chiến dịch giảm giá web (kế hoạch M5, Task 452 / M5-40; QĐ32, QĐ33): chuyển từ website (commerce.sqlite
-- discount_campaigns) về promotion, từ đây CRM là nguồn sự thật. Pha mở rộng: chỉ CREATE, GRANT, VIEW.
-- - id sinh ngẫu nhiên (uuid v4, không uuidv7): liên kết /uu-dai/<tên>-<link_key> lấy 8 hex đầu của id như website,
--   mà 8 hex đầu của uuidv7 là giờ tạo (hai chiến dịch tạo trong cùng ~65 s trùng link_key). link_key duy nhất trên cả
--   shop (đường dẫn chung của website); bên ghi sinh lại id khi trùng.
-- - revision tăng 1 mỗi lần lưu, đổi trạng thái, xóa (Seq của sự kiện promotion.web_campaign.changed); preview_* là
--   bản xem trước nhân viên đã xem cho đúng revision (bật cần khớp).
-- - Đích (web_campaign_targets) chỉ chèn và xóa theo chiến dịch đã khóa dòng. Thứ tự khóa: ADR-46 nhóm 10
--   (… → coupon_redemptions → web_campaigns → web_campaign_targets → …).

-- +goose Up
CREATE TABLE promotion.web_campaigns (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL REFERENCES platform.tenants (id),
    seller_id        uuid NOT NULL,
    name             text NOT NULL CHECK (length(name) BETWEEN 1 AND 120 AND name = btrim(name)),
    percent          int NOT NULL CHECK (percent BETWEEN 1 AND 99),
    scope            text NOT NULL CHECK (scope IN ('all', 'products', 'categories')),
    allow_coupon     boolean NOT NULL DEFAULT false,
    clearance        boolean NOT NULL DEFAULT false,
    starts_at        timestamptz NOT NULL,
    ends_at          timestamptz NOT NULL,
    state            text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'enabled', 'paused')),
    link_key         text NOT NULL GENERATED ALWAYS AS (left(replace(id::text, '-', ''), 8)) STORED,
    deleted_at       timestamptz,
    revision         int NOT NULL DEFAULT 1 CHECK (revision >= 1),
    preview_revision int,
    preview_models   int CHECK (preview_models >= 0),
    preview_variants int CHECK (preview_variants >= 0),
    created_by       uuid NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_by       uuid,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT web_campaigns_range_check CHECK (ends_at > starts_at),
    CONSTRAINT web_campaigns_link_key UNIQUE (tenant_id, link_key),
    FOREIGN KEY (tenant_id, seller_id) REFERENCES catalog.sellers (tenant_id, id)
);
SELECT platform.enable_tenant_rls('promotion.web_campaigns');
COMMENT ON INDEX promotion.web_campaigns_link_key IS
    'seller-exempt: liên kết /uu-dai/<tên>-<link_key> của website là đường dẫn chung của cả shop';
CREATE INDEX web_campaigns_list ON promotion.web_campaigns (tenant_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE promotion.web_campaign_targets (
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    campaign_id uuid NOT NULL,
    target_kind text NOT NULL CHECK (target_kind IN ('product', 'category')),
    target_id   uuid NOT NULL,
    PRIMARY KEY (tenant_id, campaign_id, target_kind, target_id),
    FOREIGN KEY (tenant_id, campaign_id) REFERENCES promotion.web_campaigns (tenant_id, id)
);
SELECT platform.enable_tenant_rls('promotion.web_campaign_targets');

-- View công bố (docs/views.md): chiến dịch chưa xóa kèm trạng thái hiển thị tính theo giờ của database (đúng
-- campaignStatus của admin: draft, paused theo state; còn lại scheduled / active / ended, ends_at đúng giờ là ended);
-- đích của chiến dịch chưa xóa. Module content (banner) và internal/queries đọc ở đây.
CREATE VIEW promotion.web_campaigns_v WITH (security_invoker = true) AS
SELECT id, tenant_id, seller_id, name, percent, scope, allow_coupon, clearance, starts_at, ends_at, state,
       CASE
           WHEN state IN ('draft', 'paused') THEN state
           WHEN now() < starts_at THEN 'scheduled'
           WHEN now() >= ends_at THEN 'ended'
           ELSE 'active'
       END AS status,
       link_key, revision, updated_at
FROM promotion.web_campaigns
WHERE deleted_at IS NULL;

CREATE VIEW promotion.web_campaign_targets_v WITH (security_invoker = true) AS
SELECT t.tenant_id, t.campaign_id, t.target_kind, t.target_id
FROM promotion.web_campaign_targets AS t
JOIN promotion.web_campaigns AS c ON c.tenant_id = t.tenant_id AND c.id = t.campaign_id
WHERE c.deleted_at IS NULL;

GRANT SELECT, INSERT ON promotion.web_campaigns TO crm_app;
GRANT UPDATE (name, percent, scope, allow_coupon, clearance, starts_at, ends_at, state, deleted_at, revision,
              preview_revision, preview_models, preview_variants, updated_by, updated_at)
    ON promotion.web_campaigns TO crm_app;
GRANT SELECT, INSERT, DELETE ON promotion.web_campaign_targets TO crm_app;
GRANT SELECT ON promotion.web_campaigns_v, promotion.web_campaign_targets_v TO crm_app;

