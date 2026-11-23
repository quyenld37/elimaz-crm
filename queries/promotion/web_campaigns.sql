-- Chiến dịch giảm giá web (kế hoạch M5, Task 452). Mọi câu chạy trong tx tenant của bên gọi (RLS lọc theo tenant).
-- Đọc catalog chỉ qua view products_v, variants_v, categories_v. Ghi chiến dịch, đích chỉ sau khi khóa dòng chiến
-- dịch (ADR-46 nhóm 10: web_campaigns → web_campaign_targets).

-- name: InsertWebCampaign :one
-- id do bên gọi sinh (uuid v4); trùng id hay link_key → 0 dòng (không hỏng tx), bên gọi sinh lại.
INSERT INTO promotion.web_campaigns (id, tenant_id, seller_id, name, percent, scope, allow_coupon, clearance, starts_at,
                                     ends_at, created_by)
VALUES (@id, NULLIF(current_setting('app.tenant_id', true), '')::uuid, @seller_id, @name, @percent, @scope,
        @allow_coupon, @clearance, @starts_at, @ends_at, @created_by)
ON CONFLICT DO NOTHING
RETURNING id, seller_id, name, percent, scope, allow_coupon, clearance, starts_at, ends_at, state, link_key, revision,
          preview_revision, preview_models, preview_variants, created_by, created_at, updated_by, updated_at;

-- name: WebCampaignByID :one
SELECT id, seller_id, name, percent, scope, allow_coupon, clearance, starts_at, ends_at, state, link_key, revision,
       preview_revision, preview_models, preview_variants, created_by, created_at, updated_by, updated_at
FROM promotion.web_campaigns
WHERE id = @id AND seller_id = @seller_id AND deleted_at IS NULL;

-- name: WebCampaignByIDForUpdate :one
-- Mọi lệnh ghi: khóa dòng chiến dịch trước đích của nó (ADR-46 nhóm 10).
SELECT id, seller_id, name, percent, scope, allow_coupon, clearance, starts_at, ends_at, state, link_key, revision,
       preview_revision, preview_models, preview_variants, created_by, created_at, updated_by, updated_at
FROM promotion.web_campaigns
WHERE id = @id AND seller_id = @seller_id AND deleted_at IS NULL
FOR UPDATE;

-- name: UpdateWebCampaign :one
-- Lưu nội dung (bên gọi đã khóa dòng, so revision): revision + 1; bản xem trước cũ tự hết hiệu lực (preview_revision
-- khác revision mới).
UPDATE promotion.web_campaigns
SET name = @name, percent = @percent, scope = @scope, allow_coupon = @allow_coupon, clearance = @clearance,
    starts_at = @starts_at, ends_at = @ends_at, revision = revision + 1, updated_by = @updated_by, updated_at = now()
WHERE id = @id AND revision = @revision AND deleted_at IS NULL
RETURNING id, seller_id, name, percent, scope, allow_coupon, clearance, starts_at, ends_at, state, link_key, revision,
          preview_revision, preview_models, preview_variants, created_by, created_at, updated_by, updated_at;

-- name: SetWebCampaignPreview :exec
-- Ghi bản xem trước cho đúng revision hiện tại (không tăng revision, không phát sự kiện).
UPDATE promotion.web_campaigns
SET preview_revision = revision, preview_models = @preview_models, preview_variants = @preview_variants
WHERE id = @id AND revision = @revision AND deleted_at IS NULL;

-- name: SetWebCampaignState :one
UPDATE promotion.web_campaigns
SET state = @state, revision = revision + 1, updated_by = @updated_by, updated_at = now()
WHERE id = @id AND revision = @revision AND deleted_at IS NULL
RETURNING id, seller_id, name, percent, scope, allow_coupon, clearance, starts_at, ends_at, state, link_key, revision,
          preview_revision, preview_models, preview_variants, created_by, created_at, updated_by, updated_at;

-- name: SoftDeleteWebCampaign :one
UPDATE promotion.web_campaigns
SET deleted_at = now(), revision = revision + 1, updated_by = @updated_by, updated_at = now()
WHERE id = @id AND revision = @revision AND deleted_at IS NULL
RETURNING revision;

-- name: DeleteWebCampaignTargets :exec
DELETE FROM promotion.web_campaign_targets
WHERE campaign_id = @campaign_id;

-- name: InsertWebCampaignTargets :exec
INSERT INTO promotion.web_campaign_targets (tenant_id, campaign_id, target_kind, target_id)
SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid, @campaign_id::uuid, @target_kind::text, t
FROM unnest(sqlc.arg(target_ids)::uuid[]) AS t;

-- name: WebCampaignTargets :many
SELECT campaign_id, target_id
FROM promotion.web_campaign_targets
WHERE campaign_id = ANY(sqlc.arg(campaign_ids)::uuid[])
ORDER BY campaign_id, target_id;

