# ADR-11 — Khởi động mới, không dùng dữ liệu Nhanh

Trạng thái: đã duyệt (spec 07-adr, 11/11/2026)
Ngày: 11/11/2026
Kiểm bằng: bước kiểm: review mỗi task (luật "CRM không bao giờ gọi Nhanh, không đọc dữ liệu Nhanh" trong `apps/crm/CLAUDE.md`); chưa có test tự động (phát hiện Task 23); đêm mở: chưa có, làm ở mốc M7

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-11. Tệp này không chép lại quyết định.

Không nhập dữ liệu, không đọc Nhanh, không chạy song song. Sản phẩm nhập mới, tồn mở sổ bằng kiểm đếm thực tế, khách
và hội thoại bắt đầu trống. Mở trong một đêm. Chi tiết ở spec 06.

## Kiểm ở đâu

- Không có mã gọi hay đọc Nhanh: chỉ dựa vào review; chưa có test tự động (phát hiện Task 23).
- Nhập sản phẩm mới bằng file Excel mẫu: `TestValidate_Template200` (internal/modules/catalog/internal/importer/validate_test.go).
- Mở sổ tồn bằng kiểm đếm: `TestStocktake_*` (internal/modules/inventory/stocktake_test.go), ví dụ `TestStocktake_Overcount`.
- Trình tự đêm mở và diễn tập: chưa có, làm ở mốc M7.
