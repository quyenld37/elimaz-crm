#!/usr/bin/env bash
# Phép thử ngắn số 2 (ADR-37, ADR-42): sao lưu WAL-G có mã hóa, rồi khôi phục vào một container mới rỗng, có bấm giờ.
# Kho S3 là SeaweedFS cục bộ, chỉ thay cho kho ngoài thật. Khóa sinh lúc chạy (drill-lib.sh).
# In "RESTORE DRILL OK rpo_seconds=<x> restore_seconds=<y> encrypted=yes ..." và thoát 0 khi đạt.
set -euo pipefail
. "$(dirname "$0")/drill-lib.sh"

MARKER=PLAINTEXT-MARKER-7f3a
RPO_LIMIT=300
work=$(mktemp -d "${TMPDIR:-/tmp}/crm-drill.XXXXXX")
cleanup() {
  drill_down
  if [ "${CRM_DRILL_KEEP:-0}" = 1 ]; then log "giữ thư mục tạm $work"; else rm -rf "$work"; fi
}
trap cleanup EXIT

drill_gen_secrets
drill_up

# crm.conf đã được nạp qua include_dir; WAL-G nói chuyện được với kho (bucket còn rỗng).
[ "$(drill_psql -c "SELECT current_setting('archive_mode') || ' ' || current_setting('archive_timeout') || ' ' || current_setting('shared_preload_libraries')")" = "on 1min pg_stat_statements" ] ||
  die "crm.conf chưa được nạp (archive_mode, archive_timeout, shared_preload_libraries)"
dc exec -T -u postgres postgres wal-g backup-list >&2 || die "wal-g backup-list lỗi trên bucket rỗng"

# Tải mọi object dưới một tiền tố của bucket về thư mục <đích>. s3_download <tiền tố hoặc ""> <đích>
s3_download() {
  mkdir -p "$2"
  dc run --rm -T -v "$2:/out" rclone copy "s3:$DRILL_BUCKET/$1" /out >&2
}

# (1) Bảng drill, 100 dòng mang chuỗi đánh dấu. Bảng thay thế platform.backup_runs đúng hình 0010_ops.sql của kế hoạch,
# để kiểm base-backup.sh ghi kết quả khi bảng có.
drill_psql <<SQL >/dev/null
CREATE TABLE drill(id bigserial PRIMARY KEY, at timestamptz NOT NULL DEFAULT clock_timestamp(), marker text NOT NULL);
INSERT INTO drill(marker) SELECT '$MARKER' FROM generate_series(1, 100);
CREATE SCHEMA platform;
CREATE TABLE platform.backup_runs(
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  kind text NOT NULL CHECK (kind IN ('base', 'restore_drill', 'restore_verify')),
  ok boolean NOT NULL,
  finished_at timestamptz NOT NULL DEFAULT now(),
  duration_seconds int,
  detail text);
SQL

# (2) base-backup.sh. Trước hết thử đường lỗi: khóa S3 sai phải thoát khác 0 và ghi ok = false.
if dc exec -T -u postgres -e AWS_SECRET_ACCESS_KEY=wrong-key-for-drill -e WALG_UPLOAD_CONCURRENCY=1 \
  postgres /opt/crm-backup/base-backup.sh >"$work/bad-backup.log" 2>&1; then
  die "base-backup.sh với khóa S3 sai lại thoát 0"
fi
[ "$(drill_psql -c "SELECT count(*) FROM platform.backup_runs WHERE kind = 'base' AND NOT ok")" = 1 ] ||
  die "base-backup.sh lỗi nhưng không ghi backup_runs ok = false"

dc exec -T -u postgres postgres /opt/crm-backup/base-backup.sh >&2 || die "base-backup.sh thất bại"
[ "$(drill_psql -c "SELECT count(*) FROM platform.backup_runs WHERE kind = 'base' AND ok")" = 1 ] ||
  die "base-backup.sh xong nhưng không ghi backup_runs ok = true"
