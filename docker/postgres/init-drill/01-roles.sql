-- Role cho cụm diễn tập (Task 2). Cùng nội dung role với docker/postgres/init/01-roles.sql của Task 1;
-- khi gộp nhánh giữ một bản (xem docs/tasks/task-02.md, Quyết định thay chủ shop, mục 1).
-- Không mật khẩu: chỉ đăng nhập qua socket trong container (pg_hba của image cho "local" là trust).
CREATE ROLE crm_owner LOGIN;
CREATE ROLE crm_app LOGIN NOBYPASSRLS;
CREATE ROLE crm_search NOLOGIN BYPASSRLS;
GRANT crm_search TO crm_owner;
-- Migration 0010 (Task 19) cấp pg_monitor cho crm_app: crm_owner cần ADMIN OPTION trên pg_monitor.
GRANT pg_monitor TO crm_owner WITH ADMIN OPTION;
