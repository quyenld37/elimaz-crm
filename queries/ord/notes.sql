-- Ghi chú nội bộ của đơn (kế hoạch M2, Task M2-24): chỉ ghi thêm (trigger chặn sửa, xóa); không bao giờ gửi cho hãng
-- (ghi chú cho hãng là ord.orders.carrier_note). Chạy trong tenancy.WithTenant.

-- name: InsertOrderNote :one
INSERT INTO ord.order_notes (tenant_id, order_id, body, author_id, created_at)
VALUES (NULLIF(current_setting('app.tenant_id', true), '')::uuid, @order_id, @body, @author_id, @created_at)
RETURNING id;
