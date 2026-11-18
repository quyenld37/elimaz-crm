# ADR-18 — Quyền là cặp (quyền, phạm vi); guard toàn cục; chống CSRF; khóa dịch vụ

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026); câu "không dùng cookie để xác thực" chỉ còn đúng cho đường Cloudflare Access (ADR-35)
Ngày: 12/11/2026
Kiểm bằng: `TestCanTable`, `TestCanProperty` (internal/modules/identity/permissions_test.go); `TestRoutesPublicSetAndPermissions` (tests/arch/routes_test.go); `TestOriginRejectsWrites` (internal/httpapi/middleware_test.go); khóa dịch vụ: chưa có, làm ở mốc M5

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-18. Tệp này không chép lại quyết định.

Mỗi lần gán vai trò kèm phạm vi bắt buộc; không có dòng phạm vi là không có quyền. Guard toàn cục, mặc định từ chối,
route công khai trong danh sách trắng. Thẻ Access chỉ đọc từ header; request ghi phải JSON và có `Origin` hợp lệ.
Website dùng khóa dịch vụ (M5).

## Kiểm ở đâu

- Điểm 1: `TestCanTable`, `TestScopesFor`, `TestCanProperty` (internal/modules/identity/permissions_test.go);
  `TestWarehouseScopedRole` (internal/httpapi/auth_test.go); `TestGrantScopeCheck` (internal/modules/identity/users_test.go).
- Điểm 2: `TestRoutesPublicSetAndPermissions`, `TestRoutesAllServed` (tests/arch/routes_test.go); `TestServerRegistersEveryRoute`
  (internal/httpapi/middleware_test.go); `TestAuthWithoutVerifierDenies` (internal/httpapi/auth_test.go).
- Điểm 3: `TestOriginRejectsWrites`, `TestOriginSkipsReads` (internal/httpapi/middleware_test.go); `TestVerify` (internal/adapters/cfaccess/cfaccess_test.go).
- Điểm 4 (khóa dịch vụ băm, thu hồi được): chưa có, làm ở mốc M5.
