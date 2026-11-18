#!/usr/bin/env bash
# Kiểm bản sao lưu hằng tuần (ADR-37 điều 3; kế hoạch M0 bản 5, Task 21). Lịch: chủ nhật 03:00 giờ Việt Nam (RUNBOOK).
# Chạy trong container PostgreSQL bằng người dùng postgres, cùng biến WAL-G với base-backup.sh.
#   1. Khôi phục bản mới nhất (wal-g backup-fetch LATEST) vào một cụm PostgreSQL tạm: thư mục tạm trên cùng ổ dữ liệu,
#      chỉ nghe socket riêng, không đẩy WAL (archive_mode=off), dừng ở điểm cuối của bản sao lưu (recovery_target=immediate).
#   2. Ba câu đối chiếu: số dòng platform.tenants, identity.users, identity.audit_logs trong cụm tạm so với database
#      thật; lệch cho phép = số dòng của database thật có created_at sau lúc bắt đầu bản sao lưu (START TIME của
#      backup_label). Mốc bắt đầu sớm hơn điểm cuối nên dung sai chỉ rộng hơn, không bao giờ báo hỏng sai.
#   3. Ghi platform.backup_runs(kind = 'restore_verify', ok, duration_seconds); dừng và xóa cụm tạm.
# Mọi bước có hạn, quá hạn là ok = false: WAL-G treo mãi khi thiếu hoặc sai khóa (Task 2, docs/do-luong/phep-thu-ngan.md).
#   CRM_VERIFY_FETCH_TIMEOUT  hạn của backup-fetch, giây (mặc định 7200)
#   CRM_VERIFY_READY_TIMEOUT  hạn phát lại WAL tới khi cụm tạm nhận ghi, giây (mặc định 3600)
#   CRM_VERIFY_DIR            thư mục cụm tạm (mặc định /var/lib/postgresql/verify; không được trùng hay chứa PGDATA)
#   CRM_VERIFY_PORT           cổng (chỉ để đặt tên socket) của cụm tạm (mặc định 5499)
#   CRM_VERIFY_MARGIN_BYTES   chỗ trống phải còn ngoài cỡ database thật trước khi tải (mặc định 1 GiB); thiếu thì hỏng
#                             ngay, không tải (cụm tạm cùng ổ với cụm thật; đầy ổ làm PostgreSQL thật dừng ghi)
# In "WEEKLY VERIFY OK ..." và thoát 0 khi đạt; thoát khác 0 khi hỏng.
set -uo pipefail
. "$(dirname "$0")/runs-lib.sh"

fetch_timeout=${CRM_VERIFY_FETCH_TIMEOUT:-7200}
ready_timeout=${CRM_VERIFY_READY_TIMEOUT:-3600}
dir=${CRM_VERIFY_DIR:-/var/lib/postgresql/verify}
port=${CRM_VERIFY_PORT:-5499}
live_db=${CRM_BACKUP_RUNS_DB:-crm}
margin=${CRM_VERIFY_MARGIN_BYTES:-1073741824}
data="$dir/data"
sock="$dir/sock"
started=0
# owned=1 khi đã giữ khóa và đã kiểm $dir không chứa cụm thật: chỉ khi đó cleanup mới được xóa $dir.
owned=0

stop_temp() {
  if [ "$started" = 1 ]; then
    pg_ctl -D "$data" -m immediate -w -t 60 stop >/dev/null 2>&1 || true
    started=0
  fi
}
cleanup() {
  [ "$owned" = 1 ] || return 0
  stop_temp
  rm -rf "$dir"
}

fail() {
  echo "weekly-verify: $1" >&2
  # Đuôi log của WAL-G và postgres tạm ra stderr (cho người chạy tay hoặc thư của cron), không vào backup_runs.
  for f in "$dir/fetch.log" "$dir/postgres.log"; do
    [ -f "$f" ] && tail -n 15 "$f" | sed "s|^|  ${f##*/}: |" >&2
  done
  cleanup
  runs_record restore_verify false "$1" || echo "weekly-verify: không ghi được backup_runs" >&2
  exit 1
}

