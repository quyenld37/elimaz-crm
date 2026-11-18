# ADR-39 — Valkey là lớp chắn và tăng tốc, không bao giờ là nguồn sự thật

Trạng thái: đã duyệt (spec 07-adr, 15/11/2026); thư viện nối là valkey-go (ADR-44)
Ngày: 15/11/2026
Kiểm bằng: luật kiến trúc (d) "chỉ `internal/platform` được nhập valkey-go" trong `TestBoundariesNotCalledModule`, `TestBoundariesRealCode` (tests/arch/boundaries_test.go); phần còn lại: chưa có, làm ở mốc M5

Nội dung đầy đủ: spec `docs/crm/specs/2026-11-10-crm-rieng/07-adr.md`, mục ADR-39. Tệp này không chép lại quyết định.

Valkey chỉ cho việc cần nhanh và chịu được mất: bộ đệm đọc, giới hạn tần suất, cổng điều tiết flash sale, tầng dưới
báo hiệu realtime. Không quyết định nghiệp vụ nào lấy từ Valkey; chỉ `platform` gọi Valkey qua `ReadCache`,
`RateLimiter`, `AdmissionGate`. Mọi đường dùng Valkey phải chạy được khi Valkey ngừng.

## Kiểm ở đâu

- Test kiến trúc cấm module nghiệp vụ gọi Valkey: luật (d) trong tests/arch/boundaries_test.go (đã có trước khi có mã Valkey).
- Giới hạn tần suất trong tiến trình (đường lùi khi Valkey ngừng): `TestUserLimiterSliding`, `TestUserRateLimitHTTP` (internal/httpapi/ratelimit_test.go).
- Tắt Valkey giữa bài thử tải; xóa sạch Valkey rồi kiểm dựng lại; "đổi giá thì bộ đệm đổi theo": chưa có, làm ở mốc M5.