dc exec -T -u postgres postgres wal-g backup-list >&2 || die "wal-g backup-list lỗi"

# (3) Thêm 50 dòng; đo lúc WAL chứa chúng đã lên kho (RPO thật); chờ tổng cộng ít nhất 70 s như kế hoạch.
drill_psql -c "INSERT INTO drill(marker) SELECT '$MARKER' FROM generate_series(1, 50)" >/dev/null
read -r t_write walfile <<EOF
$(drill_psql -F ' ' -c "SELECT extract(epoch FROM clock_timestamp())::bigint, pg_walfile_name(pg_current_wal_lsn())")
EOF
t_archived=""
i=0
while [ "$i" -lt "$RPO_LIMIT" ]; do
  last=$(drill_psql -c "SELECT coalesce(last_archived_wal, '') FROM pg_stat_archiver")
  # Cùng timeline nên so chuỗi tên tệp WAL là so thứ tự; WAL được lưu theo thứ tự.
  if [ -n "$last" ] && [ ! "$last" \< "$walfile" ]; then
    t_archived=$(drill_psql -c "SELECT extract(epoch FROM last_archived_time)::bigint FROM pg_stat_archiver")
    break
  fi
  sleep 1
  i=$((i + 1))
done
[ -n "$t_archived" ] || die "WAL $walfile chưa lên kho sau ${RPO_LIMIT}s"
rpo_seconds=$((t_archived - t_write))
[ "$rpo_seconds" -le "$RPO_LIMIT" ] || die "rpo_seconds=$rpo_seconds vượt $RPO_LIMIT"
archive_failed=$(drill_psql -c "SELECT failed_count FROM pg_stat_archiver")
waited=$(($(now_s) - t_write))
[ "$waited" -ge 70 ] || sleep $((70 - waited))
db_bytes=$(drill_psql -c "SELECT pg_database_size('crm')")
cluster_bytes=$(drill_psql -c "SELECT sum(pg_database_size(oid)) FROM pg_database")

# (4) Mọi object của bucket: không object nào được chứa chuỗi đánh dấu ở dạng đọc được.
s3_download "" "$work/all"
objects=$(find "$work/all" -type f | wc -l | tr -d ' ')
[ "$objects" -gt 0 ] || die "bucket rỗng, không có gì để kiểm"
bucket_bytes=$(du -sk "$work/all" | cut -f1)
# grep: 0 = thấy (hỏng), 1 = không thấy (đạt), 2 = lỗi đọc tệp (không kết luận được, không được coi là "không thấy").
grep_rc=0
grep -r -a -l "$MARKER" "$work/all" >"$work/plain-hits.txt" || grep_rc=$?
case $grep_rc in
0) die "tìm thấy $MARKER trong bản sao lưu: $(head -3 "$work/plain-hits.txt")" ;;
1) ;;
*) die "grep lỗi (mã $grep_rc) khi đọc bản sao lưu tải về: không kết luận được về mã hóa" ;;
esac
# Đối chứng: cùng cách tìm phải thấy chuỗi đánh dấu trong một bản sao lưu KHÔNG mã hóa (để tiền tố riêng, xóa ngay sau).
dc exec -T -u postgres postgres bash -c \
  'unset WALG_LIBSODIUM_KEY WALG_LIBSODIUM_KEY_TRANSFORM; WALG_S3_PREFIX="s3://'"$DRILL_BUCKET"'/control-plain" wal-g backup-push "$PGDATA"' \
  >"$work/control.log" 2>&1 || die "không tạo được bản đối chứng không mã hóa (xem $work/control.log)"
s3_download control-plain "$work/control"
grep -r -a -q "$MARKER" "$work/control" || die "đối chứng hỏng: bản không mã hóa cũng không tìm thấy chuỗi đánh dấu, phép tìm không có giá trị"
dc run --rm -T rclone purge "s3:$DRILL_BUCKET/control-plain" >&2

