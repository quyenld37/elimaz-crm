# Payload webhook Facebook dùng trong test (Task 147)

Các tệp `*.json` ở đây **dựng tay** theo tài liệu Meta về webhook Messenger và webhook `feed`/`live_videos` của Page
([S5], [S27] trong spec 02 mục 10). Mọi mã (Page ID, PSID, `mid`, `comment_id`, `app_id` của CRM) là mã giả; không
có dữ liệu thật của khách.

| Tệp | Nội dung |
|---|---|
| `message_text.json`, `message_quick_reply.json`, `message_attachments.json`, `postback.json` | tin khách gửi (chữ, trả lời nhanh, tệp gồm ảnh, sticker, tệp, mẫu lạ, nút bấm) |
| `echo_crm.json`, `echo_business_suite.json` | echo tin Page gửi: từ app CRM (có `metadata`) và từ Meta Business Suite |
| `delivery.json`, `read.json`, `reaction.json`, `message_edit.json`, `referral.json` | đã nhận, đã xem, cảm xúc, sửa tin, đến từ liên kết |
| `policy_enforcement.json` | Meta áp chế tài chính sách lên Page |
| `comment_add.json`, `comment_edited.json`, `comment_page.json`, `comment_remove.json`, `comment_hide.json` | comment thêm, sửa, của chính Page, xóa, ẩn |
| `live_videos.json` | phiên live bắt đầu |
| `batch_2pages_7events.json` | một lần giao gói 2 Page, 7 sự kiện |
| `feed_status.json`, `object_user.json` | loại bị bỏ qua và đếm (`feed:status`, `object:user`) |

**Việc còn phải làm:** thay các tệp này bằng payload thật đã ghi ở thí nghiệm E3 (Task 181, kế hoạch M4), đã xóa
thông tin cá nhân (tên, PSID, nội dung tin thay bằng giá trị giả), rồi chạy lại `split_test.go`; chỗ nào khác (tên
trường, kiểu `created_time`, động từ comment, khóa `policy-enforcement`) thì sửa `split.go` và bảng mong đợi.
