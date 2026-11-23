#!/usr/bin/env bash
# Cổng kiểm của MỘT task trước khi review (thay make ci trong phiên làm task; chủ shop yêu cầu tăng tốc 18/11).
# Chạy đủ mọi cổng tĩnh như make ci; test chỉ chạy gói đổi + gói phụ thuộc + cả bộ vùng rủi ro cao nếu chạm
# (-race chỉ gói sửa trực tiếp, Task 473); migration mới chỉ thêm → gói dùng đối tượng nó chạm; migration thu hẹp hay
# sửa tệp cũ, go.mod, testkit thì chạy toàn bộ test. So hợp đồng API và test-guard với ĐIỂM TÁCH NHÁNH (không với đầu main
# đang chạy, tránh báo đỏ nhầm). Toàn bộ test vẫn chạy trên main do phiên điều phối sau mỗi đợt gộp.
set -u
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 1
# 21/11: cả cổng kiểm chạy trong lượt test duy nhất của máy, ưu tiên thấp (scripts/agent/run-in-slot.py) — phiên khác chờ tới lượt.
if [ "${CRM_TEST_SLOT_HELD:-}" != 1 ]; then exec python3 scripts/agent/run-in-slot.py bash "$0" "$@"; fi
. scripts/agent/lib.sh
export HIGH_RISK_DIRS PATH="$HOME/go/bin:$PATH"
base=$(git merge-base HEAD main 2>/dev/null || echo main)
fail=""
step() { local name=$1 t0=$SECONDS; shift; echo "== $name"; if ! "$@"; then fail="$fail $name"; echo "== $name: ĐỎ"; fi; echo "== $name: $((SECONDS-t0)) giây"; }
changed=$( { git diff --name-only "$base"; git ls-files --others --exclude-standard; } | sort -u )
step build make build
step vet make vet
step lint make lint
# Bỏ bước khi đầu vào của nó không đổi (kết quả chắc chắn giống điểm tách nhánh, nơi main đã xanh), 19/11:
# gen-check khi đổi nguồn sinh hoặc chính mã sinh; sqlc-vet khi đổi câu SQL, schema hay cấu hình sqlc.
gen_in='^(go\.mod|go\.sum|tools/versions\.mk|openapi/|queries/|migrations/|sqlc\.yaml|tools/routegen/|internal/httpapi/gen/|internal/httpapi/routes_gen\.go|console/src/api/|console/package|.*/internal/store/)'
if printf '%s\n' "$changed" | grep -E -q "$gen_in"; then step gen-check make gen-check; else echo "== gen-check: bỏ (không đổi nguồn sinh hay mã sinh)"; fi
if printf '%s\n' "$changed" | grep -E -q '^(queries/|migrations/|sqlc\.yaml)'; then step sqlc-vet make sqlc-vet; else echo "== sqlc-vet: bỏ (không đổi SQL, schema, sqlc.yaml)"; fi
step catalog-check make catalog-check
step jscpd make jscpd
step gitleaks make gitleaks
step oasdiff make oasdiff OASDIFF_BASE=-base-ref="$base"
step test-guard scripts/agent/test-guard.sh "$base"
if printf '%s\n' "$changed" | grep -E -q '^(go\.mod|go\.sum)$'; then step vuln make vuln; fi
if printf '%s\n' "$changed" | grep -q '^console/'; then step console-check make console-check; fi
# Lần chạy lại chỉ test phần đổi từ lần test xanh trước (20/11, chủ shop: "chỉ test những case ảnh hưởng"). Gói không
# bị phần đổi chạm (trực tiếp hay qua phụ thuộc) có mã và test y hệt bản đã xanh nên kết quả không đổi. Dấu xanh là
# mã cây (git write-tree) của bản đã test, lưu riêng cho từng worktree; merge-task.sh cũng đọc dấu này.
snap_tree() { local idx; idx=$(mktemp); cp "$(git rev-parse --git-path index)" "$idx" 2>/dev/null; GIT_INDEX_FILE=$idx git add -A . >/dev/null 2>&1 && GIT_INDEX_FILE=$idx git write-tree; rm -f "$idx"; }
stamp="$(git rev-parse --git-dir)/crm-ci-test-xanh"
now_tree=$(snap_tree)
tchanged=$changed; tnote=""; tbase=$base
if [ -n "$now_tree" ] && [ -f "$stamp" ] && old=$(cat "$stamp") && git cat-file -e "$old^{tree}" 2>/dev/null; then
  tchanged=$(git diff --name-only "$old" "$now_tree"); tnote=", chỉ phần đổi từ lần test xanh trước"; tbase=$old
fi
fail_before=$fail
if [ -n "$tnote" ] && [ -z "$tchanged" ]; then
  echo "== test: bỏ (không đổi gì từ lần test xanh trước)"
elif printf '%s\n' "$tchanged" | grep -E -q '^(go\.mod$|go\.sum$|internal/testkit/)'; then
  step "test (toàn bộ: đổi go.mod/testkit$tnote)" make test
else
  # Task 473 (23/11, chủ shop: máy nóng): migration mới chỉ thêm → gói dùng đúng đối tượng nó chạm; migration thu hẹp hay
  # sửa tệp cũ → affected.py thoát 5 (toàn bộ). Bản so là bản đã test xanh ($tbase). -race chỉ cho gói sửa trực tiếp
  # ("race"); gói chỉ dùng tới ("plain") chạy test thường. Lưới an toàn: điều phối viên chạy toàn bộ -race trên main
  # sau mỗi đợt gộp.
  split=$(printf '%s\n' "$tchanged" | python3 scripts/agent/affected.py --base "$tbase" --split); arc=$?
  if [ "$arc" != 0 ]; then
    # Không xác định được gói bị ảnh hưởng (4), hay migration cần toàn bộ (5): chạy toàn bộ thay vì bỏ sót
    # (19/11: lỗi NFD/NFC làm mọi task chỉ chạy tests/arch).
    step "test (toàn bộ: affected.py thoát $arc$tnote)" make test
  else
    race=$(printf '%s\n' "$split" | sed -n 's/^race //p')
    plain=$(printf '%s\n' "$split" | sed -n 's/^plain //p' | grep -v '/tests/arch$')
    # shellcheck disable=SC2086
    step "test -race (gói sửa trực tiếp, vùng cao, test kiến trúc$tnote)" go test -race -count=1 $race ./tests/arch/...
    if [ -n "$plain" ]; then
      # shellcheck disable=SC2086
      step "test thường không -race (gói chỉ dùng tới phần đổi$tnote)" go test -count=1 $plain
    fi
  fi
fi
# Test cấu hình quy trình và Makefile (Task 401): affected.py chỉ xét tệp Go nên đổi Makefile không kích test nào.
if printf '%s\n' "$tchanged" | grep -E -q '^(Makefile|tools/versions\.mk|scripts/|\.github/|\.githooks/|docker/)'; then
  step "test deploy (đổi Makefile/scripts/docker)" go test -count=1 ./tests/deploy/...
fi
if [ "$fail" = "$fail_before" ] && [ -n "$now_tree" ]; then echo "$now_tree" > "$stamp"; fi
if [ -n "$fail" ]; then echo "CI-TASK ĐỎ:$fail"; exit 1; fi
echo "CI-TASK XANH (so với điểm tách nhánh $base)"
