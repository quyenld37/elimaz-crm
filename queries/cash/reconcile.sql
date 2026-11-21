-- Đối chiếu sổ COD hằng đêm (kế hoạch M3, Task M3-33; TI03). Chạy trong tenancy.WithTenant; chỉ đọc sổ và chèn lần
-- chạy, phát hiện (không khóa dòng).

-- name: ReconcileRunBySlot :one
-- Lần chạy của kỳ (Lookup của lệnh lịch, chống chạy hai lần một kỳ).
SELECT id
FROM cash.reconcile_runs
WHERE slot = @slot;

-- name: LedgerTotal :one
-- Tổng mọi dòng sổ COD của tenant (phải bằng 0).
SELECT coalesce(sum(amount), 0)::bigint AS total
FROM cash.cod_ledger_entries;

-- name: TransferChecks :many
-- Lô bút toán có mã sau @after theo mã tăng dần, tối đa @lim: số dòng, tổng, số dòng khác vận đơn của bút toán (bên
-- gọi báo bút toán ít hơn hai dòng, tổng khác 0 hay có dòng khác vận đơn).
WITH b AS (SELECT t.id, t.shipment_id
           FROM cash.cod_transfers t
           WHERE t.id > @after
           ORDER BY t.id
           LIMIT @lim)
SELECT b.id, b.shipment_id, count(e.id)::bigint AS lines, coalesce(sum(e.amount), 0)::bigint AS total,
       count(e.id) FILTER (WHERE e.shipment_id <> b.shipment_id)::bigint AS other_shipment
FROM b
LEFT JOIN cash.cod_ledger_entries e ON e.transfer_id = b.id
GROUP BY b.id, b.shipment_id
ORDER BY b.id;

-- name: ReconcileShipments :many
-- Vận đơn có COD hay có dòng sổ, mã sau @after, theo mã tăng dần, tối đa @lim: trạng thái, giao một phần, COD, COD hãng
-- báo đã thu (rỗng: COD), lúc đổi trạng thái gần nhất, số dư năm ngăn và hai cờ bỏ qua:
-- open_returned_finding — có phát hiện remitted_then_returned chưa có điều chỉnh (adjustment, reversal của nhân viên)
-- nào ghi sau nó; adjusted_since_confirm — có điều chỉnh của nhân viên ghi sau lần chốt bảng kê gần nhất có dòng của
-- vận đơn (chưa có bảng kê chốt: mọi điều chỉnh).
SELECT s.id, s.status, s.partial, s.cod_amount, coalesce(s.cod_collected_amount, s.cod_amount)::bigint AS cod_collected,
       s.last_status_at,
       coalesce(b.to_collect, 0)::bigint      AS to_collect,
       coalesce(b.carrier_holding, 0)::bigint AS carrier_holding,
       coalesce(b.shop_received, 0)::bigint   AS shop_received,
       coalesce(b.carrier_fee, 0)::bigint     AS carrier_fee,
       coalesce(b.sales_clearing, 0)::bigint  AS sales_clearing,
       EXISTS (SELECT 1
               FROM cash.reconcile_findings f
               WHERE f.shipment_id = s.id
                 AND f.kind = 'remitted_then_returned'
                 AND NOT EXISTS (SELECT 1
                                 FROM cash.cod_transfers a
                                 WHERE a.shipment_id = s.id
                                   AND a.actor_type = 'staff'
                                   AND a.kind IN ('adjustment', 'reversal')
                                   AND a.created_at > f.created_at))       AS open_returned_finding,
       EXISTS (SELECT 1
               FROM cash.cod_transfers a
               WHERE a.shipment_id = s.id
                 AND a.actor_type = 'staff'
                 AND a.kind IN ('adjustment', 'reversal')
                 AND a.created_at > coalesce((SELECT max(st.confirmed_at)
                                              FROM cash.cod_statement_lines l
                                              JOIN cash.cod_statements st ON st.id = l.statement_id
                                              WHERE l.shipment_id = s.id
                                                AND st.status = 'confirmed'), '-infinity'::timestamptz)) AS adjusted_since_confirm
FROM fulfillment.shipments_v s
LEFT JOIN LATERAL (SELECT sum(e.amount) FILTER (WHERE e.account = 'to_collect')      AS to_collect,
                          sum(e.amount) FILTER (WHERE e.account = 'carrier_holding') AS carrier_holding,
                          sum(e.amount) FILTER (WHERE e.account = 'shop_received')   AS shop_received,
                          sum(e.amount) FILTER (WHERE e.account = 'carrier_fee')     AS carrier_fee,
                          sum(e.amount) FILTER (WHERE e.account = 'sales_clearing')  AS sales_clearing
                   FROM cash.cod_ledger_entries e
                   WHERE e.shipment_id = s.id) b ON true
WHERE s.id > @after
  AND (s.cod_amount > 0 OR EXISTS (SELECT 1 FROM cash.cod_ledger_entries x WHERE x.shipment_id = s.id))
ORDER BY s.id
LIMIT @lim;

-- name: InsertReconcileRun :one
-- Ghi lần chạy của kỳ; kỳ đã có lần chạy (worker khác, chạy lại) → không dòng nào (pgx.ErrNoRows).
INSERT INTO cash.reconcile_runs (tenant_id, ran_at, ok, findings, slot)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @ran_at, @ok, @findings, @slot)
ON CONFLICT ON CONSTRAINT reconcile_runs_slot_key DO NOTHING
RETURNING id;

-- name: InsertRunFinding :exec
INSERT INTO cash.reconcile_findings (tenant_id, run_id, kind, shipment_id, detail)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @run_id, @kind, sqlc.narg(shipment_id)::uuid, @detail);

-- name: GetReconcileRun :one
-- Lần chạy theo mã; mã rỗng → lần chạy gần nhất (theo lúc chạy, rồi mã).
SELECT id, ran_at, ok, findings
FROM cash.reconcile_runs
WHERE (sqlc.narg(id)::uuid IS NULL OR id = sqlc.narg(id)::uuid)
ORDER BY ran_at DESC, id DESC
LIMIT 1;

-- name: ListRunFindings :many
-- Phát hiện của lần chạy theo mã tăng dần, sau mã @after (uuid 0: từ đầu), tối đa @lim.
SELECT id, kind, shipment_id, detail, created_at
FROM cash.reconcile_findings
WHERE run_id = @run_id
  AND id > @after
ORDER BY id
LIMIT @lim;
