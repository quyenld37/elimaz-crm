# ADR-02 — TypeScript, NestJS + Fastify

Trạng thái: đã thay bởi ADR-44 (15/11/2026)
Ngày: 11/11/2026
Kiểm bằng: không áp dụng (đã thay bởi ADR-44)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-02. Tệp này không chép lại quyết định.

Chủ shop chốt viết backend bằng Go (ADR-44). Phần còn đúng của ADR này: API REST có phiên bản (`/api/v1`), website
và màn hình nhân viên dùng client sinh từ OpenAPI; ADR-44 đổi sang viết hợp đồng OpenAPI trước rồi sinh mã cho cả
hai phía.

## Kiểm ở đâu

- Phần còn đúng (hợp đồng OpenAPI là nguồn, mã sinh không lệch) kiểm ở ADR-44: `make gen-check`, `make oasdiff`,
  `TestRealContractMatchesCommittedRoutes` (tools/routegen/main_test.go).
