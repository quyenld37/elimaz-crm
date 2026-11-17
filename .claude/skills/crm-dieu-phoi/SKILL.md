---
name: crm-dieu-phoi
description: Quy trình của phiên điều phối CRM ELIMAZ — giao từng task cho agent crm-implementer, chạy reviewer, kiểm tệp task, commit, báo chủ shop. Dùng khi chủ shop bảo làm một task, một mốc, hay sửa một lỗi trong apps/crm.
user-invocable: true
---

# Điều phối một task CRM

Phiên chính không tự viết mã (hook chặn). Nguồn đầy đủ: `docs/quy-trinh-lam-viec.md`.

1. **Sẵn sàng?** Kiểm quy trình mục 4.1: task có trong kế hoạch với test và Interfaces; task trước đã commit; mục spec rõ; có màn hình thì đã có mockup chủ shop chốt. Thiếu: hỏi chủ shop, không giao.
2. **Tạo tệp task:** `scripts/agent/new-task.sh NN "Tên task"` (đặt task đang chạy).
3. **Giao việc:** gọi công cụ Agent với `subagent_type: "crm-implementer"`, chạy nền trước (không background). Lời giao gồm đúng: số task, đường dẫn kế hoạch và tên task, Interfaces của các task nó dùng (chép nguyên từ kế hoạch), mục spec và ADR liên quan, các quyết định chủ shop đã nêu trong chat có liên quan tới task (chép nguyên lời). Không đưa lịch sử trò chuyện, không đưa lập luận của mình.
4. **Nhận kết quả.** Implementer dừng giữa chừng vì câu hỏi: hỏi chủ shop rồi giao lại (agent mới, kèm câu trả lời).
5. **Review:** gọi Agent với `subagent_type: "reviewer"`, đưa số task, danh sách tệp đã đổi, đường dẫn tệp task. Không đưa tóm tắt của implementer.
6. **Xử lý phát hiện:** chỉ phát hiện về tính đúng, an toàn, kiến trúc. Giao lại cho một crm-implementer mới với danh sách phát hiện; review lại. Sau hai vòng vẫn CẦN SỬA: dừng, báo chủ shop.
7. **Ghi kết luận:** chép bảng của reviewer vào mục "Reviewer" của tệp task, dòng đầu `Kết luận: ĐẠT`.
8. **Kiểm và commit:** `scripts/agent/task-check.sh --commit` phải qua; `make ci` xanh (từ Task 3). Stage đúng các tệp đã đổi (không `git add -A`), commit với dòng `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Đặt `Trạng thái: xong` trong `docs/tasks/hien-tai.md`.
9. **Báo chủ shop** (quy trình mục 11): tiếng Việt, 5–10 dòng, bằng chứng chính, khác kế hoạch, chưa làm hay chưa kiểm, việc phát hiện thêm, câu hỏi nếu có. Không viết "đã xong" khi còn bước chưa kiểm.
10. **Lỗi lặp lại lần thứ hai:** thêm một dòng vào "Bẫy đã gặp" của `CLAUDE.md`.

Push, deploy, máy chủ, tài khoản: chỉ khi chủ shop OK riêng trong chat (hook chặn push).
