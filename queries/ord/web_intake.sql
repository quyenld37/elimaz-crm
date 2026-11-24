-- Tạm dừng nhận đơn web (kế hoạch M5, Task 456). Một dòng mỗi tenant; chạy trong tenancy.WithTenant (RLS).

-- name: GetWebIntake :one
-- Trạng thái nhận đơn web, không khóa: tạo đơn web (order.Writer.CreateFromWeb) và đường đọc dùng câu này. Tên người
-- đổi do lớp HTTP đọc qua identity (order không gọi identity, 04 mục 3).
SELECT paused, reason, changed_by, changed_at, version
FROM ord.web_intake;

-- name: EnsureWebIntake :exec
-- Chèn dòng mặc định (đang nhận đơn, version 1) nếu tenant chưa có; RLS (WITH CHECK) từ chối tenant_id khác ngữ cảnh.
INSERT INTO ord.web_intake (tenant_id)
VALUES (@tenant_id)
ON CONFLICT (tenant_id) DO NOTHING;

-- name: LockWebIntake :one
-- Khóa dòng của tenant (nhóm 7 ADR-46, bảng cuối nhóm) trước khi so version (order.Writer.SetWebIntake).
SELECT paused, reason, changed_by, changed_at, version
FROM ord.web_intake
FOR UPDATE;

-- name: UpdateWebIntake :one
UPDATE ord.web_intake
SET paused     = @paused::boolean,
    reason     = sqlc.narg(reason)::text,
    changed_by = @changed_by::uuid,
    changed_at = @changed_at::timestamptz,
    version    = version + 1
WHERE version = @version::int
RETURNING paused, reason, changed_by, changed_at, version;
