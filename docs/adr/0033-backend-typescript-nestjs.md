# ADR-33 — Backend: giữ TypeScript và NestJS, kèm bốn điều kiện

Trạng thái: đã thay bởi ADR-44 (15/11/2026)
Ngày: 15/11/2026
Kiểm bằng: không áp dụng (đã thay bởi ADR-44)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-33. Tệp này không chép lại quyết định.

Chủ shop chốt viết backend bằng Go. Bốn điều kiện của ADR này được giữ ở dạng Go (ADR-44, mục "Bốn chốt"); phần
TypeScript, NestJS, Fastify, `@fastify/otel`, zod, SWC, `tsc --noEmit` không còn áp dụng.

## Kiểm ở đâu

- Bốn chốt ở dạng Go: kiểm ở ADR-44.
