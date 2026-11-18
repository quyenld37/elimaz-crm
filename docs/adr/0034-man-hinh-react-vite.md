# ADR-34 — Màn hình nhân viên là ứng dụng React chạy trên trình duyệt, dựng bằng Vite

Trạng thái: đã duyệt (spec 07-adr, 15/11/2026); thay phần "Next.js" của ADR-09 và ADR-22
Ngày: 15/11/2026
Kiểm bằng: `make console-check` (gồm gitleaks trên `console/dist`); console/src/shell/Banner.test.tsx, console/src/shell/apiHealth.test.ts; CSP: chưa có test (phát hiện Task 23)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-34. Tệp này không chép lại quyết định.

Màn hình là ứng dụng React một trang, dựng bằng Vite thành tệp tĩnh; container `crm-console` chỉ phục vụ tệp tĩnh.
Dữ liệu qua client sinh từ OpenAPI; realtime qua lớp bọc SSE riêng; danh sách dài dùng danh sách ảo.

## Kiểm ở đâu

- Bản dựng không chứa khóa bí mật: `make console-check` (bước `gitleaks dir console/dist`).
- Màn hình vẫn tải khi API tạm ngừng và hiện băng báo: console/src/shell/Banner.test.tsx, console/src/shell/apiHealth.test.ts,
  console/src/shell/Layout.test.tsx.
- Client sinh từ OpenAPI không lệch hợp đồng: `make gen-check`.
- Chính sách bảo mật nội dung (CSP) không cho chạy mã lạ: header mới ghi trong `docs/RUNBOOK.md`, chưa có test (phát hiện Task 23).
