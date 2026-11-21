-- Điều chỉnh sổ COD có xem trước (kế hoạch M3, Task M3-33; TI08). Chạy trong tenancy.WithTenant; chỉ đọc.

-- name: AdjustShipmentStatus :one
-- Trạng thái vận đơn (view công bố của fulfillment; RLS: tenant khác như không có).
SELECT status
FROM fulfillment.shipments_v
WHERE id = @id;

-- name: AdjustTransfers :many
-- Bút toán của vận đơn theo thứ tự ghi, kèm đã bị đảo chưa và lúc chốt của bảng kê đã chốt sinh ra nó (rỗng: không
-- thuộc bảng kê đã chốt).
SELECT t.id, t.kind,
       EXISTS (SELECT 1 FROM cash.cod_transfers r WHERE r.reverses_transfer_id = t.id) AS reversed,
       st.confirmed_at
FROM cash.cod_transfers t
LEFT JOIN cash.cod_statement_lines l ON l.id = t.statement_line_id
LEFT JOIN cash.cod_statements st ON st.id = l.statement_id AND st.status = 'confirmed'
WHERE t.shipment_id = @shipment_id
ORDER BY t.id;
