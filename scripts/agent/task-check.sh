#!/usr/bin/env bash
# Kiểm tệp task của task đang chạy đã điền đủ chưa.
# Dùng: task-check.sh [--commit]   (--commit: đòi thêm reviewer kết luận ĐẠT)
# Thoát 0: đủ, hoặc không có task đang chạy. Thoát 1: thiếu (in danh sách ra stderr).
set -u
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 0
state=docs/tasks/hien-tai.md
[ -f "$state" ] || exit 0
n=$(sed -n 's/^Task: *//p' "$state" | head -1)
st=$(sed -n 's/^Trạng thái: *//p' "$state" | head -1)
[ -z "$n" ] && exit 0
[ "$st" = "đang làm" ] || exit 0
f="docs/tasks/task-$n.md"
miss=""
[ -f "$f" ] || { echo "task-check: chưa có $f (tạo bằng scripts/agent/new-task.sh $n \"tên\")" >&2; exit 1; }
# Agent dừng giữa chừng để hỏi: hợp lệ khi đã ghi câu hỏi (không áp dụng cho commit).
if [ "${1:-}" != "--commit" ] && awk '/^## Dừng để hỏi/{on=1;next} /^## /{on=0} on && NF' "$f" | grep -q .; then
  exit 0
fi
for sec in "Ảnh hưởng" "Tái dùng" "Soát thiết kế" "Bằng chứng" "Test bị xóa hoặc tắt" "Reviewer"; do
  grep -q "^## $sec" "$f" || miss="${miss}- thiếu mục \"$sec\"\n"
done
grep -q "CHƯA ĐIỀN" "$f" && miss="${miss}- còn chỗ ghi \"CHƯA ĐIỀN\"\n"
rows=$(awk '/^## Bằng chứng/{on=1;next} /^## /{on=0} on && /^\|/ && !/^\|---/ && !/Tiêu chí/' "$f" | wc -l | tr -d ' ')
[ "$rows" -ge 1 ] || miss="${miss}- bảng Bằng chứng chưa có dòng nào\n"
if [ "${1:-}" = "--commit" ]; then
  grep -q "^Kết luận: ĐẠT" "$f" || miss="${miss}- reviewer chưa kết luận ĐẠT (mục Reviewer phải có dòng \"Kết luận: ĐẠT\")\n"
fi
if [ -n "$miss" ]; then
  printf "task-check: tệp %s chưa đủ:\n%b" "$f" "$miss" >&2
  exit 1
fi
exit 0
