-- Tệp mẫu: bảng sau dấu phẩy đứng sau JOIN … ON thuộc schema khác (phải bị báo).
-- name: JoinComma :many
SELECT a.id FROM platform.tenants a JOIN platform.tenant_routes b ON b.tenant_id = a.id, identity.users c;
