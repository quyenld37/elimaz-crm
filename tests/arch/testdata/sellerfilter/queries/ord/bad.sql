-- name: LockOrder :one
SELECT id, seller_id FROM ord.orders WHERE id = @id FOR UPDATE;
