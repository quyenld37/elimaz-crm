-- Người bán (kế hoạch M1, Task M1-27; ADR-29). Chạy trong tenancy.WithTenant: RLS giới hạn trong tenant.

-- name: GetDefaultSeller :one
-- Người bán của chính shop (kind own, active) có mã nhỏ nhất; dữ liệu khởi tạo chỉ có ELIMAZ.
SELECT id FROM catalog.sellers
WHERE kind = 'own' AND status = 'active'
ORDER BY id
LIMIT 1;

-- name: InsertSellerIfMissing :exec
INSERT INTO catalog.sellers (tenant_id, code, name, kind)
VALUES (@tenant_id, @code, @name, @kind)
ON CONFLICT (tenant_id, code) DO NOTHING;

-- name: GetSellerByCode :one
SELECT id FROM catalog.sellers WHERE code = @code;
