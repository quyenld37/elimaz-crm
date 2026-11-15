---
paths:
  - "cmd/**/*.go"
  - "internal/**/*.go"
  - "tools/**/*.go"
  - "tests/**/*.go"
---

# Luật Go của CRM

Chỉ ghi luật linter không tự bắt được hết. Phần linter bắt được nằm ở `.golangci.yml`.

1. Interface đặt ở phía **dùng**, không đặt cạnh bản hiện thực "để mock". Hàm khởi tạo trả kiểu cụ thể. Chỉ thêm interface khi có từ hai bản hiện thực, hoặc một chỗ cắm bản giả thật sự cần (đồng hồ, dịch vụ bên ngoài, database trong test đặc biệt). Không thêm interface trước cho mọi struct.
2. Lỗi: bọc một lần bằng `%w` với tên việc đang làm (không viết "failed to"); kiểm bằng `errors.Is`/`errors.As`; xử lý một lần (hoặc trả lên, hoặc log ở mép, không cả hai). Sentinel và kiểu lỗi của nghiệp vụ nằm trong module sinh ra nó; đổi sang HTTP chỉ ở `internal/httpapi/errors.go`.
3. `context.Context` là tham số đầu tiên, truyền tường minh; không cất trong struct. Mọi lời gọi ra ngoài và mọi truy vấn nhận `ctx`.
4. Không biến toàn cục thay đổi được, không `init()`. Phụ thuộc được truyền vào hàm khởi tạo và nối trong `cmd/crm/main.go`. Được phép ở cấp gói: hằng, lỗi mẫu `var ErrX = errors.New(...)`, biểu thức `regexp.MustCompile` (giống những gì linter `gochecknoglobals` cho phép).
5. Mỗi goroutine có chủ chờ nó kết thúc và có đường dừng qua `ctx`. Không `go f()` rồi bỏ đó.
6. Tên gói là danh từ nói rõ việc gói cung cấp; không `util`, `common`, `helpers`, `shared`, `misc`. Không lặp tên gói trong tên kiểu (`queue.Worker`, không `queue.QueueWorker`).
7. Module nghiệp vụ (`internal/modules/**`) không nhập `net/http`, `internal/httpapi/**` hay mã ogen. Lớp HTTP chỉ đọc yêu cầu, kiểm quyền, gọi public API của module, trả kết quả.
8. Phần riêng của module nằm trong `internal/modules/<tên>/internal/`. Module khác chỉ dùng phần công khai.
9. Tiền là `int64` đồng, không dùng số thực. Thời gian lưu và so sánh bằng UTC.
10. Test: gói `testing` chuẩn, test bảng (table-driven) khi có nhiều ca, `rapid` cho quy luật phải luôn đúng, PostgreSQL thật qua `internal/testkit`. Không giả database bằng mock cho câu SQL.
