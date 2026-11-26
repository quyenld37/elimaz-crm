# Hàm dùng chung của cổng kiểm từ xa (Task 493). Nạp bằng `. scripts/ci/lib.sh`; chạy trong thư mục của repo.

# ci_base_branch in nhánh gốc của task: CRM_BASE_BRANCH nếu đặt; không thì nhánh (m4 hay main) có điểm tách với HEAD mới
# hơn: task tách từ m4 thì m4 (kể cả khi m4 đã đi tiếp sau đó — không đòi "HEAD chứa đầu m4", lượt 26/11 dò sai ra main
# vì vậy); task tách từ main, hay m4 đã gộp vào main rồi task tách từ main sau đó, thì main. Không có m4 thì main.
ci_base_branch() {
  if [ -n "${CRM_BASE_BRANCH:-}" ]; then echo "$CRM_BASE_BRANCH"; return; fi
  local mb_m4 mb_main
  if git rev-parse --verify -q m4 >/dev/null && mb_m4=$(git merge-base HEAD m4 2>/dev/null); then
    mb_main=$(git merge-base HEAD main 2>/dev/null) || { echo m4; return; }
    if [ "$mb_main" != "$mb_m4" ] && git merge-base --is-ancestor "$mb_main" "$mb_m4"; then echo m4; return; fi
  fi
  echo main
}

# ci_mode in "remote" khi chạy được từ xa (có remote origin, gh đã đăng nhập), không thì "local <lý do>".
# CRM_CI_REMOTE=0 ép chạy máy. Mất mạng giữa chừng không dò ở đây: remote-test.sh thoát 3 và bên gọi lùi về máy.
ci_mode() {
  if [ "${CRM_CI_REMOTE:-}" = 0 ]; then echo "local CRM_CI_REMOTE=0"; return; fi
  if ! git remote get-url origin >/dev/null 2>&1; then echo "local không có remote origin"; return; fi
  if ! "${CRM_GH:-gh}" auth status >/dev/null 2>&1; then echo "local gh không dùng được (chưa cài, chưa đăng nhập hay mất mạng)"; return; fi
  echo remote
}
