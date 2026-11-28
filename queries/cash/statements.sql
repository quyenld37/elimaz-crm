-- Bảng kê COD (kế hoạch M3, Task M3-31). Chạy trong tenancy.WithTenant. Thứ tự khóa ADR-46 nhóm 9:
-- cod_statements → cod_statement_lines.

-- name: CarrierAccountKey :one
-- Khóa hãng của tài khoản hãng, đọc qua view công bố của fulfillment (RLS: tenant khác như không có).
SELECT carrier_key
FROM fulfillment.carrier_accounts_v
WHERE id = @id;

-- name: InsertStatement :one
-- Chèn bảng kê imported; cùng tệp (băm nội dung) của cùng hãng đã nhập → không dòng nào (pgx.ErrNoRows).
INSERT INTO cash.cod_statements (tenant_id, carrier_key, carrier_account_id, file_id, file_sha256, format, status,
                                 imported_by)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @carrier_key, @carrier_account_id, @file_id,
        @file_sha256, @format, 'imported', @imported_by)
ON CONFLICT ON CONSTRAINT cod_statements_file_key DO NOTHING
RETURNING *;

-- name: StatementIDByFile :one
-- Bảng kê đã nhập với cùng tệp của cùng hãng.
SELECT id
FROM cash.cod_statements
WHERE carrier_key = @carrier_key
  AND file_sha256 = @file_sha256;

-- name: GetStatement :one
SELECT *
FROM cash.cod_statements
WHERE id = @id;

-- name: ListStatements :many
-- Bảng kê mới nhất trước (id uuidv7 tăng theo thời gian), lọc tùy chọn theo trạng thái, tài khoản hãng; trang sau
-- bắt đầu sau id của dòng cuối trang trước. Chỉ bảng kê của tài khoản hãng của người bán @seller_id (ADR-29; câu con
-- theo khóa chính của tài khoản).
SELECT *
FROM cash.cod_statements
WHERE (SELECT a.seller_id FROM fulfillment.carrier_accounts_v a WHERE a.id = carrier_account_id) = @seller_id
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(carrier_account_id)::uuid IS NULL OR carrier_account_id = sqlc.narg(carrier_account_id)::uuid)
  AND (sqlc.narg(before_id)::uuid IS NULL OR id < sqlc.narg(before_id)::uuid)
ORDER BY id DESC
LIMIT @lim;

-- name: StartParsing :exec
-- Lệnh đọc tệp bắt đầu: imported → parsing (đã parsing: không đổi).
UPDATE cash.cod_statements
SET status = 'parsing', updated_at = now(), version = version + 1
WHERE id = @id
  AND status = 'imported';

-- name: FailParse :execrows
-- Đọc tệp thất bại: parse_failed kèm mã lỗi (JSON {code, column?, line?}); chỉ khi chưa đọc xong.
UPDATE cash.cod_statements
SET status = 'parse_failed', parse_error = @parse_error, updated_at = now(), version = version + 1
WHERE id = @id
  AND status IN ('imported', 'parsing');

-- name: FinishParse :one
-- Đọc tệp xong: parsed, số dòng và kỳ bảng kê (ngày chuyển nhỏ nhất, lớn nhất) đếm từ dòng đã ghi; chỉ khi chưa
-- đọc xong (đã đọc xong → không dòng nào, pgx.ErrNoRows). Trả version mới (số thứ tự của sự kiện đã đọc xong).
UPDATE cash.cod_statements s
SET status      = 'parsed',
    line_count  = l.n,
    period_from = l.first_day,
    period_to   = l.last_day,
    updated_at  = now(),
    version     = s.version + 1
FROM (SELECT count(*)::int AS n, min(paid_on) AS first_day, max(paid_on) AS last_day
      FROM cash.cod_statement_lines
      WHERE statement_id = @id) l
WHERE s.id = @id
  AND s.status IN ('imported', 'parsing')
RETURNING s.version;

-- name: InsertStatementLines :execrows
-- Ghi một lô dòng; dòng đã có (cùng số dòng) → bỏ qua, nên chạy lại cả lô an toàn. Các mảng cùng độ dài (bên gọi
-- dựng): nhiều unnest trong danh sách chọn được PostgreSQL ghép theo vị trí.
INSERT INTO cash.cod_statement_lines (tenant_id, statement_id, line_no, tracking_code, cod_amount, fee_amount,
                                      other_fee, net_amount, paid_on)
SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid, @statement_id::uuid, unnest(@line_nos::int[]),
       unnest(@tracking_codes::text[]), unnest(@cod_amounts::bigint[]), unnest(@fee_amounts::bigint[]),
       unnest(@other_fees::bigint[]), unnest(@net_amounts::bigint[]), unnest(@paid_ons::date[])
ON CONFLICT ON CONSTRAINT cod_statement_lines_line_key DO NOTHING;

-- name: ListStatementLines :many
-- Dòng của bảng kê theo số dòng tăng dần, lọc tùy chọn theo kết quả khớp; trang sau bắt đầu sau số dòng cuối. Kèm mã,
-- số đơn của vận đơn đã khớp (view công bố của fulfillment, ord; dòng chưa khớp vận đơn: NULL) cho cột "Đơn" của màn.
SELECT l.*, o.id AS order_id, o.code AS order_code
FROM cash.cod_statement_lines l
LEFT JOIN fulfillment.shipments_v s ON s.id = l.shipment_id
LEFT JOIN ord.orders_v o ON o.id = s.order_id
WHERE l.statement_id = @statement_id
  AND (sqlc.narg(match_status)::text IS NULL OR l.match_status = sqlc.narg(match_status)::text)
  AND l.line_no > @after_line_no
ORDER BY l.line_no
LIMIT @lim;

-- name: StatementTotals :many
-- Số dòng, tổng tiền COD, tổng phí hãng (phí + phí khác) và tổng phần hãng trả thiếu (chênh âm, đổi dấu) theo kết quả
-- khớp của mọi dòng của bảng kê.
SELECT match_status, count(*)::int AS lines, coalesce(sum(cod_amount), 0)::bigint AS cod_amount,
       coalesce(sum(fee_amount + other_fee), 0)::bigint AS fee_amount,
       coalesce(sum(greatest(-diff_amount, 0)), 0)::bigint AS short_amount
FROM cash.cod_statement_lines
WHERE statement_id = @statement_id
GROUP BY match_status
ORDER BY match_status;

-- name: CarrierAccountNames :many
-- Tên tài khoản hãng theo mã (view công bố của fulfillment; RLS: tenant khác như không có).
SELECT id, name
FROM fulfillment.carrier_accounts_v
WHERE id = ANY (@ids::uuid[]);

-- Khớp và chốt bảng kê (Task M3-32). Thứ tự khóa ADR-46: khóa tư vấn sổ COD theo vận đơn tăng dần (LockShipmentCOD)
-- trước mọi khóa dòng nhóm 9, rồi cod_statements → cod_statement_lines theo line_no → chèn bút toán.

-- name: LockParsedStatement :one
-- Khóa bảng kê đã đọc xong để khớp dòng; đã khớp, đã chốt (sự kiện giao lại) → không dòng nào.
SELECT id, carrier_account_id
FROM cash.cod_statements
WHERE id = @id
  AND status = 'parsed'
FOR UPDATE;

-- name: MatchFacts :many
-- Mọi dòng của bảng kê theo line_no cùng vận đơn tìm theo (tài khoản hãng của bảng kê, mã vận đơn) (Q-7, VC13: không
-- tìm chỉ theo mã), số dư carrier_holding và đã có remitted chưa bị đảo của vận đơn đó.
SELECT l.id, l.line_no, l.cod_amount, l.fee_amount, l.other_fee, l.posted_transfer_id,
       s.id AS shipment_id, coalesce(s.status = 'delivered' AND NOT s.partial, false)::boolean AS delivered,
       coalesce((SELECT sum(e.amount)
                 FROM cash.cod_ledger_entries e
                 WHERE e.shipment_id = s.id
                   AND e.account = 'carrier_holding'), 0)::bigint AS carrier_holding,
       EXISTS (SELECT 1
               FROM cash.cod_transfers t
               WHERE t.shipment_id = s.id
                 AND t.kind = 'remitted'
                 AND NOT EXISTS (SELECT 1 FROM cash.cod_transfers r WHERE r.reverses_transfer_id = t.id)) AS remitted
FROM cash.cod_statement_lines l
LEFT JOIN fulfillment.shipments_v s
       ON s.carrier_account_id = @carrier_account_id::uuid
      AND s.tracking_code = l.tracking_code
WHERE l.statement_id = @statement_id
ORDER BY l.line_no;

