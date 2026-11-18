#!/bin/sh
# Phần chung của crash-drill.sh, restore-drill.sh (Task M1-46). Chỉ `source`, không chạy.
#
# Bài diễn tập làm hỏng thật (docker kill, xóa volume) nên chỉ chạy trên dự án compose RIÊNG: COMPOSE_PROJECT (hoặc
# COMPOSE_PROJECT_NAME) bắt buộc, không nhận "crm" (bộ dev mặc định) hay "crm-shared" (bộ dùng chung). Cổng lấy từ
# môi trường như Makefile: CRM_PG_PORT, CRM_S3_PORT, CRM_S3_OFFSITE_PORT; test đọc CRM_TEST_DATABASE_URL.
set -eu

cd "$(dirname "$0")/../.."
DRILL_PROJECT=${COMPOSE_PROJECT:-${COMPOSE_PROJECT_NAME:-}}
case "$DRILL_PROJECT" in
  '' | crm | crm-shared)
    echo "diễn tập: đặt COMPOSE_PROJECT là dự án compose riêng (ví dụ crm-drill), không phải '$DRILL_PROJECT'" >&2
    exit 2
    ;;
esac
: "${CRM_PG_PORT:?đặt CRM_PG_PORT của dự án $DRILL_PROJECT}"
: "${CRM_S3_PORT:?đặt CRM_S3_PORT của dự án $DRILL_PROJECT}"
: "${CRM_S3_OFFSITE_PORT:?đặt CRM_S3_OFFSITE_PORT của dự án $DRILL_PROJECT}"
: "${CRM_TEST_DATABASE_URL:=postgres://crm_app:crm_app_dev@127.0.0.1:$CRM_PG_PORT/crm?sslmode=disable}"
export COMPOSE_PROJECT_NAME="$DRILL_PROJECT" CRM_DRILL_PROJECT="$DRILL_PROJECT" CRM_PG_PORT CRM_S3_PORT \
  CRM_S3_OFFSITE_PORT CRM_TEST_DATABASE_URL

DRILL_COMPOSE="docker compose -p $DRILL_PROJECT -f docker/compose.dev.yml"

# drill_run TÊN_TEST DÒNG_OK: dựng bộ compose (chờ sẵn sàng), chạy test diễn tập, in và kiểm dòng kết quả.
drill_run() {
  $DRILL_COMPOSE up -d --wait
  out=$(mktemp "${TMPDIR:-/tmp}/files-drill.XXXXXX")
  set +e
  go test -tags drill -count=1 -timeout 30m -run "^$1\$" -v ./tests/drills/ >"$out" 2>&1
  rc=$?
  set -e
  grep -E "^(--- |FILES |    )" "$out" || true
  if [ "$rc" -ne 0 ] || ! grep -q "^$2" "$out"; then
    echo "diễn tập $1 HỎNG (go test thoát $rc); log đầy đủ: $out" >&2
    exit 1
  fi
  rm -f "$out"
}
