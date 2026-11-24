-- Banner trang chủ (kế hoạch M5, Task 453). Mọi câu chạy trong tenancy.WithTenant (RLS). Module khác chỉ đọc qua view
-- công bố (docs/views.md): chiến dịch web của promotion, mẫu, biến thể, danh mục, ảnh của catalog, biến thể ảnh của
-- platform.

-- name: GetBannerSet :one
-- revision của cả danh sách, không khóa (đường đọc). Chưa có dòng → pgx.ErrNoRows (= rỗng, revision 0).
SELECT revision, updated_by, updated_at
FROM content.home_banner_sets;

-- name: EnsureBannerSet :exec
-- Chèn dòng revision 0 nếu tenant chưa có; RLS (WITH CHECK) từ chối tenant_id khác ngữ cảnh.
INSERT INTO content.home_banner_sets (tenant_id)
VALUES (@tenant_id)
ON CONFLICT (tenant_id) DO NOTHING;

-- name: LockBannerSet :one
-- Khóa dòng của tenant (ADR-46 nhóm 12d, bảng đầu nhóm) trước khi so revision và thay danh sách.
SELECT revision
FROM content.home_banner_sets
FOR UPDATE;

-- name: BumpBannerSet :one
UPDATE content.home_banner_sets
SET revision   = revision + 1,
    updated_by = @updated_by::uuid,
    updated_at = @updated_at::timestamptz
WHERE tenant_id = @tenant_id
RETURNING revision, updated_by, updated_at;

-- name: ListBanners :many
SELECT id, position, enabled, kind, campaign_id, category_id, kicker, title, cta, theme, starts_at, ends_at,
       product_ids, image_desktop, image_mobile, alt, link_kind, link_id
FROM content.home_banners
ORDER BY position;

-- name: DeleteBanners :exec
DELETE FROM content.home_banners
WHERE tenant_id = @tenant_id;

-- name: InsertBanner :exec
INSERT INTO content.home_banners (id, tenant_id, position, enabled, kind, campaign_id, category_id, kicker, title, cta,
                                  theme, starts_at, ends_at, product_ids, image_desktop, image_mobile, alt, link_kind,
                                  link_id)
VALUES (@id, @tenant_id, @position, @enabled, @kind, sqlc.narg(campaign_id), sqlc.narg(category_id), @kicker, @title,
        @cta, @theme, sqlc.narg(starts_at), sqlc.narg(ends_at), @product_ids::uuid[], sqlc.narg(image_desktop),
        sqlc.narg(image_mobile), @alt, sqlc.narg(link_kind), sqlc.narg(link_id));

-- name: WebCampaigns :many
-- Chiến dịch giảm giá web chưa xóa kèm đích (banner chiến dịch, đích "chiến dịch" của banner ảnh).
SELECT c.id, c.name, c.percent, c.scope, c.state, c.starts_at, c.ends_at,
       coalesce(array_agg(t.target_id ORDER BY t.target_id) FILTER (WHERE t.target_id IS NOT NULL),
                '{}')::uuid[] AS target_ids
FROM promotion.web_campaigns_v AS c
LEFT JOIN promotion.web_campaign_targets_v AS t ON t.campaign_id = c.id
GROUP BY c.id, c.name, c.percent, c.scope, c.state, c.starts_at, c.ends_at
ORDER BY c.starts_at DESC, c.id;

-- name: ActiveCategories :many
SELECT category_id, parent_id, name
FROM catalog.categories_v
WHERE status = 'active'
ORDER BY depth, position, category_id;

-- name: ProductsByIDs :many
-- Mẫu đang bán (chưa xóa, status active, người bán hoạt động) theo mã; showable = có biến thể bán được giá > 0 và có
-- ảnh đã xử lý (như item() của website: mẫu không ảnh, không giá không lên banner).
SELECT p.product_id, p.code, p.name, p.category_id,
       (EXISTS (SELECT 1 FROM catalog.variants_v AS v
                WHERE v.product_id = p.product_id AND v.sellable AND v.price > 0)
        AND EXISTS (SELECT 1 FROM catalog.product_images_v AS i
                    JOIN platform.file_variants_v AS f ON f.file_id = i.file_id
                    WHERE i.product_id = p.product_id))::boolean AS showable
FROM catalog.products_v AS p
JOIN catalog.sellers_v AS s ON s.seller_id = p.seller_id AND s.status = 'active'
WHERE p.product_id = ANY(@ids::uuid[]) AND p.deleted_at IS NULL AND p.status = 'active'
ORDER BY p.product_id;

-- name: ShowableExists :one
-- Có ít nhất một mẫu hiện được (như ProductsByIDs.showable) trong nguồn: cả shop (@whole_shop), các danh mục, hay các mẫu.
SELECT EXISTS (
    SELECT 1
    FROM catalog.products_v AS p
    JOIN catalog.sellers_v AS s ON s.seller_id = p.seller_id AND s.status = 'active'
    WHERE p.deleted_at IS NULL AND p.status = 'active'
      AND (@whole_shop::boolean OR p.category_id = ANY(@category_ids::uuid[]) OR p.product_id = ANY(@product_ids::uuid[]))
      AND EXISTS (SELECT 1 FROM catalog.variants_v AS v
                  WHERE v.product_id = p.product_id AND v.sellable AND v.price > 0)
      AND EXISTS (SELECT 1 FROM catalog.product_images_v AS i
                  JOIN platform.file_variants_v AS f ON f.file_id = i.file_id
                  WHERE i.product_id = p.product_id)
)::boolean AS found;
