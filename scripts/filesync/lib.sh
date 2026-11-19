#!/bin/sh
# Phần chung của run.sh, reconcile.sh, ls-offsite.sh (container filesync, Task M1-45; ADR-36). Chỉ `source`, không chạy.
#
# Biến môi trường (compose đặt; không biến nào là khóa của crm-app):
#   FILESYNC_DATABASE_URL            role crm_filesync, chỉ INSERT vào platform.file_sync_runs
#   FILESYNC_SRC_ENDPOINT, FILESYNC_SRC_ACCESS_KEY_ID, FILESYNC_SRC_SECRET_ACCESS_KEY, FILESYNC_SRC_REGION (us-east-1),
#   FILESYNC_SRC_BUCKET_PREFIX (crm-)
#                                    kho chính, danh tính crm-filesync (chỉ đọc); tên bucket kho chính = tiền tố +
#                                    public|originals|private, cùng giá trị với S3_BUCKET_PREFIX của ứng dụng
#   FILESYNC_DST_ENDPOINT, FILESYNC_DST_ACCESS_KEY_ID, FILESYNC_DST_SECRET_ACCESS_KEY, FILESYNC_DST_REGION (us-east-1),
#   FILESYNC_DST_PROVIDER (Other), FILESYNC_DST_BUCKET_PREFIX (crm-)
#                                    kho ngoài, danh tính chỉ ghi thêm; tên bucket kho ngoài = tiền tố + public|originals|private
#   RCLONE_CRYPT_PASSWORD, RCLONE_CRYPT_PASSWORD2
#                                    hai mật khẩu rclone crypt (chủ shop giữ; mất là không đọc lại được bản sao riêng tư)
#   FILESYNC_HEARTBEAT_URL           nhịp sống ra ngoài sau lượt chép tốt (rỗng: bỏ)
#   FILESYNC_STATE_DIR (/state)      mốc lần chép tốt cuối của từng bucket, ngày đối chiếu cuối
#
# Luật cứng: chỉ `rclone copy` (và `size`, `lsf` để đọc). Không bao giờ sync, move, delete, purge, --delete-*.

set -eu

# Bucket logic (như files.Bucket của ứng dụng); file_sync_runs.bucket ghi đúng tên này, luật cảnh báo không cần biết
# tiền tố (Task 410, review độc lập M1 C-06). tmp không chép.
FILESYNC_BUCKETS="public originals private"
FILESYNC_DIR=$(dirname "$0")
: "${FILESYNC_STATE_DIR:=/state}"
: "${FILESYNC_SRC_REGION:=us-east-1}"
: "${FILESYNC_SRC_BUCKET_PREFIX:=crm-}"
: "${FILESYNC_DST_REGION:=us-east-1}"
: "${FILESYNC_DST_PROVIDER:=Other}"
: "${FILESYNC_DST_BUCKET_PREFIX:=crm-}"
: "${FILESYNC_HEARTBEAT_URL:=}"
export FILESYNC_SRC_REGION FILESYNC_DST_REGION FILESYNC_DST_PROVIDER FILESYNC_DST_BUCKET_PREFIX

