#!/usr/bin/env bash
# Hook PreToolUse (Bash): chặn push; chặn commit có mã khi tệp task chưa đủ hoặc chưa được reviewer duyệt.
set -u
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 0
cmd=$(jq -r '.tool_input.command // empty')
case "$cmd" in
  *"git push"*)
    echo "Chặn: push cần OK riêng của chủ shop trong chat. Báo chủ shop; khi được OK, chủ shop tự chạy lệnh push hoặc xác nhận cho chạy một lần." >&2
    exit 2 ;;
esac
case "$cmd" in
  *"git commit"*)
    if git diff --cached --name-only | grep -E -q '^(internal|cmd|tools|tests|console/src|migrations|queries|openapi)/'; then
      if ! scripts/agent/task-check.sh --commit; then
        echo "Chặn commit: commit có mã cần tệp task đủ mục và reviewer kết luận ĐẠT (quy trình mục 4.3)." >&2
        exit 2
      fi
    fi ;;
esac
exit 0
