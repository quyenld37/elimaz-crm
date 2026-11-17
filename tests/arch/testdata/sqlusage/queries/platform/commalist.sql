-- Tệp mẫu: bảng thứ hai sau dấu phẩy thuộc schema khác, và bảng thứ ba thiếu schema (phải bị báo).
-- name: CommaList :many
SELECT t.id FROM platform.tenants t, identity.users u, sessions s;
