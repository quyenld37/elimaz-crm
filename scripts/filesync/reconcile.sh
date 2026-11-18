#!/bin/sh
# Đối chiếu số tệp và dung lượng hai phía, mỗi ngày 03:30 giờ Việt Nam (run.sh gọi reconcile_all trong vòng lặp; tệp
# này để chạy tay hay test: `docker compose run --rm --entrypoint /opt/filesync/reconcile.sh filesync`). Task M1-45.
# Thoát 1 khi có bucket lệch hay lỗi (đã ghi dòng reconcile ok = false).
set -eu
. "$(dirname "$0")/lib.sh"
fs_init
reconcile_all
