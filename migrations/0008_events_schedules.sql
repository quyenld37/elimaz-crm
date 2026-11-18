-- Sự kiện nội bộ, việc định kỳ, dọn dữ liệu (kế hoạch M0 bản 5, Task 13; spec 04 mục 5.1, 5.4, 11).
-- Pha mở rộng: hai bảng mới, ba index, thêm quyền DELETE cho việc dọn. Không lệnh thu hẹp nào.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md, mục "Bảng M0 ngoài thứ tự nhóm": domain_events (a) chỉ chèn;
-- schedules (b) chỉ sửa trong transaction riêng (Ticker khóa schedules rồi chỉ chèn jobs).

-- +goose Up
-- Sự kiện nội bộ: chỉ ghi thêm. Bật RLS (spec 04 mục 11 chỉ miễn bảng worker phải đọc của mọi tenant; sự kiện luôn
-- được ghi trong transaction nghiệp vụ có tenant và đọc trong handler chạy WithTenant). seq là cột version của đối
-- tượng, tăng dưới khóa dòng; payload chỉ mang mã bản ghi.
CREATE TABLE platform.domain_events (
    id             uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id      uuid        NOT NULL REFERENCES platform.tenants (id),
    aggregate_type text        NOT NULL CHECK (aggregate_type <> ''),
    aggregate_id   text        NOT NULL CHECK (aggregate_id <> ''),
    seq            bigint      NOT NULL CHECK (seq > 0),
    topic          text        NOT NULL CHECK (topic <> ''),
    payload        jsonb       NOT NULL DEFAULT '{}',
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT domain_events_tenant_id_id_key UNIQUE (tenant_id, id),
    CONSTRAINT domain_events_aggregate_seq_key UNIQUE (tenant_id, aggregate_type, aggregate_id, seq)
);
SELECT platform.enable_tenant_rls('platform.domain_events');

-- +goose StatementBegin
CREATE FUNCTION platform.domain_events_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION 'platform.domain_events chỉ ghi thêm' USING ERRCODE = 'restrict_violation';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER domain_events_append_only BEFORE UPDATE OR DELETE ON platform.domain_events
    FOR EACH ROW EXECUTE FUNCTION platform.domain_events_append_only();

GRANT SELECT, INSERT ON platform.domain_events TO crm_app;

-- Lịch việc định kỳ: bảng nền tảng không tenant (spec 04 mục 11). Ticker ghi dòng theo Define của tiến trình.
CREATE TABLE platform.schedules (
    name             text        PRIMARY KEY CHECK (name <> ''),
    every_seconds    int         NOT NULL CHECK (every_seconds >= 15),
    per_tenant       boolean     NOT NULL,
    next_run_at      timestamptz NOT NULL,
    last_enqueued_at timestamptz
);

GRANT SELECT, INSERT, UPDATE ON platform.schedules TO crm_app;

-- Dọn dữ liệu (lô 5.000 mỗi bảng mỗi lần): khóa chống trùng hết hạn theo tenant (RLS), việc done/expired và chỉ số
-- quá 30 ngày. metrics_minute đã có khóa chính bắt đầu bằng bucket.
CREATE INDEX idempotency_keys_expires ON platform.idempotency_keys (tenant_id, expires_at);
CREATE INDEX jobs_finished ON platform.jobs (finished_at) WHERE status IN ('done', 'expired');

GRANT DELETE ON platform.jobs, platform.metrics_minute TO crm_app;
