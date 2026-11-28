#!/usr/bin/env bash
# Chạy cổng tĩnh và test của một bản mã trên GitHub Actions (.github/workflows/test-remote.yml), Task 493 — để nhiều
# phiên song song không xếp hàng một lượt test của máy dev. Dùng chung cho ci-task.sh và lệnh gộp của điều phối viên.
#
# Dùng: remote-test.sh <tree|commit> <base> <tệp gói> [tệp gói -race]
#   tree|commit  bản cần test: tree (git write-tree, gồm cả tệp chưa commit; cha là HEAD) hoặc commit.
#   base         ref/sha để so (oasdiff, test-guard, gen-check…); phải là tổ tiên của bản cần test.
#   tệp gói      mỗi dòng một gói/mẫu gói (./internal/x/...), hoặc một dòng ALL (mọi gói trừ tests/e2e); tệp rỗng: không
#                test. Viết thẳng ALL thay cho tên tệp cũng được. Tệp không có: mã 2.
#   tệp -race    gói chạy -race (như trên); bỏ trống hay tệp rỗng: không gói nào -race.
# Biến: CRM_CI_STATIC=true|false (mặc định true: chạy cổng tĩnh), CRM_CI_LABEL (tên nhánh ci/<nhãn>, mặc định tên thư
# mục worktree), TASK_FILE (tệp task cho test-guard; mặc định task đang chạy của worktree), CRM_GH (lệnh gh), CRM_CI_POLL (giây giữa hai lần tìm run).
# Task 494: CRM_CI_RUNNER=github|blacksmith|ubicloud|vps chọn máy (ubicloud: Task 495) (mặc định github; scripts/ci/pool.py đặt biến này khi chọn bể);
# run nằm hàng chờ quá CRM_CI_START_TIMEOUT giây (mặc định 180) mà không job nào bắt đầu thì hủy, mã 3 (bộ điều phối
# đánh dấu bể tạm hỏng, chọn bể khác); phút của mỗi run ghi vào sổ của bộ điều phối (pool.py record).
# Task 180: CRM_CI_LOADTEST=live|chaos[:<thời lượng>] chạy thêm job loadtest (bài thử tải M4, loadtest/fb_run.sh) ở máy
# đã chọn; thường đi cùng tệp gói rỗng và CRM_CI_STATIC=false. Bảng chấm nằm trong tóm tắt của run. Task 88:
# CRM_CI_LOADTEST=m2[:<thời lượng>] chạy bài thử tải M2 (make perf-m2: tìm khách trên 1 triệu khách, lên đơn 10×).
# Task 219: m5:all hay m5:<bài>[,<bài>…][@<thời lượng>] chạy bài M5 (loadtest/m5_run.sh: flash sale, lượt đọc web).
# Task 499: CRM_CI_E2E=1 chạy thêm job e2e (go test -race ./tests/e2e/... trên compose dev đủ profile) và console-e2e
# (Playwright) ở máy đã chọn (mặc định 0: lượt của task không chạy e2e). Không nhận vps (hai job đó bỏ qua ở vps: xanh giả).
# Không đổi nhánh nào: commit tạm dựng bằng git commit-tree, đẩy -f lên refs/heads/ci/<nhãn>, xóa khi xong (cả khi lỗi).
# Thoát 0: xanh. 1: đỏ (in các dòng FAIL của job đỏ). 2: sai tham số. 3: không chạy được từ xa (gh, mạng) — bên gọi lùi
# về chạy máy.
set -u
gh=${CRM_GH:-gh}
poll=${CRM_CI_POLL:-5}
static=${CRM_CI_STATIC:-true}
runner=${CRM_CI_RUNNER:-github}
loadtest=${CRM_CI_LOADTEST:-}
here=$(cd "$(dirname "$0")" && pwd)
case $runner in
  github|blacksmith|ubicloud|vps) ;;
  *) echo "CRM_CI_RUNNER=$runner không phải máy từ xa (github, blacksmith, ubicloud, vps)" >&2; exit 2 ;;
esac
case $loadtest in
  ""|live|chaos|m2|live:[0-9]*[smh]|chaos:[0-9]*[smh]|m2:[0-9]*[smh]) ;;
  m5:*[!a-z_,@0-9smh:]*) echo "CRM_CI_LOADTEST=$loadtest: bài M5 chỉ gồm chữ thường, _, dấu phẩy, @<thời lượng>" >&2; exit 2 ;;
  m5:?*) ;;
  *) echo "CRM_CI_LOADTEST=$loadtest không phải live|chaos|m2[:<thời lượng>] hay m5:all|m5:<bài>[,<bài>…][@<thời lượng>]" >&2; exit 2 ;;
