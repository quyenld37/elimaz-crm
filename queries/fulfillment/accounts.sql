-- Tài khoản hãng vận chuyển (kế hoạch M1, Task M1-31). Chạy trong tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 8.

-- name: InsertCarrierAccount :one
-- Chèn tài khoản (seed, test: chủ bảng); đã có cùng (carrier_key, external_account_ref) thì giữ nguyên, trả mã cũ.
INSERT INTO fulfillment.carrier_accounts (tenant_id, seller_id, carrier_key, name, external_account_ref, warehouse_id)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @seller_id, @carrier_key, @name,
        @external_account_ref, @warehouse_id)
ON CONFLICT (tenant_id, carrier_key, external_account_ref) DO UPDATE SET carrier_key = EXCLUDED.carrier_key
RETURNING id;

-- name: GetCarrierAccount :one
SELECT id, seller_id, carrier_key, external_account_ref, warehouse_id, status
FROM fulfillment.carrier_accounts
WHERE id = @id;
