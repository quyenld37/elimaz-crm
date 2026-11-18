# ADR-47 — Ngoại lệ của khóa chống trùng: POST chỉ đọc và các bước đăng nhập

Trạng thái: đã duyệt (quyết định soát chéo Q-X5, 18/11/2026; điều phối viên quyết theo ủy quyền, chờ chủ shop xem lại); bổ sung ADR-41
Ngày: 18/11/2026
Kiểm bằng: `TestRejectsBadContracts`, `TestGeneratesIdempotencyModes` (tools/routegen/main_test.go); `TestIdempotencyExempt`, `TestIdempotencyNoReplay` (internal/httpapi/idempotency_test.go); `TestRoutesIdempotencyModes` (tests/arch/routes_test.go)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-47. Tệp này không chép lại quyết định.

Một phần mở rộng OpenAPI duy nhất `x-idempotency` (giá trị `exempt` hoặc `no-replay`) kèm `x-idempotency-reason` bắt
buộc; `routegen` từ chối khai sai. `exempt` chỉ cho POST chỉ đọc và các bước đăng nhập; `no-replay` cho thao tác trả
bí mật một lần (có khóa, không lưu thân trả lời).

## Kiểm ở đâu

- Luật `routegen` (mỗi dòng bảng điểm 2 thoát khác 0, tệp hợp lệ mang cờ và lý do): `TestRejectsBadContracts`, `TestGeneratesIdempotencyModes` (tools/routegen/main_test.go).
- Middleware: route `exempt` không khóa vẫn chạy, không ghi `idempotency_keys`; route bắt buộc bên cạnh vẫn 400: `TestIdempotencyExempt` (internal/httpapi/idempotency_test.go).
- `no-replay`: `response_body` NULL, lần gửi lại không chứa bí mật: `TestIdempotencyNoReplay` (internal/httpapi/idempotency_test.go); `TestCompleteNilBodyStoresNull` (internal/platform/idempotency/idempotency_test.go).
- Test kiến trúc liệt kê route: `TestRoutesIdempotencyModes` (tests/arch/routes_test.go).
- Test của từng operation được miễn (POST chỉ đọc không ghi dòng nào; bước đăng nhập gửi hai lần không tạo hai phiên): chưa có, làm ở mốc M5 và M7.
