-- Đọc sổ COD (kế hoạch M3, Task M3-02; 05 mục 2). Chạy trong tenancy.WithTenant; chỉ đọc, không khóa dòng (sổ chỉ
-- ghi thêm).

-- name: GetTransfer :one
SELECT id, shipment_id, kind
FROM cash.cod_transfers
WHERE id = @id;

-- name: TransferEntries :many
-- Các vế của bút toán, theo thứ tự chèn.
SELECT account, amount
FROM cash.cod_ledger_entries
WHERE transfer_id = @transfer_id
ORDER BY id;

-- name: ShipmentBalances :one
-- Số dư từng ngăn của vận đơn; vận đơn chưa có dòng sổ → mọi ngăn 0.
SELECT coalesce(sum(amount) FILTER (WHERE account = 'to_collect'), 0)::bigint      AS to_collect,
       coalesce(sum(amount) FILTER (WHERE account = 'carrier_holding'), 0)::bigint AS carrier_holding,
       coalesce(sum(amount) FILTER (WHERE account = 'shop_received'), 0)::bigint   AS shop_received,
       coalesce(sum(amount) FILTER (WHERE account = 'carrier_fee'), 0)::bigint     AS carrier_fee,
       coalesce(sum(amount) FILTER (WHERE account = 'sales_clearing'), 0)::bigint  AS sales_clearing
FROM cash.cod_ledger_entries
WHERE shipment_id = @shipment_id;
