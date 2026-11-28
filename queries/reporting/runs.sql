-- Lần chạy nền của báo cáo (kế hoạch M6, Task 235 = M6-12). Chạy trong tenancy.WithTenant (RLS của report_runs).
-- Mọi câu đổi trạng thái có điều kiện trên trạng thái hiện tại: hai worker cùng một lần chạy chỉ một bên ghi được.
-- Thứ tự khóa: ADR-46 nhóm 12a (report_runs) trước nhóm 13 (jobs).

-- name: InsertRun :exec
INSERT INTO reporting.report_runs (id, tenant_id, seller_id, family, report, format, params, requested_by, expires_at)
VALUES (@id, @tenant_id, @seller_id, @family, @report, @format, @params, @requested_by, @expires_at);

-- name: SetRunJob :exec
UPDATE reporting.report_runs SET job_id = @job_id WHERE id = @id;

-- name: CountPendingRuns :one
-- Số lần chạy đang chờ hay đang chạy của một người (giới hạn REPORT_MAX_PENDING_PER_USER; chỉ mục report_runs_requester).
SELECT count(*)::int AS n FROM reporting.report_runs
WHERE requested_by = @requested_by AND status IN ('queued', 'running');

-- name: MarkRunning :one
-- Worker nhận lần chạy: queued → running (running → running khi chạy lại sau khi worker chết). 0 dòng: đã kết thúc.
UPDATE reporting.report_runs SET status = 'running', started_at = now(), version = version + 1
WHERE id = @id AND status IN ('queued', 'running')
RETURNING id, seller_id, family, report, format, params, requested_by;

-- name: FinishRunReady :one
-- Ghi kết quả bảng: chỉ lần chạy đang running (một lần chuyển sang ready); 0 dòng → bên khác đã kết thúc.
UPDATE reporting.report_runs
SET status = 'ready', result = @result, row_count = @row_count, finished_at = now(),
    expires_at = now() + make_interval(secs => @ttl_seconds::bigint), version = version + 1
WHERE id = @id AND status = 'running'
RETURNING id;

-- name: FailRun :one
-- Lần chạy hỏng (lỗi nghiệp vụ, hết lượt, hết hạn): chỉ khi còn queued hay running. 0 dòng → đã kết thúc.
UPDATE reporting.report_runs
SET status = 'failed', reason = @reason, finished_at = now(), version = version + 1
WHERE id = @id AND status IN ('queued', 'running')
RETURNING id;

-- name: RunHead :one
-- Trạng thái, họ và người tạo của một lần chạy (tra lại lệnh, báo hiệu).
SELECT status, family, requested_by FROM reporting.report_runs WHERE id = @id;

-- name: GetRun :one
-- Một lần chạy kèm kết quả và tên người tạo (identity.users_v, G15).
SELECT r.id, r.family, r.report, r.format, r.status, r.reason, r.params, r.row_count, r.truncated, r.result,
       r.created_at, r.finished_at, r.expires_at, r.requested_by, coalesce(u.display_name, '')::text AS requested_by_name
FROM reporting.report_runs AS r
LEFT JOIN identity.users_v AS u ON u.id = r.requested_by
WHERE r.id = @id AND r.family = @family;

-- name: ListRunsMine :many
-- Lần chạy của một người trong một họ, mới nhất trước; keyset (created_at, id) giảm dần (chỉ mục report_runs_requester).
SELECT r.id, r.family, r.report, r.format, r.status, r.reason, r.params, r.row_count, r.truncated,
       r.created_at, r.finished_at, r.expires_at, r.requested_by, coalesce(u.display_name, '')::text AS requested_by_name
FROM reporting.report_runs AS r
LEFT JOIN identity.users_v AS u ON u.id = r.requested_by
WHERE r.requested_by = @requested_by AND r.family = @family
  AND (NOT @has_cursor::boolean OR (r.created_at, r.id) < (@cursor_at::timestamptz, @cursor_id::uuid))
ORDER BY r.created_at DESC, r.id DESC
LIMIT @lim;

-- name: ListRunsAll :many
-- Lần chạy của mọi người trong một họ (report.exports.manage); keyset như ListRunsMine (chỉ mục report_runs_family).
SELECT r.id, r.family, r.report, r.format, r.status, r.reason, r.params, r.row_count, r.truncated,
       r.created_at, r.finished_at, r.expires_at, r.requested_by, coalesce(u.display_name, '')::text AS requested_by_name
FROM reporting.report_runs AS r
LEFT JOIN identity.users_v AS u ON u.id = r.requested_by
WHERE r.family = @family
  AND (NOT @has_cursor::boolean OR (r.created_at, r.id) < (@cursor_at::timestamptz, @cursor_id::uuid))
ORDER BY r.created_at DESC, r.id DESC
LIMIT @lim;

-- name: ExpireJSONRuns :many
-- Dọn (a): kết quả bảng quá hạn → expired, bỏ kết quả. Khóa theo id tăng dần, bỏ qua dòng đang bị khóa, tối đa @lim.
WITH c AS (
    SELECT id FROM reporting.report_runs
    WHERE status = 'ready' AND format = 'json' AND expires_at <= now()
    ORDER BY id
    LIMIT @lim
    FOR UPDATE SKIP LOCKED
)
UPDATE reporting.report_runs AS r SET status = 'expired', result = NULL, version = r.version + 1
FROM c WHERE r.id = c.id
RETURNING r.id;

-- name: LockStaleRuns :many
-- Dọn (b): lần chạy còn queued/running quá expires_at + 1 giờ (lưới an toàn khi lệnh đã kết thúc mà lần chạy chưa).
SELECT id, job_id FROM reporting.report_runs
WHERE status IN ('queued', 'running') AND expires_at + interval '1 hour' <= now()
ORDER BY id
LIMIT @lim
FOR UPDATE SKIP LOCKED;

-- name: DeleteOldRuns :execrows
-- Dọn (c): dòng tạo quá 90 ngày bị xóa; khóa theo id tăng dần, bỏ qua dòng đang bị khóa, tối đa @lim.
DELETE FROM reporting.report_runs
WHERE id IN (
    SELECT id FROM reporting.report_runs
    WHERE created_at <= now() - interval '90 days'
    ORDER BY id
    LIMIT @lim
    FOR UPDATE SKIP LOCKED
);
