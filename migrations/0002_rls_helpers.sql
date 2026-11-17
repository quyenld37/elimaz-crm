-- Hàm bật RLS cho bảng nghiệp vụ (kế hoạch M0 bản 5, Task 5). Không tạo bảng nên không đổi bảng thứ tự khóa.
-- Mỗi module gọi trong migration của mình, ngay sau CREATE TABLE: SELECT platform.enable_tenant_rls('schema.bảng');
-- Bật và ép RLS (chủ bảng cũng chịu), policy tenant_isolation cho mọi thao tác. NULLIF: ngoài transaction có ngữ cảnh,
-- app.tenant_id còn lại là '' (không phải NULL) trên kết nối đã dùng lại; NULLIF biến '' thành NULL nên câu trả 0 dòng
-- thay vì lỗi "invalid input syntax for type uuid". Hàm chạy với quyền người gọi (chủ bảng trong migration).

-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION platform.enable_tenant_rls(tbl regclass)
RETURNS void
LANGUAGE plpgsql
SET search_path = pg_catalog, pg_temp
AS $fn$
BEGIN
    EXECUTE format('ALTER TABLE %s ENABLE ROW LEVEL SECURITY', tbl);
    EXECUTE format('ALTER TABLE %s FORCE ROW LEVEL SECURITY', tbl);
    EXECUTE format(
        'CREATE POLICY tenant_isolation ON %s'
        ' USING (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)'
        ' WITH CHECK (tenant_id = NULLIF(current_setting(''app.tenant_id'', true), '''')::uuid)',
        tbl);
END
$fn$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION platform.enable_tenant_rls(regclass) FROM PUBLIC;
