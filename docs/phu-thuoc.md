# Thư viện và công cụ ngoài danh sách ADR-44

Luật (ADR-32, kế hoạch M0 bản 5): thêm thư viện hay công cụ ngoài danh sách ADR-44 thì ghi một dòng ở đây trước khi dùng.

| Tên | Phiên bản | Giấy phép | Việc nó làm | Vì sao thư viện chuẩn hoặc ADR-44 không đủ | Ngày, task |
|---|---|---|---|---|---|
| gofumpt | v0.12.0 | BSD-3-Clause | Định dạng Go chặt hơn `gofmt` | Một cách định dạng duy nhất cho mọi tệp agent viết; chạy trong hook | 15/11/2026, Task 0 |
| goimports (golang.org/x/tools) | v0.52.0 | BSD-3-Clause | Sắp xếp và thêm import | `gofmt` không sửa import | 15/11/2026, Task 0 |
| gopls | v0.24.0 | BSD-3-Clause | Máy chủ ngôn ngữ Go cho plugin `gopls-lsp` | Tìm nơi dùng, đổi tên theo ký hiệu; báo lỗi biên dịch ngay sau mỗi lần sửa | 15/11/2026, Task 0 |
| typescript-language-server | 6.1.0 | Apache-2.0 (theo npm; GitHub không tự nhận ra giấy phép) | Máy chủ ngôn ngữ TypeScript cho plugin `typescript-lsp` | Như trên, cho màn hình | 15/11/2026, Task 0 |
| jscpd | 5.4.3 | MIT | Dò đoạn code trùng ở cả Go và TypeScript | `dupl` chỉ cho Go và chỉ bắt trùng gần nguyên văn | 15/11/2026, Task 0 |
| oasdiff | v1.38.0 | Apache-2.0 (GitHub API, 15/11/2026) | Báo thay đổi phá vỡ của `openapi.yaml` | Không có công cụ tương đương trong ADR-44 | 15/11/2026, Task 0 |
| rclone (image `rclone/rclone`, ghim digest) | 1.74.4 | MIT | Tải mọi object của bucket sao lưu về máy để kiểm mã hóa trong diễn tập; tạo bucket trên SeaweedFS | WAL-G không có lệnh tải object thô; chỉ dùng trong `docker/compose.backup-drill.yml`, không vào image sản phẩm | 18/11/2026, Task 2 |
| WAL-G | v3.0.9 (bản `pg-22.04`, sha256 ghim) | Apache-2.0 | Sao lưu, đẩy WAL, khôi phục PostgreSQL có mã hóa libsodium | Có trong spec (ADR-37), ghi ở đây để có phiên bản và giấy phép | 18/11/2026, Task 2 |
| SeaweedFS (image `chrislusf/seaweedfs`, ghim digest) | 4.48 | Apache-2.0 | Kho S3 cục bộ thay cho kho ngoài thật trong diễn tập | Có trong kế hoạch (cổng 8333); ghi phiên bản đang dùng | 18/11/2026, Task 2 |
