-- Sổ COD (kế hoạch M1, Task M1-32; 05 mục 2). Chạy trong tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 9; chỉ chèn.

-- name: InsertTransfer :one
-- Chèn bút toán; trùng (tenant_id, idempotency_key) → không dòng nào (pgx.ErrNoRows).
-- occurred_at rỗng → now() (giờ của transaction).
INSERT INTO cash.cod_transfers (tenant_id, shipment_id, kind, idempotency_key, reason, actor_type, actor_id,
                                statement_line_id, reverses_transfer_id, occurred_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @shipment_id, @kind, @idempotency_key, @reason,
        @actor_type, sqlc.narg(actor_id)::uuid, sqlc.narg(statement_line_id)::uuid,
        sqlc.narg(reverses_transfer_id)::uuid, coalesce(sqlc.narg(occurred_at)::timestamptz, now()))
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING id;

-- name: GetTransferByKey :one
SELECT id, shipment_id, kind
FROM cash.cod_transfers
WHERE idempotency_key = @idempotency_key;

-- name: InsertEntry :exec
INSERT INTO cash.cod_ledger_entries (tenant_id, transfer_id, shipment_id, account, amount)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @transfer_id, @shipment_id, @account, @amount);

-- name: ShipmentCOD :one
-- Số COD và mã vận đơn nội bộ (cho cảnh báo), đọc qua view công bố của fulfillment.
SELECT cod_amount, code
FROM fulfillment.shipments_v
WHERE id = @id;

-- name: LockShipmentCOD :exec
-- Khóa tư vấn sổ COD của một vận đơn đến hết transaction (ADR-46 mục advisory): đường ghi chọn bút toán theo số dư
-- (consumer sự kiện vận đơn; chốt bảng kê, điều chỉnh sau này) lấy khóa này trước khi đọc số dư.
SELECT pg_advisory_xact_lock(hashtextextended('cash.cod:' || sqlc.arg(shipment_id)::uuid::text, 0));

-- name: ShipmentTransfers :many
-- Bút toán của vận đơn theo thứ tự ghi (id uuidv7 tăng theo thời gian), kèm đã bị đảo chưa.
SELECT t.id, t.kind, t.idempotency_key,
       EXISTS (SELECT 1 FROM cash.cod_transfers r WHERE r.reverses_transfer_id = t.id) AS reversed
FROM cash.cod_transfers t
WHERE t.shipment_id = @shipment_id
ORDER BY t.id;

-- name: ShipmentEntries :many
-- Mọi dòng sổ của vận đơn theo bút toán, thứ tự chèn.
SELECT transfer_id, account, amount
FROM cash.cod_ledger_entries
WHERE shipment_id = @shipment_id
ORDER BY transfer_id, id;

-- name: InsertEventFinding :execrows
-- Phát hiện do sự kiện vận đơn mở (không thuộc lần đối chiếu nào); trùng source_key → không chèn (0 dòng).
INSERT INTO cash.reconcile_findings (tenant_id, kind, shipment_id, detail, source_key)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @kind, @shipment_id, @detail, @source_key)
ON CONFLICT ON CONSTRAINT reconcile_findings_source_key DO NOTHING;
