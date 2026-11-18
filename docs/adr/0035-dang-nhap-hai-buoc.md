# ADR-35 — Đăng nhập hai bước; tài khoản và đăng nhập nằm trong CRM

Trạng thái: đã duyệt (spec 07-adr, 15/11/2026); thay ADR-10 và mục 6 của ADR-30
Ngày: 15/11/2026
Kiểm bằng: `TestAuthTokens`, `TestAuthUsersAndGrants` (internal/httpapi/auth_test.go); `TestCheckMFA`, `TestCheckMFAProperty` (internal/modules/identity/permissions_test.go); `TestVerify` (internal/adapters/cfaccess/cfaccess_test.go); đường đăng nhập riêng: chưa có, làm ở mốc M7

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-35. Tệp này không chép lại quyết định.

CRM tự giữ bảng người dùng, vai trò, phiên. Đường chính: Cloudflare Access với nhà cung cấp danh tính bắt buộc hai
bước, API kiểm thẻ có dấu đã qua bước hai. Đường riêng của CRM: mật khẩu cộng bước hai, tắt sẵn, bật có hạn 24 giờ.
Không tự viết phần mật mã.

## Kiểm ở đâu

- Thẻ Access thiếu dấu bước hai bị từ chối: `TestAuthTokens` (internal/httpapi/auth_test.go); `TestCheckMFA`, `TestCheckMFAProperty`
  (internal/modules/identity/permissions_test.go); chữ ký, hạn, xoay khóa: `TestVerify`, `TestKeyRotation` (internal/adapters/cfaccess/cfaccess_test.go).
- Khóa tài khoản có hiệu lực ngay ở đường Access: `TestAuthUsersAndGrants` (auth_test.go); `TestLock_LastOwner` (internal/modules/identity/staff_test.go).
- Đường riêng tắt sẵn thì không tồn tại; sai mật khẩu nhiều lần thì khóa và cảnh báo; thu hồi phiên có hiệu lực ngay;
  soát OWASP ASVS 5.0: chưa có, làm ở mốc M7.
