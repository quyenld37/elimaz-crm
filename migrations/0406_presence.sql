-- Ai đang xem, đang gõ, đang lên đơn trong một hội thoại (kế hoạch M4, Task 163 / M4-23; spec 04 mục 6). Pha mở rộng:
-- CREATE TABLE, GRANT; không thu hẹp gì.
-- - UNLOGGED: dữ liệu được phép mất khi PostgreSQL khởi động lại (04 mục 6); mỗi nhịp tim 10 s ghi một dòng, không
--   cần WAL.
-- - Dòng hết hạn (expires_at) bị lịch conversation.presence_cleanup xóa mỗi 60 s; câu đọc lọc expires_at > bây giờ.
-- - Thứ tự khóa: cuối nhóm 6 ADR-46 (conversation.presence).

-- +goose Up
CREATE UNLOGGED TABLE conversation.presence (
    tenant_id       uuid NOT NULL REFERENCES platform.tenants (id),
    conversation_id uuid NOT NULL,
    user_id         uuid NOT NULL,
    state           text NOT NULL CHECK (state IN ('viewing', 'typing', 'drafting_order')),
    expires_at      timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, conversation_id, user_id),
    FOREIGN KEY (tenant_id, conversation_id) REFERENCES conversation.conversations (tenant_id, id) ON DELETE CASCADE
);
SELECT platform.enable_tenant_rls('conversation.presence');
CREATE INDEX presence_expires ON conversation.presence (tenant_id, expires_at);

REVOKE ALL ON conversation.presence FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE, DELETE ON conversation.presence TO crm_app;
