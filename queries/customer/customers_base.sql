-- Hồ sơ khách M2 (kế hoạch M2, Task M2-03; 05 mục 1): câu đọc nền theo id, cho Get của Task M2-09. Chạy trong
-- tenancy.WithTenant (RLS lọc tenant). Chỉ số chỉ đúng ở dòng có id = root_id.

-- name: GetCustomerBase :one
SELECT id, root_id, code, full_name, name_search, lifecycle, birthday, level_id, order_count, delivered_count,
       returned_count, total_spent, last_order_at, stats_updated_at, created_by, created_at, updated_at, version
FROM customer.customers
WHERE id = @id;
