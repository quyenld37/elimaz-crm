-- Gợi ý "có thể là cùng một khách" (kế hoạch M2, Task M2-11; 05 mục 1: không bao giờ tự gộp). Chạy trong
-- tenancy.WithTenant. Đọc ord.orders_v (ngoại lệ chỉ đọc view, Q19; docs/views.md).

-- name: ListMergeCandidates :many
-- Nhóm khác (gốc khác @root_id) có danh tính phone trùng SĐT trong địa chỉ (chưa xóa) của nhóm @root_id hoặc SĐT người
-- nhận của 50 đơn gần nhất của nhóm. Mã, tên, chỉ số lấy ở hồ sơ gốc của nhóm khớp.
WITH grp AS (SELECT id FROM customer.customers WHERE root_id = @root_id),
     phones AS (SELECT a.phone
                FROM customer.customer_addresses a
                WHERE a.customer_id IN (SELECT id FROM grp)
                  AND a.deleted_at IS NULL
                UNION
                SELECT o.ship_phone
                FROM (SELECT ov.ship_phone
                      FROM ord.orders_v ov
                      WHERE ov.customer_id IN (SELECT id FROM grp)
                      ORDER BY ov.created_at DESC, ov.id DESC
                      LIMIT 50) o
                WHERE o.ship_phone <> '')
SELECT r.id                                                  AS root_id,
       r.code,
       r.full_name,
       r.order_count,
       r.delivered_count,
       r.returned_count,
       r.total_spent,
       r.last_order_at,
       array_agg(DISTINCT i.value ORDER BY i.value)::text[] AS matched_phones
FROM customer.customer_identities i
JOIN customer.customers c ON c.tenant_id = i.tenant_id AND c.id = i.customer_id
JOIN customer.customers r ON r.tenant_id = c.tenant_id AND r.id = c.root_id
WHERE i.kind = 'phone'
  AND i.scope = ''
  AND i.value IN (SELECT phone FROM phones)
  AND c.root_id <> @root_id
GROUP BY r.id, r.code, r.full_name, r.order_count, r.delivered_count, r.returned_count, r.total_spent, r.last_order_at
ORDER BY r.id;
