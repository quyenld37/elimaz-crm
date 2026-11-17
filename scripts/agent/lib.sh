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

# Đường dẫn tương đối so với gốc repo. Chuẩn hóa Unicode (NFC) cả hai phía, vì macOS và git có thể
# trả cùng một tên thư mục có dấu ("Thời trang") theo hai cách mã hóa khác nhau.
# In ra rỗng nếu tệp nằm ngoài repo.
rel_to_root() {
  python3 - "$1" "$(crm_root)" <<'PY'
import os, sys, unicodedata
n = lambda p: unicodedata.normalize("NFC", os.path.abspath(p))
path, root = n(sys.argv[1]), n(sys.argv[2])
if path == root or path.startswith(root + os.sep):
    print(os.path.relpath(path, root))
PY
}

# Mã băm của phần MÃ đã stage (không tính tài liệu, tệp task), dùng cho dấu duyệt reviewer.
CODE_PATHS="internal cmd tools tests console/src migrations queries openapi go.mod go.sum Makefile sqlc.yaml .golangci.yml"
staged_code_hash() {
  # shellcheck disable=SC2086
  git ls-files --stage -- $CODE_PATHS 2>/dev/null | shasum -a 256 | cut -c1-40
}

# Thư mục lưu dấu duyệt reviewer, đúng cả trong git worktree (ở đó .git là một tệp).
review_dir() { echo "$(git rev-parse --git-dir)/crm-review"; }
