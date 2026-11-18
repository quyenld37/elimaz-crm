# ADR-28 — Bỏ trang admin; mọi việc quản lý nằm trên màn CRM

Trạng thái: đã duyệt (chủ shop chốt 12/11/2026); thay phần "admin giữ nguyên" của ADR-22
Ngày: 12/11/2026
Kiểm bằng: chưa có, làm ở mốc M5 (yêu cầu đơn hàng, hãng giao hàng trên web) và sau ngày mở (chiến dịch, banner, bộ phối, tắt admin)

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-28. Tệp này không chép lại quyết định.

Đích đến là một địa chỉ: màn CRM. Mọi mục của admin được dời sang CRM hoặc bỏ, rồi tắt ứng dụng admin. Website khi đó
đọc banner, bộ phối, chiến dịch từ CRM giống như đọc sản phẩm và tồn. Lịch dời theo bảng trong spec.

## Kiểm ở đâu

- Không còn đường dẫn nào của website gọi tới admin; tắt admin trên bản thử mà website và CRM chạy bình thường: chưa có,
  làm ở mốc M5 và sau ngày mở.
