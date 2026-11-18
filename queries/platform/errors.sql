-- Gom lỗi ứng dụng theo dấu vân tay (kế hoạch M0 bản 5, Task 20). Chỉ internal/platform/observability dùng.

-- Ghi một lần lỗi: dòng mới với count = 1, hoặc cộng 1 vào dòng cùng dấu vân tay. Mẫu mã yêu cầu, mã việc giữ lần
-- gần nhất có giá trị (log giữ 30 ngày, mẫu mới dễ tra hơn); tên, nội dung, khung gọi giữ lần đầu. is_new: dòng vừa
-- được chèn (chỉ lần chèn có count = 1, vì mọi lần cập nhật đều cộng thêm).
-- name: UpsertErrorEvent :one
INSERT INTO platform.error_events AS e (fingerprint, role, name, message, top_frames, sample_request_id, sample_job_id)
VALUES (@fingerprint, @role, @name, @message, @top_frames, sqlc.narg(sample_request_id), sqlc.narg(sample_job_id))
ON CONFLICT (fingerprint) DO UPDATE
SET count             = e.count + 1,
    last_seen_at      = now(),
    sample_request_id = COALESCE(EXCLUDED.sample_request_id, e.sample_request_id),
    sample_job_id     = COALESCE(EXCLUDED.sample_job_id, e.sample_job_id)
RETURNING (e.count = 1)::boolean AS is_new;

-- Dấu vân tay lần đầu thấy từ since và chưa từng được báo (luật new-error): chưa có alert_events của key
-- 'new-error:' || <prefix_len ký tự đầu>. Lần gửi hỏng hay lịch chạy trễ thì lượt sau vẫn thấy (không mất cảnh báo).
-- name: ListNewErrorFingerprints :many
SELECT e.fingerprint
FROM platform.error_events e
WHERE e.first_seen_at >= @since
  AND NOT EXISTS (
      SELECT 1 FROM platform.alert_events a
      WHERE a.key = 'new-error:' || left(e.fingerprint, sqlc.arg(prefix_len)::int)
  )
ORDER BY e.first_seen_at, e.fingerprint;

-- Tổng counter errors của mọi tiến trình trong các phút bắt đầu từ since trở đi (luật error-spike; metrics_minute ghi
-- mỗi phút, bucket là đầu phút).
-- name: SumErrorsSince :one
SELECT coalesce(sum(count), 0)::bigint AS total
FROM platform.metrics_minute
WHERE name = 'errors' AND bucket >= @since;

-- Lỗi gặp từ since, nhiều lần nhất trước (tóm tắt sức khỏe: 10 dòng, 24 giờ).
-- name: ListTopErrors :many
SELECT fingerprint, role, name, message, count, first_seen_at, last_seen_at
FROM platform.error_events
WHERE last_seen_at >= @since
ORDER BY count DESC, last_seen_at DESC, fingerprint
LIMIT @max_rows;
