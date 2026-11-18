-- Nhật ký thao tác chỉ ghi thêm của module identity (kế hoạch M0 bản 5, Task 8; spec 04 mục 11, 05 bảng audit_logs).
-- Pha mở rộng: một bảng, một hàm trigger, một index. Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md, mục "Bảng M0 ngoài
-- thứ tự nhóm" (a) chỉ chèn. Ghi trong cùng transaction với thay đổi (identity.RecordAudit). Trường dữ liệu cá nhân
-- chỉ ghi tên trong changed_fields, giá trị trong before/after là "[Redacted]".
-- Chỉ ghi thêm: crm_app chỉ có SELECT, INSERT; trigger theo câu lệnh chặn UPDATE, DELETE, TRUNCATE của mọi role, kể
-- cả chủ bảng và cả khi RLS lọc hết dòng (trigger theo dòng sẽ không chạy trong hai trường hợp đó).

-- +goose Up
CREATE TABLE identity.audit_logs (
    id             uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      uuid NOT NULL REFERENCES platform.tenants (id),
    actor_type     text NOT NULL CHECK (actor_type IN ('staff', 'system', 'carrier', 'customer', 'service')),
    actor_id       text,
    action         text NOT NULL CHECK (action <> ''),
    entity_type    text NOT NULL CHECK (entity_type <> ''),
    entity_id      text,
    changed_fields text[] NOT NULL DEFAULT '{}',
    before         jsonb,
    after          jsonb,
    ip             inet,
    request_id     text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id)
);
SELECT platform.enable_tenant_rls('identity.audit_logs');
CREATE INDEX audit_logs_entity ON identity.audit_logs (tenant_id, entity_type, entity_id, created_at DESC);

-- +goose StatementBegin
CREATE FUNCTION identity.audit_logs_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION 'audit_logs is append-only (% bị chặn)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER audit_logs_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON identity.audit_logs
    FOR EACH STATEMENT EXECUTE FUNCTION identity.audit_logs_append_only();

REVOKE ALL ON identity.audit_logs FROM PUBLIC;
REVOKE UPDATE, DELETE, TRUNCATE ON identity.audit_logs FROM crm_app;
GRANT SELECT, INSERT ON identity.audit_logs TO crm_app;
