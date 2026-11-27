-- Đảo "Thành công" (kế hoạch M3, Task M3-27). Chạy trong tenancy.WithTenant. Tiền đọc qua view công bố của cash
-- (docs/views.md); vận đơn chưa có dòng sổ thì không có dòng ở cash.shipment_cod_v (LEFT JOIN, coalesce 0).

-- name: CorrectionFacts :one
-- Mã vận đơn nội bộ, tiền hãng báo đã thu, số dư shop_received và còn bút toán remitted chưa bị bút toán nào đảo
-- không (khớp luật delivery_reversed của cash: shop_received ≠ 0 hoặc remitted còn sống thì không đảo tiền).
SELECT s.code, s.cod_collected_amount,
       coalesce(b.shop_received, 0)::bigint AS shop_received,
       EXISTS (
           SELECT 1 FROM cash.cod_transfers_v t
           WHERE t.tenant_id = s.tenant_id AND t.shipment_id = s.id AND t.kind = 'remitted'
             AND NOT EXISTS (
                 SELECT 1 FROM cash.cod_transfers_v r
                 WHERE r.tenant_id = t.tenant_id AND r.reverses_transfer_id = t.id
             )
       ) AS remitted
FROM fulfillment.shipments s
LEFT JOIN cash.shipment_cod_v b ON b.tenant_id = s.tenant_id AND b.shipment_id = s.id
WHERE s.id = @id;
