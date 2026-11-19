-- Index danh sách đơn mặc định (kế hoạch M2, Task M2-24): mọi trạng thái của một người bán, mới nhất trước, keyset
-- (created_at, id) giảm dần. orders_status_created chỉ phục vụ khi lọc trạng thái; orders_open_created chỉ trạng thái
-- mở. Pha mở rộng: chỉ thêm index. Không CONCURRENTLY: CRM chưa chạy dữ liệu thật, bảng còn nhỏ.

-- +goose Up
CREATE INDEX orders_seller_created ON ord.orders (tenant_id, seller_id, created_at DESC, id DESC);
