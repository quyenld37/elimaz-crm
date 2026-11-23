#!/usr/bin/env bash
# Hook Stop: không cho agent báo xong khi cổng kiểm của phần vừa đổi chưa qua.
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

# Phần Go: chỉ khi có go.mod và có tệp Go hay go.mod/go.sum đổi.
if [ -f go.mod ] && has '(\.go|go\.mod|go\.sum)$'; then
  if need go "build và test"; then
    run "go build ./..." go build ./...
    # --split (Task 473): vet, lint trên mọi gói bị ảnh hưởng; -race chỉ gói sửa trực tiếp, gói chỉ dùng tới test thường.
    # Không --base: migration do ci-task xét (hook chạy mỗi lần báo xong, giữ nhẹ).
    split=$(printf '%s\n' "$changed" | python3 scripts/agent/affected.py --split)
    if [ $? -eq 3 ]; then
      fail="${fail}== go list lỗi, không tính được gói bị ảnh hưởng ==
"
    fi
    race=$(printf '%s\n' "$split" | sed -n 's/^race //p')
    plain=$(printf '%s\n' "$split" | sed -n 's/^plain //p')
    pkgs="$race $plain"
    if [ -n "${pkgs// /}" ]; then
      # shellcheck disable=SC2086
      run "go vet (gói bị ảnh hưởng)" go vet $pkgs
      if [ -f .golangci.yml ] && need golangci-lint "lint"; then
        # shellcheck disable=SC2086
        # golangci-lint cần đường dẫn thư mục, không nhận tên gói dạng elimaz.com.vn/... (Task 4 phát hiện).
        lintdirs=$(go list -f '{{.Dir}}' $pkgs 2>/dev/null | sed "s#^$(pwd)/#./#")
        # shellcheck disable=SC2086
        run "golangci-lint (gói bị ảnh hưởng)" golangci-lint run $lintdirs
      fi
      if [ -n "$race" ]; then
        # shellcheck disable=SC2086
        run "go test -race (gói sửa trực tiếp, vùng cao)" go test -race -count=1 $race
      fi
      if [ -n "$plain" ]; then
        # shellcheck disable=SC2086
        run "go test (gói chỉ dùng tới phần đổi, không -race)" go test -count=1 $plain
      fi
    fi
  fi
fi

# Module Go riêng trong spikes/ (go.mod riêng, ngoài ./... của module gốc): build, vet, test module có tệp đổi.
for mod in $(printf '%s\n' "$changed" | sed -n 's#^\(spikes/[^/]*\)/.*#\1#p' | sort -u); do
  if [ -f "$mod/go.mod" ] && need go "build và test $mod"; then
    run "go vet ($mod)" sh -c "cd '$mod' && go vet ./..."
    run "go test -race ($mod)" sh -c "cd '$mod' && go test -race -count=1 ./..."
  fi
done

# Mã sinh không được lệch nguồn.
if has '^(openapi/|queries/|sqlc\.yaml)' && [ -f Makefile ] && grep -q '^gen:' Makefile; then
  run "make gen" make gen
  run "mã sinh khác bản trong repo sau make gen" git diff --exit-code -- internal/httpapi/gen internal/httpapi/routes_gen.go console/src/api
fi

# Màn hình.
if has '^console/' && [ -f console/package.json ]; then
  run "tsc --noEmit (console)" npx --prefix console tsc -p console --noEmit
  run "eslint (console)" npx --prefix console eslint console
fi

# Đoạn trùng mới trên tệp vừa đổi.
if [ -f .jscpd.json ] && need jscpd "dò code trùng"; then
  files=$(printf '%s\n' "$changed" | grep -E '\.(go|ts|tsx)$' | while read -r f; do [ -f "$f" ] && printf '%s ' "$f"; done)
  # shellcheck disable=SC2086
  [ -n "$files" ] && run "jscpd (tệp vừa đổi)" jscpd --config .jscpd.json $files
fi

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