esac
case ${CRM_CI_E2E:-0} in
  1) e2e=true ;;
  0) e2e=false ;;
  *) echo "CRM_CI_E2E=$CRM_CI_E2E không phải 0|1" >&2; exit 2 ;;
esac
if [ "$e2e" = true ] && [ "$runner" = vps ]; then
  echo "CRM_CI_E2E=1 không chạy trên vps (dịch vụ dùng chung; job e2e bỏ qua ở đó): chọn blacksmith, ubicloud hay github" >&2; exit 2
fi
[ $# -ge 3 ] || { echo "Dùng: remote-test.sh <tree|commit> <base> <tệp gói> [tệp gói -race]" >&2; exit 2; }
src=$1 base_ref=$2 pkg_file=$3 race_file=${4:-}
cd "$(git rev-parse --show-toplevel)" || exit 2
label=${CRM_CI_LABEL:-$(basename "$(pwd)")}
label=$(printf '%s' "$label" | LC_ALL=C tr -c 'A-Za-z0-9._-' '-')
branch="ci/$label"
# Tệp task cho test-guard: TASK_FILE, không thì task đang chạy của worktree (trạng thái new-task.sh ghi ngoài git, như
# test-guard.sh đọc ở máy) — runner không có trạng thái đó.
task_file=${TASK_FILE:-}
if [ -z "$task_file" ]; then
  cur=$(sed -n 's/^Task: *//p' "$(git rev-parse --git-dir)/crm-task-state" 2>/dev/null | head -1)
  if [ -n "$cur" ] && [ -f "docs/tasks/task-$cur.md" ]; then task_file="docs/tasks/task-$cur.md"; fi
fi

infra() { echo "REMOTE-TEST KHÔNG CHẠY ĐƯỢC: $*" >&2; exit 3; }
# words: nội dung tệp gói trên một dòng. Chữ ALL viết thẳng làm tham số cũng nhận; tệp không có là sai tham số (mã 2),
# không bao giờ hiểu thành "không test" (review Task 493: gõ ALL hay sai đường dẫn từng cho XANH mà không gói nào chạy).
words() {
  if [ "$1" = ALL ]; then echo ALL; return; fi
  [ -f "$1" ] || { echo "không có tệp gói: $1" >&2; exit 2; }
  tr -s ' \t\n' ' ' < "$1" | sed 's/^ //; s/ $//'
}

case $(git cat-file -t "$src" 2>/dev/null) in
  tree) tree=$src; parent=$(git rev-parse HEAD) ;;
  commit) tree=$(git rev-parse "$src^{tree}"); parent=$(git rev-parse "$src^{commit}") ;;
  *) echo "không phải tree hay commit: $src" >&2; exit 2 ;;
esac
base=$(git rev-parse --verify -q "$base_ref^{commit}") || { echo "base không có: $base_ref" >&2; exit 2; }
git merge-base --is-ancestor "$base" "$parent" || { echo "base $base_ref không phải tổ tiên của bản cần test" >&2; exit 2; }
pkgs=$(words "$pkg_file") || exit 2
race=""; if [ -n "$race_file" ]; then race=$(words "$race_file") || exit 2; fi
command -v "$gh" >/dev/null 2>&1 || infra "không có lệnh gh"
command -v jq >/dev/null 2>&1 || infra "không có lệnh jq"

sha=$(git commit-tree "$tree" -p "$parent" -m "ci: ảnh chụp để test từ xa ($label)") || infra "git commit-tree lỗi"
echo "== commit tạm $sha (tree $tree) → $branch"
pushed=0 id="" conclusion=""
cleanup() {
  # Bỏ cuộc khi run chưa xong (mất mạng, Ctrl-C): hủy run để không tốn phút trong lúc bên gọi lùi về chạy máy.
  if [ -n "$id" ] && [ -z "$conclusion" ]; then "$gh" run cancel "$id" >/dev/null 2>&1 || true; fi
  if [ "$pushed" = 1 ]; then git push -q origin --delete "refs/heads/$branch" >/dev/null 2>&1 || echo "== không xóa được $branch trên origin" >&2; fi
}
trap cleanup EXIT
trap 'exit 130' INT TERM
git push -q -f origin "$sha:refs/heads/$branch" || infra "git push lỗi"
pushed=1

