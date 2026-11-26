-- Trả lời comment, nhắn riêng, ẩn/hiện comment (kế hoạch M4, Task 158 / M4-18). Pha mở rộng: chỉ nới thân hàm
-- trigger chặn xóa tin; không thu hẹp gì.
-- Gộp câu trả lời comment của CRM về một dòng: webhook feed của chính câu trả lời có thể tới trước khi lệnh
-- command.fb.reply_comment ghi xong, inbound chèn dòng page_external cùng mã comment. OnDone của lệnh xóa dòng đó và giữ
-- dòng của CRM (có sender_staff_id). Trigger của 0413 chỉ cho xóa own_echo_unmatched hay khi dọn theo hạn giữ; nay cho
-- thêm comment page_external khi transaction đặt app.merge_reply = on (chỉ OnDone, Lookup của lệnh trả lời comment đặt
-- cờ, đặt lại off ngay sau câu xóa; tệp task-158, Quyết định 2). Mọi tin khác vẫn bị chặn; RLS áp cả DELETE.
-- Thứ tự khóa: không thêm bảng; xóa dòng tin nằm trong nhóm 6 sau khi đã khóa hội thoại (docs/adr/0046-thu-tu-khoa.md).

-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION conversation.messages_delete_guard()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    IF OLD.origin = 'own_echo_unmatched' OR current_setting('app.retention', true) = 'on'
        OR (OLD.origin = 'page_external' AND OLD.kind = 'comment' AND current_setting('app.merge_reply', true) = 'on') THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'conversation.messages: chỉ xóa được tin own_echo_unmatched (xóa % bị chặn)', OLD.origin
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd
