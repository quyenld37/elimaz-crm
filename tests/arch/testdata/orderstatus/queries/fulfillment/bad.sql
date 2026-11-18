-- Mẫu sai: đổi trạng thái đơn ngoài transition.sql.

-- name: CancelOrderBehindBack :exec
UPDATE ord.orders
SET status = 'cancelled', version = version + 1
WHERE id = @id;
