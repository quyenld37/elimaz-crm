# ADR-09 — Giao diện CRM là ứng dụng Next.js riêng gọi API

Trạng thái: đã duyệt (spec 07-adr, 11/11/2026); phần "Next.js" thay bởi ADR-34; câu "mở rộng ứng dụng admin hiện có" làm rõ bởi ADR-22 (ứng dụng riêng)
Ngày: 11/11/2026
Kiểm bằng: `make console-check` (tsc, eslint, vitest, bản dựng); `TestBoundariesRealCode` (tests/arch/boundaries_test.go) cho "nghiệp vụ nằm ở API"; phần Next.js không áp dụng (ADR-34)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-09. Tệp này không chép lại quyết định.

Phần còn hiệu lực: toàn bộ nghiệp vụ nằm ở API; màn hình chỉ dựng giao diện, dùng hệ thiết kế đã duyệt; mọi màn
hình có mockup được duyệt trước. Trang CTV nằm trên website.

## Kiểm ở đâu

- Màn hình là ứng dụng riêng trong `console/`, gọi API qua client sinh: `make console-check`; `console/src/app/*.test.ts`.
- Mọi kiểm quyền nằm ở API: `TestRoutesPublicSetAndPermissions` (tests/arch/routes_test.go).
- Trang CTV trên website: chưa có, làm ở P2.
