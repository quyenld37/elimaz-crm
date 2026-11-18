-- Khóa chống trùng của yêu cầu ghi (kế hoạch M0 bản 5, Task 9; ADR-41, ADR-47; spec 05 bảng idempotency_keys).
-- Pha mở rộng: một bảng mới. Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 13 (luôn cuối transaction nghiệp vụ).
-- Phạm vi khóa (tenant, người gọi, route, khóa); RLS ép bằng platform.enable_tenant_rls. request_hash là sha256 của
-- method, đường dẫn và thân đã chuẩn hóa (không lưu thân yêu cầu). response_body NULL khi handler không trả thân
-- hoặc route no-replay. Dòng hết hạn (expires_at) do việc dọn của Task 13 xóa.

-- +goose Up
CREATE TABLE platform.idempotency_keys (
    tenant_id       uuid NOT NULL,
    actor           text NOT NULL CHECK (actor <> ''),
    route           text NOT NULL CHECK (route <> ''),
    key             text NOT NULL CHECK (key ~ '^[A-Za-z0-9_-]{1,128}$'),
    request_hash    bytea NOT NULL,
    status          text NOT NULL CHECK (status IN ('running', 'done')),
    locked_until    timestamptz,
    response_status int,
    response_body   jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, actor, route, key),
    CHECK ((status = 'running') = (response_status IS NULL))
);
SELECT platform.enable_tenant_rls('platform.idempotency_keys');

GRANT SELECT, INSERT, UPDATE, DELETE ON platform.idempotency_keys TO crm_app;
