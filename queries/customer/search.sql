-- Tìm khách và danh sách khách gốc (kế hoạch M2, Task M2-10; 04 mục 7). Chạy trong tenancy.WithTenant (RLS lọc
-- tenant). Câu tìm trả root_id của hồ sơ khớp (gốc hay con), bên gọi gom theo gốc rồi đọc dòng gốc bằng
-- ListSearchRoots. Toán tử dùng ở đây (=, ~>=~, ~<~ trên text) là leakproof nên đi index dưới RLS
-- (search_explain_test).

-- name: SearchPhoneTail :many
-- Đuôi số: value_reversed (chữ số đảo ngược) trong khoảng [lo, hi) theo thứ tự byte (index
-- customer_identities_reversed); lo = đảo các số đã gõ, hi = lo với ký tự cuối tăng một.
SELECT DISTINCT c.root_id
FROM customer.customer_identities AS i
JOIN customer.customers AS c ON c.tenant_id = i.tenant_id AND c.id = i.customer_id
WHERE i.kind = 'phone'
  AND i.value_reversed ~>=~ @lo::text
  AND i.value_reversed ~<~ @hi::text
LIMIT @lim::int;

-- name: SearchPhoneExact :many
SELECT c.root_id
FROM customer.customer_identities AS i
JOIN customer.customers AS c ON c.tenant_id = i.tenant_id AND c.id = i.customer_id
WHERE i.kind = 'phone'
  AND i.scope = ''
  AND i.value = @value::text;

-- name: SearchCode :many
SELECT root_id
FROM customer.customers
WHERE code = @code::text;

-- name: SearchByName :many
-- Tên qua hàm SECURITY DEFINER (migration 0207): words là các từ đã qua textnorm.Fold.
SELECT s.id::uuid AS id, s.root_id::uuid AS root_id
FROM customer.search_by_name(@words::text[], @lim::int) AS s (id, root_id);

-- name: ListSearchRoots :many
-- Dòng gốc của các nhóm, mua gần nhất trước, chưa mua sau cùng, rồi theo id; tối đa lim. tag_id: chỉ nhóm có nhãn ở
-- bất kỳ hồ sơ nào trong nhóm (như ListRoots).
SELECT c.id, c.code, c.full_name, c.order_count, c.delivered_count, c.returned_count, c.total_spent, c.last_order_at
FROM customer.customers AS c
WHERE c.id = ANY (@root_ids::uuid[])
  AND c.id = c.root_id
  AND (sqlc.narg(tag_id)::uuid IS NULL
       OR EXISTS (SELECT 1
                  FROM customer.customer_tag_links AS l
                  JOIN customer.customers AS m ON m.tenant_id = l.tenant_id AND m.id = l.customer_id
                  WHERE l.tag_id = sqlc.narg(tag_id)::uuid AND m.root_id = c.id))
ORDER BY c.last_order_at DESC NULLS LAST, c.id
LIMIT @lim::int;

-- name: ListRoots :many
-- Danh sách khách gốc mới nhất trước, trang theo (created_at, id) (index customers_roots_created); tag_id: chỉ nhóm có
-- nhãn ở bất kỳ hồ sơ nào trong nhóm.
SELECT c.id, c.code, c.full_name, c.order_count, c.delivered_count, c.returned_count, c.total_spent, c.last_order_at,
       c.created_at
FROM customer.customers AS c
WHERE c.id = c.root_id
  AND (sqlc.narg(after_created)::timestamptz IS NULL
       OR (c.created_at, c.id) < (sqlc.narg(after_created)::timestamptz, sqlc.narg(after_id)::uuid))
  AND (sqlc.narg(tag_id)::uuid IS NULL
       OR EXISTS (SELECT 1
                  FROM customer.customer_tag_links AS l
                  JOIN customer.customers AS m ON m.tenant_id = l.tenant_id AND m.id = l.customer_id
                  WHERE l.tag_id = sqlc.narg(tag_id)::uuid AND m.root_id = c.id))
ORDER BY c.created_at DESC, c.id DESC
LIMIT @lim::int;

-- name: ListGroupPhones :many
-- SĐT của mọi hồ sơ trong các nhóm, theo gốc rồi thời điểm thêm.
SELECT c.root_id, i.value
FROM customer.customer_identities AS i
JOIN customer.customers AS c ON c.tenant_id = i.tenant_id AND c.id = i.customer_id
WHERE c.root_id = ANY (@root_ids::uuid[])
  AND i.kind = 'phone'
ORDER BY c.root_id, i.created_at, i.id;
