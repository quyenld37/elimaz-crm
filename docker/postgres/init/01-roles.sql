-- Ba role của CRM (kế hoạch M0 bản 5, Global Constraints). Chỉ cho database dev:
-- mật khẩu cố định, chỉ nằm trong compose dev, không bao giờ dùng ở máy chủ.
CREATE ROLE crm_owner LOGIN PASSWORD 'crm_owner_dev';
CREATE ROLE crm_app LOGIN NOBYPASSRLS PASSWORD 'crm_app_dev';
CREATE ROLE crm_search NOLOGIN BYPASSRLS;
GRANT crm_search TO crm_owner;

-- crm_owner tạo schema và làm chủ bảng; crm_app chỉ được nối vào.
GRANT CONNECT, CREATE ON DATABASE crm TO crm_owner;
GRANT CONNECT ON DATABASE crm TO crm_app;
