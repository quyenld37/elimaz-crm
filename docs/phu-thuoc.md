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
