#!/usr/bin/env bash
# Hook PreToolUse (Edit|Write|MultiEdit|NotebookEdit): chặn sửa tay mã sinh và migration đã commit.
# Thoát mã 2 thì Claude Code không thực hiện lần sửa và đưa thông báo ở stderr cho agent.
set -u
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 0
. scripts/agent/lib.sh

input=$(cat)
path=$(printf '%s' "$input" | jq -r '.tool_input.file_path // .tool_input.notebook_path // empty')
agent=$(printf '%s' "$input" | jq -r '.agent_type // empty')
[ -z "$path" ] && exit 0

case "$path" in
  /*) rel=$(rel_to_root "$path"); [ -z "$rel" ] && exit 0 ;;   # ngoài repo CRM: không phải việc của hook này
  *) rel=$path ;;
esac

if is_generated_path "$rel"; then
  echo "Chặn: $rel là mã sinh tự động. Sửa nguồn (openapi/openapi.yaml hoặc queries/**/*.sql) rồi chạy 'make gen'." >&2
  exit 2
fi

# Mã chỉ do agent crm-implementer sửa (quy trình mục 1, 13). Phiên chính và agent khác chỉ sửa tài liệu, cấu hình agent.
case "$rel" in
  internal/*|cmd/*|tools/*|tests/*|console/src/*|migrations/*|queries/*|openapi/*|go.mod|go.sum|Makefile|sqlc.yaml|.golangci.yml)
    if [ "$agent" != "crm-implementer" ]; then
      echo "Chặn: $rel là mã. Mã chỉ do agent crm-implementer sửa (docs/quy-trinh-lam-viec.md mục 13). Nếu bạn là phiên chính: giao việc cho agent crm-implementer theo skill crm-dieu-phoi. Nếu bạn là agent khác: dừng và báo lại người giao việc." >&2
      exit 2
    fi ;;
esac

case "$rel" in
  migrations/*.sql)
    if git rev-parse --verify -q HEAD >/dev/null && git cat-file -e "HEAD:$rel" 2>/dev/null; then
      echo "Chặn: $rel đã được commit. Không sửa migration cũ; thêm một migration mới (luật mở rộng rồi thu hẹp)." >&2
      exit 2
    fi ;;
esac
exit 0
