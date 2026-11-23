-- "Đã bán 30 ngày" cho website (kế hoạch M5, Task 191 / M5-08; định nghĩa ở docs/m5/doi-chieu-gia-dinh.md mục 4):
-- tổng số lượng các dòng đơn theo biến thể, của đơn tạo trong [since, until), mọi kênh, trừ đơn nháp, đã hủy, đã hoàn.
-- Chỉ đọc view _v đã công bố.

-- name: Sold30 :many
-- Đơn lọc theo index orders_status_created (tenant_id, status, created_at); dòng đơn theo đơn.
SELECT v.product_id, i.variant_id, sum(i.quantity)::bigint AS sold
FROM ord.orders_v AS o
JOIN ord.order_items_v AS i ON i.order_id = o.id
JOIN catalog.variants_v AS v ON v.variant_id = i.variant_id
WHERE o.created_at >= @since::timestamptz AND o.created_at < @until::timestamptz
  AND o.status <> ALL(@excluded::text[])
GROUP BY v.product_id, i.variant_id
ORDER BY i.variant_id;