# $dir bị xóa cả cây: không được là /, PGDATA hay thư mục chứa PGDATA.
[ -n "${PGDATA:-}" ] || fail "thiếu PGDATA"
case "${dir%/}" in
"" | / | "${PGDATA%/}" | "${PGDATA%/}"/*) fail "CRM_VERIFY_DIR không hợp lệ: trùng hoặc nằm trong dữ liệu thật" ;;
esac
case "${PGDATA:-/khong-co}/" in
"${dir%/}"/*) fail "CRM_VERIFY_DIR không hợp lệ: chứa dữ liệu thật" ;;
esac

# Một lượt mỗi lúc: hai lượt cùng thư mục sẽ xóa cụm của nhau. Khóa giữ tới khi tiến trình thoát.
exec 9>"$dir.lock" || fail "không mở được tệp khóa"
flock -n 9 || {
  echo "weekly-verify: đang có lượt khác" >&2
  runs_record restore_verify false "đang có lượt khác" || true
  exit 1
}
trap cleanup EXIT

owned=1

[ -n "${WALG_LIBSODIUM_KEY:-}" ] || fail "thiếu WALG_LIBSODIUM_KEY"
# Không dừng postgres tạm sót lại theo postmaster.pid: số PID trong tệp cũ có thể đã quay vòng thành một tiến trình
# của cụm thật (pg_ctl không kiểm), gửi tín hiệu vào đó làm cụm thật khởi động lại. Chỉ xóa thư mục: postmaster còn
# sống (nếu có) tự tắt khi thấy postmaster.pid của nó mất (PostgreSQL kiểm mỗi phút).
rm -rf "$dir"

# Cụm tạm nằm cùng ổ với cụm thật: tải bản sao lưu vào ổ không đủ chỗ sẽ làm đầy ổ và PostgreSQL thật dừng ghi.
# Cần chỗ trống ≥ cỡ mọi database của cụm thật + khoảng dự phòng (CRM_VERIFY_MARGIN_BYTES, mặc định 1 GiB).
need=$(psql -X -At -v ON_ERROR_STOP=1 -d "$live_db" -c "SELECT sum(pg_database_size(oid))::bigint FROM pg_database" 2>/dev/null) ||
  fail "không đọc được cỡ database thật"
avail_kib=$(df -Pk "$(dirname "$dir")" 2>/dev/null | awk 'NR==2 {print $4}')
[ -n "$need" ] && [ -n "$avail_kib" ] || fail "không đọc được chỗ trống của ổ"
if [ $((avail_kib * 1024)) -lt $((need + margin)) ]; then
  fail "không đủ chỗ trống cho cụm tạm: trống $((avail_kib / 1024)) MiB, cần $(((need + margin) / 1048576)) MiB"
fi

mkdir -p "$data" "$sock" && chmod 700 "$data" || fail "không tạo được thư mục cụm tạm"

# (1) Tải bản mới nhất. 124/137: quá hạn (WAL-G treo khi khóa sai hay object hỏng).
rc=0
timeout -k 10 "$fetch_timeout" wal-g backup-fetch "$data" LATEST >"$dir/fetch.log" 2>&1 || rc=$?
case $rc in
0) ;;
124 | 137) fail "quá hạn ${fetch_timeout}s ở bước backup-fetch" ;;
*) fail "lỗi ở bước backup-fetch" ;;
esac
start_time=$(sed -n 's/^START TIME: //p' "$data/backup_label" 2>/dev/null)
# Dạng "2026-11-18 06:53:01 UTC"; kiểm dạng trước khi đưa vào câu SQL.
printf '%s' "$start_time" | grep -Eqx '[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} [A-Z]+' ||
  fail "không đọc được START TIME của backup_label"
[ -f "$data/global/pg_control" ] || fail "bản tải về thiếu global/pg_control"

touch "$data/recovery.signal"
# Cụm tạm: chỉ socket riêng, không đẩy WAL lên kho, ít bộ nhớ; phát lại WAL tới điểm nhất quán đầu tiên rồi nhận ghi.
opts="-c port=$port -c listen_addresses='' -c unix_socket_directories=$sock -c archive_mode=off"
opts="$opts -c restore_command='timeout -k 10 600 wal-g wal-fetch %f %p' -c recovery_target=immediate"
# hot_standby=off: chỉ nhận kết nối khi đã phát lại xong và nhận ghi (pg_ctl -w chờ tới lúc đó); hot standby còn đòi
# max_connections… không nhỏ hơn máy gốc.
opts="$opts -c recovery_target_action=promote -c hot_standby=off -c shared_buffers=128MB"
started=1
pg_ctl -D "$data" -l "$dir/postgres.log" -o "$opts" -w -t "$ready_timeout" start >/dev/null 2>&1 ||
  fail "cụm tạm không khởi động trong ${ready_timeout}s"
tpsql() { psql -X -At -v ON_ERROR_STOP=1 -h "$sock" -p "$port" -d "$live_db" "$@"; }
i=0
until [ "$(tpsql -c 'SELECT pg_is_in_recovery()' 2>/dev/null)" = f ]; do
  [ "$i" -lt "$ready_timeout" ] || fail "cụm tạm chưa phát lại xong sau ${ready_timeout}s"
  sleep 1
  i=$((i + 1))
done

# (2) Đối chiếu.
summary=""
bad=""
for tbl in platform.tenants identity.users identity.audit_logs; do
  restored=$(tpsql -c "SELECT count(*) FROM $tbl" 2>/dev/null) || fail "cụm tạm không đếm được $tbl"
  read -r live after <<EOF
$(psql -X -At -F ' ' -v ON_ERROR_STOP=1 -d "$live_db" \
    -c "SELECT count(*), count(*) FILTER (WHERE created_at > '$start_time'::timestamptz) FROM $tbl" 2>/dev/null)
EOF
  [ -n "${live:-}" ] && [ -n "${after:-}" ] || fail "database thật không đếm được $tbl"
  diff=$((live - restored))
  [ "$diff" -lt 0 ] && diff=$((-diff))
  summary="$summary ${tbl#*.}=$restored/$live(+$after)"
  [ "$diff" -le "$after" ] || bad="$bad ${tbl#*.}"
done
stop_temp
[ -z "$bad" ] || fail "lệch số dòng quá dung sai:$bad;$summary"

cleanup
runs_record restore_verify true "ok;$summary" || {
  echo "weekly-verify: kiểm đạt nhưng không ghi được backup_runs" >&2
  exit 1
}
echo "WEEKLY VERIFY OK backup_start='$start_time' duration_seconds=$(($(date +%s) - runs_t0))$summary"
