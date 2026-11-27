-- Báo cáo tồn ngày đã qua (kế hoạch M6, Task 234). Chạy trong tenancy.WithTenant, transaction chỉ đọc của báo cáo.

-- name: SnapshotDayInfo :one
-- Ngày chụp tồn đầu tiên của tenant (chuỗi rỗng khi chưa chụp ngày nào) và ngày @day đã chụp chưa.
SELECT coalesce(min(day)::text, '')::text AS first_day,
       coalesce(bool_or(day = sqlc.arg(day)::text::date), false)::boolean AS taken
FROM reporting.stock_snapshot_days;
