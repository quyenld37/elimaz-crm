-- Câu nhận việc không quét cả bảng jobs; hạn giữ việc xong (kế hoạch M0, Task 394; thử tải M0 phát hiện 2, 4).
-- Pha mở rộng: chỉ thêm index. Index cũ jobs_order (0007) không còn câu nào dùng từ bản này (ClaimJobs so tenant bằng
-- biểu thức coalesce bên dưới); xóa ở pha thu hẹp sau.
-- Thứ tự khóa (docs/adr/0046-thu-tu-khoa.md nhóm 13) không đổi.

-- +goose Up
-- Tìm việc đứng trước cùng (tenant, order_key) còn chặn. tenant_id NULL (việc chưa định tuyến, việc toàn hệ thống) đổi
-- thành uuid nil để so bằng "=" dùng được index (IS NOT DISTINCT FROM không dùng được index); uuid nil không bao giờ là
-- id của tenant (platform.tenants.id sinh bằng uuidv7()).
CREATE INDEX jobs_order_key ON platform.jobs
    ((coalesce(tenant_id, '00000000-0000-0000-0000-000000000000'::uuid)), order_key, order_seq, id)
    WHERE order_key IS NOT NULL AND status NOT IN ('done', 'expired');
