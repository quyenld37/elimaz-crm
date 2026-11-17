-- Ba role của CRM (kế hoạch M0 bản 5, Global Constraints). Chỉ cho database dev:
-- mật khẩu cố định, chỉ nằm trong compose dev, không bao giờ dùng ở máy chủ.
CREATE ROLE crm_owner LOGIN PASSWORD 'crm_owner_dev';
CREATE ROLE crm_app LOGIN NOBYPASSRLS PASSWORD 'crm_app_dev';
CREATE ROLE crm_search NOLOGIN BYPASSRLS;
GRANT crm_search TO crm_owner;

-- crm_owner tạo schema và làm chủ bảng; crm_app chỉ được nối vào.
GRANT CONNECT, CREATE ON DATABASE crm TO crm_owner;
GRANT CONNECT ON DATABASE crm TO crm_app;

-- Extension của migration 0001 (Task 4) tạo trước bằng quyền quản trị: pg_stat_statements không "trusted" nên
-- crm_owner không tự tạo được; crm_owner cũng không có quyền CREATE trên schema public của database crm.
-- Câu CREATE EXTENSION IF NOT EXISTS trong 0001 khi đó không làm gì.
\connect crm
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
