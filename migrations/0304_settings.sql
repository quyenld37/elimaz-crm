-- Cài đặt theo tenant dạng khóa → JSON (kế hoạch M3, Task M3-04; Q-35, B17; spec 05 mục 5). M3 là chủ bảng và gói
-- internal/platform/settings (Get, Put). Khóa đầu tiên: "fulfillment" (cài đặt vận chuyển). Thứ tự khóa: ADR-46 nhóm
-- 12c (chỉ sửa một mình trong transaction của PUT cài đặt). Pha mở rộng: chỉ thêm bảng.

-- +goose Up
CREATE TABLE platform.settings (
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    key        text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_.]{0,63}$'),
    value      jsonb NOT NULL CHECK (jsonb_typeof(value) = 'object'),
    version    bigint NOT NULL DEFAULT 1 CHECK (version >= 1),
    updated_by uuid,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, key)
);
SELECT platform.enable_tenant_rls('platform.settings');
GRANT SELECT, INSERT, UPDATE ON platform.settings TO crm_app;
