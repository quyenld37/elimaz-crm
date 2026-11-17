-- Nền tảng (kế hoạch M0 bản 5, Task 4): extension, schema platform và identity, bảng tenant, checksum migration.
-- Chạy bằng crm_owner. pg_stat_statements không phải extension "trusted": người quản trị cụm tạo trước
-- (docker/postgres/init, testkit); câu dưới đây khi đó chỉ là không làm gì.
-- Thứ tự khóa của các bảng mới: docs/adr/0046-thu-tu-khoa.md (nhóm 1 và mục "Bảng M0 ngoài thứ tự nhóm").

-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- Migrate tạo sẵn schema platform (chứa bảng phiên bản của goose) trước khi goose chạy.
CREATE SCHEMA IF NOT EXISTS platform;
CREATE SCHEMA identity;
GRANT USAGE ON SCHEMA platform, identity TO crm_app, crm_search;

CREATE TABLE platform.tenants (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE platform.tenant_routes (
    kind         text NOT NULL,
    external_key text NOT NULL,
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, external_key)
);

-- Chỉ vai trò migrate đọc và ghi, dưới pg_advisory_lock(727001).
CREATE TABLE platform.migration_checksums (
    version text PRIMARY KEY,
    sha256  text NOT NULL
);

GRANT SELECT ON platform.tenants, platform.tenant_routes TO crm_app;