log() { printf '%s filesync: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }

# fs_init kiểm biến, điền cấu hình rclone vào /tmp (tmpfs, 600), rồi xóa hai mật khẩu crypt khỏi môi trường: rclone
# đọc RCLONE_CRYPT_PASSWORD* như giá trị mặc định của mọi remote crypt (đòi dạng obscure), để lại sẽ lẫn cấu hình.
fs_init() {
  for v in FILESYNC_DATABASE_URL FILESYNC_SRC_ENDPOINT FILESYNC_SRC_ACCESS_KEY_ID FILESYNC_SRC_SECRET_ACCESS_KEY \
    FILESYNC_DST_ENDPOINT FILESYNC_DST_ACCESS_KEY_ID FILESYNC_DST_SECRET_ACCESS_KEY RCLONE_CRYPT_PASSWORD RCLONE_CRYPT_PASSWORD2; do
    eval "val=\${$v:-}"
    if [ -z "$val" ]; then
      log "thiếu biến $v"
      exit 2
    fi
  done
  if [ "${#RCLONE_CRYPT_PASSWORD}" -lt 32 ] || [ "${#RCLONE_CRYPT_PASSWORD2}" -lt 32 ]; then
    log "RCLONE_CRYPT_PASSWORD, RCLONE_CRYPT_PASSWORD2 phải dài ít nhất 32 ký tự"
    exit 2
  fi
  FILESYNC_CRYPT_PASSWORD_OBSCURED=$(printf '%s' "$RCLONE_CRYPT_PASSWORD" | rclone obscure -)
  FILESYNC_CRYPT_PASSWORD2_OBSCURED=$(printf '%s' "$RCLONE_CRYPT_PASSWORD2" | rclone obscure -)
  unset RCLONE_CRYPT_PASSWORD RCLONE_CRYPT_PASSWORD2
  export FILESYNC_CRYPT_PASSWORD_OBSCURED FILESYNC_CRYPT_PASSWORD2_OBSCURED FILESYNC_SRC_ENDPOINT \
    FILESYNC_SRC_ACCESS_KEY_ID FILESYNC_SRC_SECRET_ACCESS_KEY FILESYNC_DST_ENDPOINT FILESYNC_DST_ACCESS_KEY_ID \
    FILESYNC_DST_SECRET_ACCESS_KEY
  RCLONE_CONFIG=$(mktemp /tmp/filesync-rclone.XXXXXX)
  export RCLONE_CONFIG
  (umask 077 && awk '{
      out = ""; s = $0
      while (match(s, /\$\{[A-Z0-9_]+\}/)) {
        out = out substr(s, 1, RSTART - 1) ENVIRON[substr(s, RSTART + 2, RLENGTH - 3)]
        s = substr(s, RSTART + RLENGTH)
      }
      print out s
    }' "$FILESYNC_DIR/rclone.conf.tmpl" >"$RCLONE_CONFIG")
  unset FILESYNC_CRYPT_PASSWORD_OBSCURED FILESYNC_CRYPT_PASSWORD2_OBSCURED FILESYNC_SRC_SECRET_ACCESS_KEY \
    FILESYNC_DST_SECRET_ACCESS_KEY
  mkdir -p "$FILESYNC_STATE_DIR"
}

# src_of BUCKET / dst_of BUCKET: remote rclone hai phía của bucket logic BUCKET. public không mã hóa (ảnh công khai,
# đã ở CDN); originals, private qua crypt (rclone.conf.tmpl). Bucket lạ: lỗi, không chép nhầm chỗ.
src_of() {
  case "$1" in
    public | originals | private) printf 'main:%s%s' "$FILESYNC_SRC_BUCKET_PREFIX" "$1" ;;
    *) log "bucket không biết: $1"; return 2 ;;
  esac
}
dst_of() {
  case "$1" in
    public) printf 'offsite:%spublic' "$FILESYNC_DST_BUCKET_PREFIX" ;;
    originals) printf 'offsite-originals:' ;;
    private) printf 'offsite-private:' ;;
    *) log "bucket không biết: $1"; return 2 ;;
  esac
}

# Tham số chung của mọi lệnh rclone: hạn mạng để lượt treo không giữ mãi (lượt treo = không có dòng tốt = cảnh báo).
rclone_common() { printf '%s\n' --timeout=5m --contimeout=30s --retries=3 --low-level-retries=10; }

