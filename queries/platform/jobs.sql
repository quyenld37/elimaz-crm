-- Hàng đợi việc nền (kế hoạch M0 bản 5, Task 11; spec 04 mục 5.1). Chỉ internal/platform/queue dùng.
-- Mọi lần đổi trạng thái của một việc đang chạy có điều kiện status = 'running' AND locked_by = <worker giữ chỗ>: worker
-- đã mất giữ chỗ (bị nhận lại sau khi lease_until qua) không ghi được gì. Thời gian theo đồng hồ của database.

-- Chống trùng tầng 1 (UNIQUE NULLS NOT DISTINCT (tenant_id, kind, dedupe_key)): trùng thì không trả dòng nào.
-- name: InsertJob :one
INSERT INTO platform.jobs (tenant_id, kind, mode, payload, dedupe_key, order_key, order_seq, block_on_dead,
                           on_lease_expired, max_attempts, run_at, expires_at, trace_context)
VALUES (sqlc.narg(tenant_id), sqlc.arg(kind), sqlc.arg(mode), sqlc.arg(payload), sqlc.arg(dedupe_key),
        sqlc.narg(order_key), sqlc.narg(order_seq), sqlc.arg(block_on_dead), sqlc.arg(on_lease_expired),
        sqlc.arg(max_attempts), coalesce(sqlc.narg(run_at)::timestamptz, now()),
        now() + nullif(sqlc.arg(expires_after_ms)::bigint, 0) * interval '1 millisecond', sqlc.narg(trace_context))
ON CONFLICT DO NOTHING
RETURNING id;

-- Câu riêng sau InsertJob (ảnh chụp mới của READ COMMITTED thấy dòng bên kia vừa commit).
-- name: FindJobID :one
SELECT id FROM platform.jobs
WHERE tenant_id IS NOT DISTINCT FROM sqlc.narg(tenant_id) AND kind = @kind AND dedupe_key = @dedupe_key;

-- (1) Giữ chỗ hết hạn, chính sách retry, còn lượt: về hàng chờ, chạy ngay.
-- name: RequeueLeaseExpired :execrows
UPDATE platform.jobs
SET status = 'pending', lease_until = NULL, locked_by = NULL, run_at = now(), last_error = 'lease expired'
WHERE status = 'running' AND lease_until < now() AND on_lease_expired = 'retry' AND attempts < max_attempts;

-- (1) Giữ chỗ hết hạn mà phải kết thúc: chính sách in_doubt, hoặc retry đã hết lượt (→ dead).
-- name: ListLeaseExpiredTerminal :many
SELECT id, tenant_id, kind, payload, attempts, created_at, locked_by, on_lease_expired
FROM platform.jobs
WHERE status = 'running' AND lease_until < now() AND (on_lease_expired = 'in_doubt' OR attempts >= max_attempts)
ORDER BY lease_until, id
LIMIT @lim;

-- Câu cuối của transaction OnTerminal; locked_by là của worker cũ đọc ở ListLeaseExpiredTerminal: nếu việc đã bị
-- nhận lại hay đổi trạng thái thì 0 dòng và transaction lùi.
-- name: FinishLeaseExpired :execrows
UPDATE platform.jobs
SET status = @status, lease_until = NULL, locked_by = NULL, finished_at = now(), last_error = sqlc.arg(last_error)::text
WHERE id = @id AND status = 'running' AND lease_until < now() AND locked_by = sqlc.arg(locked_by)::text;

-- (2) Việc pending đã quá hạn sống.
-- name: ListExpiredPending :many
SELECT id, tenant_id, kind, payload, attempts, created_at
FROM platform.jobs
WHERE status = 'pending' AND expires_at < now()
ORDER BY expires_at, id
LIMIT @lim;

-- name: ExpirePending :execrows
UPDATE platform.jobs
SET status = 'expired', finished_at = now()
WHERE id = @id AND status = 'pending' AND expires_at < now();

-- (3) Nhận việc tới hạn. Việc có order_key chỉ được nhận khi không còn việc nào cùng
-- (tenant_id, order_key) có (order_seq, id) nhỏ hơn đang pending, running, in_doubt hoặc dead có block_on_dead.
-- Việc command (lệnh ra ngoài, Task 12) chỉ được nhận khi worker có commands.Runner (with_commands).
-- name: ClaimJobs :many
WITH picked AS (
    SELECT j.id
    FROM platform.jobs j
    WHERE j.status = 'pending' AND (j.mode = 'transactional' OR sqlc.arg(with_commands)::boolean) AND j.run_at <= now()
      AND (j.expires_at IS NULL OR j.expires_at >= now())
      AND (j.order_key IS NULL OR NOT EXISTS (
          SELECT 1 FROM platform.jobs b
          WHERE b.tenant_id IS NOT DISTINCT FROM j.tenant_id AND b.order_key = j.order_key
            AND (b.order_seq, b.id) < (j.order_seq, j.id)
            AND b.status NOT IN ('done', 'expired')
            AND (b.status <> 'dead' OR b.block_on_dead)
      ))
    ORDER BY j.run_at, j.id
    LIMIT @lim
    FOR UPDATE OF j SKIP LOCKED
)
UPDATE platform.jobs AS u
SET status = 'running', attempts = u.attempts + 1, lease_until = now() + sqlc.arg(lease_ms)::bigint * interval '1 millisecond',
    locked_by = sqlc.arg(locked_by)::text
