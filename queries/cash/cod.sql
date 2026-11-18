-- Sổ COD (kế hoạch M1, Task M1-32; 05 mục 2). Chạy trong tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 9; chỉ chèn.

-- name: InsertTransfer :one
-- Chèn bút toán; trùng (tenant_id, idempotency_key) → không dòng nào (pgx.ErrNoRows).
INSERT INTO cash.cod_transfers (tenant_id, shipment_id, kind, idempotency_key, reason, actor_type, actor_id)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @shipment_id, @kind, @idempotency_key, @reason,
        @actor_type, sqlc.narg(actor_id)::uuid)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING id;

-- name: GetTransferByKey :one
SELECT id, shipment_id, kind
FROM cash.cod_transfers
WHERE idempotency_key = @idempotency_key;

-- name: InsertEntry :exec
INSERT INTO cash.cod_ledger_entries (tenant_id, transfer_id, account, amount)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @transfer_id, @account, @amount);

-- name: TransferKinds :many
-- Các loại bút toán đã ghi cho vận đơn.
SELECT DISTINCT kind
FROM cash.cod_transfers
WHERE shipment_id = @shipment_id;

-- name: ShipmentCOD :one
-- Số COD của vận đơn, đọc qua view công bố của fulfillment.
SELECT cod_amount
FROM fulfillment.shipments_v
WHERE id = @id;
