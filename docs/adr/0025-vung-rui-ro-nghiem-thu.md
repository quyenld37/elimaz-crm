# ADR-25 — Vùng rủi ro và cách nghiệm thu

Trạng thái: đã duyệt (spec 07-adr, 12/11/2026)
Ngày: 12/11/2026
Kiểm bằng: test rapid ở các gói vùng cao (ví dụ `TestPropertyQueue`, `TestCODLedgerProperty`, `TestPropertyNeverSentTwice`); mutation testing gremlins `make mutation`, `.gremlins.yaml`, kết quả `docs/mutation.md` (18/11/2026: mọi gói vùng cao ≥ 80% sau test thêm; queue, commands tính bằng cài tay từng đột biến, chờ lượt gremlins theo lịch xác nhận); review độc lập: agent `reviewer` mỗi task, Task 23 bước 6

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-25. Tệp này không chép lại quyết định.

Hai vùng rủi ro; vùng cao thêm test sinh tình huống ngẫu nhiên, mutation testing trước khi đóng mốc, review độc lập.
Chủ shop nghiệm thu bằng thứ nhìn thấy được. Luật phải luôn đúng thì gài vào máy (CI, bước kiểm trước commit).

## Kiểm ở đâu

- Property-based testing vùng cao: `TestPropertyQueue` (internal/platform/queue/property_test.go), `TestPropertyNeverSentTwice`
  (internal/platform/commands/property_test.go), `TestPropertyQueriesOnlySeeOwnTenant` (internal/platform/tenancy/tenancy_test.go),
  `TestCODLedgerProperty` (internal/modules/cash/property_test.go), `TestInventoryInvariants` (internal/modules/inventory/property_test.go).
- Danh sách thư mục vùng cao trong repo và mutation testing: mutation testing gremlins `make mutation`, `.gremlins.yaml`, kết quả `docs/mutation.md` (18/11/2026: mọi gói vùng cao ≥ 80% sau test thêm; queue, commands tính bằng cài tay từng đột biến, chờ lượt gremlins theo lịch xác nhận).
- Review độc lập: agent `reviewer` (dấu duyệt do hook ghi); review toàn nhánh: Task 23 bước 6.
- Nghiệm thu bằng kịch bản chạy được: `TestMotDonDiTron` (tests/e2e/mot_don_di_tron_test.go); `docs/nghiem-thu-m1.md`.
- Gài luật vào máy: `make ci` (lint, gen-check, sqlc-vet, test-guard, gitleaks…).
