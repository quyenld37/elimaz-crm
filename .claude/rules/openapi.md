---
paths:
  - "openapi/**"
---

# Luật hợp đồng API

1. `openapi/openapi.yaml` là nguồn; mã máy chủ (ogen), bảng route và client của màn hình đều sinh từ đây. Sửa xong chạy `make gen`.
2. Mỗi operation có `operationId` và đúng một trong `x-permission: <khóa quyền>` hoặc `x-public: true`. Route công khai chỉ là ba đường health và các đường đăng nhập riêng của CRM (ADR-47, kế hoạch M7); mỗi route công khai ngoài health phải có giới hạn tần suất.
3. Mọi operation ghi (`POST`, `PUT`, `PATCH`, `DELETE`) có tham số `$ref: '#/components/parameters/IdempotencyKey'`. Ngoại lệ duy nhất (ADR-47): operation có `x-idempotency: exempt` kèm `x-idempotency-reason: "<lý do>"`, chỉ cho POST không đổi dữ liệu (báo giá, báo phí ship) và các bước đăng nhập. Thao tác trả bí mật một lần (khóa dịch vụ, mã khôi phục) khai `x-idempotency: no-replay` kèm lý do: vẫn có `IdempotencyKey`, nhưng không lưu thân câu trả lời. Thiếu lý do thì `routegen` và test kiến trúc chặn.
4. Trường tiền: `type: integer, format: int64, maximum: 9007199254740991`.
5. Thay đổi phá vỡ (xóa hay đổi tên trường, đổi kiểu, thêm trường bắt buộc vào yêu cầu) đi theo mở rộng rồi thu hẹp: thêm cái mới bên cạnh, chuyển nơi dùng, rồi một task riêng xóa cái cũ. oasdiff chặn thay đổi phá vỡ nào chưa có dòng trong `openapi/breaking-approved.yaml` (ghi `operationId`, lý do, ngày chủ shop OK).
6. Đường của website (M5): mọi path `/api/v1/web/*` khai `x-auth: service_key` (khóa dịch vụ, chỉ mang quyền `web.*`); path khác không bao giờ khai `x-auth: service_key`. `x-admission: <tên>` (cổng nhận vào, ví dụ `flash`) chỉ trên operation `service_key` có khóa chống trùng bắt buộc. `x-body-limit: <byte>` trong khoảng 1 KiB – 8 MiB, mặc định 1 MiB. `routegen` kiểm ba luật này và nêu `operationId` khi sai.
