-- Sản phẩm ẩn với khách, điều kiện lên kệ, hiện hàng loạt (kế hoạch M7, Task 261 / M7-16; MO01, G04, G14). Pha MỞ
-- RỘNG trên bảng của M1 (chủ catalog, Q-X2): hai cột có mặc định, một view mới, quyền ghi một cột.
-- - catalog.products.published_at: lúc Publish đưa mẫu nháp lên kệ (NULL: chưa hiện, hay đã ẩn lại).
-- - catalog.attributes.required_for_shelf: thuộc tính phải có giá trị thì mẫu mới đủ điều kiện lên kệ (sửa bằng chủ
--   bảng / seed-staging tới khi có màn thuộc tính M7-29; crm_app chưa có quyền UPDATE cột này).
-- - catalog.product_readiness_v: một dòng mỗi mẫu draft hoặc active chưa xóa: ready, reasons (mã lý do theo thứ tự cố
--   định: no_variant, no_price, no_image | image_processing, no_category, missing_attr:<mã> theo vị trí rồi mã, no_tag).
--   Một nguồn cho API điều kiện lên kệ, Publish (đọc lại sau khi khóa mẫu) và báo cáo sẵn sàng (Task 269).
--   Đọc platform.files.status của ảnh: ngoại lệ ADR-27 mục 4 như khóa ngoại product_images → files của 0125 (ảnh
--   sản phẩm là tệp của catalog; chỉ đọc trạng thái, không khóa nhóm 12b).

-- +goose Up
ALTER TABLE catalog.products ADD COLUMN published_at timestamptz NULL;
ALTER TABLE catalog.attributes ADD COLUMN required_for_shelf boolean NOT NULL DEFAULT false;

CREATE VIEW catalog.product_readiness_v WITH (security_invoker = true) AS
SELECT p.id AS product_id, p.status, cardinality(r.reasons) = 0 AS ready, r.reasons
FROM catalog.products AS p
CROSS JOIN LATERAL (
    SELECT array_remove(ARRAY[
        CASE WHEN NOT EXISTS (
            SELECT 1 FROM catalog.variants AS v
            WHERE v.tenant_id = p.tenant_id AND v.seller_id = p.seller_id AND v.product_id = p.id AND v.status = 'active'
        ) THEN 'no_variant' END,
        CASE WHEN EXISTS (
            SELECT 1 FROM catalog.variants AS v
            WHERE v.tenant_id = p.tenant_id AND v.seller_id = p.seller_id AND v.product_id = p.id AND v.status = 'active'
              AND v.price <= 0
        ) THEN 'no_price' END,
        CASE
            WHEN NOT EXISTS (
                SELECT 1 FROM catalog.product_images AS i
                WHERE i.tenant_id = p.tenant_id AND i.product_id = p.id AND i.deleted_at IS NULL
            ) THEN 'no_image'
            WHEN NOT EXISTS (
                SELECT 1 FROM catalog.product_images AS i
                JOIN platform.files AS f ON f.tenant_id = i.tenant_id AND f.id = i.file_id
                WHERE i.tenant_id = p.tenant_id AND i.product_id = p.id AND i.deleted_at IS NULL
                  AND f.status = 'ready' AND f.deleted_at IS NULL
            ) THEN 'image_processing'
        END,
        CASE WHEN p.category_id IS NULL THEN 'no_category' END
    ]::text[], NULL)
    || coalesce((
        SELECT array_agg('missing_attr:' || a.code ORDER BY a.position, a.code)
        FROM catalog.attributes AS a
        WHERE a.tenant_id = p.tenant_id AND a.required_for_shelf
          AND NOT EXISTS (
              SELECT 1 FROM catalog.product_attribute_values AS pav
              JOIN catalog.attribute_values AS av ON av.tenant_id = pav.tenant_id AND av.id = pav.attribute_value_id
              WHERE pav.tenant_id = p.tenant_id AND pav.product_id = p.id AND av.attribute_id = a.id
          )
    ), '{}'::text[])
    || CASE WHEN NOT EXISTS (
        SELECT 1 FROM catalog.product_tags AS pt WHERE pt.tenant_id = p.tenant_id AND pt.product_id = p.id
    ) THEN ARRAY['no_tag']::text[] ELSE '{}'::text[] END AS reasons
) AS r
WHERE p.status IN ('draft', 'active') AND p.deleted_at IS NULL;

-- Publish/Unpublish khóa mẫu nháp (hay đang bán) của cả shop theo mã: index một phần theo trạng thái.
CREATE INDEX products_launch_status ON catalog.products (tenant_id, status, id)
    WHERE deleted_at IS NULL AND status IN ('draft', 'active');

GRANT UPDATE (published_at) ON catalog.products TO crm_app;
GRANT SELECT ON catalog.product_readiness_v TO crm_app;
