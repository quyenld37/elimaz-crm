-- Lịch sử tin cũ và hạn giữ hội thoại (kế hoạch M4, Task 164 / M4-24; Q33, Q17). Pha mở rộng: chỉ nới ràng buộc,
-- thay thân hàm trigger, INDEX, GRANT; không thu hẹp gì.
-- - messages_direction_origin: tin lịch sử (origin = history, Q33) có cả tin khách (direction in) lẫn tin Page
--   (direction out); ràng buộc cũ đòi in ⇔ customer nên tin khách đọc lại không ghi được. Ràng buộc mới chỉ nới:
--   mọi dòng thỏa ràng buộc cũ đều thỏa ràng buộc mới (tệp task-164, Quyết định 1).
-- - conversation_assignments: chỉ ghi thêm như cũ; DELETE được khi transaction đặt app.retention = on (dọn hội thoại
--   quá hạn giữ, cùng cờ của message_revisions, messages ở 0402, 0413).
-- - GRANT DELETE trên conversations, conversation_assignments cho việc dọn theo hạn giữ (chạy bằng crm_app trong
--   ngữ cảnh tenant; RLS áp cả DELETE).
-- - messages_sent_at: việc dọn chọn tin quá hạn theo sent_at của cả tenant. messages_attachment_files: tìm tệp còn được
--   tin khác dùng (attachments @> [{"fileId": …}]) trước khi đánh dấu xóa tệp.
-- - Thứ tự khóa: nhóm 6 ADR-46 (conversations → … → messages → message_revisions), rồi 12b (platform.files); ngoại
--   lệ của việc dọn ghi ở docs/adr/0046-thu-tu-khoa.md.

-- +goose Up
ALTER TABLE conversation.messages DROP CONSTRAINT messages_direction_origin;
ALTER TABLE conversation.messages
    ADD CONSTRAINT messages_direction_origin
        CHECK (origin = 'history' OR (direction = 'in') = (origin = 'customer')) NOT VALID;
ALTER TABLE conversation.messages VALIDATE CONSTRAINT messages_direction_origin;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION conversation.conversation_assignments_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    IF TG_OP = 'DELETE' AND current_setting('app.retention', true) = 'on' THEN
        RETURN NULL;
    END IF;
    RAISE EXCEPTION 'conversation_assignments is append-only (% bị chặn)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE INDEX messages_sent_at ON conversation.messages (tenant_id, sent_at);
CREATE INDEX messages_attachment_files ON conversation.messages USING gin (attachments jsonb_path_ops);

GRANT DELETE ON conversation.conversations, conversation.conversation_assignments TO crm_app;
