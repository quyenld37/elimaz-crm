#!/usr/bin/env bash
# Cổng kiểm của MỘT task trước khi review (thay make ci trong phiên làm task; chủ shop yêu cầu tăng tốc 18/11).
# Chạy đủ mọi cổng tĩnh như make ci; test chỉ chạy gói đổi + gói phụ thuộc + cả bộ vùng rủi ro cao nếu chạm;
# đổi migration hoặc go.mod thì chạy toàn bộ test. So hợp đồng API và test-guard với ĐIỂM TÁCH NHÁNH (không với đầu main
# đang chạy, tránh báo đỏ nhầm). Toàn bộ test vẫn chạy trên main do phiên điều phối sau mỗi đợt gộp.
set -u
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 1
. scripts/agent/lib.sh
export HIGH_RISK_DIRS PATH="$HOME/go/bin:$PATH"
base=$(git merge-base HEAD main 2>/dev/null || echo main)
fail=""
step() { local name=$1; shift; echo "== $name"; if ! "$@"; then fail="$fail $name"; echo "== $name: ĐỎ"; fi; }
changed=$( { git diff --name-only "$base"; git ls-files --others --exclude-standard; } | sort -u )
step build make build
step vet make vet
step lint make lint
step gen-check make gen-check
step sqlc-vet make sqlc-vet
step catalog-check make catalog-check
step jscpd make jscpd
step gitleaks make gitleaks
step oasdiff make oasdiff OASDIFF_BASE=-base-ref="$base"
step test-guard scripts/agent/test-guard.sh "$base"
if printf '%s\n' "$changed" | grep -E -q '^(go\.mod|go\.sum)$'; then step vuln make vuln; fi
if printf '%s\n' "$changed" | grep -q '^console/'; then step console-check make console-check; fi
if printf '%s\n' "$changed" | grep -E -q '^(migrations/|go\.mod$|go\.sum$|internal/testkit/)'; then
  step "test (toàn bộ: đổi migration/go.mod/testkit)" make test
else
  pkgs=$(printf '%s\n' "$changed" | python3 scripts/agent/affected.py); arc=$?
  if [ "$arc" != 0 ]; then
    # Không xác định được gói bị ảnh hưởng: chạy toàn bộ thay vì bỏ sót (19/11: lỗi NFD/NFC làm mọi task chỉ chạy tests/arch).
    step "test (toàn bộ: affected.py thoát $arc)" make test
  elif [ -n "$pkgs" ]; then
    # shellcheck disable=SC2086
    step "test (gói đổi, phụ thuộc, vùng cao)" go test -race -count=1 $pkgs ./tests/arch/...
  else
    step "test kiến trúc" go test -count=1 ./tests/arch/...
  fi
fi
# Test cấu hình quy trình và Makefile (Task 401): affected.py chỉ xét tệp Go nên đổi Makefile không kích test nào.
if printf '%s\n' "$changed" | grep -E -q '^(Makefile|tools/versions\.mk|scripts/|\.github/|\.githooks/|docker/)'; then
  step "test deploy (đổi Makefile/scripts/docker)" go test -count=1 ./tests/deploy/...
fi
if [ -n "$fail" ]; then echo "CI-TASK ĐỎ:$fail"; exit 1; fi
echo "CI-TASK XANH (so với điểm tách nhánh $base)"
