# ADR-01 — Modular monolith

Trạng thái: đã duyệt (spec 07-adr, 11/11/2026); bổ sung bởi ADR-16 (vai trò `ingest`), ADR-20 (đọc qua view), ADR-44 (ranh giới bằng `internal`), ADR-45 (tiến trình `imager`)
Ngày: 11/11/2026
Kiểm bằng: `TestBoundariesRealCode`, `TestBoundariesNotCalledModule` (tests/arch/boundaries_test.go); `TestBootEachRoleThenGracefulStop` (tests/e2e/boot_test.go); `TestProdCompose_ImagerIsolated` (tests/deploy/compose_test.go)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-01. Tệp này không chép lại quyết định.

Một codebase, một database, chia module có ranh giới kiểm bằng công cụ trong CI. Cùng một image chạy các vai trò
`ingest`, `api`, `worker` (cập nhật 12/11, 15/11); `imager` là image riêng có libvips (ADR-45).

## Kiểm ở đâu

- Ranh giới module (không nhập phần `internal` của module khác, chỉ gọi module trong cột "Được gọi"):
  `TestBoundariesTestdata`, `TestBoundariesNotCalledModule`, `TestBoundariesRealCode` (tests/arch/boundaries_test.go).
- Một tệp chạy, nhiều vai trò, tắt êm: `TestBootEachRoleThenGracefulStop`, `TestBootUnknownRoleFails` (tests/e2e/boot_test.go).
- `imager` tách khỏi image chính: `TestProdCompose_ImagerIsolated` (tests/deploy/compose_test.go).
