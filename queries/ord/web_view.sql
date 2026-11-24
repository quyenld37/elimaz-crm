-- Khách hủy đơn web (kế hoạch M5, Task 196 / M5-13). Chạy trong tenancy.WithTenant (RLS). Phần vận đơn và luồng
-- thay đổi của đơn web đọc ở internal/queries/weborders (ghép view của ord và fulfillment; order không gọi fulfillment).

-- name: OpenCancelRequestRef :one
-- Mã yêu cầu hủy đang mở của đơn order_id (mỗi đơn tối đa một, index của 0504).
SELECT ref
FROM ord.order_requests
WHERE order_id = @order_id AND kind = 'cancel' AND status IN ('received', 'in_review', 'needs_info');
