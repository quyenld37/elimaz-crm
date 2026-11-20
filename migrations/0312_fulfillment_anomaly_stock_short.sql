-- Vận đơn cần xem loại stock_short (kế hoạch M3, Task M3-11, Q-T91-11): lệnh tra trạng thái gặp tồn thực không đủ khi
-- áp lấy hàng, giao (kiểm kho ghi thấp hơn phần giữ, Task 406) thì mở bất thường stock_short thay cho thử lại vô ích.
-- Pha mở rộng: chỉ nới CHECK kind (giữ đủ mười một giá trị của 0300, thêm một). Số 0312: 0310, 0311 để cho các task
-- M3 chạy song song; goose cho chạy lệch số (Task 318). Không khóa bảng lâu: bảng còn nhỏ, CHECK mới rộng hơn cũ.

-- +goose Up
ALTER TABLE fulfillment.shipment_anomalies
    DROP CONSTRAINT shipment_anomalies_kind_check,
    ADD CONSTRAINT shipment_anomalies_kind_check CHECK (kind IN ('in_doubt_create', 'in_doubt_cancel', 'manual_cancel',
        'invalid_edge', 'unmapped_status', 'tracking_conflict', 'cancel_conflict', 'partial_reported', 'lost_or_damaged',
        'missing_items', 'not_current_moved', 'stock_short'));
