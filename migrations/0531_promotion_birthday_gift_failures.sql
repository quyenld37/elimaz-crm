-- Quà sinh nhật website gửi lỗi (Task 491; website Task 213 việc phát hiện 7). Pha mở rộng: chỉ ADD COLUMN có mặc định,
-- CHECK, GRANT UPDATE cột mới.
-- - birthday_gifts.send_failures: số lần website báo không kênh nào tới được khách (webBirthdayGiftFailed).
-- - birthday_gifts.retry_at: trước lúc này quà không vào outbox của website; NULL khi chưa lỗi hay đã hết lượt tự gửi.
--   Quà hết lượt (promotion.maxGiftSendFailures) rời outbox nhưng vẫn ở tab "Cần nhắn" cho nhân viên.
-- - Không dữ liệu cá nhân. Thứ tự khóa không đổi: coupons → birthday_gifts (ADR-46 nhóm 10).

-- +goose Up
ALTER TABLE promotion.birthday_gifts
    ADD COLUMN send_failures int NOT NULL DEFAULT 0 CHECK (send_failures >= 0),
    ADD COLUMN retry_at timestamptz;

GRANT UPDATE (send_failures, retry_at) ON promotion.birthday_gifts TO crm_app;
