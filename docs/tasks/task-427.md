# Task 427 — main đỏ: TestM2Routes_Unimplemented dùng route đã làm (M2-x1)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m2-khach-don.md` (mục Task 427, M2-x1). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài sẽ đổi (LSP findReferences): không có. Chỉ đổi tệp test của gói `internal/httpapi`: `errors_m2_test.go` (`TestM2Routes_Unimplemented`) và `serve_test.go` (hàm không xuất `testServerDeps` mới, `testServer` gọi nó; `testServer` có nhiều chỗ gọi trong gói, chữ ký giữ nguyên).
- Gói phụ thuộc ngược (go list): không áp dụng — chỉ đổi tệp `_test.go`, không gói nào nhập test của `httpapi`.
- Operation OpenAPI bị chạm, oasdiff: không. Đã dò: 129 operation trong `Routes`, mọi operation có phương thức trên `*handler` (trừ `stream`, đi handler SSE riêng) → không còn route M2 nào trả 501 thật.
- Chỗ dùng trên màn hình: không.
- Câu sqlc, bảng, pha migration: không.
- Test sẽ chạy: `go test -race ./internal/httpapi/ -run 'TestM2Routes_Unimplemented'` lúc viết; cả gói `./internal/httpapi/...` qua hook/ci-task.sh.

## Tái dùng

- Đã kiểm trong danh mục dùng chung và tìm theo tên, theo việc: `docs/danh-muc-dung-chung.md` không có trình dựng máy chủ giả; trong gói có `testServer`, `serveAs`, `expect`, `chain`, `writeHandlerError`, `gen.UnimplementedHandler`.
- Dùng lại / viết mới vì: dùng lại `chain` (đúng chuỗi auth → guard → … của route thật), `writeHandlerError` (đường đổi `ht.ErrNotImplemented` → 501), `gen.UnimplementedHandler` (handler giả: mọi operation trả `ht.ErrNotImplemented`, đúng thứ mà route chưa làm trả trong máy chủ thật), `serveAs`, `expect`. Mới: `testServerDeps` (tách phần dựng Deps khỏi `testServer` để không chép Deps).

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5.1–5.12: không áp dụng: chỉ đổi test của tầng HTTP, không đổi mã chạy, hợp đồng, database, quyền hay hàng đợi. Test giữ nguyên tiêu chí cũ (501 `not_implemented` có `request_id`, 403 khi không grant, 401 khi không thẻ, 400 thiếu Idempotency-Key).
- 6.1–6.13: không áp dụng: không có đường chạy đồng thời, transaction hay khóa mới.

## Quyết định thay chủ shop

- Không còn route nào chưa làm (đã dò cả 129 operation), nên theo nhánh thứ hai của kế hoạch: kiểm 501 bằng handler giả (`gen.UnimplementedHandler`) gắn vào route thật `getOrderTimeline` qua `chain` thật và `writeHandlerError` thật. Vì sao: test không còn phụ thuộc việc route nào đang dở, không gãy khi làm thêm route. Phương án khác: (a) thêm chỗ cắm handler trong `newMux` ở mã chạy — phủ thêm 3 dòng `gen.WithErrorHandler` nhưng đổi mã chạy và phải tách commit cấu trúc; (b) dò route chưa làm bằng AST lúc test — hiện ra tập rỗng, test sẽ luôn rơi vào nhánh giả, thêm mã vô ích. Dễ đảo ngược: chỉ tệp test.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| `go test ./internal/httpapi/` xanh với route chưa làm | `TestM2Routes_Unimplemented` (`internal/httpapi/errors_m2_test.go`) | `go test -race ./internal/httpapi/ -run TestM2Routes_Unimplemented -count=1` | `ok elimaz.com.vn/crm/internal/httpapi 1.766s` (trước sửa: 404 `not_found` thay vì 501 vì Task 81 đã làm route) |
| Test đỏ khi tạm phá body lỗi 501 (mã lỗi sai) | như trên; tạm đổi `codeNotImplemented` → `codeInternal` ở `errors.go:175`, rồi khôi phục | như trên | `FAIL … chủ, chưa làm: lỗi "internal", mong "not_implemented"` |
| Test đỏ khi tạm phá body lỗi 501 (thiếu `request_id`) | như trên; tạm viết thân `{"error":"not_implemented"}` thẳng ở `errors.go:175`, rồi khôi phục | như trên | `FAIL … body lỗi chỉ được có error và request_id, có: {"error":"not_implemented"}` |
| Phần được kiểm là mã thật, không phải bản giả | handler giả chỉ là `gen.UnimplementedHandler` (mã sinh, trả `ht.ErrNotImplemented`); `chain` (auth, guard, userRateLimit), `writeHandlerError`, `writeError` là mã chạy thật | — | 403 `forbidden`, 401 `unauthenticated` vẫn kiểm trên cùng máy chủ giả; 400 `idempotency_key_required` trên máy chủ thật `testServer` |
| ci-task.sh xanh | cổng kiểm của task | `ci-task.sh` (sau `git merge main`: Already up to date) | `ok …/cmd/crm`, `ok …/internal/httpapi 115.686s`, `ok …/platform/realtime`, `ok …/tests/arch`, `ok …/tests/e2e`; `CI-TASK XANH (so với điểm tách nhánh 8d43c64)` |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT (lần 1). Không có phát hiện phải sửa. Reviewer đếm lại: 129 operation trong `Routes`, 128 phương thức trên `*handler` cộng `stream` (SSE riêng), nên đúng nhánh handler giả. Ghi chú không bắt buộc: closure `gen.WithErrorHandler` của `newMux` không được test đi qua trực tiếp (đã nêu ở phương án (a), rủi ro thấp); chú thích "(Task 427)" có thể đọc nhầm — không ảnh hưởng tính đúng, giữ nguyên.
