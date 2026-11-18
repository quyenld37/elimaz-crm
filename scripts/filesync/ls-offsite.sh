#!/bin/sh
# Liệt kê tệp ở kho ngoài (đã giải mã với bucket mã hóa) dưới một tiền tố, qua đúng cấu hình của container. Chỉ đọc.
# Dùng khi kiểm bản sao bằng tay và trong tests/e2e/filesync_test.go:
#   docker compose run --rm --entrypoint /opt/filesync/ls-offsite.sh filesync crm-private <tiền tố>
set -eu
. "$(dirname "$0")/lib.sh"
fs_init
d=$(dst_of "$1")
case "$d" in
  *:) path="$d${2:-}" ;;
  *) path="$d/${2:-}" ;;
esac
# shellcheck disable=SC2046
rclone lsf -R --files-only $(rclone_common) "$path"
