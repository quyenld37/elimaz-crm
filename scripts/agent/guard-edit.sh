#!/usr/bin/env bash
# Hook PreToolUse (Edit|Write|MultiEdit|NotebookEdit): chặn sửa tay mã sinh và migration đã commit.
# Thoát mã 2 thì Claude Code không thực hiện lần sửa và đưa thông báo ở stderr cho agent.
set -u
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 0
. scripts/agent/lib.sh

input=$(cat)
path=$(printf '%s' "$input" | jq -r '.tool_input.file_path // .tool_input.notebook_path // empty')
[ -z "$path" ] && exit 0

case "$path" in
  /*) rel=$(rel_to_root "$path"); [ -z "$rel" ] && exit 0 ;;   # ngoài repo CRM: không phải việc của hook này
  *) rel=$path ;;
esac

if is_generated_path "$rel"; then
  echo "Chặn: $rel là mã sinh tự động. Sửa nguồn (openapi/openapi.yaml hoặc queries/**/*.sql) rồi chạy 'make gen'." >&2
  exit 2
fi

case "$rel" in
  migrations/*.sql)
    if git rev-parse --verify -q HEAD >/dev/null && git cat-file -e "HEAD:$rel" 2>/dev/null; then
      echo "Chặn: $rel đã được commit. Không sửa migration cũ; thêm một migration mới (luật mở rộng rồi thu hẹp)." >&2
      exit 2
    fi ;;
esac
exit 0
