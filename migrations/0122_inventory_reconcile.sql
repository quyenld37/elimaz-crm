-- Đối chiếu tồn hằng đêm (kế hoạch M1, Task M1-36; spec 05 mục 2, KH13). Pha mở rộng: CREATE, GRANT, chèn dòng lịch.
-- Mỗi lần chạy (mỗi tenant mỗi đêm) ghi một dòng: số dòng số dư đã so, các mục lệch. mismatches chỉ chứa
-- {variantId, warehouseId, check, bucket?, expected, actual} (không tên, không dữ liệu khách). Đối chiếu chỉ báo,
-- không sửa số.
-- Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 11 (… → reconcile_runs, cuối nhóm); chỉ chèn, không khóa dòng.

-- +goose Up
CREATE TABLE inventory.reconcile_runs (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    tenant_id  uuid NOT NULL REFERENCES platform.tenants (id),
    ran_at     timestamptz NOT NULL,
    ok         boolean NOT NULL,
    checked    int NOT NULL CHECK (checked >= 0),
    mismatches jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(mismatches) = 'array'),
    UNIQUE (tenant_id, id),
    CHECK (ok = (jsonb_array_length(mismatches) = 0))
);
SELECT platform.enable_tenant_rls('inventory.reconcile_runs');
CREATE INDEX reconcile_runs_ran_at ON inventory.reconcile_runs (tenant_id, ran_at);

CREATE VIEW inventory.reconcile_runs_v WITH (security_invoker = true) AS
SELECT id AS run_id, ran_at, ok, checked, jsonb_array_length(mismatches) AS mismatch_count
FROM inventory.reconcile_runs;

-- crm_app: đọc, chèn; không sửa, không xóa.
GRANT SELECT, INSERT ON inventory.reconcile_runs TO crm_app;
GRANT SELECT ON inventory.reconcile_runs_v TO crm_app;

-- Lịch đối chiếu (quyết định D2 của Task M1-24): kỳ đầu 19:30 UTC kế tiếp (02:30 giờ Việt Nam); Ticker gặp dòng đã có
-- giữ next_run_at (InsertSchedule … ON CONFLICT DO NOTHING) nên các kỳ sau neo theo giờ này. Đảo ngược: xóa dòng.
INSERT INTO platform.schedules (name, every_seconds, per_tenant, next_run_at)
SELECT 'inventory.reconcile', 86400, true, CASE WHEN s.t > now() THEN s.t ELSE s.t + interval '1 day' END
FROM (SELECT date_trunc('day', now(), 'UTC') + interval '19 hours 30 minutes' AS t) AS s
ON CONFLICT (name) DO NOTHING;
