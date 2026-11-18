-- Thứ tự theo đối tượng không chặn sự kiện vào (kế hoạch M0 bản 5, Task 314; spec 04 mục 5.1).
-- Pha mở rộng: thêm cột order_mode và một index duy nhất một phần; cột block_on_dead (0007) ngừng dùng từ bản này
-- (code không ghi, không đọc), xóa ở pha thu hẹp sau.
--   strict      (mặc định, sự kiện nội bộ): mọi việc đứng trước chưa done/expired chặn việc sau, kể cả dead.
--   best_effort (sự kiện vào inbound.*): chỉ việc cùng (tenant_id, order_key) đang running chặn; việc chờ thử lại hay
--               dead không chặn tin sau của cùng hội thoại.
-- Thứ tự khóa (docs/adr/0046-thu-tu-khoa.md nhóm 13) không đổi: vẫn chỉ dòng platform.jobs.

-- +goose Up
ALTER TABLE platform.jobs
    ADD COLUMN order_mode text NOT NULL DEFAULT 'strict' CHECK (order_mode IN ('strict', 'best_effort'));

-- Dòng cũ: block_on_dead = false là sự kiện vào (Task 15) → best_effort; còn lại giữ strict.
UPDATE platform.jobs SET order_mode = 'best_effort' WHERE order_key IS NOT NULL AND NOT block_on_dead;

-- Chốt chặn cuối cho best_effort: không bao giờ hai việc cùng (tenant_id, order_key) cùng running (hai worker đọc ảnh
-- chụp cũ cùng lúc). Lần nhận thua cuộc đua gặp 23505 trên index này và không nhận gì (Worker.claim).
CREATE UNIQUE INDEX jobs_best_effort_running ON platform.jobs (tenant_id, order_key) NULLS NOT DISTINCT
    WHERE status = 'running' AND order_mode = 'best_effort' AND order_key IS NOT NULL;

