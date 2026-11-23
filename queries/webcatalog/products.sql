-- Danh mục cho website (kế hoạch M5, Task 190 / M5-07). Chỉ đọc view _v đã công bố (docs/views.md; luật
-- tests/arch/sqlusage_test: gói queries/webcatalog không nhắc bảng gốc, không nhắc schema platform). Không có giá nhập.
-- Câu trên bảng lớn (mẫu, biến thể, số dư, ảnh, nhãn, thuộc tính của mẫu) không nối bảng nhỏ (người bán, kho, nhãn,
-- thuộc tính): bên gọi đọc bảng nhỏ bằng câu riêng rồi truyền mã, để kế hoạch luôn đi theo index (không Seq Scan).

-- name: ActiveSellers :many
SELECT seller_id FROM catalog.sellers_v WHERE status = 'active' ORDER BY seller_id;

-- name: WarehouseByCode :many
SELECT warehouse_id FROM inventory.warehouses_v WHERE code = @code::text;

-- name: ProductPage :many
-- Trang mẫu theo khóa product_id tăng dần (keyset), mẫu chưa xóa của các người bán sellers (đang hoạt động, ADR-29).
SELECT p.product_id, p.seller_id, p.code, p.name, p.description, p.article, p.status, p.category_id, p.updated_at,
       p.version
FROM catalog.products_v AS p
WHERE p.product_id > @after::uuid AND p.deleted_at IS NULL AND p.seller_id = ANY(@sellers::uuid[])
ORDER BY p.product_id
LIMIT @lim::int;

-- name: ProductsByIDs :many
-- Mẫu theo danh sách mã (cùng điều kiện với ProductPage): mã không có, đã xóa, của người bán ngừng thì không có dòng.
SELECT p.product_id, p.seller_id, p.code, p.name, p.description, p.article, p.status, p.category_id, p.updated_at,
       p.version
FROM catalog.products_v AS p
WHERE p.product_id = ANY(@ids::uuid[]) AND p.deleted_at IS NULL AND p.seller_id = ANY(@sellers::uuid[])
ORDER BY p.product_id;

-- name: VariantsOf :many
-- Biến thể của các mẫu (theo thứ tự tạo) kèm số có thể bán ở kho warehouse_id (không có dòng số dư → 0). Đọc từng mẫu
-- (LATERAL … OFFSET 0 giữ vòng lặp theo mẫu: index biến thể theo mẫu, khóa chính của mẫu và của số dư).
SELECT v.variant_id, v.product_id, v.sku, v.barcode, v.color, v.size, v.price, v.variant_status, v.sellable,
       v.weight_grams,
       coalesce((SELECT sa.available FROM inventory.stock_available_v AS sa
                 WHERE sa.variant_id = v.variant_id AND sa.warehouse_id = @warehouse_id::uuid), 0)::int AS available
FROM unnest(@product_ids::uuid[]) AS pid (id)
CROSS JOIN LATERAL (
    SELECT x.variant_id, x.product_id, x.sku, x.barcode, x.color, x.size, x.price, x.variant_status, x.sellable,
           x.weight_grams
    FROM catalog.variants_v AS x
    WHERE x.product_id = pid.id
    OFFSET 0
) AS v
ORDER BY v.product_id, v.variant_id;

-- name: ImagesOf :many
-- Ảnh đang hiện của các mẫu theo thứ tự hiện.
SELECT i.product_id, i.file_id, i.color, i.position
FROM catalog.product_images_v AS i
WHERE i.product_id = ANY(@product_ids::uuid[])
ORDER BY i.product_id, i.position, i.image_id;

-- name: TagIDsOf :many
SELECT pt.product_id, pt.tag_id
FROM catalog.product_tags_v AS pt
WHERE pt.product_id = ANY(@product_ids::uuid[])
ORDER BY pt.product_id, pt.tag_id;

-- name: AttributeValueIDsOf :many
SELECT pav.product_id, pav.attribute_value_id
FROM catalog.product_attribute_values_v AS pav
WHERE pav.product_id = ANY(@product_ids::uuid[])
ORDER BY pav.product_id, pav.attribute_value_id;
