# ADR-27 — Mỗi module một schema; view `security_invoker`

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026); bổ sung ADR-20
Ngày: 12/11/2026
Kiểm bằng: `TestSQLUsageProductCode`, `TestSQLUsageViewRules` (tests/arch/sqlusage_test.go); `TestCheckSchemaRulesFourViolationsThenPublic`, `TestCheckSchemaRulesViewsAndSeller` (internal/platform/tenancy/schemarules_test.go); `TestUsersView` (internal/modules/identity/staff_test.go)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-27. Tệp này không chép lại quyết định.

Mỗi module giữ bảng trong schema riêng, không bảng nào trong `public`. Module chỉ nhắc schema của chính nó và view đã
công bố của module khác. View công bố có `security_invoker = true`. Khóa ngoại xuyên module là đánh đổi có chủ ý.

## Kiểm ở đâu

- Điểm 1 (không bảng trong `public`): `TestCheckSchemaRulesFourViolationsThenPublic` (internal/platform/tenancy/schemarules_test.go).
- Điểm 2 (quét SQL): `TestSQLUsageReportsSample`, `TestSQLUsageProductCode`, `TestSQLUsageViewRules` (tests/arch/sqlusage_test.go).
- Điểm 3 (`security_invoker`): `TestCheckSchemaRulesViewsAndSeller` (schemarules_test.go); `TestViewsDocMatchesDatabase` (tests/arch/viewsdoc_test.go).
- View công bố không trả dòng tenant khác: `TestUsersView` (internal/modules/identity/staff_test.go), `TestFileVariantsView`
  (internal/platform/files/variants_view_test.go).
