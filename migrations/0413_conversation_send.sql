-- Gửi tin của nhân viên (kế hoạch M4, Task 156 / M4-16). Pha mở rộng: thêm dòng danh mục, thêm quyền DELETE kèm
-- trigger chặn; không thu hẹp gì.
-- (a) Mục đích tệp chat_outgoing (ảnh nhân viên tải lên để gửi khách): bucket originals, chế độ standard, xử lý bằng
--     file.process của M1 (biến thể công khai img/<sha>/…); conversation.RegisterFilePurposes đăng ký bên Go (R5;
--     tệp task-156, Quyết định 1).
-- (b) Gộp echo của CRM về một dòng: lệnh gửi ghi xong thì xóa dòng own_echo_unmatched cùng mid (Q23). crm_app được
--     DELETE trên conversation.messages, nhưng trigger theo dòng chỉ cho xóa tin own_echo_unmatched, hoặc khi
--     transaction đặt app.retention = on (việc dọn theo hạn giữ, Task 164); mọi tin khác bị chặn (tệp task-156,
--     Quyết định 2). RLS của bảng áp cả DELETE.
-- Thứ tự khóa: không thêm bảng; xóa dòng tin nằm trong nhóm 6 sau khi đã khóa hội thoại (docs/adr/0046-thu-tu-khoa.md).

-- +goose Up
INSERT INTO platform.file_purposes (key, bucket, mode)
VALUES ('chat_outgoing', 'originals', 'standard')
ON CONFLICT DO NOTHING;

-- +goose StatementBegin
CREATE FUNCTION conversation.messages_delete_guard()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    IF OLD.origin = 'own_echo_unmatched' OR current_setting('app.retention', true) = 'on' THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'conversation.messages: chỉ xóa được tin own_echo_unmatched (xóa % bị chặn)', OLD.origin
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER messages_delete_guard
    BEFORE DELETE ON conversation.messages
    FOR EACH ROW EXECUTE FUNCTION conversation.messages_delete_guard();

GRANT DELETE ON conversation.messages TO crm_app;
