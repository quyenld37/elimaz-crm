-- Loại hủy staff_after_unknown (kế hoạch M3, Task M3-14, Q-T91-12): vận đơn creating kẹt (không rõ hãng đã tạo chưa,
-- hay lệnh tạo bị bỏ) mà nhân viên xác nhận đã tra trang hãng, hãng không có kiện → cancelled với loại này. Pha mở
-- rộng: chỉ nới CHECK cancel_kind (giữ đủ năm giá trị của 0300, thêm một; hợp đồng Shipment.cancelKind đã liệt kê).
-- Bảng còn nhỏ, CHECK mới rộng hơn cũ: không khóa lâu.

-- +goose Up
ALTER TABLE fulfillment.shipments
    DROP CONSTRAINT shipments_cancel_kind_check,
    ADD CONSTRAINT shipments_cancel_kind_check
        CHECK (cancel_kind IN ('order_cancel', 'replace', 'create_rejected', 'carrier_cancelled', 'manual',
            'staff_after_unknown'));
