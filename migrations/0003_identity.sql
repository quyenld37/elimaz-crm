-- Người dùng, vai trò, quyền theo phạm vi của module identity (kế hoạch M0 bản 5, Task 7; spec 05 mục 7.2, 04 mục 11,
-- ADR-18, ADR-35). Pha mở rộng: bốn bảng mới. user_credentials, user_second_factors, user_sessions, login_attempts
-- thuộc đường đăng nhập riêng, không tạo ở M0.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 2 (users → roles → role_permissions → user_role_grants).
-- Mọi bảng có tenant_id, UNIQUE (tenant_id, id), khóa ngoại ghép (tenant_id, …), RLS ép. crm_app chỉ đọc ở M0
-- (chưa có đường ghi người dùng; lệnh crm seed-dev chạy bằng chủ bảng).

-- +goose Up
CREATE TABLE identity.users (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    email        citext NOT NULL CHECK (email <> ''),
    display_name text NOT NULL,
    status       text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'locked')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, email)
);
SELECT platform.enable_tenant_rls('identity.users');

CREATE TABLE identity.roles (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    key        text NOT NULL CHECK (key <> ''),
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, key)
);
SELECT platform.enable_tenant_rls('identity.roles');

-- permission là khóa quyền của identity.Permissions (mã Go); quyền lạ không cấp được gì vì Can chỉ so khóa đúng.
CREATE TABLE identity.role_permissions (
    tenant_id  uuid NOT NULL,
    role_id    uuid NOT NULL,
    permission text NOT NULL CHECK (permission <> ''),
    PRIMARY KEY (tenant_id, role_id, permission),
    FOREIGN KEY (tenant_id, role_id) REFERENCES identity.roles (tenant_id, id) ON DELETE CASCADE
);
SELECT platform.enable_tenant_rls('identity.role_permissions');

-- Phạm vi bắt buộc (ADR-18): all, một Page, hoặc một kho. all không có mã đích; page, warehouse phải có.
CREATE TABLE identity.user_role_grants (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL,
    user_id    uuid NOT NULL,
    role_id    uuid NOT NULL,
    scope_type text NOT NULL CHECK (scope_type IN ('all', 'page', 'warehouse')),
    scope_id   text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((scope_type = 'all') = (scope_id = '')),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, user_id, role_id, scope_type, scope_id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES identity.users (tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, role_id) REFERENCES identity.roles (tenant_id, id) ON DELETE CASCADE
);
SELECT platform.enable_tenant_rls('identity.user_role_grants');
CREATE INDEX user_role_grants_role ON identity.user_role_grants (tenant_id, role_id);

GRANT SELECT ON identity.users, identity.roles, identity.role_permissions, identity.user_role_grants TO crm_app;
