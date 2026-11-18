# Sổ tay vận hành CRM

Tệp này lớn dần theo từng task; Task 23 soát lại toàn bộ.

## Màn hình nhân viên (`crm-console`, Task 17)

- Dựng: `cd console && npm ci && npm run build` → tệp tĩnh trong `console/dist`. Container `crm-console` chỉ phục vụ
  tệp tĩnh, không chạy mã phía máy chủ (ADR-34). Trước khi phát hành: `gitleaks dir --no-banner --redact console/dist`
  không thấy gì (`make console-check` đã chạy).
- API và màn hình cùng một nguồn gốc (origin): `/api/*` và `/health/*` chuyển tới `api`, mọi đường khác trả tệp tĩnh;
  đường không có tệp trả `index.html` (ứng dụng một trang).
- Header đề xuất cho mọi câu trả lời của `crm-console`:

  ```
  Content-Security-Policy: default-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'
  X-Content-Type-Options: nosniff
  Referrer-Policy: same-origin
  ```

  `connect-src 'self'` đủ cho client sinh và luồng `/api/v1/stream` vì cùng nguồn gốc. Thêm nguồn ảnh (kho tệp, M1)
  thì thêm đúng tên miền đó vào `img-src`, không dùng `*`.
- `index.html` gửi kèm `Cache-Control: no-cache`; tệp trong `assets/` có mã băm trong tên, gửi
  `Cache-Control: public, max-age=31536000, immutable`.
- Luồng `/api/v1/stream` phải đi qua HTTP/2 suốt đường và không bị cổng vào giữ lại (buffering) (spec 04 mục 6).
- Dev: `cd console && CRM_VITE_PORT=<cổng> npx vite` (mặc định 4192, chuyển `/api`, `/health` sang `CRM_API_URL`,
  mặc định `http://127.0.0.1:4190`). Không dùng 4175, 4176, 4180, 4181.

## Migration (`crm migrate`, Task 3, Task 318)

- `crm migrate` giữ `pg_advisory_lock(727001)` suốt lần chạy: hai tiến trình migrate cùng lúc chạy lần lượt, mỗi tệp
  áp dụng đúng một lần.
- Trước khi chạy, mọi tệp đã áp dụng được so sha256 với `platform.migration_checksums`; tệp bị sửa hay bị mất thì
  từ chối cả lần chạy và nêu tên tệp. Cách sửa: trả tệp về đúng bản đã áp dụng, thay đổi đặt trong tệp mới.
- Tệp sắp chạy có lệnh thu hẹp (`DROP`, `RENAME`, đổi kiểu, `SET NOT NULL`) thiếu dòng `-- contract:` thì từ chối.
- **Số đến lệch thứ tự** (Task 318): tệp có số nhỏ hơn số lớn nhất đã áp dụng (ví dụ `0010` gộp sau `0012`, `0114`
  sau `0124`, do các làn làm song song) vẫn chạy: goose chạy các tệp lệch số trước (theo số tăng dần), rồi tới tệp
  mới. Bảng `platform.goose_db_version` vì thế ghi theo thứ tự chạy, không theo số. Checksum và luật `-- contract:`
  vẫn kiểm cả tệp lệch số.
- Luật gộp không đổi: trong một mốc vẫn gộp theo thứ tự số khi được; lệch thì vẫn chạy, không đổi số của migration
  đã gộp. Tệp đến sau không được giả định tệp số lớn hơn chưa chạy (ví dụ không tạo lại bảng mà tệp số lớn đã tạo).
