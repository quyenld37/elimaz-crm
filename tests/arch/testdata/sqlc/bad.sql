-- Câu mẫu sai tên cột (phải bị sqlc vet báo).
-- name: BadColumn :many
SELECT no_such_column FROM platform.tenants;
