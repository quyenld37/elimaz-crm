-- Chỉ mục cho danh sách "Giữ hàng quá hạn" (kế hoạch M3, Task M3-22): lọc theo trạng thái và mốc đổi trạng thái của
-- giữ hàng (queries/inventory/aged.sql). M1 chỉ có stock_reservations_balance_state (theo số dư). Pha mở rộng: chỉ
-- thêm chỉ mục. Không CONCURRENTLY: CREATE INDEX CONCURRENTLY chờ mọi phiên đang mở trên database, kể cả lần migrate
-- thứ hai đang chờ khóa migrate (db.Migrate giữ khóa cố vấn suốt lượt) → hai lần migrate song song kẹt nhau
-- (TestMigrateConcurrentRunsApplyOnce); CRM chưa chạy dữ liệu thật, bảng còn nhỏ (như 0211).

-- +goose Up
CREATE INDEX stock_reservations_state_age ON inventory.stock_reservations (tenant_id, state, state_changed_at);
