# Địa giới theo hãng (Task 109, M3-18, Q-19)

Ghi chung cho mọi hãng; tệp `docs/carriers/<hãng>.md` của từng hãng nhắc tới tệp này khi hãng có tên địa giới đặc biệt.

## Cách khớp

- Nút "Đồng bộ địa giới" (`POST /api/v1/carrier-accounts/{id}/locations/sync`) ghi lệnh nền `command.carrier.locations`:
  đọc tỉnh của hãng, rồi phường/xã của từng tỉnh đã khớp; mỗi lần chạy tối đa 25 s, quá thì việc kế tiếp chạy tiếp từ
  tỉnh còn lại.
- Tên hai bên so sau khi bỏ dấu, chữ thường, bỏ dấu câu và một tiền tố cấp ở đầu: "tỉnh", "thành phố", "tp", "phường",
  "p", "xã", "đặc khu". Phường/xã chỉ so trong tỉnh đã khớp.
- Tên trùng (hai đơn vị cùng tên sau chuẩn hóa, ở bên nào cũng vậy) không ghép. Mã chưa khớp không bao giờ được gửi
  đoán: báo phí, tạo vận đơn trả `address_unmapped`.
- Danh sách mã của CRM chưa có mã hãng: `GET /api/v1/carrier-locations/unmapped?carrier=<khóa hãng>`.

## Thêm mã tay cho tên không khớp [P]

M3 không có API sửa tay (không có `PUT`). Khi một tên của hãng khác hẳn tên của bộ địa giới 2025 và cần gửi được ngay:

1. Lấy mã CRM từ danh sách chưa khớp, mã hãng từ tài liệu hoặc API địa giới của hãng.
2. Thêm migration dữ liệu mới (không sửa migration cũ), chạy bằng chủ bảng, theo từng shop:

   ```sql
   -- +goose Up
   INSERT INTO fulfillment.carrier_locations (tenant_id, carrier_key, level, our_code, carrier_code, carrier_name, synced_at)
   VALUES ('<tenant_id>', '<khóa hãng>', 'ward', '<mã CRM>', '<mã hãng>', '<tên của hãng>', now())
   ON CONFLICT (tenant_id, carrier_key, level, our_code) DO NOTHING;
   ```

3. Lần đồng bộ sau không xóa dòng này; nếu tên hai bên về sau khớp được thì đồng bộ ghi đè mã bằng mã hãng trả về.
