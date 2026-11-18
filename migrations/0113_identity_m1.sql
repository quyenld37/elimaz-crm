-- Identity M1 (kế hoạch M1, Task M1-26; PQ07): cột version cho người sửa đồng thời, cột vai trò cho Task M1-56,
-- hàm ghi tuyến email nhân viên chỉ cho tenant trong ngữ cảnh (Q25), view công bố identity.users_v (B2-11), quyền ghi
-- của crm_app cho màn quản lý nhân viên. Pha mở rộng: chỉ thêm cột có DEFAULT, hàm, view, quyền.
-- Không có bảng mới: thứ tự khóa giữ nguyên docs/adr/0046-thu-tu-khoa.md nhóm 1, 2.

-- +goose Up
ALTER TABLE identity.users ADD COLUMN version bigint NOT NULL DEFAULT 1;

ALTER TABLE identity.roles
    ADD COLUMN customized boolean NOT NULL DEFAULT false,
    ADD COLUMN status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    ADD COLUMN version bigint NOT NULL DEFAULT 1;

-- Tuyến staff_email → tenant đang trong ngữ cảnh (app.tenant_id do tenancy.WithTenant đặt). crm_app không có INSERT
-- trên platform.tenant_routes, nên đây là đường duy nhất của mã ứng dụng; không nhận tham số tenant. Thiếu ngữ cảnh
-- → lỗi; email đã có tuyến (ở bất kỳ tenant nào) → lỗi unique của khóa chính (23505).
-- +goose StatementBegin
CREATE FUNCTION platform.add_staff_route(email citext)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
AS $fn$
DECLARE
    tid uuid := NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid;
BEGIN
    IF tid IS NULL THEN
        RAISE EXCEPTION 'add_staff_route: thiếu ngữ cảnh tenant' USING ERRCODE = '42501';
    END IF;
    IF email IS NULL OR pg_catalog.btrim(email::text) = '' THEN
        RAISE EXCEPTION 'add_staff_route: email rỗng' USING ERRCODE = '22023';
    END IF;
    INSERT INTO platform.tenant_routes (kind, external_key, tenant_id)
    VALUES ('staff_email', pg_catalog.lower(email::text), tid);
END
$fn$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION platform.add_staff_route(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.add_staff_route(citext) TO crm_app;

-- View công bố cho module khác (M6 hiện tên người thao tác, M7 đọc). Không có email. security_invoker: RLS của
-- identity.users áp cho người đọc.
CREATE VIEW identity.users_v WITH (security_invoker = true) AS
SELECT id, display_name, status FROM identity.users;
GRANT SELECT ON identity.users_v TO crm_app;

-- Quản lý nhân viên chạy bằng crm_app (trước đây chỉ đọc): tạo nhân viên, đổi trạng thái và version, thay grant.
GRANT INSERT ON identity.users TO crm_app;
GRANT UPDATE (display_name, status, version) ON identity.users TO crm_app;
GRANT INSERT, DELETE ON identity.user_role_grants TO crm_app;
