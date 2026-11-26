#!/usr/bin/env bash
# Test bản đã gộp của điều phối viên qua bộ điều phối máy (Task 494): cổng tĩnh + test trên chỗ trống nhanh nhất
# (scripts/ci/pool.py: blacksmith → ubicloud → github → vps; máy dev chỉ là dự phòng cuối, Task 495), không xếp hàng một
# lượt test của máy dev.
#
# Dùng: merge-test.sh [bản] [base] [tệp gói] [tệp gói -race]
#   bản      commit hay tree cần test (mặc định HEAD). Tree: cha là HEAD.
#   base     ref để so (oasdiff, test-guard, gen-check); mặc định cha thứ nhất của commit gộp (nhánh gốc trước khi gộp),
#            với tree là HEAD.
#   tệp gói  như remote-test.sh: ALL (mặc định: mọi gói trừ tests/e2e) hoặc tệp mỗi dòng một gói; -race mặc định ALL.
# Máy dev (bể mac, dự phòng cuối) chỉ nhận khi bản cần test chính là HEAD của thư mục này và thư mục sạch (test chạy
# trên tệp thật).
# In "MERGE-TEST XANH (<bể>) …" (thoát 0) hoặc "MERGE-TEST ĐỎ (<bể>) …" (thoát 1); 2 sai tham số; 3 không máy nào chạy được.
# Biến: như pool.py (CRM_CI_RUNNER ép bể…) và remote-test.sh (CRM_CI_STATIC=false bỏ cổng tĩnh, cả ở máy dev);
# CRM_CI_LABEL mặc định merge-<tên thư mục>.
set -u
here=$(cd "$(dirname "$0")" && pwd)
cd "$(git rev-parse --show-toplevel)" || exit 2

if [ "${1:-}" = __inner ]; then
  shift
  src=$1 base=$2 pkgs=$3 race=$4
  if [ "${CRM_CI_RUNNER:-}" != mac ]; then
    # Nhãn riêng mỗi lượt (pid): hai lượt cùng lúc chung nhánh ci/<nhãn> thì concurrency của workflow hủy run của nhau.
    CRM_CI_LABEL=${CRM_CI_LABEL:-merge-$(basename "$(pwd)")-$$} "$here/remote-test.sh" "$src" "$base" "$pkgs" "$race"; rc=$?
    case $rc in
      0) echo "MERGE-TEST XANH ($CRM_CI_RUNNER)" ;;
      1) echo "MERGE-TEST ĐỎ ($CRM_CI_RUNNER)" ;;
    esac
    exit "$rc"
  fi
  # Máy dev: cùng cổng tĩnh với job static, rồi test (gói -race chạy -race, còn lại test thường).
  # go list lỗi (gói gõ sai) là sai tham số: không bao giờ thành "không gói nào" rồi XANH (review Task 494, như 493).
  list() {
    local out
    if [ "$1" = ALL ]; then out=$(go list ./...) || return 1; printf '%s\n' "$out" | grep -v '/tests/e2e$'
    elif [ -s "$1" ]; then xargs go list < "$1" || return 1; fi
  }
  r=$(list "$race") || { echo "MERGE-TEST: go list lỗi với gói -race ($race)" >&2; exit 2; }
  p=$(list "$pkgs") || { echo "MERGE-TEST: go list lỗi với gói ($pkgs)" >&2; exit 2; }
  r=$(printf '%s\n' "$r" | grep . | sort -u)
  p=$(printf '%s\n' "$p" | grep . | sort -u | grep -v -x -F -f <(printf '%s\n' "$r" | grep . || echo '#'))
  srv=0; if [ "${CRM_CI_STATIC:-true}" = true ]; then "$here/static-gates.sh" "$base"; srv=$?; fi
  make --no-print-directory test-part RACE="$(echo $r)" PLAIN="$(echo $p)"; trc=$?
  if [ "$srv" = 0 ] && [ "$trc" = 0 ]; then echo "MERGE-TEST XANH (mac)"; exit 0; fi
  echo "MERGE-TEST ĐỎ (mac: cổng tĩnh thoát $srv, test thoát $trc)"; exit 1
fi

src=${1:-HEAD} pkgs=${3:-ALL} race=${4:-ALL}
case $(git cat-file -t "$src" 2>/dev/null) in
  commit) src=$(git rev-parse "$src^{commit}"); def_base=$(git rev-parse -q --verify "$src^1") || def_base=$src ;;
  tree) def_base=HEAD ;;
  *) echo "không phải commit hay tree: $src" >&2; exit 2 ;;
esac
base=${2:-$def_base}
for f in "$pkgs" "$race"; do [ "$f" = ALL ] || [ -f "$f" ] || { echo "không có tệp gói: $f" >&2; exit 2; }; done
# Tệp chỉ chứa ALL (cách ci-task ghi) là ALL.
isall() { [ "$1" = ALL ] || [ "$(tr -d ' \t\n' < "$1")" = ALL ]; }
isall "$pkgs" && pkgs=ALL
isall "$race" && race=ALL
# Số gói sau khi mở mẫu (./internal/x/... là nhiều gói): vps chỉ nhận ≤ 15. Không mở được thì coi như ALL (không vps).
if [ "$pkgs" = ALL ]; then size=(--all)
elif n=$(xargs go list < "$pkgs" 2>/dev/null | grep -c .); then size=(--count "$n")
else size=(--all); fi
flags=()
if [ "$src" != "$(git rev-parse HEAD)" ] || [ -n "$(git status --porcelain)" ]; then flags+=(--no-mac); fi
. "$here/lib.sh"
mode=$(ci_mode)
if [ "${mode%% *}" != remote ]; then
  echo "== không chạy được từ xa (${mode#local }): chỉ máy dev"
  [ ${#flags[@]} -eq 0 ] || { echo "MERGE-TEST: bản cần test không phải HEAD sạch của thư mục này, không chạy được ở máy" >&2; exit 3; }
  flags+=(--local-only)
fi
if [ "${CRM_CI_RUNNER:-}" = mac ] && [ "${flags[0]:-}" = --no-mac ]; then
  echo "CRM_CI_RUNNER=mac: bản cần test không phải HEAD sạch của thư mục này" >&2; exit 2
fi
echo "== merge-test: $src so với $base; gói: $pkgs; -race: $race"
exec python3 "$here/pool.py" run "${size[@]}" ${flags[@]+"${flags[@]}"} -- bash "$here/merge-test.sh" __inner "$src" "$base" "$pkgs" "$race"
