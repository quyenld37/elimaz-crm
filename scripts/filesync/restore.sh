#!/bin/sh
# Dựng lại kho chính từ kho ngoài (Task M1-46; RUNBOOK "Dựng lại kho tệp"): chép ngược crm-public, crm-originals,
# crm-private bằng `rclone copy` (chỉ thêm; crm-originals, crm-private giải mã qua rclone crypt bằng đúng hai mật khẩu
# của lúc chép đi). Chạy SAU KHI kho chính đã có bucket (bootstrap.sh), bằng danh tính GHI được kho chính (crm-admin):
#   docker compose --profile filesync run --rm -e FILESYNC_SRC_ACCESS_KEY_ID=<khóa crm-admin> \
#     -e FILESYNC_SRC_SECRET_ACCESS_KEY=<bí mật crm-admin> --entrypoint /opt/filesync/restore.sh filesync
# Không bao giờ sync, move, delete; không đụng kho ngoài ngoài việc đọc. Thoát 1 khi có bucket lỗi.
set -eu
. "$(dirname "$0")/lib.sh"
fs_init

all=0
for b in $FILESYNC_BUCKETS; do
  started=$(date +%s)
  # shellcheck disable=SC2046 # rclone_common in mỗi tham số một dòng, không có khoảng trắng
  if rclone copy "$(dst_of "$b")" "$(src_of "$b")" --immutable $(rclone_common); then
    counts=$(size_of "$(src_of "$b")" || echo "? ?")
    log "restore $b: xong sau $(($(date +%s) - started)) s; kho chính có ${counts%% *} tệp ${counts#* } byte"
  else
    log "restore $b: LỖI (rclone thoát khác 0)"
    all=1
  fi
done
exit "$all"
