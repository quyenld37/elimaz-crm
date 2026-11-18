#!/usr/bin/env bash
# Hook SessionStart (startup, resume, clear, compact): nhắc trạng thái và luật bắt buộc cho mọi phiên mới.
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 0
n=$(sed -n 's/^Task: *//p' "$(git rev-parse --git-dir)/crm-task-state" 2>/dev/null | head -1)
st=$(sed -n 's/^Trạng thái: *//p' "$(git rev-parse --git-dir)/crm-task-state" 2>/dev/null | head -1)
cat <<MSG
[CRM ELIMAZ] Phiên này làm trong apps/crm. Task gần nhất: ${n:-chưa có} (${st:-không rõ}).
Làm theo mục "Cách làm mỗi task" trong CLAUDE.md. Mỗi task một phiên sạch (/clear giữa hai task).
- Không sửa tay mã sinh, migration đã commit (hook chặn). Khi định báo xong, hook chạy build, lint, test phần vừa đổi.
- Commit có mã chỉ qua khi tệp task đủ mục và reviewer kết luận ĐẠT. Không push, deploy, vào máy chủ, tạo tài khoản khi chủ shop chưa OK riêng.
- Báo chủ shop bằng tiếng Việt; nói rõ cái gì chưa làm, chưa kiểm.
MSG
