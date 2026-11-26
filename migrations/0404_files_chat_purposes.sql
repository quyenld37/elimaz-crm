-- Mục đích tệp của tin khách gửi vào hộp chat (kế hoạch M4, Task 155 / M4-15; TP02, TP04, CH08; R5, R10). Pha mở
-- rộng: chỉ một câu INSERT vào danh mục, không đụng CHECK hay bảng platform.files.
-- - chat_image: ảnh khách gửi; chế độ private (R10): chỉ file.process_private của M1 xử lý, biến thể ở bucket private.
-- - chat_attachment: tệp không phải ảnh (mọi loại, máy chủ ghi), chỉ cho tải về.
-- - chat_avatar: ảnh đại diện của khách đọc từ hồ sơ kênh.
-- chat_outgoing (ảnh nhân viên gửi khách) do Task 156 thêm ở 0413. conversation.RegisterFilePurposes khai bên Go.

-- +goose Up
INSERT INTO platform.file_purposes (key, bucket, mode)
VALUES ('chat_image', 'private', 'private'), ('chat_attachment', 'private', 'standard'),
       ('chat_avatar', 'private', 'standard')
ON CONFLICT DO NOTHING;
