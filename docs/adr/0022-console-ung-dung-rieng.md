# ADR-22 — Console CRM là ứng dụng Next.js riêng

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026); phần "ứng dụng admin hiện có giữ nguyên" thay bởi ADR-28; phần Next.js thay bởi ADR-34
Ngày: 12/11/2026
Kiểm bằng: `make console-check`; phần Next.js không áp dụng (ADR-34); phần admin: kiểm ở ADR-28

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-22. Tệp này không chép lại quyết định.

Phần còn hiệu lực: Console CRM (Inbox, Đơn, Kho, Khách, CTV, Báo cáo) là một ứng dụng riêng, gọi API, dùng chung hệ
thiết kế đã duyệt (`DESIGN.md`).

## Kiểm ở đâu

- Ứng dụng riêng trong `console/`: `make console-check` (tsc, eslint, prettier, vitest, bản dựng); e2e `console/e2e/shell.spec.ts`.
- Dời admin sang CRM: kiểm ở ADR-28.