# record KIND BUCKET OK STARTED FINISHED OBJECTS BYTES DETAIL: một dòng file_sync_runs (mốc là giây epoch). Giá trị đi
# qua biến psql (:'x'), không ghép chuỗi SQL. Lỗi ghi: báo ra log, trả 1 (không có dòng tốt thì cảnh báo tự lo).
record() {
  if PGCONNECT_TIMEOUT=10 PGOPTIONS='-c statement_timeout=10000' PGAPPNAME=crm-filesync \
    psql "$FILESYNC_DATABASE_URL" -X -q -v ON_ERROR_STOP=1 \
    -v kind="$1" -v bucket="$2" -v ok="$3" -v started="$4" -v finished="$5" -v objects="$6" -v bytes="$7" \
    -v detail="$8" >/dev/null <<'SQL'
INSERT INTO platform.file_sync_runs (kind, bucket, ok, started_at, finished_at, objects, bytes, detail)
VALUES (:'kind', :'bucket', :'ok'::boolean, to_timestamp(:'started'::float8), to_timestamp(:'finished'::float8),
        nullif(:'objects', '')::bigint, nullif(:'bytes', '')::bigint, left(nullif(:'detail', ''), 1000));
SQL
  then
    return 0
  fi
  log "không ghi được dòng $1 của $2 vào file_sync_runs"
  return 1
}

# is_count S: S là số nguyên không âm (chặn lỗi số học của sh, lỗi đó dừng cả vòng lặp).
is_count() {
  case "$1" in
    '' | *[!0-9]*) return 1 ;;
    *) return 0 ;;
  esac
}

