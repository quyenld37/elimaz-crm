-- Dòng ghi nhận chốt đơn từ comment live (kế hoạch M4, Task 172 / M4-32; LV03, LV07, LV08, LV09). Pha mở rộng: CREATE
-- TABLE, INDEX, GRANT; không thu hẹp gì.
-- - Một comment một dòng (khóa chính (tenant_id, message_id)): hai nhân viên cùng chốt một comment chỉ ra một đơn.
-- - order_id là đơn nháp của M2 (không khóa ngoại sang ord: module chỉ nhắc schema của mình; đọc qua ord.orders_v).
-- - Trạng thái thật của tin xin địa chỉ đọc từ messages.send_status qua ask_message_id (không hook chéo).
-- - Thứ tự khóa: nhóm 6 ADR-46 (… live_sessions → live_comment_claims → messages …).

-- +goose Up
CREATE TABLE conversation.live_comment_claims (
    tenant_id        uuid NOT NULL REFERENCES platform.tenants (id),
    message_id       uuid NOT NULL,
    live_session_id  uuid NOT NULL,
    conversation_id  uuid NOT NULL,
    order_id         uuid NOT NULL,
    claimed_by       uuid NOT NULL,
    claimed_at       timestamptz NOT NULL DEFAULT now(),
    ask_message_id   uuid,
    ask_status       text NOT NULL CHECK (ask_status IN ('queued', 'skipped_policy', 'not_requested')),
    ask_reason       text CHECK (length(ask_reason) <= 500),
    remind_count     int NOT NULL DEFAULT 0 CHECK (remind_count >= 0),
    last_reminded_at timestamptz,
    PRIMARY KEY (tenant_id, message_id),
    CONSTRAINT live_comment_claims_ask CHECK ((ask_status = 'queued') = (ask_message_id IS NOT NULL)),
    FOREIGN KEY (tenant_id, message_id) REFERENCES conversation.messages (tenant_id, id),
    FOREIGN KEY (tenant_id, live_session_id) REFERENCES conversation.live_sessions (tenant_id, id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES conversation.conversations (tenant_id, id),
    FOREIGN KEY (tenant_id, ask_message_id) REFERENCES conversation.messages (tenant_id, id),
    FOREIGN KEY (tenant_id, claimed_by) REFERENCES identity.users (tenant_id, id)
);
SELECT platform.enable_tenant_rls('conversation.live_comment_claims');
-- Danh sách comment, đơn nháp "đã chốt, thiếu địa chỉ" của một phiên theo thời gian chốt (Task 173).
CREATE INDEX live_comment_claims_session ON conversation.live_comment_claims (tenant_id, live_session_id, claimed_at);
-- Đơn nháp đã có ghi nhận nào (thêm vào nháp đang có kiểm đơn thuộc phiên).
CREATE INDEX live_comment_claims_order ON conversation.live_comment_claims (tenant_id, order_id);

GRANT SELECT, INSERT, UPDATE ON conversation.live_comment_claims TO crm_app;
