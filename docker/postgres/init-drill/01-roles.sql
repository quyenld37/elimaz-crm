-- Role cho cụm diễn tập (Task 2). Cùng nội dung role với docker/postgres/init/01-roles.sql của Task 1;
-- khi gộp nhánh giữ một bản (xem docs/tasks/task-02.md, Quyết định thay chủ shop, mục 1).
-- Không mật khẩu: chỉ đăng nhập qua socket trong container (pg_hba của image cho "local" là trust).
CREATE ROLE crm_owner LOGIN;
CREATE ROLE crm_app LOGIN NOBYPASSRLS;
CREATE ROLE crm_search NOLOGIN BYPASSRLS;
-- Container chép tệp sang kho ngoài (Task M1-45): migration 0126 chỉ cấp INSERT vào platform.file_sync_runs.
CREATE ROLE crm_filesync LOGIN NOBYPASSRLS CONNECTION LIMIT 1;
GRANT crm_search TO crm_owner;
-- Migration 0010 (Task 19) cấp pg_monitor cho crm_app: crm_owner cần ADMIN OPTION trên pg_monitor.
GRANT pg_monitor TO crm_owner WITH ADMIN OPTION;

-- Task 21: như bước dựng role ở máy chủ (RUNBOOK, mục "Dựng PostgreSQL lần đầu"), để migrate thật chạy được trên cụm
-- diễn tập (scripts/backup/verify-drill.sh). crm_owner tạo schema; crm_app chỉ được nối vào.
GRANT CONNECT, CREATE ON DATABASE crm TO crm_owner;
GRANT CONNECT ON DATABASE crm TO crm_app;
GRANT CONNECT ON DATABASE crm TO crm_filesync;
-- pg_stat_statements không "trusted": tạo trước bằng quyền quản trị (câu trong 0001 khi đó không làm gì).
\connect crm
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