# copy_bucket BUCKET: một lượt `rclone copy --immutable --checksum` sang kho ngoài, một dòng `copy`. Chỉ thêm: tệp đã
# có ở kho ngoài mà khác nội dung (kho chính bị sửa, kho ngoài bị ghi đè) là lỗi (ok = false), không ghi đè. Bucket
# mã hóa: crypt không có tổng kiểm chung với kho chính nên rclone so theo kích thước. Trả 0 khi ok và đã ghi dòng.
copy_bucket() {
  b=$1
  started=$(date +%s)
  out=$(mktemp /tmp/filesync-copy.XXXXXX)
  set +e
  # shellcheck disable=SC2046 # rclone_common in mỗi tham số một dòng, không có khoảng trắng
  rclone copy "$(src_of "$b")" "$(dst_of "$b")" --immutable --checksum --stats-one-line --use-json-log -v \
    --stats=0 $(rclone_common) 2>"$out"
  rc=$?
  set -e
  finished=$(date +%s)
  # Log JSON của rclone: mỗi tệp chép một dòng "Copied (new)" có size; mỗi lỗi một dòng level error (lỗi của một tệp
  # có .object; rclone thêm dòng lỗi tổng kết không có .object).
  summary=$(jq -R -s -r '[split("\n")[] | fromjson? | select(type == "object")] as $l
    | ($l | map(select((.msg // "") | startswith("Copied")))) as $c
    | ($l | map(select(.level == "error"))) as $e
    | [($c | length), ($c | map(.size // 0) | add // 0), ($e | length), ($e | map(select(.object)) | length),
       (($e[0] // {}) | ((.object // "") + (if .object then ": " else "" end) + (.msg // "")))] | @tsv' "$out")
  rm -f "$out"
  objects=$(printf '%s' "$summary" | cut -f1)
  bytes=$(printf '%s' "$summary" | cut -f2)
  errors=$(printf '%s' "$summary" | cut -f3)
  failed_files=$(printf '%s' "$summary" | cut -f4)
  first=$(printf '%s' "$summary" | cut -f5-)
  if ! is_count "$objects" || ! is_count "$bytes" || ! is_count "$errors" || ! is_count "$failed_files"; then
    ok=false
    objects='' bytes=''
    detail="không đọc được log của rclone (rclone thoát $rc)"
  elif [ "$rc" -eq 0 ] && [ "$errors" -eq 0 ]; then
    ok=true
    detail="chép $objects tệp, $bytes byte"
  else
    ok=false
    detail="$failed_files tệp lỗi (rclone thoát $rc), chép $objects tệp; lỗi đầu: $first"
  fi
  log "copy $b: $detail"
  record copy "$b" "$ok" "$started" "$finished" "$objects" "$bytes" "$detail" || return 1
  [ "$ok" = true ] || return 1
  # Mốc BẮT ĐẦU của lượt tốt: tệp tải lên sau mốc này có thể chưa được chép (đối chiếu trừ chúng ra).
  printf '%s\n' "$started" >"$FILESYNC_STATE_DIR/last-ok-$b"
}

# copy_all: chép ba bucket (một bucket lỗi không chặn bucket khác); cả ba tốt thì gửi nhịp sống ra ngoài. Trả 0 khi cả
# ba tốt.
copy_all() {
  all=0
  for b in $FILESYNC_BUCKETS; do
    copy_bucket "$b" || all=1
  done
  if [ "$all" -eq 0 ] && [ -n "$FILESYNC_HEARTBEAT_URL" ]; then
    curl -fsS -m 10 -o /dev/null "$FILESYNC_HEARTBEAT_URL" || log "gửi nhịp sống ra ngoài lỗi"
  fi
  return "$all"
}

# size_of REMOTE [tham số thêm]: "<số tệp> <số byte>" theo `rclone size --json`.
size_of() {
  r=$1
  shift
  # shellcheck disable=SC2046
  rclone size --json $(rclone_common) "$@" "$r" 2>/dev/null | jq -r '"\(.count) \(.bytes)"'
}

# reconcile_bucket BUCKET: đếm hai phía, một dòng `reconcile`. ok khi số tệp kho ngoài >= số tệp kho chính trừ số tệp
# kho chính mới hơn mốc bắt đầu lượt chép tốt cuối (chưa có mốc: không trừ). Kho ngoài nhiều hơn là bình thường (chỉ
# thêm, tệp đã xóa ở kho chính vẫn giữ). objects, bytes là của kho ngoài; detail có cả hai phía.
reconcile_bucket() {
  b=$1
  started=$(date +%s)
  ok=false
  src='' dst='' newer=0 detail=''
  if ! src=$(size_of "$(src_of "$b")") || ! is_count "${src%% *}" || ! is_count "${src#* }"; then
    src='' detail="không đếm được kho chính"
  elif ! dst=$(size_of "$(dst_of "$b")") || ! is_count "${dst%% *}" || ! is_count "${dst#* }"; then
    dst='' detail="không đếm được kho ngoài"
  else
    if [ -f "$FILESYNC_STATE_DIR/last-ok-$b" ]; then
      since=$(cat "$FILESYNC_STATE_DIR/last-ok-$b")
      if is_count "$since"; then
        age=$((started - since))
        [ "$age" -gt 0 ] || age=1
        newer=$(size_of "$(src_of "$b")" --max-age "${age}s" | cut -d' ' -f1) || newer=0
        is_count "$newer" || newer=0
      fi
    fi
    src_n=${src%% *} dst_n=${dst%% *}
    if [ "$dst_n" -ge $((src_n - newer)) ]; then ok=true; fi
    detail="kho chính $src_n tệp ${src#* } byte; kho ngoài $dst_n tệp ${dst#* } byte; mới sau lần chép tốt cuối $newer"
  fi
  finished=$(date +%s)
  log "reconcile $b: ok=$ok; $detail"
  record reconcile "$b" "$ok" "$started" "$finished" "${dst%% *}" "${dst#* }" "$detail" || return 1
  [ "$ok" = true ]
}

# reconcile_all: đối chiếu ba bucket, ghi ngày (giờ Việt Nam) đã đối chiếu. Trả 0 khi cả ba khớp.
reconcile_all() {
  all=0
  for b in $FILESYNC_BUCKETS; do
    reconcile_bucket "$b" || all=1
  done
  TZ=Asia/Ho_Chi_Minh date +%Y-%m-%d >"$FILESYNC_STATE_DIR/last-reconcile"
  return "$all"
}
