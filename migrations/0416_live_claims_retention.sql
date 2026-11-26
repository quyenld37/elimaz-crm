-- Dọn theo hạn giữ 24 tháng (Task 164, Q17) với ghi nhận chốt từ comment live (Task 172, 0410). Pha mở rộng: chỉ cấp
-- thêm DELETE cho crm_app; không thu hẹp gì.
-- live_comment_claims có khóa ngoại tới conversation.messages (message_id, ask_message_id) không ON DELETE: việc dọn
-- xóa ghi nhận của các tin trong lô trước khi xóa tin (như message_revisions), không thì DELETE tin lỗi 23503 và việc
-- dọn của tenant đứng ở tin đó. Đơn nháp (order_id) không bị đụng. Số 0416: số kế tiếp còn trống trong dải M4 lúc gộp
-- Task 172 vào m4 (kế hoạch M4, Global Constraints).
-- Thứ tự khóa: nhóm 6 ADR-46 (… live_comment_claims → messages …), không thêm bảng.

-- +goose Up
GRANT DELETE ON conversation.live_comment_claims TO crm_app;
