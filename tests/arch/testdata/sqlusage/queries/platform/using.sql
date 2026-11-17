-- Tệp mẫu: bảng sau USING thuộc schema khác (phải bị báo).
-- name: DeleteUsing :exec
DELETE FROM platform.tenant_routes r USING identity.users u WHERE r.external_key = u.email;
