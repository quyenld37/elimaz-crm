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
-- bắt đầu sau id của dòng cuối trang trước.
SELECT *
FROM cash.cod_statements
WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
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

-- name: FinishParse :execrows
-- Đọc tệp xong: parsed, số dòng và kỳ bảng kê (ngày chuyển nhỏ nhất, lớn nhất) đếm từ dòng đã ghi; chỉ khi chưa
-- đọc xong.
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
  AND s.status IN ('imported', 'parsing');

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
-- Dòng của bảng kê theo số dòng tăng dần, lọc tùy chọn theo kết quả khớp; trang sau bắt đầu sau số dòng cuối.
SELECT *
FROM cash.cod_statement_lines
WHERE statement_id = @statement_id
  AND (sqlc.narg(match_status)::text IS NULL OR match_status = sqlc.narg(match_status)::text)
  AND line_no > @after_line_no
ORDER BY line_no
LIMIT @lim;

-- name: StatementTotals :many
-- Số dòng và tổng tiền COD theo kết quả khớp của mọi dòng của bảng kê.
SELECT match_status, count(*)::int AS lines, coalesce(sum(cod_amount), 0)::bigint AS cod_amount
FROM cash.cod_statement_lines
WHERE statement_id = @statement_id
GROUP BY match_status
ORDER BY match_status;
