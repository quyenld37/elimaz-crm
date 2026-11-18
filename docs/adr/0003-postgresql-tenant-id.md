# ADR-03 — PostgreSQL duy nhất, có `tenant_id`

Trạng thái: đã duyệt (spec 07-adr, 11/11/2026); bổ sung bởi ADR-14
Ngày: 11/11/2026
Kiểm bằng: `TestCheckSchemaRulesEachRule` (internal/platform/tenancy/schemarules_test.go); `TestTenantsSeeOnlyOwnRows`, `TestPropertyQueriesOnlySeeOwnTenant` (internal/platform/tenancy/tenancy_test.go)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-03. Tệp này không chép lại quyết định.

PostgreSQL cho mọi dữ liệu giao dịch. Mọi bảng có `tenant_id`, mọi index và ràng buộc unique bắt đầu bằng
`tenant_id`, bật Row Level Security làm lớp chặn thứ hai. Không xây tính năng cho thuê (gói cước, tự đăng ký).

## Kiểm ở đâu

- Mọi bảng có `tenant_id`, unique bắt đầu bằng `tenant_id`, RLS bật: `TestCheckSchemaRulesCleanSchema`,
  `TestCheckSchemaRulesEachRule`, `TestCheckSchemaRulesFourViolationsThenPublic` (internal/platform/tenancy/schemarules_test.go);
  `TestSchemaRulesStillClean` (internal/modules/identity/users_test.go).
- RLS chặn dữ liệu tenant khác: `TestTenantsSeeOnlyOwnRows`, `TestInsertForOtherTenantViolatesPolicy`,
  `TestPropertyQueriesOnlySeeOwnTenant` (internal/platform/tenancy/tenancy_test.go).
