-- Bản nhận webhook nguyên văn (kế hoạch M0 bản 5, Task 14; spec 04 mục 5.2, 11).
-- Pha mở rộng: một bảng nền tảng mới, không RLS và không tenant_id (spec 04 mục 11: ingest lưu trước khi biết tenant;
-- worker tách gói và định tuyến ở Task 15). Chỉ internal/platform/webhooks đọc, ghi.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md, mục "Bảng M0 ngoài thứ tự nhóm" (a): chỉ chèn ON CONFLICT DO NOTHING,
-- rồi platform.jobs cuối transaction.
-- body là thân gốc từng byte (chữ ký tính trên đó); headers đã che qua logging.Redact. Dọn sau 30 ngày.

-- +goose Up
CREATE TABLE platform.webhook_deliveries (
    id          uuid        PRIMARY KEY DEFAULT uuidv7(),
    source      text        NOT NULL CHECK (source <> ''),
    body_sha256 text        NOT NULL CHECK (body_sha256 ~ '^[0-9a-f]{64}$'),
    headers     jsonb       NOT NULL,
    body        bytea       NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source, body_sha256)
);

-- Dọn theo tuổi (CleanupDeliveries, mỗi lần tối đa 5.000 dòng).
CREATE INDEX webhook_deliveries_received_at ON platform.webhook_deliveries (received_at);

GRANT SELECT, INSERT, DELETE ON platform.webhook_deliveries TO crm_app;
