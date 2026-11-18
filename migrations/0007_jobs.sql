-- Hàng đợi việc nền (kế hoạch M0 bản 5, Task 11; spec 04 mục 5.1, 17; ADR-46 nhóm 13).
-- Pha mở rộng: một bảng nền tảng mới, không RLS (Global Constraints: worker nhận việc của mọi tenant; tenant_id NULL
-- với việc chưa định tuyến và việc toàn hệ thống). Chỉ internal/platform/queue ghi, qua JobQueue và Worker.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 13, sau platform.idempotency_keys: luôn là dòng khóa cuối transaction.
-- payload chỉ mang mã bản ghi, không mang nội dung (spec 04 mục 11).

-- +goose Up
CREATE TABLE platform.jobs (
    id               uuid        PRIMARY KEY DEFAULT uuidv7(),
    tenant_id        uuid        REFERENCES platform.tenants (id),
    kind             text        NOT NULL CHECK (kind <> ''),
    mode             text        NOT NULL CHECK (mode IN ('transactional', 'command')),
    payload          jsonb       NOT NULL DEFAULT '{}',
    dedupe_key       text        NOT NULL CHECK (dedupe_key <> ''),
    order_key        text,
    order_seq        bigint,
    block_on_dead    boolean     NOT NULL DEFAULT false,
    on_lease_expired text        NOT NULL CHECK (on_lease_expired IN ('retry', 'in_doubt')),
    max_attempts     int         NOT NULL CHECK (max_attempts > 0),
    status           text        NOT NULL DEFAULT 'pending'
                                 CHECK (status IN ('pending', 'running', 'done', 'dead', 'in_doubt', 'expired')),
    attempts         int         NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    run_at           timestamptz NOT NULL DEFAULT now(),
    lease_until      timestamptz,
    locked_by        text,
    expires_at       timestamptz,
    last_error       text,
    result           jsonb,
    trace_context    jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    finished_at      timestamptz,
    UNIQUE NULLS NOT DISTINCT (tenant_id, kind, dedupe_key),
    CHECK ((order_key IS NULL) = (order_seq IS NULL)),
    CHECK ((status = 'running') = (lease_until IS NOT NULL AND locked_by IS NOT NULL))
);

-- Nhận việc: việc tới hạn theo run_at.
CREATE INDEX jobs_pending_run_at ON platform.jobs (run_at) WHERE status = 'pending';
-- Thứ tự theo đối tượng: tìm việc đứng trước cùng (tenant_id, order_key) còn chặn.
CREATE INDEX jobs_order ON platform.jobs (tenant_id, order_key, order_seq, id)
    WHERE order_key IS NOT NULL AND status NOT IN ('done', 'expired');
-- Quét giữ chỗ hết hạn.
CREATE INDEX jobs_running_lease ON platform.jobs (lease_until) WHERE status = 'running';
-- Quét việc pending quá hạn sống.
CREATE INDEX jobs_pending_expires ON platform.jobs (expires_at) WHERE status = 'pending' AND expires_at IS NOT NULL;

-- Đánh thức worker (LISTEN jobs): NOTIFY chỉ tới khi transaction ghi việc commit. Kênh mang tên loại, không mang dữ liệu.
-- +goose StatementBegin
CREATE FUNCTION platform.jobs_notify()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    PERFORM pg_notify('jobs', NEW.kind);
    RETURN NULL;
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER jobs_notify AFTER INSERT ON platform.jobs FOR EACH ROW EXECUTE FUNCTION platform.jobs_notify();

GRANT SELECT, INSERT, UPDATE ON platform.jobs TO crm_app;
