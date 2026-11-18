-- name: SetStatus :exec
UPDATE ord.orders SET status = @to WHERE id = @id;
