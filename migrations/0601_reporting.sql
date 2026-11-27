-- Schema reporting của M6 (kế hoạch M6, Task 226 / M6-03; ADR-20; spec 05 schema reporting). Pha mở rộng: chỉ CREATE,
-- GRANT. Số 0602 bỏ trống (G15: identity.users_v do M1 công bố ở 0113, M6 chỉ đọc).
-- - reporting.report_runs: một lần chạy báo cáo (xem tại chỗ lưu kết quả json, chạy nền xuất xlsx thành tệp). Không
--   khóa ngoại sang catalog, identity: module chỉ đọc view của module khác; seller_id, requested_by là bản ghi lịch sử.
--   crm_app sửa, xóa được (đổi trạng thái; dọn sau 90 ngày).
-- - reporting.stock_snapshot_days: mỗi ngày đã chụp tồn một dòng (mốc cắt, số dòng); reporting.stock_daily_snapshots:
--   số dư của từng biến thể ở từng kho tại mốc cắt. Hai bảng chỉ ghi thêm (trigger chặn UPDATE, DELETE, TRUNCATE của
--   mọi role, kể cả chủ bảng; crm_app không có quyền sửa, xóa). Không khóa ngoại sang catalog, inventory: bản ghi lịch
--   sử, không giữ khóa trên hàng chục nghìn dòng biến thể mỗi đêm.
-- Thứ tự khóa: ADR-46 nhóm 12a (report_runs → stock_snapshot_days → stock_daily_snapshots).

-- +goose Up
CREATE SCHEMA reporting;
GRANT USAGE ON SCHEMA reporting TO crm_app;

CREATE TABLE reporting.report_runs (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    seller_id    uuid NOT NULL,
    family       text NOT NULL CHECK (family IN ('sales', 'inventory', 'customers')),
    report       text NOT NULL CHECK (report IN ('dashboard', 'orders', 'order_drilldown', 'carriers', 'best_sellers',
                                                 'stock', 'stock_movements', 'products', 'customers')),
    format       text NOT NULL CHECK (format IN ('json', 'xlsx')),
    params       jsonb NOT NULL,
    requested_by uuid NOT NULL,
    status       text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'ready', 'failed', 'expired')),
    reason       text CHECK (reason IN ('too_heavy', 'busy', 'forbidden', 'error')),
    row_count    int CHECK (row_count >= 0),
    truncated    boolean NOT NULL DEFAULT false,
    result       jsonb,
    file_id      uuid,
    file_bytes   bigint CHECK (file_bytes >= 0),
    job_id       uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    started_at   timestamptz,
    finished_at  timestamptz,
    expires_at   timestamptz NOT NULL,
    version      int NOT NULL DEFAULT 1 CHECK (version >= 1),
    UNIQUE (tenant_id, id),
    CONSTRAINT report_runs_result_format_check CHECK (format = 'json' OR result IS NULL),
    CONSTRAINT report_runs_file_format_check CHECK (format = 'xlsx' OR file_id IS NULL),
    CONSTRAINT report_runs_failed_reason_check CHECK ((status = 'failed') = (reason IS NOT NULL)),
    CONSTRAINT report_runs_customers_family_check CHECK (family <> 'customers' OR (report = 'customers' AND format = 'xlsx')),
    CONSTRAINT report_runs_products_family_check CHECK (report <> 'products' OR (family = 'inventory' AND format = 'xlsx'))
);
SELECT platform.enable_tenant_rls('reporting.report_runs');
CREATE INDEX report_runs_requester ON reporting.report_runs (tenant_id, requested_by, created_at DESC, id DESC);
CREATE INDEX report_runs_family ON reporting.report_runs (tenant_id, family, created_at DESC, id DESC);
CREATE INDEX report_runs_expiry ON reporting.report_runs (tenant_id, expires_at)
    WHERE status IN ('queued', 'ready', 'failed');

CREATE TABLE reporting.stock_snapshot_days (
    id        uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id uuid NOT NULL REFERENCES platform.tenants (id),
    day       date NOT NULL,
    cutoff_at timestamptz NOT NULL,
    row_count int NOT NULL CHECK (row_count >= 0),
    taken_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CONSTRAINT stock_snapshot_days_day_key UNIQUE (tenant_id, day)
);
SELECT platform.enable_tenant_rls('reporting.stock_snapshot_days');

CREATE TABLE reporting.stock_daily_snapshots (
    tenant_id    uuid NOT NULL REFERENCES platform.tenants (id),
    day          date NOT NULL,
    warehouse_id uuid NOT NULL,
    variant_id   uuid NOT NULL,
    seller_id    uuid NOT NULL,
    on_hand      int NOT NULL CHECK (on_hand >= 0),
    held         int NOT NULL CHECK (held >= 0),
    in_transit   int NOT NULL CHECK (in_transit >= 0),
    damaged      int NOT NULL CHECK (damaged >= 0),
    PRIMARY KEY (tenant_id, day, warehouse_id, variant_id),
    FOREIGN KEY (tenant_id, day) REFERENCES reporting.stock_snapshot_days (tenant_id, day)
);
SELECT platform.enable_tenant_rls('reporting.stock_daily_snapshots');
CREATE INDEX stock_snapshots_variant ON reporting.stock_daily_snapshots (tenant_id, day, seller_id, variant_id);

-- Trigger theo câu lệnh như inventory.stock_ledger (0115): chặn UPDATE, DELETE, TRUNCATE của mọi role, kể cả chủ bảng
-- và cả khi RLS lọc hết dòng.
-- +goose StatementBegin
CREATE FUNCTION reporting.stock_snapshots_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    RAISE EXCEPTION 'stock snapshots are append-only (% trên % bị chặn)', TG_OP, TG_TABLE_NAME
        USING ERRCODE = 'insufficient_privilege';
END
$fn$;
-- +goose StatementEnd

CREATE TRIGGER stock_snapshot_days_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON reporting.stock_snapshot_days
    FOR EACH STATEMENT EXECUTE FUNCTION reporting.stock_snapshots_append_only();
CREATE TRIGGER stock_daily_snapshots_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON reporting.stock_daily_snapshots
    FOR EACH STATEMENT EXECUTE FUNCTION reporting.stock_snapshots_append_only();

GRANT SELECT, INSERT, UPDATE, DELETE ON reporting.report_runs TO crm_app;
GRANT SELECT, INSERT ON reporting.stock_snapshot_days, reporting.stock_daily_snapshots TO crm_app;
REVOKE UPDATE, DELETE, TRUNCATE ON reporting.stock_snapshot_days, reporting.stock_daily_snapshots FROM crm_app;
