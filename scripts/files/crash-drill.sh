#!/bin/sh
# Diễn tập TP08 (Task M1-46): 200 lượt tải lên song song qua đường thật; giữa chừng `docker kill -s KILL` SeaweedFS
# của dự án diễn tập rồi khởi động lại; mọi tệp đã báo xong đọc ra đúng sha256, lượt dở tải lại được.
#   COMPOSE_PROJECT=crm-drill CRM_PG_PORT=… CRM_S3_PORT=… CRM_S3_OFFSITE_PORT=… scripts/files/crash-drill.sh
# In "FILES CRASH OK …" khi đạt; thoát khác 0 khi hỏng.
set -eu
. "$(dirname "$0")/drill-lib.sh"
drill_run TestFileStoreCrash "FILES CRASH OK"
