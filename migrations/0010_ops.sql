-- Cảnh báo đã gửi, lần sao lưu, đọc thống kê PostgreSQL (kế hoạch M0 bản 5, Task 19; spec 04 mục 8, ADR-30, 37, 42).
-- Pha mở rộng: hai bảng nền tảng mới, không RLS, không tenant_id. Chỉ internal/platform/observability đọc, ghi (và
-- scripts/backup ghi backup_runs). Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md, mục "Bảng M0 ngoài thứ tự nhóm" (a):
-- chỉ chèn, không khóa dòng đã có.
-- text của cảnh báo chỉ mang tên luật, số lượng, kind, mã việc; không bao giờ có dữ liệu khách (B11).
--
-- GRANT pg_monitor cần crm_owner có ADMIN OPTION trên pg_monitor (cụm dev: docker/postgres/init/01-roles.sql; máy chủ:
-- bước dựng role của RUNBOOK). Database dev cũ đã áp 0012/0112 phải dựng lại (goose không chạy số nhỏ đến sau).

-- +goose Up
CREATE TABLE platform.alert_events (
    id       uuid        PRIMARY KEY DEFAULT uuidv7(),
    key      text        NOT NULL,
    severity text        NOT NULL CHECK (severity IN ('urgent', 'normal')),
    text     text        NOT NULL,
    sent_at  timestamptz NOT NULL DEFAULT now()
);

-- Chống lặp (lần gửi cuối của một key) và tổng kết 24 giờ.
CREATE INDEX alert_events_key_sent_at ON platform.alert_events (key, sent_at DESC);

CREATE TABLE platform.backup_runs (
    id               uuid        PRIMARY KEY DEFAULT uuidv7(),
    kind             text        NOT NULL CHECK (kind IN ('base', 'restore_drill', 'restore_verify')),
    ok               boolean     NOT NULL,
    finished_at      timestamptz NOT NULL DEFAULT now(),
    duration_seconds int,
    detail           text
);

GRANT SELECT, INSERT ON platform.alert_events, platform.backup_runs TO crm_app;

-- Tổng dung lượng thư mục pg_wal. pg_ls_waldir cần pg_monitor; hàm chạy bằng quyền chủ (crm_owner) và chỉ trả một số.
-- +goose StatementBegin
CREATE FUNCTION platform.wal_bytes()
RETURNS bigint
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $fn$
    SELECT coalesce(sum(size), 0)::bigint FROM pg_ls_waldir()
$fn$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION platform.wal_bytes() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.wal_bytes() TO crm_app;

-- Đọc pg_stat_activity (mọi phiên), pg_stat_statements, pg_stat_archiver.
GRANT pg_monitor TO crm_app;

-- Luật dead:<kind>, in-doubt:<kind>, kiểm sâu và tổng kết đếm việc dead, in_doubt mỗi phút: không quét cả bảng jobs
-- (việc done giữ 30 ngày). Bảng jobs chưa có dữ liệu thật ở máy chủ (M0 chưa chạy production) nên không cần CONCURRENTLY.
CREATE INDEX jobs_attention ON platform.jobs (kind, finished_at) WHERE status IN ('dead', 'in_doubt');