"$gh" workflow run test-remote.yml --ref "$branch" -f base="$base" -f packages="$pkgs" -f race_packages="$race" \
  -f static="$static" -f task_file="$task_file" -f runner="$runner" -f loadtest="$loadtest" -f e2e="$e2e" || infra "gh workflow run lỗi"

for _ in $(seq 1 36); do
  id=$("$gh" run list --workflow test-remote.yml --branch "$branch" --event workflow_dispatch --limit 20 \
    --json databaseId,headSha 2>/dev/null | jq -r --arg s "$sha" '[.[] | select(.headSha == $s)][0].databaseId // empty')
  [ -n "$id" ] && break
  sleep "$poll"
done
[ -n "$id" ] || infra "không tìm thấy run của $sha"
echo "== run $id trên $runner: chờ job đầu tiên bắt đầu"
# Bắt đầu: run đã xong (mọi job bỏ qua) hay một job đang chạy / đã chạy xong (không tính job bỏ qua).
started='.status == "completed" or ([.jobs[]? | select(.status == "in_progress" or (.status == "completed" and .conclusion != "skipped"))] | length > 0)'
start_timeout=${CRM_CI_START_TIMEOUT:-180} t0=$SECONDS
until [ "$("$gh" run view "$id" --json status,jobs 2>/dev/null | jq -r "$started" 2>/dev/null)" = true ]; do
  [ $((SECONDS - t0)) -ge "$start_timeout" ] && infra "run $id không bắt đầu sau $start_timeout giây (không máy $runner nào nhận)"
  sleep "$poll"
done
echo "== run $id đang chạy (chờ tới khi xong)"
# gh run watch có lúc thoát khi run chưa xong (lỗi mạng chập chờn, 26/11): hỏi lại trạng thái, chờ tiếp; bỏ cuộc (mã 3)
# sau CRM_CI_WATCH_TRIES lần liền không đọc được hay run vẫn chưa xong.
view="" tries=${CRM_CI_WATCH_TRIES:-120}
for _ in $(seq 1 "$tries"); do
  "$gh" run watch "$id" --interval 30 --exit-status >/dev/null 2>&1
  if view=$("$gh" run view "$id" --json conclusion,url,jobs 2>/dev/null); then
    conclusion=$(printf '%s' "$view" | jq -r '.conclusion // empty')
    [ -n "$conclusion" ] && break
  fi
  sleep "$poll"
done
[ -n "$conclusion" ] || infra "không đọc được kết quả run $id (mất kết nối khi chờ?)"
url=$(printf '%s' "$view" | jq -r '.url')

# Thời gian từng job; GitHub tính tiền mỗi job làm tròn lên phút.
ran='.jobs[] | select(.conclusion != "skipped" and (.startedAt // "") > "2000" and (.completedAt // "") > "2000")
  | ((.completedAt | fromdate) - (.startedAt | fromdate)) as $d | select($d >= 0)'
printf '%s' "$view" | jq -r "$ran"' | "   \(.name): \(.conclusion), \($d / 60 | floor) phút \($d % 60) giây"'
printf '%s' "$view" | jq -r '.jobs[] | select(.conclusion == "skipped") | "   \(.name): bỏ qua"'
minutes=$(printf '%s' "$view" | jq "[$ran"' | $d / 60 | ceil] | add // 0')
echo "== phút máy (làm tròn lên từng job): $minutes phút trên $runner"
python3 "$here/pool.py" record --runner "$runner" --run "$id" --minutes "$minutes" || echo "== không ghi được sổ phút" >&2

if [ "$conclusion" = success ]; then echo "REMOTE-TEST XANH $url"; exit 0; fi
echo "== các dòng lỗi của job đỏ:"
# Dòng log: "<job>\t<bước>\t<giờ> <nội dung>". Bỏ giờ và các dòng lệnh của chính bước (in màu ESC[36;1m).
# Playwright (Task 499): dòng tên spec đỏ "  1) [chromium] › e2e/x.spec.ts:…" và dòng đếm "  N failed".
"$gh" run view "$id" --log-failed 2>/dev/null | awk -F'\t' '{sub(/^[0-9][0-9-]*T[0-9:.]*Z /, "", $NF); print $1 ": " $NF}' \
  | grep -v $'\e\\[36;1m' | grep -E -- '--- FAIL|: FAIL|panic:|ĐỎ|##\[error\]|[0-9]+\) \[(chromium|webkit)\] ›|: +[0-9]+ (failed|flaky)$' | head -80
echo "REMOTE-TEST ĐỎ ($conclusion) $url"
exit 1
