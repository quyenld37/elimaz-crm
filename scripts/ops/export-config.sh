#!/usr/bin/env bash
# Xuất cấu hình máy chủ (kế hoạch M0 bản 5, Task 23; ADR-30 mục 5, docs/REBUILD.md). Chạy trên máy chủ mỗi tuần
# (lịch ở RUNBOOK) và sau mỗi lần đổi cấu hình. Gom vào một tệp nén crm-config-<UTC>.tar.gz:
#   - tệp compose (CRM_COMPOSE_FILE) và cấu hình cloudflared (CRM_TUNNEL_CONFIG, nếu có);
#   - images.txt: tên container, image, digest đang chạy của dự án compose (COMPOSE_PROJECT);
#   - env-names.txt: TÊN các biến trong tệp biến (CRM_ENV_FILE), KHÔNG kèm giá trị.
# Trước khi nén, gitleaks quét bản gom: thấy giá trị bí mật thì dừng (thoát 3), không tạo tệp nén, không gửi gì.
# Giữ 8 bản trong thư mục ra; CRM_CONFIG_S3_URI (ví dụ s3://<bucket>/crm) đặt thì gửi lên kho ngoài dưới tiền tố
# config/ bằng aws CLI (CRM_S3_ENDPOINT cho kho S3 không phải AWS) và giữ 8 bản ở đó.
#
# Dùng: CRM_COMPOSE_FILE=/srv/crm/compose.prod.yml CRM_ENV_FILE=/srv/crm/crm.env COMPOSE_PROJECT=crm \
#         scripts/ops/export-config.sh /srv/crm/config-export
# Cần: bash, docker, gitleaks, tar; aws khi có CRM_CONFIG_S3_URI.
set -euo pipefail

KEEP=8
out=${1:?dùng: export-config.sh <thư mục ra>}
compose=${CRM_COMPOSE_FILE:?đặt CRM_COMPOSE_FILE (tệp compose của máy chủ)}
envfile=${CRM_ENV_FILE:?đặt CRM_ENV_FILE (tệp biến của máy chủ; chỉ đọc tên biến)}
tunnel=${CRM_TUNNEL_CONFIG:-}
project=${COMPOSE_PROJECT:-crm}
s3uri=${CRM_CONFIG_S3_URI:-}

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
die() {
  log "LỖI: $*"
  exit "${2:-1}"
}

for c in docker gitleaks tar; do command -v "$c" >/dev/null || die "thiếu lệnh $c"; done
[ -z "$s3uri" ] || command -v aws >/dev/null || die "thiếu lệnh aws (cần khi đặt CRM_CONFIG_S3_URI)"
[ -f "$compose" ] || die "không thấy tệp compose $compose"
[ -f "$envfile" ] || die "không thấy tệp biến $envfile"
[ -z "$tunnel" ] || [ -f "$tunnel" ] || die "không thấy cấu hình cloudflared $tunnel"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
name="crm-config-$(date -u +%Y%m%dT%H%M%SZ)"
stage="$work/$name"
mkdir -p "$stage"

cp "$compose" "$stage/"
[ -z "$tunnel" ] || cp "$tunnel" "$stage/"

# Tên biến: dòng KEY=... (có thể có "export "); bỏ dòng trống, chú thích. Giá trị không bao giờ rời tệp biến.
sed -n -E 's/^[[:space:]]*(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)=.*/\2/p' "$envfile" | LC_ALL=C sort -u >"$stage/env-names.txt"

# Image đang chạy: tên container, image theo cấu hình, digest thật của image.
ids=$(docker compose -p "$project" -f "$compose" ps -q) || die "docker compose ps thất bại"
if [ -n "$ids" ]; then
  # shellcheck disable=SC2086 # mỗi mã container là một đối số
  docker inspect --format '{{.Name}} {{.Config.Image}} {{.Image}}' $ids >"$stage/images.txt" || die "docker inspect thất bại"
else
  : >"$stage/images.txt"
fi

# Quét bí mật trên bản gom. --redact: báo cáo không in giá trị.
if ! gitleaks dir --no-banner --redact --exit-code 3 "$stage" >"$work/gitleaks.txt" 2>&1; then
  cat "$work/gitleaks.txt" >&2
  die "bản xuất chứa giá trị bí mật: gỡ giá trị khỏi tệp trên (để trong tệp biến), rồi chạy lại" 3
fi

mkdir -p "$out"
tar -C "$work" -czf "$out/$name.tar.gz" "$name"
chmod 600 "$out/$name.tar.gz"
log "đã xuất $out/$name.tar.gz"

# Giữ KEEP bản mới nhất (tên có giờ UTC nên sắp theo tên là sắp theo thời gian).
prune_local() {
  local all n
  all=$(find "$out" -maxdepth 1 -name 'crm-config-*.tar.gz' -print | LC_ALL=C sort)
  n=$(printf '%s\n' "$all" | grep -c . || true)
  if [ "$n" -gt "$KEEP" ]; then
    printf '%s\n' "$all" | head -n $((n - KEEP)) | while IFS= read -r f; do rm -f "$f"; done
  fi
}
prune_local

if [ -n "$s3uri" ]; then
  ep=()
  [ -z "${CRM_S3_ENDPOINT:-}" ] || ep=(--endpoint-url "$CRM_S3_ENDPOINT")
  dest="${s3uri%/}/config"
  aws s3 cp "$out/$name.tar.gz" "$dest/$name.tar.gz" ${ep[@]+"${ep[@]}"} >&2 || die "gửi lên kho ngoài thất bại"
  remote=$({
    aws s3 ls "$dest/" ${ep[@]+"${ep[@]}"} | awk '{print $NF}' | grep -E '^crm-config-.*\.tar\.gz$' || true
    echo "$name.tar.gz"
  } | LC_ALL=C sort -u)
  n=$(printf '%s\n' "$remote" | grep -c . || true)
  if [ "$n" -gt "$KEEP" ]; then
    printf '%s\n' "$remote" | head -n $((n - KEEP)) | while IFS= read -r f; do
      aws s3 rm "$dest/$f" ${ep[@]+"${ep[@]}"} >&2 || die "xóa bản cũ $f trên kho ngoài thất bại"
    done
  fi
  log "đã gửi $dest/$name.tar.gz, giữ $KEEP bản"
fi