# (5) Máy cũ mất. BẤM GIỜ: container mới rỗng, backup-fetch LATEST, phát lại WAL tới hết, nhận ghi.
dc stop postgres >&2
t0=$(now_s)
dc --profile restore up -d --no-deps pg-restore >&2
i=0
state=""
while [ "$i" -lt 3600 ]; do
  state=$(dc exec -T pg-restore psql -X -At -U postgres -d crm -c 'SELECT pg_is_in_recovery()' 2>/dev/null || true)
  [ "$state" = f ] && break
  if [ -z "$(dc ps -q pg-restore)" ]; then
    dc logs --tail 80 pg-restore >&2 || true
    die "container khôi phục đã dừng"
  fi
  sleep 1
  i=$((i + 1))
done
[ "$state" = f ] || die "khôi phục chưa xong sau 3600s"
t1=$(now_s)
restore_seconds=$((t1 - t0))

# (6) Đủ 150 dòng.
got=$(dc exec -T pg-restore psql -X -At -U postgres -d crm -c 'SELECT count(*) FROM drill')
[ "$got" = 150 ] || die "khôi phục được $got dòng, cần 150"

# (7) Không có khóa, hoặc khóa sai: tải bản sao lưu phải thất bại. Cùng lệnh với đúng khóa phải chạy được (đối chứng).
# "Thất bại" = lệnh không thoát 0 trong 120 s VÀ thư mục đích không có global/pg_control đọc được bằng pg_controldata.
# Có giới hạn thời gian vì WAL-G có thể thử lại mãi thay vì thoát (ghi ở docs/do-luong/phep-thu-ngan.md).
fetch_cmd='if [ "${DRILL_NOKEY:-}" = 1 ]; then unset WALG_LIBSODIUM_KEY WALG_LIBSODIUM_KEY_TRANSFORM; fi;
d=$(mktemp -d); rc=0; timeout -k 5 120 wal-g backup-fetch "$d" LATEST || rc=$?; echo "fetch_rc=$rc";
if [ "$rc" = 0 ] && pg_controldata "$d" >/dev/null 2>&1; then echo FETCH_USABLE; exit 0; fi; exit 1'
fetch_try() { # fetch_try <tên> [tham số thêm cho docker compose run]
  local name=$1
  shift
  dc run --rm -T --no-deps "$@" --entrypoint bash pg-restore -c "$fetch_cmd" >"$work/fetch-$name.log" 2>&1
}
fetch_try key || die "đối chứng hỏng: tải bản sao lưu với đúng khóa cũng thất bại (xem $work/fetch-key.log)"
if fetch_try nokey -e DRILL_NOKEY=1; then
  die "tải bản sao lưu KHÔNG có khóa lại thành công: bản sao lưu không được mã hóa"
fi
if fetch_try wrongkey -e WALG_LIBSODIUM_KEY="$(openssl rand -hex 32)"; then
  die "tải bản sao lưu với khóa SAI lại thành công"
fi
nokey_rc=$(sed -n 's/^fetch_rc=//p' "$work/fetch-nokey.log")
wrongkey_rc=$(sed -n 's/^fetch_rc=//p' "$work/fetch-wrongkey.log")
log "không khóa: $(grep -v '^fetch_rc=' "$work/fetch-nokey.log" | grep -i -m1 -E 'error|crypt|decrypt' | cut -c1-200)"
log "khóa sai: $(grep -v '^fetch_rc=' "$work/fetch-wrongkey.log" | grep -i -m1 -E 'error|crypt|decrypt' | cut -c1-200)"

echo "RESTORE DRILL OK rpo_seconds=$rpo_seconds restore_seconds=$restore_seconds encrypted=yes" \
  "nokey_rc=$nokey_rc wrongkey_rc=$wrongkey_rc rows=$got db_bytes=$db_bytes cluster_bytes=$cluster_bytes bucket_kib=$bucket_bytes objects=$objects archive_failed=$archive_failed"
