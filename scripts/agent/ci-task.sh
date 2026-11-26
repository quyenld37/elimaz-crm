#!/usr/bin/env bash
# Cổng kiểm của MỘT task trước khi review (thay make ci trong phiên làm task; chủ shop yêu cầu tăng tốc 18/11).
# Chạy đủ mọi cổng tĩnh như make ci; test chỉ chạy gói đổi + gói phụ thuộc + cả bộ vùng rủi ro cao nếu chạm
# (-race chỉ gói sửa trực tiếp, Task 473); migration mới chỉ thêm → gói dùng đối tượng nó chạm; migration thu hẹp hay
# sửa tệp cũ, go.mod, testkit thì chạy toàn bộ test. So hợp đồng API và test-guard với ĐIỂM TÁCH NHÁNH của nhánh gốc
# (main, hay m4 với task M4 — Task 493), không với đầu nhánh gốc đang chạy, tránh báo đỏ nhầm. Toàn bộ test vẫn chạy
# trên nhánh gốc do phiên điều phối sau mỗi đợt gộp.
# Task 493 (26/11): mặc định chạy TỪ XA trên GitHub Actions (scripts/ci/remote-test.sh, workflow test-remote.yml) khi có
# remote origin và gh dùng được — không xếp hàng lượt test của máy; CRM_CI_REMOTE=0 ép chạy máy; không chạy được từ xa
# (mất mạng, gh lỗi) thì tự lùi về chạy máy như cũ. CRM_BASE_BRANCH chọn nhánh gốc (mặc định dò: m4 hay main).
set -u
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 1
. scripts/ci/lib.sh
mode=$(ci_mode)
[ "${CI_TASK_E2E:-0}" = 1 ] && mode="local CI_TASK_E2E=1 (e2e chỉ chạy ở máy)"
# 21/11: chạy máy thì cả cổng kiểm chạy trong lượt test duy nhất của máy, ưu tiên thấp (scripts/agent/run-in-slot.py).
if [ "${mode%% *}" != remote ] && [ "${CRM_TEST_SLOT_HELD:-}" != 1 ]; then exec python3 scripts/agent/run-in-slot.py bash "$0" "$@"; fi
. scripts/agent/lib.sh
export HIGH_RISK_DIRS PATH="$HOME/go/bin:$PATH"
base_branch=$(ci_base_branch)
base=$(git merge-base HEAD "$base_branch" 2>/dev/null || echo "$base_branch")
echo "== chế độ: $mode; nhánh gốc $base_branch, điểm tách $base"
fail=""
step() { local name=$1 t0=$SECONDS; shift; echo "== $name"; if ! "$@"; then fail="$fail $name"; echo "== $name: ĐỎ"; fi; echo "== $name: $((SECONDS-t0)) giây"; }
changed=$( { git diff --name-only "$base"; git ls-files --others --exclude-standard; } | sort -u )
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
# Chủ shop 24/11: "chạy e2e chưa cần … chạy e2e sau này khi hoàn thành hết toàn bộ các task". tests/e2e vẫn được biên
# dịch và vet (make build/vet, go vet ./... gồm tệp test) nhưng không chạy ở ci-task; CI_TASK_E2E=1 để chạy lại.
noe2e() { if [ "${CI_TASK_E2E:-0}" = 1 ]; then cat; else grep -v '/tests/e2e$'; fi; }
# test_plan in kế hoạch test, mỗi dòng một mục: "skip <lý do>", "all <lý do>", "race <gói…>", "plain <gói…>".
test_plan() {
  if [ -n "$tnote" ] && [ -z "$tchanged" ]; then echo "skip không đổi gì từ lần test xanh trước"; return; fi
  if printf '%s\n' "$tchanged" | grep -E -q '^(go\.mod$|go\.sum$|internal/testkit/)'; then echo "all đổi go.mod/testkit$tnote"; return; fi
  # Task 473 (23/11, chủ shop: máy nóng): migration mới chỉ thêm → gói dùng đúng đối tượng nó chạm; migration thu hẹp hay
  # sửa tệp cũ → affected.py thoát 5 (toàn bộ). Bản so là bản đã test xanh ($tbase). -race chỉ cho gói sửa trực tiếp
  # ("race"); gói chỉ dùng tới ("plain") chạy test thường.
  local split arc race plain
  split=$(printf '%s\n' "$tchanged" | python3 scripts/agent/affected.py --base "$tbase" --split); arc=$?
  # Không xác định được gói bị ảnh hưởng (4), hay migration cần toàn bộ (5): chạy toàn bộ thay vì bỏ sót (19/11).
  if [ "$arc" != 0 ]; then echo "all affected.py thoát $arc$tnote"; return; fi
  race=$(printf '%s\n' "$split" | sed -n 's/^race //p' | noe2e | grep -v '/tests/arch$')
  plain=$(printf '%s\n' "$split" | sed -n 's/^plain //p' | grep -v '/tests/arch$' | noe2e)
  # Test cấu hình quy trình và Makefile (Task 401): giữ dù affected.py đã chọn test đọc tệp dữ liệu theo đường dẫn —
  # test deploy đọc nhiều tệp qua glob, tên dựng lúc chạy, affected.py không bắt hết.
  if printf '%s\n' "$tchanged" | grep -E -q '^(Makefile|tools/versions\.mk|scripts/|\.github/|\.githooks/|docker/)'; then
    plain=$(printf '%s\n%s/tests/deploy\n' "$plain" "$(go list -m)")
  fi
  # Mỗi gói một lần: gói đã chạy -race không chạy lại ở lượt thường.
  plain=$(printf '%s\n' "$plain" | grep . | sort -u | grep -v -x -F -f <(printf '%s\n' "$race" | grep . || echo '#'))
  [ -n "$race" ] && echo "race $(printf '%s\n' "$race" | tr '\n' ' ')"
  [ -n "$plain" ] && echo "plain $(printf '%s\n' "$plain" | grep . | tr '\n' ' ')"
  return 0
}
plan=$(test_plan)

