#!/usr/bin/env bash
# Tạo tệp task từ mẫu và đặt nó là task đang chạy. Dùng: new-task.sh 01 "Tên task"
set -eu
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}"
n=$(printf '%02d' "$((10#$1))"); name=${2:?thiếu tên task}
f="docs/tasks/task-$n.md"
[ -e "$f" ] || sed -e "s/^# Task NN — TÊN$/# Task $n — $name/" docs/tasks/_mau.md > "$f"
printf '# Task đang chạy\n\nTask: %s\nTrạng thái: đang làm\n' "$n" > "$(git rev-parse --git-dir)/crm-task-state"
echo "$f"
