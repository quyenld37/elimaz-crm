-- Biến thể màu/size (kế hoạch M1, Task M1-27). Chạy trong tenancy.WithTenant. Mọi câu đọc, khóa, sửa có seller_id (ADR-29;
-- tests/arch/sellerfilter_test.go, Task M1-37).

-- name: InsertVariant :one
-- id rỗng (uuid.Nil) → uuidv7() như mặc định của cột (id đặt trước: lệnh seed-web-fixture, Task 218). Vị trí (cuối + 1
-- trong mẫu) do trigger variants_position của 0533 điền (Task 513): câu này không nhắc cột position để còn chạy trên schema
-- trước 0533 (test dựng dữ liệu cũ, bản cũ của máy chủ lúc triển khai); bên gọi đã khóa dòng mẫu (hay vừa tạo mẫu trong
-- tx) nên hai lần thêm không cùng vị trí.
INSERT INTO catalog.variants
    (id, tenant_id, seller_id, product_id, sku, barcode, color, color_hex, size, price, cost_price)
VALUES (coalesce(nullif(@id::uuid, '00000000-0000-0000-0000-000000000000'), uuidv7()), @tenant_id, @seller_id, @product_id, @sku, @barcode, @color, sqlc.narg(color_hex), @size, @price,
        sqlc.narg(cost_price))
RETURNING id, status, version;

-- name: ListProductVariants :many
-- Theo vị trí nhân viên đặt rồi mã (Task 513).
SELECT id, seller_id, product_id, sku, barcode, color, color_hex, size, price, cost_price, status, version, weight_grams
FROM catalog.variants
WHERE seller_id = @seller_id AND product_id = @product_id
ORDER BY position, id;

-- name: GetVariantProductID :one
-- Mã mẫu của biến thể (không khóa): sửa biến thể khóa mẫu trước rồi biến thể (ADR-46 nhóm 4).
SELECT product_id FROM catalog.variants WHERE seller_id = @seller_id AND id = @id;

-- name: LockVariant :one
SELECT id, seller_id, product_id, sku, barcode, color, color_hex, size, price, cost_price, status, version, weight_grams
FROM catalog.variants
WHERE seller_id = @seller_id AND id = @id
FOR UPDATE;

-- name: UpdateVariant :one
UPDATE catalog.variants
SET sku = @sku, barcode = @barcode, color = @color, color_hex = sqlc.narg(color_hex), size = @size, price = @price,
    cost_price = sqlc.narg(cost_price), status = @status, weight_grams = sqlc.narg(weight_grams), version = version + 1,
    updated_at = now()
WHERE seller_id = @seller_id AND id = @id AND version = @version
RETURNING version;

-- name: SetVariantPositions :execrows
-- Đặt vị trí 1..n của các biến thể ids (theo thứ tự trong mảng) của mẫu product_id (Task 513). Bên gọi đã khóa dòng mẫu
-- và kiểm ids đúng tập biến thể của mẫu; version biến thể không đổi (thứ tự là của mẫu).
UPDATE catalog.variants AS v
SET position = o.pos::int, updated_at = now()
FROM unnest(@ids::uuid[]) WITH ORDINALITY AS o (id, pos)
WHERE v.seller_id = @seller_id AND v.product_id = @product_id AND v.id = o.id;

-- name: BarcodeExists :one
SELECT EXISTS (SELECT 1 FROM catalog.variants WHERE seller_id = @seller_id AND barcode = @barcode);

-- name: GetSaleVariant :one
SELECT variant_id, seller_id, product_id, sku, product_name, color, size, price, sellable
FROM catalog.variants_v
WHERE seller_id = @seller_id AND variant_id = @variant_id;

-- name: ListSaleVariants :many
SELECT variant_id, seller_id, product_id, sku, product_name, color, size, price
FROM catalog.variants_v
WHERE seller_id = @seller_id AND variant_id = ANY(@variant_ids::uuid[]) AND sellable;
