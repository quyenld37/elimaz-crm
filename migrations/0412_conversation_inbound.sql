-- Đường tin vào của M4 (kế hoạch M4, Task 153 / M4-13). Pha mở rộng: thay thân hàm (chỉ thêm một loại được phép) và
-- thêm khóa ngoại; không thu hẹp gì.
-- (a) platform.put_tenant_route (0290) nhận thêm loại tuyến 'fakechat_page': sự kiện của kênh giả (adapter fakechat,
--     Task 145, chỉ ngoài production) cần tuyến tenant như 'fb_page'. Thân hàm giữ nguyên từng dòng của 0290 trừ danh
--     sách loại; tệp task-153, Quyết định 2.
-- (b) Khóa ngoại ghép (tenant_id, channel_account_id) → channel.channel_accounts (tenant_id, id) cho các bảng của 0402
--     (Task 151 để lại vì 0401 chưa gộp lúc viết). Bảng hội thoại chưa có dữ liệu ở máy chủ (M4 chưa chạy).
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md; khóa ngoại chỉ lấy KEY SHARE trên channel.channel_accounts (nhóm 3, đứng
-- trước nhóm 6), không xung đột với UPDATE cột thường của channel.NoteInbound.

-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.put_tenant_route(p_kind text, p_key text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
AS $fn$
DECLARE
    tid   uuid := NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid;
    owner uuid;
BEGIN
    IF tid IS NULL THEN
        RAISE EXCEPTION 'put_tenant_route: thiếu ngữ cảnh tenant' USING ERRCODE = '42501';
    END IF;
    IF p_kind IS NULL OR p_kind NOT IN ('carrier_account', 'fb_page', 'fakechat_page', 'service_key') THEN
        RAISE EXCEPTION 'put_tenant_route: loại tuyến không được phép' USING ERRCODE = '22023';
    END IF;
    IF p_key IS NULL OR pg_catalog.btrim(p_key) = '' THEN
        RAISE EXCEPTION 'put_tenant_route: khóa rỗng' USING ERRCODE = '22023';
    END IF;
    INSERT INTO platform.tenant_routes (kind, external_key, tenant_id)
    VALUES (p_kind, p_key, tid)
    ON CONFLICT (kind, external_key) DO NOTHING;
    IF NOT FOUND THEN
        SELECT r.tenant_id INTO owner FROM platform.tenant_routes AS r
        WHERE r.kind = p_kind AND r.external_key = p_key;
        IF owner IS DISTINCT FROM tid THEN
            RAISE EXCEPTION 'put_tenant_route: khóa đã thuộc tenant khác' USING ERRCODE = '23505';
        END IF;
    END IF;
END
$fn$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION platform.put_tenant_route(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.put_tenant_route(text, text) TO crm_app;

ALTER TABLE conversation.page_settings
    ADD CONSTRAINT page_settings_channel_account_fk FOREIGN KEY (tenant_id, channel_account_id)
        REFERENCES channel.channel_accounts (tenant_id, id);
ALTER TABLE conversation.posts
    ADD CONSTRAINT posts_channel_account_fk FOREIGN KEY (tenant_id, channel_account_id)
        REFERENCES channel.channel_accounts (tenant_id, id);
ALTER TABLE conversation.conversations
    ADD CONSTRAINT conversations_channel_account_fk FOREIGN KEY (tenant_id, channel_account_id)
        REFERENCES channel.channel_accounts (tenant_id, id);
ALTER TABLE conversation.messages
    ADD CONSTRAINT messages_channel_account_fk FOREIGN KEY (tenant_id, channel_account_id)
        REFERENCES channel.channel_accounts (tenant_id, id);
