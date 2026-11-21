#!/usr/bin/env bash
# Báo test bị xóa, bị tắt, hoặc bớt câu kiểm trong vùng rủi ro cao mà chưa được ghi tên và lý do
# trong mục "## Test bị xóa hoặc tắt" của tệp task.
# Dùng: test-guard.sh [BASE]   (mặc định HEAD; CI truyền main)   Biến TASK_FILE chọn tệp task.
# Thoát 0: sạch hoặc mọi mục đã được ghi. Thoát 1: có mục chưa ghi.
set -u
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 1
. scripts/agent/lib.sh
base=${1:-HEAD}
git rev-parse --verify -q "$base" >/dev/null || exit 0   # chưa có commit nào để so

is_test() { case "$1" in *_test.go|*.test.ts|*.test.tsx|*.spec.ts|*.spec.tsx) return 0;; esac; return 1; }
in_high_risk() { for d in $HIGH_RISK_DIRS; do case "$1" in "$d"|"$d"/*) return 0;; esac; done; return 1; }
# Biến test đứng sau ký tự không phải chữ/số/dấu chấm, để `fmt.Errorf(` không bị đếm nhầm là `t.Errorf(` (Task 399 phát hiện).
count_checks() { grep -o -E '(^|[^A-Za-z0-9_.])t\.(Fatal|Fatalf|Error|Errorf)\(|require\.|assert\.|expect\(' 2>/dev/null | wc -l | tr -d ' '; }

findings=""
add() { findings="${findings}$1"$'\n'; }

# 1. Tệp test bị xóa.
while IFS=$'\t' read -r st f; do
  [ "$st" = "D" ] && is_test "$f" && add "$f|tệp test bị xóa"
done < <(git diff --name-status "$base" --)

# 2. Dòng tắt test mới thêm.
for f in $( { git diff --name-only "$base" --; git ls-files --others --exclude-standard; } | sort -u ); do
  is_test "$f" || continue
  [ -f "$f" ] || continue
  if git cat-file -e "$base:$f" 2>/dev/null; then
    added=$(git diff "$base" -- "$f" | grep -E '^\+' | grep -v -E '^\+\+\+')
  else
    added=$(sed 's/^/+/' "$f")
  fi
  if printf '%s\n' "$added" | grep -E -q 't\.Skip(Now|f)?\(|\.(skip|only)\(|(^|[^A-Za-z])it\.todo\('; then
    add "$f|thêm lệnh bỏ qua hoặc chỉ chạy riêng (t.Skip, .skip, .only)"
  fi
  # 3. Bớt câu kiểm trong vùng rủi ro cao.
  if in_high_risk "$f" && git cat-file -e "$base:$f" 2>/dev/null; then
    before=$(git show "$base:$f" | count_checks); after=$(count_checks < "$f")
    [ "${after:-0}" -lt "${before:-0}" ] && add "$f|số câu kiểm giảm từ $before xuống $after"
  fi
done

[ -z "$findings" ] && exit 0

# 21/11 (Task 451 ghi): chọn tệp task theo task ĐANG CHẠY (crm-task-state do new-task.sh ghi), không theo tệp mới sửa nhất —
# sau khi gộp main, tệp task của task khác có thể mới hơn và test-guard đòi ghi lý do nhầm chỗ.
cur=$(sed -n 's/^Task: *//p' "$(git rev-parse --git-dir 2>/dev/null)/crm-task-state" 2>/dev/null | head -1)
[ -n "$cur" ] && [ -f "docs/tasks/task-$cur.md" ] && cur_file="docs/tasks/task-$cur.md" || cur_file=""
task=${TASK_FILE:-${cur_file:-$(ls -t docs/tasks/task-*.md 2>/dev/null | head -1)}}
section=""
[ -n "$task" ] && [ -f "$task" ] && section=$(awk '/^## Test bị xóa hoặc tắt/{on=1;next} /^## /{on=0} on' "$task")

missing=""
while IFS='|' read -r f why; do
  [ -z "$f" ] && continue
  printf '%s' "$section" | grep -F -q "$f" || missing="${missing}- $f: $why"$'\n'
done <<< "$findings"

if [ -n "$missing" ]; then
  echo "test-guard: các thay đổi sau chưa được ghi tên và lý do trong mục \"## Test bị xóa hoặc tắt\" của ${task:-docs/tasks/task-NN.md}:" >&2
  printf '%s' "$missing" >&2
  exit 1
fi
exit 0
