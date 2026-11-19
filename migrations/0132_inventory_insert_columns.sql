-- Quyền INSERT theo cột của số dư và giữ hàng (Task 414, phát hiện A-02 của review độc lập M1). 0115 cho crm_app
-- INSERT mọi cột của inventory.stock_balances và inventory.stock_reservations, nên tiến trình ứng dụng chèn được dòng
-- số dư có on_hand ≠ 0 (hay giữ hàng đã consumed) mà không có dòng sổ: lớp database không giữ được luật "số dư chỉ đổi
-- bằng câu UPDATE có điều kiện và sổ" (ADR-07), lệch chỉ lộ ra ở lần đối chiếu đêm.
-- Sau tệp này crm_app chỉ chèn được đúng các cột mà mã chèn: EnsureBalance (queries/inventory/balances.sql: khóa,
-- số 0 mặc định) và InsertReservation (queries/inventory/reservations.sql: trạng thái mặc định held). Cột số lượng,
-- version, trạng thái, mốc thời gian lấy mặc định. Quyền SELECT, UPDATE theo cột của 0115 giữ nguyên; stock_ledger
-- vẫn INSERT mọi cột (sổ chỉ ghi thêm, mỗi dòng là dữ liệu do mã ghi).
-- Thu hẹp quyền, tương thích ngược: bản mã đang chạy không chèn cột nào ngoài danh sách dưới.

-- +goose Up
-- contract: không bản mã nào chèn stock_balances ngoài cột khóa, hay stock_reservations ngoài 7 cột dưới (A-02)
REVOKE INSERT ON inventory.stock_balances, inventory.stock_reservations FROM crm_app;
GRANT INSERT (tenant_id, variant_id, warehouse_id, seller_id) ON inventory.stock_balances TO crm_app;
GRANT INSERT (id, tenant_id, order_item_id, variant_id, warehouse_id, quantity, expires_at)
    ON inventory.stock_reservations TO crm_app;
