#!/usr/bin/env bash
# Hook Stop: không cho agent báo xong khi phần vừa đổi chưa build, vet được (kiểm đủ ở ci-task, chạy từ xa).
# Thoát 2 thì Claude Code đưa stderr cho agent và agent phải làm tiếp.
set -u
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 0
. scripts/agent/lib.sh
export HIGH_RISK_DIRS

# Dừng để hỏi chủ shop là lối ra hợp lệ, kể cả khi test đang đỏ (ghi rõ trong tệp task).
n=$(sed -n 's/^Task: *//p' "$(git rev-parse --git-dir)/crm-task-state" 2>/dev/null | head -1)
if [ -n "$n" ] && [ -f "docs/tasks/task-$n.md" ] && awk '/^## Dừng để hỏi/{on=1;next} /^## /{on=0} on && NF' "docs/tasks/task-$n.md" | grep -q .; then
  exit 0
fi

changed=$(changed_files)
if [ -z "$changed" ] && [ "${1:-}" != "--task" ]; then exit 0; fi

fail=""
run() {  # run "<mô tả>" lệnh...
  local label=$1; shift
  local out
  if ! out=$("$@" 2>&1); then
    fail="${fail}== ${label} KHÔNG QUA ==
$(printf '%s\n' "$out" | tail -40)
"
  fi
}
need() {  # need <công cụ> <vì sao>
  command -v "$1" >/dev/null 2>&1 && return 0
  fail="${fail}== Thiếu công cụ '$1' ($2). Cài theo tools/versions.mk rồi chạy lại. ==
"
  return 1
}
has() { printf '%s\n' "$changed" | grep -E -q "$1"; }

# Chủ shop 26/11 ("sao test chưa chạy trong bể, mà vẫn chạy trên máy t à, thấy vẫn lag lắm" → chọn "Đổi ngay cả hai"):
# máy Mac chỉ xử lý task; hook Stop chỉ còn build + vet nhanh trên máy. Lint, test, mã sinh, dò trùng, màn hình do
# scripts/agent/ci-task.sh kiểm đủ — mặc định chạy từ xa (Task 493). Trước đây hook chạy lint + test gói đổi sau mỗi
# lượt, không xếp hàng, làm máy lag khi nhiều phiên song song.
# Phần Go: khi có go.mod và có tệp đổi (tệp task docs/tasks/ đổi mỗi lượt: bỏ qua).
if [ -f go.mod ] && { has '(\.go|go\.mod|go\.sum)$' || printf '%s\n' "$changed" | grep -v '^docs/tasks/' | grep -q .; }; then
  if need go "build và vet"; then
    run "go build ./..." go build ./...
    split=$(printf '%s\n' "$changed" | python3 scripts/agent/affected.py --split)
    if [ $? -eq 3 ]; then
      fail="${fail}== go list lỗi, không tính được gói bị ảnh hưởng ==
"
    fi
    pkgs=$(printf '%s\n' "$split" | sed -E -n 's/^(race|plain) //p' | tr '\n' ' ')
    if [ -n "${pkgs// /}" ]; then
      # shellcheck disable=SC2086
      run "go vet (gói bị ảnh hưởng)" go vet $pkgs
    fi
  fi
fi

# Module Go riêng trong spikes/ (go.mod riêng, ngoài ./... của module gốc): vet module có tệp đổi.
for mod in $(printf '%s\n' "$changed" | sed -n 's#^\(spikes/[^/]*\)/.*#\1#p' | sort -u); do
  if [ -f "$mod/go.mod" ] && need go "vet $mod"; then
    run "go vet ($mod)" sh -c "cd '$mod' && go vet ./..."
  fi
done

# Test không được lặng lẽ biến mất.
run "test-guard (so với HEAD)" scripts/agent/test-guard.sh HEAD

# --task: tệp task phải đủ mục (hoặc có mục "Dừng để hỏi").
if [ "${1:-}" = "--task" ]; then
  run "tệp task chưa đủ (docs/tasks/task-NN.md)" scripts/agent/task-check.sh
fi

if [ -n "$fail" ]; then
  printf 'Chưa được báo xong. Sửa các mục dưới rồi thử lại (không tắt hook, không nới test):\n%s' "$fail" >&2
  exit 2
fi
exit 0
