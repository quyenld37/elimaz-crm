---
paths:
  - "openapi/**"
---

# Luật hợp đồng API

1. `openapi/openapi.yaml` là nguồn; mã máy chủ (ogen), bảng route và client của màn hình đều sinh từ đây. Sửa xong chạy `make gen`.
2. Mỗi operation có `operationId` và đúng một trong `x-permission: <khóa quyền>` hoặc `x-public: true`. Route công khai chỉ là ba đường health và các đường đăng nhập riêng của CRM (ADR-47, kế hoạch M7); mỗi route công khai ngoài health phải có giới hạn tần suất.
3. Mọi operation ghi (`POST`, `PUT`, `PATCH`, `DELETE`) có tham số `$ref: '#/components/parameters/IdempotencyKey'`. Ngoại lệ duy nhất (ADR-47): operation có `x-idempotency: exempt` kèm `x-idempotency-reason: "<lý do>"`, chỉ cho POST không đổi dữ liệu (báo giá, báo phí ship) và các bước đăng nhập. Thiếu lý do thì test kiến trúc chặn.
4. Trường tiền: `type: integer, format: int64, maximum: 9007199254740991`.
5. Thay đổi phá vỡ (xóa hay đổi tên trường, đổi kiểu, thêm trường bắt buộc vào yêu cầu) đi theo mở rộng rồi thu hẹp: thêm cái mới bên cạnh, chuyển nơi dùng, rồi một task riêng xóa cái cũ. oasdiff chặn thay đổi phá vỡ nào chưa có dòng trong `openapi/breaking-approved.yaml` (ghi `operationId`, lý do, ngày chủ shop OK).
