-- Báo phí các hãng trên đơn (kế hoạch M3, Task M3-13). Mọi câu trong tenancy.WithTenant (crm_app). Không khóa dòng:
-- chỉ chèn dòng mới và đổi dòng còn pending bằng câu có điều kiện (ADR-46 nhóm 8: shipping_quotes sau
-- carrier_accounts, mà đường này chỉ đọc carrier_accounts không khóa).

-- name: QuoteAccounts :many
-- Tài khoản hãng của người bán ở kho (bỏ trống ids — mảng rỗng, không NULL: mọi tài khoản của người bán ở kho đó;
-- có ids: đúng các tài khoản đó của người bán, mọi kho, mọi trạng thái để bên gọi báo lý do). Xếp web_sort rồi id.
-- Dùng cho so phí và ShipAccounts (tài khoản gửi được của đơn, Task 449).
SELECT id, name, carrier_key, mode, status, warehouse_id, default_options
FROM fulfillment.carrier_accounts
WHERE seller_id = @seller_id
  AND (CASE WHEN cardinality(@ids::uuid[]) = 0 THEN warehouse_id = @warehouse_id ELSE id = ANY(@ids::uuid[]) END)
ORDER BY web_sort, id;

-- name: InsertPendingQuote :exec
INSERT INTO fulfillment.shipping_quotes (tenant_id, order_id, request_id, carrier_account_id, carrier_key, status,
    input_hash)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @order_id, @request_id, @carrier_account_id,
    @carrier_key, 'pending', @input_hash);

-- name: PendingQuote :one
-- Dòng pending của (lần báo phí, tài khoản); không có (đã có kết quả, đã dọn) → không dòng nào.
SELECT order_id
FROM fulfillment.shipping_quotes
WHERE request_id = @request_id AND carrier_account_id = @carrier_account_id AND status = 'pending';

-- name: DeletePendingQuote :one
-- Xóa dòng pending của (lần báo phí, tài khoản) để chèn một dòng mỗi dịch vụ; không còn pending → không dòng nào.
DELETE FROM fulfillment.shipping_quotes
WHERE request_id = @request_id AND carrier_account_id = @carrier_account_id AND status = 'pending'
RETURNING order_id, carrier_key;

-- name: InsertOkQuotes :exec
-- Một dòng ok mỗi dịch vụ (mảng cùng độ dài; expected_days âm = NULL, valid_until chung của lần ghi).
INSERT INTO fulfillment.shipping_quotes (tenant_id, order_id, request_id, carrier_account_id, carrier_key, status,
    service, service_name, fee_total, fee_breakdown, expected_days, input_hash, valid_until)
SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid, @order_id::uuid, @request_id::uuid,
    @carrier_account_id::uuid, @carrier_key::text, 'ok', s.service, s.service_name, s.fee_total, s.fee_breakdown,
    NULLIF(s.expected_days, -1), s.input_hash, s.valid_until
FROM (SELECT unnest(@services::text[]) AS service, unnest(@service_names::text[]) AS service_name,
          unnest(@fee_totals::bigint[]) AS fee_total, unnest(@fee_breakdowns::jsonb[]) AS fee_breakdown,
          unnest(@expected_days::int[]) AS expected_days, unnest(@input_hashes::bytea[]) AS input_hash,
          unnest(@valid_untils::timestamptz[]) AS valid_until) AS s;

-- name: FailPendingQuote :one
-- Dòng pending → error (error_code) hay unavailable (error_code NULL); không còn pending → không dòng nào.
UPDATE fulfillment.shipping_quotes
SET status = @status, error_code = sqlc.narg('error_code'), updated_at = now()
WHERE request_id = @request_id AND carrier_account_id = @carrier_account_id AND status = 'pending'
RETURNING order_id;

-- name: LatestQuoteRequest :one
-- Lần báo phí gần nhất của đơn.
SELECT request_id
FROM fulfillment.shipping_quotes
WHERE order_id = @order_id
ORDER BY created_at DESC, request_id DESC
LIMIT 1;

-- name: ListQuotes :many
-- Báo phí của một lần (của đơn order_id): ok theo phí tăng dần, rồi pending, rồi lỗi (error, unavailable).
SELECT q.id, q.carrier_account_id, a.name AS account_name, q.carrier_key, q.status, q.service, q.service_name,
       q.fee_total, q.expected_days, q.valid_until, q.error_code
FROM fulfillment.shipping_quotes q
JOIN fulfillment.carrier_accounts a ON a.id = q.carrier_account_id
WHERE q.order_id = @order_id AND q.request_id = @request_id
ORDER BY CASE q.status WHEN 'ok' THEN 0 WHEN 'pending' THEN 1 ELSE 2 END, q.fee_total NULLS LAST, a.name,
    q.carrier_account_id, q.service;

-- name: DeleteOldQuotes :execrows
-- Dọn báo phí tạo trước @before (lô tối đa @max_rows), trừ báo phí đã được vận đơn dùng (shipments.quote_id).
DELETE FROM fulfillment.shipping_quotes
WHERE id IN (
    SELECT q.id FROM fulfillment.shipping_quotes q
    WHERE q.created_at < @before
      AND NOT EXISTS (SELECT 1 FROM fulfillment.shipments s WHERE s.quote_id = q.id)
    ORDER BY q.created_at
    LIMIT @max_rows);
