# Hàm dùng chung cho các script hook. Chạy được với bash 3.2 của macOS.
# shellcheck shell=bash

crm_root() { git -C "${CLAUDE_PROJECT_DIR:-$(pwd)}" rev-parse --show-toplevel; }

# Vùng rủi ro cao (ADR-25, Global Constraints của kế hoạch M0 bản 5).
HIGH_RISK_DIRS="internal/platform/tenancy internal/platform/queue internal/platform/commands internal/platform/idempotency internal/modules/identity internal/httpapi/guard.go"

# Mã sinh: không sửa tay.
is_generated_path() {
  case "$1" in
    internal/httpapi/gen/*|internal/httpapi/routes_gen.go|console/src/api/*) return 0 ;;
    */internal/store/*|internal/store/*) return 0 ;;
  esac
  return 1
}

# Tệp đổi so với HEAD, kể cả tệp mới chưa theo dõi và tệp đã xóa. Mỗi dòng một đường dẫn tương đối.
changed_files() {
  if git rev-parse --verify -q HEAD >/dev/null; then
    { git diff --name-only HEAD; git ls-files --others --exclude-standard; } | sort -u
  else
    { git ls-files; git ls-files --others --exclude-standard; } | sort -u
  fi
}
