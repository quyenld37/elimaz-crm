-- Mẫu đúng: chỉ đọc trạng thái trong WHERE, đổi cột khác (packing_status không phải status).

-- name: Attach :execrows
UPDATE ord.orders
SET current_shipment_id = @shipment_id, packing_status = 'none'
WHERE id = @id AND status = 'confirmed';
