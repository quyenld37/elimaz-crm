-- Danh sách "Lệch đối soát", "Quá hạn chưa có tiền" (kế hoạch M3, Task M3-32). Chạy trong tenancy.WithTenant; chỉ đọc.

-- name: ListDiscrepancies :many
-- Hợp của: dòng duplicate, external của bảng kê đã chốt; dòng amount_mismatch của bảng kê đã chốt khi vận đơn còn
-- carrier_holding ≠ 0 (TI05: trả đủ phần còn lại thì rời danh sách); vận đơn đã có remitted chưa bị đảo mà
-- carrier_holding ≠ 0 và không có dòng amount_mismatch nào ở trên (phần còn mở, diff = −carrier_holding); phát hiện
-- remitted_then_returned. Mới nhất trước theo mã mục (uuidv7); trang sau bắt đầu sau mã mục cuối. Số dư khác 0 đọc ở
-- cash.cod_open_holdings; mỗi nhánh tự lọc trang và lấy tối đa @lim mục theo index của nó rồi mới hợp (Task M3-48: không
-- cộng sổ, không quét vận đơn của mọi mục).
-- Chỉ mục của người bán @seller_id (ADR-29): dòng bảng kê theo tài khoản hãng của bảng kê (cả dòng ngoài hệ thống, không
-- vận đơn; câu con theo khóa chính), mục khác theo người bán của vận đơn.
-- Cột không có ở một loại mục: mã rỗng (uuid toàn 0), số 0, chuỗi rỗng (bên đọc bỏ qua).
SELECT kind, item_id, coalesce(statement_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS statement_id,
       coalesce(line_no, 0)::int AS line_no, shipment_id, order_id, coalesce(order_code, '')::text AS order_code,
       coalesce(carrier_key, '')::text AS carrier_key,
       coalesce(tracking_code, '')::text AS tracking_code, coalesce(cod_amount, 0)::bigint AS cod_amount,
       expected_amount, diff_amount, coalesce(match_status, '')::text AS match_status
FROM ((SELECT 'statement_line'::text AS kind, l.id AS item_id, l.statement_id::uuid AS statement_id,
             l.line_no::int AS line_no, l.shipment_id AS shipment_id, o.id AS order_id, o.code::text AS order_code,
             st.carrier_key::text AS carrier_key,
             l.tracking_code::text AS tracking_code, l.cod_amount::bigint AS cod_amount,
             l.expected_amount AS expected_amount, l.diff_amount AS diff_amount, l.match_status::text AS match_status
      FROM cash.cod_statement_lines l
      JOIN cash.cod_statements st ON st.id = l.statement_id AND st.status = 'confirmed'
      LEFT JOIN fulfillment.shipments_v s ON s.id = l.shipment_id
      LEFT JOIN ord.orders_v o ON o.id = s.order_id
      WHERE l.match_status IN ('duplicate', 'external', 'amount_mismatch')
        AND (SELECT a.seller_id FROM fulfillment.carrier_accounts_v a WHERE a.id = st.carrier_account_id) = @seller_id
        AND (l.match_status <> 'amount_mismatch'
          OR EXISTS (SELECT 1 FROM cash.cod_open_holdings h WHERE h.shipment_id = l.shipment_id))
        AND (sqlc.narg(before_id)::uuid IS NULL OR l.id < sqlc.narg(before_id)::uuid)
      ORDER BY l.id DESC
      LIMIT @lim)
      UNION ALL
      (SELECT 'open_remainder', b.shipment_id, NULL, NULL, b.shipment_id, o.id, o.code, s.carrier_key, s.tracking_code,
             s.cod_amount,
             b.carrier_holding, -b.carrier_holding, NULL
      FROM cash.cod_open_holdings b
      JOIN fulfillment.shipments_v s ON s.id = b.shipment_id
      LEFT JOIN ord.orders_v o ON o.id = s.order_id
      WHERE s.seller_id = @seller_id
        AND EXISTS (SELECT 1
                    FROM cash.cod_transfers t
                    WHERE t.shipment_id = b.shipment_id
                      AND t.kind = 'remitted'
                      AND NOT EXISTS (SELECT 1 FROM cash.cod_transfers r WHERE r.reverses_transfer_id = t.id))
        AND NOT EXISTS (SELECT 1
                        FROM cash.cod_statement_lines ml
                        JOIN cash.cod_statements mst ON mst.id = ml.statement_id AND mst.status = 'confirmed'
                        WHERE ml.shipment_id = b.shipment_id
                          AND ml.match_status = 'amount_mismatch')
        AND (sqlc.narg(before_id)::uuid IS NULL OR b.shipment_id < sqlc.narg(before_id)::uuid)
      ORDER BY b.shipment_id DESC
      LIMIT @lim)
      UNION ALL
      (SELECT 'remitted_then_returned', f.id, NULL, NULL, f.shipment_id, o.id, o.code, s.carrier_key, s.tracking_code,
             s.cod_amount, NULL,
             NULL, NULL
      FROM cash.reconcile_findings f
      JOIN fulfillment.shipments_v s ON s.id = f.shipment_id
      LEFT JOIN ord.orders_v o ON o.id = s.order_id
      WHERE f.kind = 'remitted_then_returned'
        AND s.seller_id = @seller_id
        AND (sqlc.narg(before_id)::uuid IS NULL OR f.id < sqlc.narg(before_id)::uuid)
      ORDER BY f.id DESC
      LIMIT @lim)) d
ORDER BY item_id DESC
LIMIT @lim;

-- name: ListOverdue :many
-- Vận đơn delivered trước cutoff (now − CODOverdueDays) còn carrier_holding > 0 (hãng chưa trả tiền), theo mã vận đơn
-- tăng dần; trang sau bắt đầu sau mã vận đơn cuối. Đi từ cash.cod_open_holdings (chỉ vận đơn còn số dư, Task M3-48)
-- thay vì cộng sổ của mọi vận đơn đã giao. Chỉ vận đơn của người bán @seller_id (ADR-29).
SELECT s.id, s.order_id, o.code AS order_code, s.carrier_key, s.tracking_code, s.delivered_at::timestamptz AS delivered_at,
       b.carrier_holding
FROM cash.cod_open_holdings b
JOIN fulfillment.shipments_v s ON s.id = b.shipment_id
JOIN ord.orders_v o ON o.id = s.order_id
WHERE b.carrier_holding > 0
  AND s.seller_id = @seller_id
  AND s.status = 'delivered'
  AND s.delivered_at < @cutoff::timestamptz
  AND (sqlc.narg(after_id)::uuid IS NULL OR b.shipment_id > sqlc.narg(after_id)::uuid)
ORDER BY b.shipment_id
LIMIT @lim;
