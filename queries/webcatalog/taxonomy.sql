-- Phân loại cho website (kế hoạch M5, Task 190 / M5-07): danh mục đang dùng, thuộc tính và giá trị, nhãn.

-- name: Categories :many
SELECT category_id, parent_id, name, slug, depth, position
FROM catalog.categories_v
WHERE status = 'active'
ORDER BY depth, position, name, category_id;

-- name: Attributes :many
SELECT attribute_id, code, name, position
FROM catalog.attributes_v
ORDER BY position, name, attribute_id;

-- name: AttributeValues :many
SELECT attribute_value_id, attribute_id, value, position
FROM catalog.attribute_values_v
ORDER BY attribute_id, position, value, attribute_value_id;

-- name: Tags :many
SELECT tag_id, code, name
FROM catalog.tags_v
ORDER BY name, code, tag_id;