-- name: SetLineResults :exec
-- Ghi kết quả khớp (và bút toán đã ghi) cho các dòng; các mảng cùng độ dài. Mã rỗng (uuid toàn 0) → NULL; has_amount
-- false → expected, diff NULL; bút toán rỗng giữ bút toán cũ của dòng.
UPDATE cash.cod_statement_lines l
SET match_status       = u.status,
    shipment_id        = NULLIF(u.shipment_id, '00000000-0000-0000-0000-000000000000'::uuid),
    expected_amount    = CASE WHEN u.has_amount THEN u.expected END,
    diff_amount        = CASE WHEN u.has_amount THEN u.diff END,
    posted_transfer_id = coalesce(NULLIF(u.posted, '00000000-0000-0000-0000-000000000000'::uuid), l.posted_transfer_id),
    updated_at         = now()
FROM (SELECT unnest(@ids::uuid[])            AS id,
             unnest(@statuses::text[])       AS status,
             unnest(@shipment_ids::uuid[])   AS shipment_id,
             unnest(@has_amounts::boolean[]) AS has_amount,
             unnest(@expected::bigint[])     AS expected,
             unnest(@diffs::bigint[])        AS diff,
             unnest(@posted::uuid[])         AS posted) u
WHERE l.id = u.id;

-- name: MarkMatched :one
-- Bảng kê đã khớp xong: parsed → matched; trả version mới.
UPDATE cash.cod_statements
SET status = 'matched', updated_at = now(), version = version + 1
WHERE id = @id
  AND status = 'parsed'
RETURNING version;

-- name: ConfirmStatement :one
-- Chốt bảng kê khi version còn khớp và đang matched (TI07: người lưu sau → không dòng nào).
UPDATE cash.cod_statements
SET status = 'confirmed', confirmed_by = @confirmed_by, confirmed_at = now(), updated_at = now(), version = version + 1
WHERE id = @id
  AND version = @version
  AND status = 'matched'
RETURNING *;

-- name: PostableNet :one
-- Tổng thực nhận của các dòng sẽ ghi tiền (matched, amount_mismatch, fee_only) để so với tổng khai trên tệp.
SELECT coalesce(sum(net_amount), 0)::bigint
FROM cash.cod_statement_lines
WHERE statement_id = @statement_id
  AND match_status IN ('matched', 'amount_mismatch', 'fee_only');

-- name: PostBatchLines :many
-- Lô dòng có vận đơn kế tiếp sau line_no after (không khóa: chỉ để biết vận đơn cần khóa tư vấn).
SELECT line_no, shipment_id::uuid AS shipment_id
FROM cash.cod_statement_lines
WHERE statement_id = @statement_id
  AND line_no > @after_line_no
  AND shipment_id IS NOT NULL
ORDER BY line_no
LIMIT @lim;

-- name: TouchConfirmed :one
-- Khóa bảng kê đã chốt cho một lô ghi tiền và tăng version (số thứ tự của sự kiện lô sau); chưa chốt → không dòng nào.
UPDATE cash.cod_statements
SET updated_at = now(), version = version + 1
WHERE id = @id
  AND status = 'confirmed'
RETURNING version, confirmed_by;

-- name: LockLineRange :many
-- Khóa các dòng có vận đơn của lô theo line_no tăng dần.
SELECT id
FROM cash.cod_statement_lines
WHERE statement_id = @statement_id
  AND line_no BETWEEN @from_line_no AND @to_line_no
  AND shipment_id IS NOT NULL
ORDER BY line_no
FOR UPDATE;

-- name: PostFacts :many
-- Như MatchFacts cho các dòng có vận đơn của lô (vận đơn đã gắn lúc khớp), đọc sau khóa: số dư mới nhất.
SELECT l.id, l.line_no, l.cod_amount, l.fee_amount, l.other_fee, l.posted_transfer_id,
       s.id AS shipment_id, coalesce(s.status = 'delivered' AND NOT s.partial, false)::boolean AS delivered,
       coalesce((SELECT sum(e.amount)
                 FROM cash.cod_ledger_entries e
                 WHERE e.shipment_id = s.id
                   AND e.account = 'carrier_holding'), 0)::bigint AS carrier_holding,
       EXISTS (SELECT 1
               FROM cash.cod_transfers t
               WHERE t.shipment_id = s.id
                 AND t.kind = 'remitted'
                 AND NOT EXISTS (SELECT 1 FROM cash.cod_transfers r WHERE r.reverses_transfer_id = t.id)) AS remitted
FROM cash.cod_statement_lines l
JOIN fulfillment.shipments_v s ON s.id = l.shipment_id
WHERE l.statement_id = @statement_id
  AND l.line_no BETWEEN @from_line_no AND @to_line_no
ORDER BY l.line_no;
