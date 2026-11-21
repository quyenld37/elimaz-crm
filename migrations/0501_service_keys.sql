-- Khóa dịch vụ của website (kế hoạch M5, Task 189 / M5-06; ADR-18). Chủ dữ liệu: module identity. Chỉ lưu băm
-- SHA-256 của khóa đầy đủ (bí mật chỉ trả một lần lúc tạo); tiền tố (12 ký tự, không bí mật) định tuyến khóa → tenant
-- qua platform.tenant_routes (kind service_key, ghi bằng platform.put_tenant_route của 0290, crm_app không chèn thẳng).
-- Pha mở rộng: chỉ thêm bảng và quyền. Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 2 (… → user_sessions →
-- service_keys); thu hồi khóa một dòng service_keys, tạo khóa chỉ chèn.

-- +goose Up
CREATE TABLE identity.service_keys (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id     uuid NOT NULL REFERENCES platform.tenants (id),
    name          text NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
    key_prefix    text NOT NULL CHECK (key_prefix ~ '^[a-z2-7]{12}$'),
    key_hash      bytea NOT NULL CHECK (length(key_hash) = 32),
    permissions   text[] NOT NULL CHECK (cardinality(permissions) > 0),
    allowed_cidrs cidr[] NOT NULL DEFAULT '{}',
    created_by    uuid NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    revoked_at    timestamptz,
    revoked_by    uuid,
    last_used_at  timestamptz,
    version       int NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, key_prefix),
    UNIQUE (tenant_id, key_hash),
    FOREIGN KEY (tenant_id, created_by) REFERENCES identity.users (tenant_id, id),
    FOREIGN KEY (tenant_id, revoked_by) REFERENCES identity.users (tenant_id, id),
    CHECK (revoked_by IS NULL OR revoked_at IS NOT NULL)
);
SELECT platform.enable_tenant_rls('identity.service_keys');

GRANT SELECT, INSERT ON identity.service_keys TO crm_app;
GRANT UPDATE (revoked_at, revoked_by, last_used_at, version) ON identity.service_keys TO crm_app;
