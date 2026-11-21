#!/usr/bin/env bash
# Tạo tệp task từ mẫu và đặt nó là task đang chạy. Dùng: new-task.sh 01 "Tên task"
set -eu
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}"
n=$(printf '%02d' "$((10#$1))"); name=${2:?thiếu tên task}
f="docs/tasks/task-$n.md"
# 21/11: tên có dấu / (vd "M1-x10: A/B") làm hỏng biểu thức sed cũ và để lại tệp task rỗng — thay bằng awk, không cần thoát ký tự.
[ -e "$f" ] || awk -v t="# Task $n — $name" '$0 == "# Task NN — TÊN" { print t; next } { print }' docs/tasks/_mau.md > "$f"
printf '# Task đang chạy\n\nTask: %s\nTrạng thái: đang làm\n' "$n" > "$(git rev-parse --git-dir)/crm-task-state"
echo "$f"
