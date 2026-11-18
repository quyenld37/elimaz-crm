# ADR-13 — Tự xây toàn bộ, không lấy repo mã nguồn mở làm nền

Trạng thái: đã duyệt (spec 07-adr, 11/11/2026); câu "một ngôn ngữ (TypeScript)" không còn đúng theo ADR-44, quyết định và luật license giữ nguyên
Ngày: 11/11/2026
Kiểm bằng: bước kiểm: review mỗi task; mỗi thư viện ngoài ADR-44 ghi tên, phiên bản, giấy phép vào `docs/phu-thuoc.md`; không có test tự động

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-13. Tệp này không chép lại quyết định.

Tự xây toàn bộ theo thiết kế. Không fork, không nhúng dự án nào làm nền. Chỉ mượn thiết kế (đọc code để học rồi tự
viết lại) và dùng lại một bộ dữ liệu địa chỉ có license MIT. Code AGPL/LGPL chỉ đọc, không chép nguyên đoạn.

## Kiểm ở đâu

- Giấy phép thư viện: `docs/phu-thuoc.md` (luật trong `apps/crm/CLAUDE.md`), review của mỗi task.
- Bộ dữ liệu địa chỉ MIT: chưa có, làm ở mốc M2 (`customer`).
