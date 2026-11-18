-- Vai trò tự định nghĩa (kế hoạch M1, Task M1-56; Q35): quyền ghi của crm_app trên bảng vai trò. Trước đây crm_app chỉ
-- đọc identity.roles, identity.role_permissions (vai trò mẫu do chủ bảng ghi qua EnsureDefaultRoles). Pha mở rộng: chỉ
-- cấp quyền. UPDATE theo cột (không có key, tenant_id) cũng là quyền cần cho SELECT … FOR UPDATE / FOR SHARE trên
-- identity.roles (sửa, lưu trữ vai trò; ReplaceGrants khóa vai trò được gán). Không có bảng mới: thứ tự khóa giữ nguyên
-- docs/adr/0046-thu-tu-khoa.md nhóm 2.

-- +goose Up
GRANT INSERT ON identity.roles TO crm_app;
GRANT UPDATE (name, customized, status, version) ON identity.roles TO crm_app;
GRANT INSERT, DELETE ON identity.role_permissions TO crm_app;