if [ "${mode%% *}" = remote ]; then
  # Từ xa: cổng tĩnh (job static) và test (ma trận tối đa 6 phần) chạy song song trên GitHub cho đúng tree hiện tại.
  tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
  case "$plan" in
    skip*) : > "$tmp/pkgs"; : > "$tmp/race" ;;
    all*) echo ALL > "$tmp/pkgs"; echo ALL > "$tmp/race" ;;
    *) printf '%s\n' "$plan" | sed -n -E 's/^(race|plain) //p' > "$tmp/pkgs"; printf '%s\n' "$plan" | sed -n 's/^race //p' > "$tmp/race" ;;
  esac
  echo "== test từ xa: ${plan:-không gói nào}"
  CRM_CI_STATIC=true scripts/ci/remote-test.sh "$now_tree" "$base" "$tmp/pkgs" "$tmp/race"; rc=$?
  case $rc in
    0) echo "$now_tree" > "$stamp"; echo "CI-TASK XANH (từ xa, so với điểm tách nhánh $base)"; exit 0 ;;
    2|3) echo "== không chạy được từ xa (mã $rc, xem dòng ở trên): lùi về chạy máy như cũ"
       rm -rf "$tmp"; CRM_CI_REMOTE=0 exec bash "$0" "$@" ;;
    *) echo "CI-TASK ĐỎ: từ xa (cổng tĩnh hay test, xem các dòng lỗi ở trên)"; exit 1 ;;
  esac
fi

# Chạy máy. Cổng tĩnh (một danh sách với job static của GitHub): scripts/ci/static-gates.sh.
slog=$(mktemp)
scripts/ci/static-gates.sh "$base" 2>&1 | tee "$slog"; src=${PIPESTATUS[0]}
fail="$fail$(sed -n 's/^STATIC ĐỎ://p' "$slog")"; rm -f "$slog"
# Mã thoát là nguồn sự thật: script chết giữa chừng (không in dòng STATIC ĐỎ) vẫn là đỏ (review Task 493).
if [ "$src" != 0 ] && [ -z "$fail" ]; then fail=" static-gates(thoát $src)"; fi
# Dừng sớm (24/11, chủ shop: "tăng tốc độ xử lý các task và vẫn đảm bảo chất lượng"). Cổng tĩnh đỏ thì bản này chắc
# chắn phải sửa và chạy lại ci-task, nên test của bản này vô ích: bỏ test, báo ngay mọi cổng đỏ. Dấu xanh chỉ ghi khi
# test xanh. CI_TASK_FULL=1 để chạy test luôn.
if [ -n "$fail" ] && [ "${CI_TASK_FULL:-0}" != 1 ]; then
  echo "== test: bỏ lần này (cổng tĩnh đỏ:$fail) — sửa các cổng đỏ rồi chạy lại ci-task, test chạy ở lần đó"
  echo "CI-TASK ĐỎ:$fail"; exit 1
fi
fail_before=$fail
alltests() { go list ./... | noe2e; }
while read -r kind rest; do
  # shellcheck disable=SC2046,SC2086
  case $kind in
    skip) echo "== test: bỏ ($rest)" ;;
    all) step "test (toàn bộ trừ e2e: $rest)" go test -race -count=1 $(alltests) ;;
    race) step "test -race (gói sửa trực tiếp, vùng cao$tnote)" go test -race -count=1 $rest ;;
    plain) step "test thường không -race (gói chỉ dùng tới phần đổi, test deploy$tnote)" go test -count=1 $rest ;;
  esac
done <<< "$plan"
if [ "$fail" = "$fail_before" ] && [ -n "$now_tree" ]; then echo "$now_tree" > "$stamp"; fi
if [ -n "$fail" ]; then echo "CI-TASK ĐỎ:$fail"; exit 1; fi
echo "CI-TASK XANH (so với điểm tách nhánh $base)"
