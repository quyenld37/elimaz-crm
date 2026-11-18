#!/usr/bin/env bash
# Hook PostToolUse (Edit|Write|MultiEdit): định dạng tệp vừa sửa. Không bao giờ chặn.
set -u
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 0
path=$(jq -r '.tool_input.file_path // empty')
[ -z "$path" ] || [ ! -f "$path" ] && exit 0
case "$path" in
  *.go)
    case "$path" in */internal/httpapi/gen/*|*/internal/store/*|*_gen.go) exit 0 ;; esac
    if command -v gofumpt >/dev/null 2>&1; then gofumpt -w "$path"; else echo "format.sh: chưa có gofumpt (xem tools/versions.mk)" >&2; fi
    if command -v goimports >/dev/null 2>&1; then goimports -w "$path"; fi ;;
  */console/src/api/*) : ;;   # mã sinh, không định dạng tay
  */console/*)
    # Prettier, chốt ở Task 17 (docs/chon-cong-cu-console.md); dùng bản trong console/node_modules, không tải mạng.
    if [ -x console/node_modules/.bin/prettier ]; then console/node_modules/.bin/prettier --write "$path" >/dev/null 2>&1 || true; fi ;;
esac
exit 0
