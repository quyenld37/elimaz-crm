#!/usr/bin/env bash
# Hook Stop: không cho agent báo xong khi cổng kiểm của phần vừa đổi chưa qua.
# Thoát 2 thì Claude Code đưa stderr cho agent và agent phải làm tiếp.
set -u
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 0
. scripts/agent/lib.sh
export HIGH_RISK_DIRS

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
    pkgs=$(printf '%s\n' "$changed" | python3 scripts/agent/affected.py)
    if [ $? -eq 3 ]; then
      fail="${fail}== go list lỗi, không tính được gói bị ảnh hưởng ==
"
    elif [ -n "$pkgs" ]; then
      # shellcheck disable=SC2086
      run "go vet (gói bị ảnh hưởng)" go vet $pkgs
      if [ -f .golangci.yml ] && need golangci-lint "lint"; then
        # shellcheck disable=SC2086
        run "golangci-lint (gói bị ảnh hưởng)" golangci-lint run $pkgs
      fi
      # shellcheck disable=SC2086
      run "go test -race (gói đổi và gói phụ thuộc)" go test -race -count=1 $pkgs
    fi
  fi
fi

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

# Agent crm-implementer: tệp task phải đủ mục (hoặc có mục "Dừng để hỏi").
if [ "${1:-}" = "--task" ]; then
  run "tệp task chưa đủ (docs/tasks/task-NN.md)" scripts/agent/task-check.sh
fi

if [ -n "$fail" ]; then
  printf 'Chưa được báo xong. Sửa các mục dưới rồi thử lại (không tắt hook, không nới test):\n%s' "$fail" >&2
  exit 2
fi
exit 0
