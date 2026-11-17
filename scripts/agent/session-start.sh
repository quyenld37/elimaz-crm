#!/usr/bin/env bash
# Hook SessionStart (startup, resume, clear, compact): nhắc trạng thái và luật bắt buộc cho mọi phiên mới.
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 0
n=$(sed -n 's/^Task: *//p' docs/tasks/hien-tai.md 2>/dev/null | head -1)
st=$(sed -n 's/^Trạng thái: *//p' docs/tasks/hien-tai.md 2>/dev/null | head -1)
cat <<MSG
[CRM ELIMAZ] Phiên này làm trong apps/crm. Task gần nhất: ${n:-chưa có} (${st:-không rõ}).
Quy trình bắt buộc: docs/quy-trinh-lam-viec.md. Phiên chính là ĐIỀU PHỐI (skill crm-dieu-phoi), không tự viết code.
- Mọi sửa đổi mã (internal/, cmd/, tools/, tests/, console/src/, migrations/, queries/, openapi/) chỉ do agent crm-implementer làm; hook chặn nơi khác.
- Commit có mã chỉ qua được khi tệp task đủ mục và reviewer kết luận ĐẠT. Không push, deploy, vào máy chủ, tạo tài khoản khi chủ shop chưa OK riêng.
- Báo chủ shop bằng tiếng Việt; nói rõ cái gì chưa làm, chưa kiểm.
MSG
