-- Trường còn thiếu cho các màn hộp chat (Task 501, sửa sau gộp M4 đợt 4). Pha mở rộng: chỉ ADD COLUMN có giá trị mặc
-- định hoặc NULL và một dòng danh mục mục đích tệp; không thu hẹp gì. Số 0417: số kế tiếp còn trống trong dải M4.
-- - posts.image_file_id: ảnh bài (full_picture của kênh) chép về bucket private khi đọc bài (lệnh command.fb.fetch_post);
--   không khóa ngoại tới platform.files (như conversations.avatar_file_id).
-- - conversations.created_customer: hồ sơ khách được tạo tự động từ chính hội thoại này (CH08 "Khách tiềm năng mới").
-- - message_revisions.reason: lý do của dòng tự ẩn (source automation): phone (chế độ phone_only, comment có SĐT), all
--   (ẩn mọi comment). Dòng cũ, dòng nguồn khác: NULL.
-- - file_purposes chat_post_image: ảnh bài viết của Page (standard, private bucket, không xử lý bằng imager).
-- Thứ tự khóa: không thêm bảng (nhóm 6 ADR-46 giữ nguyên).

-- +goose Up
ALTER TABLE conversation.posts ADD COLUMN image_file_id uuid;
ALTER TABLE conversation.conversations ADD COLUMN created_customer boolean NOT NULL DEFAULT false;
ALTER TABLE conversation.message_revisions
    ADD COLUMN reason text CONSTRAINT message_revisions_reason_check CHECK (reason IN ('phone', 'all'));

INSERT INTO platform.file_purposes (key, bucket, mode)
VALUES ('chat_post_image', 'private', 'standard')
ON CONFLICT DO NOTHING;

