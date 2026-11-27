# Task 162 — Biến {ma_van_don} trong mẫu (M4-22)

Kế hoạch: `../../docs/superpowers/plans/2026-11-18-crm-m4-chat-facebook-live.md` (mục Task 162). Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài: thêm `queries.TrackingCodeOfOrder` (mới, 0 chỗ tham chiếu cũ). Không đổi chữ ký xuất khẩu nào: `templates.Render`, `Fill`, `VarTrackingCode` giữ nguyên (`VarTrackingCode`, `knownVariable` đã có từ Task 161). Câu sqlc `TemplateLatestOrder` đổi cột trả về (thêm `id`): chỉ `render.go` dùng (grep).
- Gói phụ thuộc ngược: `internal/queries` được `internal/httpapi`, `modules/reporting` nhập, nay thêm `conversation/internal/templates` (luật (f) cấm queries nhập module, không cấm chiều ngược; reporting đã làm vậy). `templates` được `conversation` và `httpapi` (handler render) nhập: không đổi chữ ký.
- Không đổi OpenAPI: 422 `template_variable_missing` kèm `name` đã có từ Task 161; `name = ma_van_don` là giá trị mới của trường chuỗi đã có.
- Màn hình: không đổi; biến `ma_van_don` đã nằm trong danh sách biến của Task 161. Chỗ dùng: mọi đường gọi `templates.Render` (màn hình, quy tắc tự động, Live) tự có biến mới.
- Không migration, không bảng mới. sqlc: `queries/conversation/templates.sql` `TemplateLatestOrder` thêm cột `id` (mã sinh qua `sqlc generate`). SQL chạy lúc chạy: một câu đọc `ord.orders_v` JOIN `fulfillment.shipments_v` trong `internal/queries/shipment_of_order_dynsql.go` (view đã công bố; `docs/views.md` thêm M4 vào cột Mốc đọc của `shipments_v`).
- Test sẽ chạy: `TestRender_*` (gói templates, trên PostgreSQL thật), `TestTrackingCodeOfOrder` (gói queries); ci-task chạy cả gói đổi, gói phụ thuộc và test kiến trúc (sqlusage, boundaries).

## Tái dùng

- Đã kiểm `docs/danh-muc-dung-chung.md` và grep `tracking`, `current_shipment_id`: chưa có hàm nào đọc mã vận đơn của vận đơn hiện hành của một đơn (`OrderShippingTimeline` đọc sự kiện, không đọc mã).
- Dùng lại: logic chọn đơn của `{ma_don}` (nay tách thành `values.order`, dùng chung cho cả hai biến để luôn cùng một đơn); mẫu hàm đọc view ở `internal/queries/*_dynsql.go`. Viết mới `TrackingCodeOfOrder` vì conversation không được gọi fulfillment (04 mục 3, tests/arch boundaries) nên phải đọc view qua `internal/queries` như kế hoạch ghi.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

5.x (khớp kiến trúc): ranh giới module (conversation không nhập fulfillment; đọc view đã công bố qua internal/queries, 5.1), không tạo gói chung mới, không bảng/migration mới, không đổi OpenAPI. Lỗi: dùng sentinel `*VariableMissingError` có sẵn, không so chuỗi; bọc `%w` một lần. Không log, không dữ liệu cá nhân (mã vận đơn không phải dữ liệu cá nhân và không vào log).
6.x (đồng thời): chỉ đọc, trong tx tenant của bên gọi (`WithTenant`, RLS áp qua view security_invoker; test tenant khác không thấy). Không khóa, không ghi, không hàng đợi, không tiền. `{ma_van_don}` đọc ở thời điểm render: vận đơn đổi giữa chừng cho ra giá trị mới ở lần render sau (không giữ cache giữa các lần Render). Hai biến `{ma_don}` và `{ma_van_don}` cùng một đơn vì cùng một hàm chọn đơn trong cùng tx.
Không áp dụng: 5.x/6.x về migration, idempotency, outbox, tiền, quyền mới (quyền đọc đã kiểm ở đầu Render).

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| VTP123456 → "Mã vận đơn của bạn là VTP123456"; vận đơn cũ hủy + mới SPX9 → SPX9; chưa có vận đơn / không hiện hành / chưa có mã → 422 `name = ma_van_don`; cùng đơn với `{ma_don}`; đơn khách khác → ErrOrderNotOfCustomer | `TestRender_TrackingCode`, `TestRender_TrackingCodeQuickReply` | `go test -race -count=1 -run '^TestRender_' ./internal/modules/conversation/internal/templates/` | `ok` (trước khi code: FAIL "biến ma_van_don không có giá trị" ở ca VTP) |
| Câu đọc view: đơn không có, chưa vận đơn hiện hành, chưa có mã, vận đơn hiện hành có mã (kể cả có vận đơn cũ hủy), tenant khác | `TestTrackingCodeOfOrder` | `go test -race -count=1 -run '^TestTrackingCodeOfOrder$' ./internal/queries/` | `ok` |
| Cổng tĩnh + test gói đổi và phụ thuộc, test kiến trúc | ci-task | ci-task (từ xa, github) | `CI-TASK XANH` (run 46041339264; lần đầu đỏ vì gofumpt tệp test, đã sửa) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

- Kế hoạch ghi tệp `internal/queries/shipment_of_order.go`; đặt tên `shipment_of_order_dynsql.go` (+ `_dynsql_test.go`) theo luật CLAUDE.md "SQL dựng lúc chạy chỉ ở `*_dynsql.go`". Hàm là `queries.TrackingCodeOfOrder`.

## Quyết định thay chủ shop

- Khách chưa có đơn nào mà mẫu có `{ma_van_don}` → thiếu giá trị tên `ma_van_don` (không phải `ma_don`), vì biến đang hỏi là mã vận đơn. Phương án khác: báo `ma_don`; bỏ vì khó hiểu cho người dùng mẫu.
- Vận đơn hiện hành chưa có mã (đang tạo) → thiếu giá trị (không điền chuỗi rỗng, không lùi về vận đơn khác). Đúng spec "chỉ vận đơn hiện hành".

## Việc phát hiện thêm

(không có)

## Reviewer

Kết luận: ĐẠT. Không phát hiện ảnh hưởng tính đúng; mục Ảnh hưởng và bảng bằng chứng khớp mã thật. Ghi chú không bắt buộc: một câu chú thích ở `trackingCode` chép từ `orderCode` (không ảnh hưởng).
