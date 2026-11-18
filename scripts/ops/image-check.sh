#!/usr/bin/env bash
# Kiểm image chính (kế hoạch M0 bản 5, Task 21, Step 1) trên compose dev của phiên này:
#   1. docker build -t crm:dev . (kèm một tệp .env mồi trong ngữ cảnh dựng để chứng minh .dockerignore chặn được);
#   2. crm-migrate chạy một lần, rồi crm-ingest, crm-api, crm-worker từ image;
#   3. /health/ready của api và ingest trả 200; docker inspect cả ba "healthy";
#   4. tiến trình trong container chạy bằng uid khác 0; image không chứa tệp .env*;
#   5. in kích thước image.
# Dùng: make image-check   (hoặc COMPOSE_PROJECT=crm-tNN CRM_PG_PORT=... scripts/ops/image-check.sh)
# Cần: compose dev đã bật (make db-up). Dịch vụ ứng dụng được gỡ khi xong; CRM_IMAGE_CHECK_KEEP=1 để giữ lại xem.
# In "IMAGE CHECK OK size_bytes=<n> ..." và thoát 0 khi đạt.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
PROJECT=${COMPOSE_PROJECT:-crm}
API_PORT=${CRM_API_PORT:-4190}
INGEST_PORT=${CRM_INGEST_PORT:-4191}
APP=(crm-ingest crm-api crm-worker)

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
die() {
  log "LỖI: $*"
  exit 1
}
dc() { docker compose -p "$PROJECT" -f "$ROOT/docker/compose.dev.yml" --profile app "$@"; }

# Tệp mồi nằm trong internal/, thư mục Dockerfile chép vào stage dựng: chỉ .dockerignore chặn được nó.
decoy="$ROOT/internal/.env.image-check-decoy"
cleanup() {
  rm -f "$decoy"
  docker image rm crm:dev-build >/dev/null 2>&1 || true
  if [ "${CRM_IMAGE_CHECK_KEEP:-0}" != 1 ]; then
    dc rm -sf crm-migrate "${APP[@]}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

# (1) Dựng. Tệp mồi .env nằm trong ngữ cảnh dựng; .dockerignore phải loại nó.
printf 'DECOY=1\n' >"$decoy"
t0=$(date +%s)
docker build -q -t crm:dev "$ROOT" >/dev/null || die "docker build thất bại"
docker build -q --target build -t crm:dev-build "$ROOT" >/dev/null || die "docker build --target build thất bại"
if docker run --rm --entrypoint sh crm:dev-build -c 'find /src -name ".env*" | grep .' >&2; then
  die "ngữ cảnh dựng chứa tệp .env*: .dockerignore không chặn"
fi
build_seconds=$(($(date +%s) - t0))
rm -f "$decoy"

# (4b) Image không chứa tệp .env* nào (kể cả tệp mồi); stage dựng cũng không nhận tệp mồi.
cid=$(docker create crm:dev)
files=$(docker export "$cid" | tar -t)
docker rm "$cid" >/dev/null
if printf '%s\n' "$files" | grep -E '(^|/)\.env' >&2; then die "image chứa tệp .env*"; fi
printf '%s\n' "$files" | grep -qx 'crm' || die "image thiếu /crm"
user=$(docker image inspect -f '{{.Config.User}}' crm:dev)
[ "$user" = "65532:65532" ] || die "USER của image là '$user', muốn 65532:65532"

# (2) Migrate một lần, rồi ba vai trò. up --wait đợi healthcheck của cả ba.
dc run --rm -T crm-migrate >&2 || die "crm-migrate thất bại"
dc up -d --wait --wait-timeout 180 --no-deps "${APP[@]}" >&2 || {
  dc ps -a >&2
  dc logs --tail 40 "${APP[@]}" >&2
  die "ba vai trò chưa healthy"
}

# (3) /health/ready trả 200 trên cổng của máy; trạng thái docker inspect.
for p in "$API_PORT" "$INGEST_PORT"; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$p/health/ready")
  [ "$code" = 200 ] || die "/health/ready cổng $p trả $code"
done
states=""
for s in "${APP[@]}"; do
  c=$(dc ps -q "$s")
  st=$(docker inspect -f '{{.State.Health.Status}}' "$c")
  [ "$st" = healthy ] || die "$s: $st"
  # (4a) uid của tiến trình đang chạy trong container.
  uid=$(docker top "$c" -o pid,uid | awk 'NR==2 {print $2}')
  [ -n "$uid" ] && [ "$uid" != 0 ] && [ "$uid" != root ] || die "$s chạy bằng uid '$uid'"
  states="$states $s=$st,uid=$uid"
done
# Chạy thẳng image không đặt gì: vẫn không root, thoát vì thiếu cấu hình (mã 2), không phải lỗi quyền.
rc=0
docker run --rm crm:dev >/dev/null 2>&1 || rc=$?
[ "$rc" = 2 ] || die "docker run --rm crm:dev thoát $rc, muốn 2 (thiếu CRM_ROLE)"

size=$(docker image inspect -f '{{.Size}}' crm:dev)
echo "IMAGE CHECK OK size_bytes=$size build_seconds=$build_seconds user=$user$states"
