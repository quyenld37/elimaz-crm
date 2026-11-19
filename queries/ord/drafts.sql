-- Đơn nháp hết hạn (kế hoạch M2, Task M2-21; Q8). Chạy trong tenancy.WithTenant của việc nền order.expire_drafts.

-- name: LockExpiredDrafts :many
-- Đơn nháp quá hạn của tenant (mọi người bán: việc của hệ thống), theo mã đơn tăng dần, khóa dòng (ADR-46 nhóm 7);
-- đơn đang bị khóa (nhân viên đang chốt, sửa) bị bỏ qua tới kỳ sau. Index orders_draft_expires.
SELECT id, seller_id
FROM ord.orders
WHERE status = 'draft' AND draft_expires_at <= @now::timestamptz
ORDER BY id
LIMIT @max_rows
FOR UPDATE SKIP LOCKED;
