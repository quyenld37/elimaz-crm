#!/bin/sh
# Container filesync (Task M1-45; ADR-36, spec 04 mục 16): chép kho chính sang kho ngoài một lượt khi khởi động rồi
# mỗi FILESYNC_INTERVAL_S giây (mặc định 3600), đối chiếu mỗi ngày từ 03:30 giờ Việt Nam. Chỉ thêm: không bao giờ
# sync, move, delete. Mỗi bucket mỗi lượt một dòng platform.file_sync_runs; luật cảnh báo của worker đọc bảng đó.
#
#   run.sh           vòng lặp (ENTRYPOINT của image)
#   run.sh --once    một lượt chép rồi thoát (thoát 1 khi có bucket lỗi)
set -eu
. "$(dirname "$0")/lib.sh"
fs_init

if [ "${1:-}" = "--once" ]; then
  copy_all
  exit $?
fi

interval=${FILESYNC_INTERVAL_S:-3600}
trap 'log "nhận tín hiệu dừng"; exit 0' TERM INT

# reconcile_due: đã qua 03:30 giờ Việt Nam hôm nay và hôm nay chưa đối chiếu.
reconcile_due() {
  hm=$(TZ=Asia/Ho_Chi_Minh date +%H%M)
  today=$(TZ=Asia/Ho_Chi_Minh date +%Y-%m-%d)
  [ "1$hm" -ge 10330 ] && [ "$(cat "$FILESYNC_STATE_DIR/last-reconcile" 2>/dev/null || true)" != "$today" ]
}

last=$(date +%s)
copy_all || true
while :; do
  sleep 60 &
  wait $! || true
  now=$(date +%s)
  if [ $((now - last)) -ge "$interval" ]; then
    last=$now
    copy_all || true
  fi
  if reconcile_due; then
    reconcile_all || true
  fi
done
