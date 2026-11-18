-- Kết quả chép tệp sang kho ngoài và đối chiếu số tệp (kế hoạch M1, Task M1-45; spec 05 mục 7.3, ADR-36; TP10).
-- Pha mở rộng: một bảng nền tảng mới, không RLS, không tenant_id. Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md mục (d):
-- chỉ chèn, không khóa dòng đã có.
--
-- Ghi: container filesync (scripts/filesync) bằng role crm_filesync, chỉ INSERT. Đọc: luật cảnh báo và /health/deep
-- (internal/platform/observability) bằng crm_app, chỉ SELECT. detail chỉ có số đếm và dòng lỗi rút gọn của rclone
-- (tên đối tượng là mã do hệ thống sinh), không dữ liệu khách.
--
-- Role crm_filesync (LOGIN NOBYPASSRLS CONNECTION LIMIT 1) do người quản trị tạo trước, như crm_owner, crm_app:
-- crm_owner chạy migration không có CREATEROLE (task-45.md, "Quyết định thay chủ shop" Q1). Cụm dev:
-- docker/postgres/init/01-roles.sql; cụm thử: testkit (prepareEmpty); máy chủ: RUNBOOK "Dựng PostgreSQL lần đầu".
-- Thiếu role thì migrate dừng ở GRANT dưới đây (lỗi rõ, không bỏ qua).

-- +goose Up
CREATE TABLE platform.file_sync_runs (
    id          uuid        PRIMARY KEY DEFAULT uuidv7(),
    kind        text        NOT NULL CHECK (kind IN ('copy', 'reconcile', 'restore_drill')),
    bucket      text        NOT NULL CHECK (bucket <> ''),
    ok          boolean     NOT NULL,
    started_at  timestamptz NOT NULL,
    finished_at timestamptz NOT NULL CHECK (finished_at >= started_at),
    objects     bigint      CHECK (objects >= 0),
    bytes       bigint      CHECK (bytes >= 0),
    detail      text        CHECK (length(detail) <= 2000)
);

-- Luật cảnh báo: lần mới nhất theo (bucket, kind), lần ok mới nhất.
CREATE INDEX file_sync_runs_bucket_kind_finished ON platform.file_sync_runs (bucket, kind, finished_at DESC);

GRANT SELECT ON platform.file_sync_runs TO crm_app;

-- crm_filesync: dùng schema platform để nhắc tên bảng, chèn vào đúng một bảng; không gì khác (không SELECT, kể cả
-- bảng này; id có DEFAULT nên INSERT không cần quyền trên sequence).
GRANT USAGE ON SCHEMA platform TO crm_filesync;
GRANT INSERT ON platform.file_sync_runs TO crm_filesync;
