-- Lịch sử trạng thái của yêu cầu về đơn hàng (Task 486, mockup M5 order-requests.html thẻ "Lịch sử"). Pha mở rộng:
-- CREATE TABLE, RLS, INDEX, trigger chỉ ghi thêm, GRANT SELECT, INSERT.
-- - Mỗi lần đổi trạng thái (nhân viên, khách trả lời hay rút qua website) một dòng: từ, sang, loại người làm (staff:
--   mã nhân viên; customer: mã khóa dịch vụ của website, như order_request_messages.author_id), lúc. Không nội dung tin.
-- - Ghi trong cùng transaction với lần đổi trạng thái (order.moveRequest). Thứ tự khóa ADR-46 nhóm 7: chỉ chèn sau khi
--   đã khóa dòng yêu cầu, như messages, files.

-- +goose Up
CREATE TABLE ord.order_request_events (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id   uuid NOT NULL REFERENCES platform.tenants (id),
    request_id  uuid NOT NULL,
    from_status text NOT NULL CHECK (from_status IN ('received', 'in_review', 'needs_info', 'confirmed',
                    'items_received', 'resolved', 'rejected', 'cancelled')),
    to_status   text NOT NULL CHECK (to_status IN ('received', 'in_review', 'needs_info', 'confirmed',
                    'items_received', 'resolved', 'rejected', 'cancelled')),
    actor_type  text NOT NULL CHECK (actor_type IN ('staff', 'customer', 'system')),
    actor_id    text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, request_id) REFERENCES ord.order_requests (tenant_id, id)
);
SELECT platform.enable_tenant_rls('ord.order_request_events');
CREATE INDEX order_request_events_request ON ord.order_request_events (tenant_id, request_id, created_at, id);

-- +goose StatementBegin
CREATE FUNCTION ord.order_request_events_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION 'order_request_events is append-only (% bị chặn)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER order_request_events_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON ord.order_request_events
    FOR EACH STATEMENT EXECUTE FUNCTION ord.order_request_events_append_only();

GRANT SELECT, INSERT ON ord.order_request_events TO crm_app;
