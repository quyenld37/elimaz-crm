# Hàm dùng chung cho ba phép thử ngắn chạy trên compose diễn tập (kế hoạch M0 bản 5, Task 2).
# Dùng: . scripts/backup/drill-lib.sh   (từ gốc repo hoặc bất kỳ đâu)
# Chạy được với bash 3.2 của macOS.
# shellcheck shell=bash

DRILL_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
DRILL_PROJECT=crm-t02
DRILL_COMPOSE_FILE="$DRILL_ROOT/docker/compose.backup-drill.yml"
DRILL_BUCKET=crm-backup

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
die() { log "LỖI: $*"; exit 1; }

dc() { docker compose -p "$DRILL_PROJECT" -f "$DRILL_COMPOSE_FILE" "$@"; }

# Khóa diễn tập: sinh lúc chạy, chỉ sống trong biến môi trường. Không in, không ghi tệp.
drill_gen_secrets() {
  command -v openssl >/dev/null || die "thiếu openssl"
  AWS_ACCESS_KEY_ID="drill$(openssl rand -hex 8)"
  AWS_SECRET_ACCESS_KEY=$(openssl rand -hex 24)
  WALG_LIBSODIUM_KEY=$(openssl rand -hex 32) # 32 byte dạng hex
  CRM_DRILL_PG_PASSWORD=$(openssl rand -hex 16)
  export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY WALG_LIBSODIUM_KEY CRM_DRILL_PG_PASSWORD
}

# psql trong container PostgreSQL chính, qua socket, bằng role chỉ định (mặc định postgres).
# Dùng: drill_psql [-U role] [-d db] -c "..."
drill_psql() { dc exec -T postgres psql -X -v ON_ERROR_STOP=1 -At -U postgres -d crm "$@"; }

# Chờ một service nhận kết nối. Hỏi qua TCP, vì lúc khởi tạo cụm docker-entrypoint chạy một server tạm chỉ nghe socket:
# hỏi qua socket có thể trúng server tạm đó trước khi init xong. drill_wait_ready <service> [giây]
drill_wait_ready() {
  local svc=$1 limit=${2:-120} i=0
  while [ "$i" -lt "$limit" ]; do
    if dc exec -T "$svc" pg_isready -q -h 127.0.0.1 -U postgres -d crm >/dev/null 2>&1; then return 0; fi
    sleep 1
    i=$((i + 1))
  done
  dc logs --tail 50 "$svc" >&2 || true
  die "$svc không nhận kết nối sau ${limit}s"
}

# Chờ SeaweedFS nhận yêu cầu S3 và tạo bucket.
drill_make_bucket() {
  local i=0
  while [ "$i" -lt 60 ]; do
    if dc run --rm -T rclone mkdir "s3:$DRILL_BUCKET" >/dev/null 2>&1; then return 0; fi
    sleep 1
    i=$((i + 1))
  done
  dc logs --tail 50 seaweedfs >&2 || true
  die "không tạo được bucket $DRILL_BUCKET trên SeaweedFS"
}

# Bật cụm diễn tập từ đầu: xóa container và volume cũ của riêng dự án crm-t02, dựng image, bật, tạo bucket.
drill_up() {
  drill_purge
  dc build postgres >&2 || die "dựng image PostgreSQL thất bại"
  dc up -d seaweedfs >&2 || die "không bật được SeaweedFS"
  drill_make_bucket
  dc up -d postgres >&2 || die "không bật được PostgreSQL"
  drill_wait_ready postgres 120
}

# Dọn: chỉ container và volume của dự án crm-t02. Đặt CRM_DRILL_KEEP=1 để giữ lại xem.
drill_down() {
  if [ "${CRM_DRILL_KEEP:-0}" = 1 ]; then
    log "giữ lại cụm diễn tập (CRM_DRILL_KEEP=1); dọn bằng: docker compose -p $DRILL_PROJECT -f docker/compose.backup-drill.yml --profile restore --profile tools down -v (cần đặt bốn biến khóa bất kỳ)"
    return 0
  fi
  drill_purge
}

# Xóa container và volume của dự án crm-t02, kể cả của service trong profile (down -v không có --profile thì bỏ sót
# volume pgrestore). Thất bại thì dừng: chạy tiếp trên volume cũ cho kết quả sai.
drill_purge() {
  dc --profile restore --profile tools down -v --remove-orphans >/dev/null 2>&1 ||
    die "không dọn được dự án $DRILL_PROJECT"
  if docker volume ls -q --filter "label=com.docker.compose.project=$DRILL_PROJECT" | grep -q .; then
    die "còn volume của $DRILL_PROJECT sau khi dọn"
  fi
}

now_s() { date +%s; }
