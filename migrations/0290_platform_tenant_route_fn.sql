-- Ghi tuyến tenant từ mã ứng dụng (kế hoạch M2, Task M2-33; A24, Q-X8, R6): crm_app chỉ đọc platform.tenant_routes, nên
-- đây là đường duy nhất để M3 (carrier_account), M4 (fb_page) và M5 (service_key) ghi tuyến của tenant trong ngữ cảnh.
-- Theo mẫu platform.add_staff_route (0113): SECURITY DEFINER, search_path rỗng, tenant lấy từ app.tenant_id (do
-- tenancy.WithTenant đặt), không nhận tham số tenant. Không mốc nào CREATE OR REPLACE lại hàm này.
-- Pha mở rộng: chỉ thêm hàm và quyền. Thứ tự khóa: docs/adr/0046-thu-tu-khoa.md nhóm 1 (platform.tenant_routes); chỉ
-- chèn ON CONFLICT DO NOTHING rồi đọc, không khóa dòng đã có.
-- Lỗi: thiếu ngữ cảnh tenant → 42501; kind ngoài ba loại hay khóa rỗng → 22023; khóa đã thuộc tenant khác → 23505
-- (tenancy.PutRoute đổi thành ErrRouteTaken). Khóa đã thuộc chính tenant này → không làm gì.

-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION platform.put_tenant_route(p_kind text, p_key text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = ''
AS $fn$
DECLARE
    tid   uuid := NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid;
    owner uuid;
BEGIN
    IF tid IS NULL THEN
        RAISE EXCEPTION 'put_tenant_route: thiếu ngữ cảnh tenant' USING ERRCODE = '42501';
    END IF;
    IF p_kind IS NULL OR p_kind NOT IN ('carrier_account', 'fb_page', 'service_key') THEN
        RAISE EXCEPTION 'put_tenant_route: loại tuyến không được phép' USING ERRCODE = '22023';
    END IF;
    IF p_key IS NULL OR pg_catalog.btrim(p_key) = '' THEN
        RAISE EXCEPTION 'put_tenant_route: khóa rỗng' USING ERRCODE = '22023';
    END IF;
    INSERT INTO platform.tenant_routes (kind, external_key, tenant_id)
    VALUES (p_kind, p_key, tid)
    ON CONFLICT (kind, external_key) DO NOTHING;
    IF NOT FOUND THEN
        SELECT r.tenant_id INTO owner FROM platform.tenant_routes AS r
        WHERE r.kind = p_kind AND r.external_key = p_key;
        IF owner IS DISTINCT FROM tid THEN
            RAISE EXCEPTION 'put_tenant_route: khóa đã thuộc tenant khác' USING ERRCODE = '23505';
        END IF;
    END IF;
END
$fn$;
-- +goose StatementEnd

REVOKE EXECUTE ON FUNCTION platform.put_tenant_route(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.put_tenant_route(text, text) TO crm_app;
