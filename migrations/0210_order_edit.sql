-- Sửa đơn (kế hoạch M2, Task M2-20): crm_app được xóa dòng đơn bị bỏ (0204 mục 9: "Task sửa đơn tự thêm khi cần";
-- dòng giữ hàng của dòng đó đã nhả trước, khóa ngoại ON DELETE SET NULL (order_item_id) từ 0204 giữ lại dòng giữ hàng
-- released và sổ tồn) và đổi số lượng của dòng giữ hàng held (inventory.Writer.ResizeHeld, chỉ khi state = 'held').
-- Pha mở rộng: chỉ GRANT.

-- +goose Up
GRANT DELETE ON ord.order_items TO crm_app;
GRANT UPDATE (quantity) ON inventory.stock_reservations TO crm_app;

