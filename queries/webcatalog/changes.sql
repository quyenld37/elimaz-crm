-- Luồng thay đổi cho website (kế hoạch M5, Task 190 / M5-07): mẫu có mẫu, biến thể hay số dư tồn đổi từ mốc since.
-- Mọi lần ghi biến thể (tạo, sửa giá, ngừng bán, cân nặng) và gắn, xếp, xóa ảnh đều tăng version và updated_at của
-- mẫu trong cùng transaction (catalog.Writer → bumpProduct), nên products_v.updated_at phủ cả biến thể và ảnh; chỉ số
-- dư tồn (module inventory) cần nhánh riêng.

-- name: Changes :many
-- Mỗi mẫu một dòng với mốc đổi lớn nhất, xếp theo mốc rồi mã mẫu. Hai nhánh đọc theo index updated_at; nhánh số dư
-- tra mẫu của từng biến thể (LATERAL … OFFSET 0: khóa chính của biến thể và của mẫu).
SELECT c.product_id, max(c.ts)::timestamptz AS changed_at
FROM (
    SELECT p.product_id, p.updated_at AS ts FROM catalog.products_v AS p WHERE p.updated_at >= @since::timestamptz
    UNION ALL
    SELECT v.product_id, sa.updated_at AS ts
    FROM inventory.stock_available_v AS sa
    CROSS JOIN LATERAL (SELECT x.product_id FROM catalog.variants_v AS x WHERE x.variant_id = sa.variant_id OFFSET 0) AS v
    WHERE sa.updated_at >= @since::timestamptz
) AS c
GROUP BY c.product_id
ORDER BY changed_at, c.product_id
LIMIT @lim::int;

-- name: Now :one
SELECT now()::timestamptz AS read_at;