-- name: ListWebCampaigns :many
-- Mới nhất trước (created_at, id giảm dần), con trỏ là id của dòng cuối trang trước. status tính như
-- promotion.CampaignStatus với mốc @now của bên gọi; q tìm trong tên (không phân biệt hoa thường).
SELECT id, seller_id, name, percent, scope, allow_coupon, clearance, starts_at, ends_at, state, link_key, revision,
       preview_revision, preview_models, preview_variants, created_by, created_at, updated_by, updated_at
FROM promotion.web_campaigns AS c
WHERE c.seller_id = @seller_id AND c.deleted_at IS NULL
  AND (sqlc.narg(q)::text IS NULL OR c.name ILIKE '%' || sqlc.narg(q)::text || '%')
  AND (sqlc.narg(status)::text IS NULL OR sqlc.narg(status)::text = CASE
          WHEN c.state IN ('draft', 'paused') THEN c.state
          WHEN @now::timestamptz < c.starts_at THEN 'scheduled'
          WHEN @now::timestamptz >= c.ends_at THEN 'ended'
          ELSE 'active' END)
  AND (sqlc.narg(after)::uuid IS NULL OR (c.created_at, c.id) < (
          SELECT a.created_at, a.id FROM promotion.web_campaigns AS a WHERE a.id = sqlc.narg(after)::uuid))
ORDER BY c.created_at DESC, c.id DESC
LIMIT @row_limit;

-- name: LiveWebCampaigns :many
-- Chiến dịch đã bật, chưa xóa, chưa hết giờ ở @now (đang chạy hoặc đã hẹn giờ); seller_id rỗng = mọi người bán.
SELECT id, seller_id, name, percent, scope, allow_coupon, clearance, starts_at, ends_at, state, link_key, revision,
       preview_revision, preview_models, preview_variants, created_by, created_at, updated_by, updated_at
FROM promotion.web_campaigns
WHERE state = 'enabled' AND deleted_at IS NULL AND ends_at > @now::timestamptz
  AND (sqlc.narg(seller_id)::uuid IS NULL OR seller_id = sqlc.narg(seller_id)::uuid)
ORDER BY id;

-- name: WebCampaignByLinkKey :one
SELECT id, seller_id, name, percent, scope, allow_coupon, clearance, starts_at, ends_at, state, link_key, revision,
       preview_revision, preview_models, preview_variants, created_by, created_at, updated_by, updated_at
FROM promotion.web_campaigns
WHERE link_key = @link_key AND deleted_at IS NULL;

-- name: CoveredVariants :many
-- Biến thể đang bán của người bán thuộc phạm vi (all; mẫu trong product_ids; danh mục của mẫu trong category_ids),
-- theo mẫu rồi biến thể. Xem trước, bật chiến dịch đếm trên tập này.
SELECT v.product_id, v.product_name, v.variant_id, v.price
FROM catalog.variants_v AS v
JOIN catalog.products_v AS p ON p.product_id = v.product_id AND p.seller_id = v.seller_id
WHERE v.seller_id = @seller_id AND v.sellable AND p.deleted_at IS NULL
  AND (@scope::text = 'all'
       OR (@scope::text = 'products' AND v.product_id = ANY(sqlc.arg(product_ids)::uuid[]))
       OR (@scope::text = 'categories' AND p.category_id = ANY(sqlc.arg(category_ids)::uuid[])))
ORDER BY v.product_id, v.variant_id;

-- name: ExistingProducts :many
-- Mã mẫu (chưa xóa) của người bán trong ids: kiểm đích khi lưu.
SELECT product_id
FROM catalog.products_v
WHERE seller_id = @seller_id AND deleted_at IS NULL AND product_id = ANY(sqlc.arg(ids)::uuid[]);

-- name: ExistingCategories :many
SELECT category_id
FROM catalog.categories_v
WHERE category_id = ANY(sqlc.arg(ids)::uuid[]);

-- name: TargetProducts :many
-- Chọn đích loại mẫu: mẫu chưa xóa của người bán, q theo mã hoặc tên (không dấu), theo id; con trỏ là id cuối trang.
SELECT product_id AS id, code, name
FROM catalog.products_v
WHERE seller_id = @seller_id AND deleted_at IS NULL
  AND (sqlc.narg(q)::text IS NULL OR code ILIKE '%' || sqlc.narg(q)::text || '%'
       OR name_search LIKE '%' || sqlc.narg(q_folded)::text || '%')
  AND (sqlc.narg(after)::uuid IS NULL OR product_id > sqlc.narg(after)::uuid)
ORDER BY product_id
LIMIT @row_limit;

-- name: TargetCategories :many
SELECT category_id AS id, slug AS code, name
FROM catalog.categories_v
WHERE (sqlc.narg(q)::text IS NULL OR name ILIKE '%' || sqlc.narg(q)::text || '%' OR slug ILIKE '%' || sqlc.narg(q)::text || '%')
  AND (sqlc.narg(after)::uuid IS NULL OR category_id > sqlc.narg(after)::uuid)
ORDER BY category_id
LIMIT @row_limit;
