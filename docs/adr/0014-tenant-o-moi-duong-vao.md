# ADR-14 — Tenant có ngữ cảnh ở mọi đường vào

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026)
Ngày: 12/11/2026
Kiểm bằng: internal/platform/tenancy (tenancy_test.go, schemarules_test.go, search_test.go); `TestAppRolePrivilegesAndRLS` (internal/modules/identity/users_test.go); `TestHandlerRunsInTenantContext` (internal/platform/queue/worker_test.go)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-14. Tệp này không chép lại quyết định.

Policy dùng `NULLIF(current_setting('app.tenant_id', true), '')::uuid`; bảng định tuyến `tenant_routes`; mọi việc
mang `tenant_id`, handler chạy trong `withTenant`; tiến trình chỉ dùng role `crm_app`; khóa ngoại và unique ghép
`tenant_id`; tìm theo tên qua hàm `SECURITY DEFINER` tự lọc tenant.

## Kiểm ở đâu

- Điểm 1 (`NULLIF`, thiếu ngữ cảnh thấy 0 dòng): `TestReusedConnectionOutsideContextSeesNothing`, `TestTenantsSeeOnlyOwnRows` (internal/platform/tenancy/tenancy_test.go).
- Điểm 2 (`tenant_routes`, việc mang tenant, việc định kỳ cho từng tenant): `TestResolveAndListTenantIDs` (tenancy_test.go);
  `TestHandlerRunsInTenantContext` (internal/platform/queue/worker_test.go); `TestTickPerTenant` (internal/platform/schedules/schedules_test.go).
- Điểm 3 (chỉ role `crm_app`): `TestAppRolePrivilegesAndRLS` (internal/modules/identity/users_test.go);
  `TestAppRoleCannotCreateTable` (internal/platform/db/db_test.go); `TestLoadNonMigrateNeverHoldsOwnerURL` (internal/platform/config/config_test.go).
- Điểm 4 (khóa ghép, đọc catalog báo thiếu): `TestChildPointingToOtherTenantParentViolatesForeignKey` (tenancy_test.go);
  `TestCheckSchemaRulesEachRule` (internal/platform/tenancy/schemarules_test.go).
- Điểm 5 (hàm tìm kiếm, `EXPLAIN` bằng role ứng dụng): `TestSearchFunctionSeesOnlyOwnTenant`, `TestSearchPlans` (internal/platform/tenancy/search_test.go).
- Tìm theo đuôi SĐT trên bảng khách: chưa có, làm ở mốc M2.
