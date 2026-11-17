#!/usr/bin/env bash
# Đánh dấu task đang chạy là xong (sau khi đã commit). Dùng: finish-task.sh
set -eu
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}"
n=$(sed -n 's/^Task: *//p' docs/tasks/hien-tai.md | head -1)
printf '# Task đang chạy\n\nTask: %s\nTrạng thái: xong\n' "$n" > docs/tasks/hien-tai.md
echo "Task $n: xong"
