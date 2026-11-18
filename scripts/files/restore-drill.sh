#!/bin/sh
# Diễn tập TP09, TP12 (Task M1-46), có bấm giờ: chép một lượt sang kho ngoài giả, tải thêm 2 ảnh, rồi dừng SeaweedFS
# chính của dự án diễn tập, XÓA VOLUME, khởi động rỗng, bootstrap.sh, chép ngược từ kho ngoài (giải mã bucket riêng
# tư); verifyFiles; đúng 2 ảnh tải sau lần chép cuối thành lost.
#   COMPOSE_PROJECT=crm-drill CRM_PG_PORT=… CRM_S3_PORT=… CRM_S3_OFFSITE_PORT=… scripts/files/restore-drill.sh
# In "FILES RESTORE OK restore_seconds=<x> lost=<n> …" khi đạt; thoát khác 0 khi hỏng.
set -eu
. "$(dirname "$0")/drill-lib.sh"
# Container filesync chạy script của mã đang có (test gắn scripts/filesync chỉ đọc), không dựng lại image.
drill_run TestFileStoreRestore "FILES RESTORE OK"
