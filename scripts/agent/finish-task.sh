#!/usr/bin/env bash
# Đánh dấu task đang chạy là xong (sau khi đã commit). Dùng: finish-task.sh
set -eu
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}"
n=$(sed -n 's/^Task: *//p' "$(git rev-parse --git-dir)/crm-task-state" | head -1)
printf '# Task đang chạy\n\nTask: %s\nTrạng thái: xong\n' "$n" > "$(git rev-parse --git-dir)/crm-task-state"
echo "Task $n: xong"