FROM picked
WHERE u.id = picked.id
RETURNING u.id, u.tenant_id, u.kind, u.mode, u.payload, u.attempts, u.max_attempts, u.created_at, u.trace_context,
    (extract(epoch FROM now() - u.run_at) * 1000)::float8 AS pickup_ms;

-- (4) Câu cuối của transaction của handler.
-- name: CompleteJob :execrows
UPDATE platform.jobs
SET status = 'done', result = sqlc.narg(result), finished_at = now(), lease_until = NULL, locked_by = NULL,
    last_error = NULL
WHERE id = @id AND status = 'running' AND locked_by = sqlc.arg(locked_by)::text;

-- Lỗi tạm: hẹn chạy lại sau delay_ms.
-- name: RescheduleJob :execrows
UPDATE platform.jobs
SET status = 'pending', run_at = now() + sqlc.arg(delay_ms)::bigint * interval '1 millisecond', last_error = sqlc.arg(last_error)::text,
    lease_until = NULL, locked_by = NULL
WHERE id = @id AND status = 'running' AND locked_by = sqlc.arg(locked_by)::text;

-- Lỗi vĩnh viễn, hết lượt hoặc không có handler: dead. Câu cuối của transaction OnTerminal.
-- name: KillJob :execrows
UPDATE platform.jobs
SET status = 'dead', last_error = sqlc.arg(last_error)::text, finished_at = now(), lease_until = NULL, locked_by = NULL
WHERE id = @id AND status = 'running' AND locked_by = sqlc.arg(locked_by)::text;

-- Tắt êm hủy việc chưa xong: về hàng chờ, trả lại lần thử vừa tính.
-- name: ReleaseJob :execrows
UPDATE platform.jobs
SET status = 'pending', attempts = attempts - 1, lease_until = NULL, locked_by = NULL
WHERE id = @id AND status = 'running' AND locked_by = sqlc.arg(locked_by)::text;

-- Tuổi (giây) của việc pending tới hạn lâu nhất; 0 khi không có.
-- name: OldestPendingAgeSeconds :one
SELECT coalesce(extract(epoch FROM now() - min(run_at)), 0)::float8 AS age_s
FROM platform.jobs
WHERE status = 'pending' AND run_at <= now();

-- name: ListenJobs :exec
LISTEN jobs;

-- Quản lý việc qua API (Task 12, platform.jobs.manage). Bảng không RLS (Global Constraints): mọi câu lọc tường minh
-- tenant_id của người gọi, chạy trong WithTenant của người gọi. Không trả payload.

-- name: ListAttentionJobs :many
SELECT id, kind, status, attempts, last_error, created_at
FROM platform.jobs
WHERE tenant_id = @tenant_id AND status = ANY (sqlc.arg(statuses)::text[])
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- Khóa dòng việc của tenant trước khi đổi (thứ tự khóa ADR-46 ngoại lệ (c): jobs trước idempotency_keys).
-- name: LockTenantJob :one
SELECT id, kind, status, attempts, last_error, created_at
FROM platform.jobs
WHERE id = @id AND tenant_id = @tenant_id
FOR UPDATE;

-- Chạy lại do người quyết: dead, in_doubt, expired → pending, chạy ngay, lượt về 0, bỏ hạn sống.
-- name: RequeueTenantJob :one
UPDATE platform.jobs
SET status = 'pending', attempts = 0, run_at = now(), expires_at = NULL, lease_until = NULL, locked_by = NULL,
    finished_at = NULL
WHERE id = @id AND tenant_id = @tenant_id AND status IN ('dead', 'in_doubt', 'expired')
RETURNING id, kind, status, attempts, last_error, created_at;

-- Bỏ việc do người quyết: dead, in_doubt → expired.
-- name: AbandonTenantJob :one
UPDATE platform.jobs
SET status = 'expired', finished_at = now()
WHERE id = @id AND tenant_id = @tenant_id AND status IN ('dead', 'in_doubt')
RETURNING id, kind, status, attempts, last_error, created_at;
