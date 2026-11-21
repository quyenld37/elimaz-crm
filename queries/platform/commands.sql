-- Lệnh ra ngoài (kế hoạch M0 bản 5, Task 12; spec 04 mục 5.3). Chỉ internal/platform/commands dùng.
-- Ghi kết quả của một lệnh đang chạy có điều kiện status = 'running' AND locked_by = <worker giữ chỗ> (như jobs.sql);
-- lệnh done, dead, về hàng chờ khi chưa gửi dùng lại CompleteJob, KillJob, ReleaseJob của jobs.sql.

-- Chắc chắn chưa gửi, còn lượt, chưa quá hạn sống: hẹn lại sau delay_ms. 0 dòng → bên gọi đặt dead.
-- keep_attempt (Outcome.KeepAttempt, lỗi hạ tầng tạm): trả lại lượt tính lúc nhận việc (như ReleaseJob) và không xét
-- hết lượt; hạn sống vẫn xét.
-- name: RescheduleNotSent :execrows
UPDATE platform.jobs
SET status = 'pending', run_at = now() + sqlc.arg(delay_ms)::bigint * interval '1 millisecond',
    last_error = sqlc.arg(last_error)::text, lease_until = NULL, locked_by = NULL,
    attempts = attempts - CASE WHEN sqlc.arg(keep_attempt)::boolean THEN 1 ELSE 0 END
WHERE id = @id AND status = 'running' AND locked_by = sqlc.arg(locked_by)::text
  AND (sqlc.arg(keep_attempt)::boolean OR attempts < max_attempts) AND (expires_at IS NULL OR expires_at > now());

-- Chắc chắn chưa gửi: lệnh đã quá hạn của loại chưa (cùng now() của transaction với RescheduleNotSent nên hai câu không
-- trái nhau). Không khóa dòng: hook OnTerminal chạy trước câu đổi jobs (ADR-46).
-- name: CommandPastExpiry :one
SELECT coalesce(expires_at <= now(), false)::bool AS past FROM platform.jobs WHERE id = @id;

-- Chắc chắn chưa gửi và đã quá hạn của loại: expired (spec 04 mục 5.3), không dead. Câu cuối của transaction OnTerminal.
-- name: ExpireNotSent :execrows
UPDATE platform.jobs
SET status = 'expired', last_error = sqlc.arg(last_error)::text, finished_at = now(), lease_until = NULL,
    locked_by = NULL
WHERE id = @id AND status = 'running' AND locked_by = sqlc.arg(locked_by)::text;

-- Không biết đã gửi chưa: in_doubt, không tự gửi lại. Câu cuối của transaction OnTerminal.
-- name: MarkInDoubt :execrows
UPDATE platform.jobs
SET status = 'in_doubt', last_error = sqlc.arg(last_error)::text, finished_at = now(), lease_until = NULL,
    locked_by = NULL
WHERE id = @id AND status = 'running' AND locked_by = sqlc.arg(locked_by)::text;

-- Tra lại lệnh in_doubt của các loại có Looker; finished_at là dấu của lần thành in_doubt này.
-- name: ListInDoubt :many
SELECT id, tenant_id, kind, payload, attempts, created_at, finished_at
FROM platform.jobs
WHERE status = 'in_doubt' AND kind = ANY (sqlc.arg(kinds)::text[])
ORDER BY finished_at, id
LIMIT @lim;

-- Kết quả tra chỉ áp cho đúng lần in_doubt đã thấy (finished_at không đổi): việc đã được chạy lại rồi lại in_doubt
-- trong lúc tra thì 0 dòng, không áp kết quả cũ.
-- name: ResolveInDoubtDone :execrows
UPDATE platform.jobs
SET status = 'done', finished_at = now(), last_error = NULL
WHERE id = @id AND status = 'in_doubt' AND finished_at = sqlc.arg(seen_finished_at)::timestamptz;

-- name: ResolveInDoubtRequeue :execrows
UPDATE platform.jobs
SET status = 'pending', run_at = now(), finished_at = NULL, last_error = 'in_doubt: tra thấy chưa gửi'
WHERE id = @id AND status = 'in_doubt' AND finished_at = sqlc.arg(seen_finished_at)::timestamptz;

-- Q-5 (Task M2-33): handler không tự gửi lại (AutoRetryNotSent = false) mà tra thấy chắc chắn chưa gửi → dead, người
-- quyết gửi lại (04 mục 5.3). Cùng điều kiện "đúng lần in_doubt đã thấy" như hai câu trên.
-- name: ResolveInDoubtDead :execrows
UPDATE platform.jobs
SET status = 'dead', finished_at = now(), last_error = 'confirmed_not_sent'
WHERE id = @id AND status = 'in_doubt' AND finished_at = sqlc.arg(seen_finished_at)::timestamptz;

-- Bỏ lệnh do mã nghiệp vụ quyết (commands.Abandon): pending, in_doubt → expired. Chỉ lệnh (mode command) của tenant
-- trong ngữ cảnh của transaction (jobs không RLS: lọc tường minh; ngoài ngữ cảnh chỉ thấy lệnh không tenant). Câu cuối
-- của transaction nghiệp vụ (ADR-46 nhóm 13).
-- name: AbandonCommand :execrows
UPDATE platform.jobs
SET status = 'expired', finished_at = now(), last_error = sqlc.arg(last_error)::text, lease_until = NULL,
    locked_by = NULL
WHERE id = @id AND mode = 'command' AND status IN ('pending', 'in_doubt')
  AND tenant_id IS NOT DISTINCT FROM NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- Trạng thái của một lệnh trong ngữ cảnh tenant như AbandonCommand (không có dòng: không có hoặc của tenant khác).
-- name: CommandStatus :one
SELECT status FROM platform.jobs
WHERE id = @id AND mode = 'command'
  AND tenant_id IS NOT DISTINCT FROM NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- Người quyết kết quả của lệnh (Task M3-12, commands.ResolveManually). Đọc không khóa (ADR-46: dòng nghiệp vụ của bên
-- gọi khóa trước, platform.jobs sửa cuối); cùng ngữ cảnh tenant như AbandonCommand.
-- name: CommandForResolve :one
SELECT id, tenant_id, kind, payload, attempts, created_at, status
FROM platform.jobs
WHERE id = @id AND mode = 'command'
  AND tenant_id IS NOT DISTINCT FROM NULLIF(current_setting('app.tenant_id', true), '')::uuid;

-- Lệnh in_doubt hay dead → done kèm kết quả người nhập; trạng thái đã đổi từ lúc đọc → 0 dòng.
-- name: ResolveCommandManually :execrows
UPDATE platform.jobs
SET status = 'done', result = sqlc.arg(result)::jsonb, finished_at = now(), last_error = NULL, lease_until = NULL,
    locked_by = NULL
WHERE id = @id AND mode = 'command' AND status IN ('in_doubt', 'dead')
  AND tenant_id IS NOT DISTINCT FROM NULLIF(current_setting('app.tenant_id', true), '')::uuid;
