-- Index cho danh sách nhật ký thao tác (kế hoạch M5, Task 457 / M5-45; queries/identity/audit_list.sql): mới nhất trước
-- theo (created_at, id), lọc theo hành động hay người làm. Index có sẵn audit_logs_entity (0004) chỉ phục vụ lọc theo
-- thực thể; không có index này thì danh sách không lọc phải quét cả bảng audit rồi xếp (đo bằng EXPLAIN trên 100.000
-- dòng, TestListAuditLogs_Plan). Pha mở rộng: chỉ thêm index. Không CONCURRENTLY (cùng lý do 0307: kẹt với lần migrate
-- song song; CRM chưa chạy dữ liệu thật, bảng còn nhỏ).

-- +goose Up
CREATE INDEX audit_logs_created ON identity.audit_logs (tenant_id, created_at DESC, id DESC);
CREATE INDEX audit_logs_action ON identity.audit_logs (tenant_id, action, created_at DESC, id DESC);
CREATE INDEX audit_logs_actor ON identity.audit_logs (tenant_id, actor_id, created_at DESC, id DESC)
    WHERE actor_id IS NOT NULL;
