# Task 521 — Gộp khách giữ cờ không liên hệ

Kế hoạch: `../../docs/superpowers/plans/2026-11-15-crm-m0-nen-tang-ban-5.md`. Loại việc (quy trình mục 3): A

## Ảnh hưởng

- Ký hiệu Go xuất ra ngoài: không đổi chữ ký nào. `Writer.MergeCustomers` đổi hành vi (thêm bước chép cờ `do_not_contact` từ gốc bị gộp sang gốc giữ lại, cùng tx). Hàng đọc `store.LockCustomersRow` (mã sinh, nội bộ gói) thêm 2 cột.
- Gói phụ thuộc ngược: người gọi `MergeCustomers` (httpapi gộp khách, luồng gộp theo SĐT của order/customer) không đổi chữ ký; `IsDoNotContact` và truy vấn `followups.sql` (conversation, M4) đọc cờ ở gốc nên tự thấy cờ đã chép.
- Không có (không đổi openapi).
- Không có (màn hình đọc cờ ở `customers_v` của gốc).
- Câu `PendingBirthdayGifts` (queries/promotion/birthday.sql) join thêm `customer.customers_v` (view đã công bố, có `do_not_contact` ở gốc) và lọc `NOT root.do_not_contact`; người gọi `BirthdayOutbox` và "Cần nhắn" tự được lọc. Không migration. `docs/views.md`: thêm `promotion` vào nơi dùng `customers_v`.
- Câu `LockCustomers` (queries/customer/merge.sql) chọn thêm `do_not_contact`, `do_not_contact_at`; dùng lại câu `SetDoNotContact`. Không migration.
- `go test -race -run '^(TestBirthdayGifts_SkipDoNotContact|TestMergeDoNotContact.*|TestDoNotContactFollowsRoot|TestMergeDH14|TestUndoMerge.*)$' ./internal/modules/customer/`; cả gói và gói phụ thuộc ở ci-task.

## Tái dùng

- Đã kiểm `docs/danh-muc-dung-chung.md` và grep `do_not_contact`, `consent`: cờ không liên hệ chỉ có ở `customer.customers` (đọc ở gốc); đồng ý sinh nhật nằm ở `birthday_profiles` theo từng hồ sơ, gộp không dời nên không mất.
- Dùng lại câu `SetDoNotContact` và `Writer.audit`; không viết hàm chung mới.

## Soát thiết kế

Trả lời từng mục 5.1–5.12 (khớp kiến trúc) và 6.1–6.13 (đúng khi chạy đồng thời) của `docs/quy-trinh-lam-viec.md`; mục không áp dụng ghi "không áp dụng: <lý do>".

- 5 (kiến trúc): logic nằm trong module customer, một tx của bên gọi, không gọi module khác; không đổi hợp đồng; không migration. Không áp dụng các mục 5 còn lại (không API, không việc nền, không dữ liệu cá nhân mới).
- 6 (đồng thời): chép cờ nằm trong `mergeOnce`, dưới khóa `LockCustomers` (id tăng dần, ADR-46 nhóm 5) của gốc giữ lại và cả nhóm bị gộp; cờ đọc từ hàng đã khóa (không đọc ngoài khóa), câu ghi là câu UPDATE có điều kiện. `SetDoNotContact` song song khóa cùng hàng gốc nên tuần tự hóa với gộp. `mergeOnce` chạy trong savepoint, có thể chạy lại: biến cờ gán lại mỗi lần. Test hai gộp ngược chiều song song. Không áp dụng: khóa mới, hàng đợi, tiền, idempotency.
- Log/audit: audit `customer.do_not_contact` chỉ giá trị cờ và mã lần gộp, không tên, không SĐT.

## Quyết định thay chủ shop

- Cờ "không liên hệ" của gốc bị gộp được CHÉP sang gốc giữ lại khi gốc giữ lại chưa có cờ (giữ mốc `do_not_contact_at` gốc, tức lúc khách thật sự yêu cầu). Vì sao: an toàn hơn (không nhắn nhầm khách đã từ chối), spec chỉ đọc cờ ở gốc. Gốc bị gộp KHÔNG bị xóa cờ nên hoàn tác gộp trả nó về đúng trạng thái. Phương án khác: HOẶC cờ của mọi hồ sơ trong nhóm (tắt cờ phải tắt cả nhóm, phức tạp, dễ sót); hoặc cột đánh dấu để hoàn tác gỡ cờ đã chép (cần migration).
- Sau hoàn tác gộp, gốc giữ lại vẫn giữ cờ đã chép (thiên về an toàn); nhân viên tắt tay được bằng `SetDoNotContact`. Audit có mã lần gộp để truy.
- Cờ đồng ý sinh nhật (`consent_zalo/mail`, `stopped_at`) ở từng hồ sơ sinh nhật theo khách và SĐT, gộp không dời nên không mất: không đổi.

## Bằng chứng

| Tiêu chí | Test | Lệnh | Kết quả |
|---|---|---|---|
| Gộp A(có cờ)+B, B+A, chuỗi, song song giữ cờ | `TestMergeDoNotContactKeepsFlag`, `...ReverseOrder`, `...Chain`, `...Concurrent` | `go test -race -count=1 -run '^(TestMergeDoNotContact.*|TestDoNotContactFollowsRoot|TestMergeDH14|TestUndoMerge.*)$' ./internal/modules/customer/` | ok (5,2 s) |
| Quà sinh nhật bỏ khách có cờ ở "Cần nhắn" và outbox, tắt cờ thì quay lại | `TestBirthdayGifts_SkipDoNotContact` | `go test -race -count=1 -run '^(TestBirthdayGifts_SkipDoNotContact|TestBirthdayOutbox.*)$' ./internal/modules/promotion/` | ok (2,3 s) |

## Test bị xóa hoặc tắt

(không có)

## Khác kế hoạch

(không có)

## Việc phát hiện thêm

(không có; quà sinh nhật đã đưa vào phạm vi 521 theo chỉ đạo 28/11)

## Reviewer

Kết luận: ĐẠT (reviewer, bản đã gộp main). Không có phát hiện phải sửa. Ci-task từ xa: 6/6 phần test xanh; cổng tĩnh đỏ lần đầu do gocognit và jscpd trong test mới, đã tách hàm và bỏ đoạn lặp, jscpd cục bộ sạch trên customer, promotion.
