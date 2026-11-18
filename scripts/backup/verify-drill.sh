#!/usr/bin/env bash
# Diễn tập sao lưu theo lịch và kiểm bản sao lưu hằng tuần (kế hoạch M0 bản 5, Task 21, Step 2) trên compose diễn tập
# (dự án crm-t02, SeaweedFS thay kho ngoài). Database có schema thật: migrate và seed-dev bằng image crm:dev.
#   1. base-backup.sh thiếu WALG_LIBSODIUM_KEY → thoát khác 0, backup_runs base ok = false.
#   2. base-backup.sh → base ok = true kèm duration_seconds.
#   3. Ghi thêm dòng sau bản sao lưu; weekly-verify.sh → restore_verify ok = true (lệch nằm trong dung sai), cụm tạm dọn.
#   4. Một dòng "cũ" chỉ có ở database thật (ngày tạo trước bản sao lưu) → lệch ngoài dung sai → ok = false, thoát ≠ 0.
#   5. Lượt khác đang giữ khóa → ok = false, thoát ≠ 0.
#   5b. Không đủ chỗ trống → ok = false, không tải. 5c. CRM_VERIFY_DIR chứa PGDATA → từ chối, không xóa gì.
#   6. Khóa sai → WAL-G treo, bị cắt theo hạn → ok = false, thoát ≠ 0 trong hạn.
#   7. Xóa một object của bản sao lưu mới nhất → ok = false, thoát ≠ 0.
# Dùng: make verify-drill  (CRM_DRILL_PG_PORT đổi cổng máy, mặc định 5452; CRM_DRILL_KEEP=1 giữ cụm lại xem).
# In "VERIFY DRILL OK ..." và thoát 0 khi đạt.
set -euo pipefail
. "$(dirname "$0")/drill-lib.sh"

work=$(mktemp -d "${TMPDIR:-/tmp}/crm-verify-drill.XXXXXX")
cleanup() {
  drill_down
  rm -rf "$work"
}
trap cleanup EXIT

drill_gen_secrets
docker build -q -t crm:dev "$DRILL_ROOT" >/dev/null || die "dựng image crm:dev thất bại"
drill_up

# Cụm diễn tập không mật khẩu (socket); image crm nối qua TCP nên đặt mật khẩu sinh lúc chạy cho hai role.
drill_psql -c "ALTER ROLE crm_owner PASSWORD '$CRM_DRILL_PG_PASSWORD'; ALTER ROLE crm_app PASSWORD '$CRM_DRILL_PG_PASSWORD'" >/dev/null
crm() { # crm <tham số…>: tệp chạy crm (vai trò migrate) trong mạng của compose diễn tập
  docker run --rm --network "${DRILL_PROJECT}_default" \
    -e CRM_ROLE=migrate -e CRM_ENV=dev -e OTEL_ENABLED=false \
    -e DATABASE_URL="postgres://crm_app:$CRM_DRILL_PG_PASSWORD@postgres:5432/crm?sslmode=disable" \
    -e DATABASE_OWNER_URL="postgres://crm_owner:$CRM_DRILL_PG_PASSWORD@postgres:5432/crm?sslmode=disable" \
    crm:dev "$@"
}
crm >"$work/migrate.log" 2>&1 || {
  tail -20 "$work/migrate.log" >&2
  die "migrate thất bại"
}
crm seed-dev owner@drill.invalid >"$work/seed.log" 2>&1 || {
  tail -20 "$work/seed.log" >&2
  die "seed-dev thất bại"
}
# Có dòng ở cả ba bảng được đối chiếu (superuser không chịu RLS).
drill_psql >/dev/null <<'SQL'
INSERT INTO identity.audit_logs (tenant_id, actor_type, action, entity_type)
SELECT id, 'system', 'drill.before', 'drill' FROM platform.tenants, generate_series(1, 20);
SQL
counts() { drill_psql -F ' ' -c "SELECT (SELECT count(*) FROM platform.tenants), (SELECT count(*) FROM identity.users), (SELECT count(*) FROM identity.audit_logs)"; }
log "trước sao lưu: tenants users audit_logs = $(counts)"
# Cụm thật không được khởi động lại vì sao lưu hay kiểm (kể cả khi WAL-G bị cắt theo hạn): so lúc postmaster bắt đầu.
pg_started=$(drill_psql -c "SELECT pg_postmaster_start_time()")

runs() { # runs <kind> <ok>: số dòng backup_runs
  drill_psql -c "SELECT count(*) FROM platform.backup_runs WHERE kind = '$1' AND ok = $2"
}
in_pg() { dc exec -T -u postgres "$@"; }

# (1) Thiếu khóa mã hóa.
if in_pg -e WALG_LIBSODIUM_KEY= postgres /opt/crm-backup/base-backup.sh >"$work/nokey.log" 2>&1; then
  die "base-backup.sh thiếu WALG_LIBSODIUM_KEY lại thoát 0"
fi
[ "$(runs base false)" = 1 ] || die "thiếu khóa nhưng không ghi base ok = false"
[ "$(drill_psql -c "SELECT detail LIKE '%WALG_LIBSODIUM_KEY%' FROM platform.backup_runs WHERE kind = 'base' AND NOT ok")" = t ] ||
  die "detail không nói thiếu khóa"

# (2) Sao lưu đạt, có thời gian.
in_pg postgres /opt/crm-backup/base-backup.sh >&2 || die "base-backup.sh thất bại"
[ "$(runs base true)" = 1 ] || die "không ghi base ok = true"
base_dur=$(drill_psql -c "SELECT duration_seconds FROM platform.backup_runs WHERE kind = 'base' AND ok")
[ -n "$base_dur" ] || die "base ok thiếu duration_seconds"

# (3) Ghi sau bản sao lưu (nằm trong dung sai), rồi kiểm.
sleep 1
drill_psql >/dev/null <<'SQL'
INSERT INTO platform.tenants (name) VALUES ('drill-after');
INSERT INTO identity.audit_logs (tenant_id, actor_type, action, entity_type)
SELECT t.id, 'system', 'drill.after', 'drill' FROM platform.tenants t CROSS JOIN generate_series(1, 5) WHERE t.name = 'drill-after';
SQL
verify() { # verify <tên log> [tham số thêm cho docker compose exec]
  local name=$1
  shift
  in_pg -e CRM_VERIFY_FETCH_TIMEOUT="${FETCH_TIMEOUT:-300}" "$@" postgres /opt/crm-backup/weekly-verify.sh >"$work/$name.log" 2>&1
}
verify ok || {
  cat "$work/ok.log" >&2
  die "weekly-verify.sh thất bại trên bản sao lưu tốt"
}
grep -q '^WEEKLY VERIFY OK' "$work/ok.log" || die "weekly-verify.sh không in WEEKLY VERIFY OK"
ok_line=$(grep '^WEEKLY VERIFY OK' "$work/ok.log")
[ "$(runs restore_verify true)" = 1 ] || die "không ghi restore_verify ok = true"
verify_dur=$(drill_psql -c "SELECT duration_seconds FROM platform.backup_runs WHERE kind = 'restore_verify' AND ok")
[ -n "$verify_dur" ] || die "restore_verify thiếu duration_seconds"
in_pg postgres bash -c '! ls -d /var/lib/postgresql/verify* 2>/dev/null | grep -v "verify.lock$"' ||
  die "cụm tạm chưa được dọn (thư mục còn: postgres tạm có thể còn chạy)"

fails=0
expect_fail() { # expect_fail <tên> <chuỗi phải có trong detail>
  fails=$((fails + 1))
  [ "$(runs restore_verify false)" = "$fails" ] || die "$1: không ghi restore_verify ok = false"
  local d
  d=$(drill_psql -c "SELECT detail FROM platform.backup_runs WHERE kind = 'restore_verify' ORDER BY finished_at DESC LIMIT 1")
  case $d in *"$2"*) ;; *) die "$1: detail '$d' thiếu '$2'" ;; esac
  log "$1: ok = false, detail: $d"
}

# (4) Dòng chỉ có ở database thật nhưng ngày tạo trước bản sao lưu: lệch ngoài dung sai.
drill_psql -c "INSERT INTO platform.tenants (name, created_at) VALUES ('drill-ghost', now() - interval '2 days')" >/dev/null
if verify mismatch; then die "weekly-verify.sh không thấy lệch số dòng"; fi
expect_fail "lệch" "lệch"
drill_psql -c "DELETE FROM platform.tenants WHERE name = 'drill-ghost'" >/dev/null

# (5) Lượt khác đang giữ khóa.
in_pg -d postgres flock /var/lib/postgresql/verify.lock sleep 15
sleep 2
if verify locked; then die "weekly-verify.sh chạy dù lượt khác đang giữ khóa"; fi
expect_fail "khóa" "đang có lượt khác"
sleep 14

# (5b) Không đủ chỗ trống (khoảng dự phòng 1 PiB): hỏng ngay, không tải.
if verify nospace -e CRM_VERIFY_MARGIN_BYTES=1125899906842624; then die "weekly-verify.sh chạy dù không đủ chỗ trống"; fi
expect_fail "không đủ chỗ" "không đủ chỗ trống"
# (5c) Thư mục cụm tạm trỏ vào dữ liệu thật: từ chối, không xóa gì (cụm thật còn chạy: kiểm ở cuối).
if verify baddir -e CRM_VERIFY_DIR=/var/lib/postgresql; then die "weekly-verify.sh nhận CRM_VERIFY_DIR chứa PGDATA"; fi
expect_fail "thư mục sai" "CRM_VERIFY_DIR không hợp lệ"
[ "$(drill_psql -c "SELECT count(*) FROM platform.tenants")" -ge 1 ] || die "dữ liệu thật bị mất sau thử thư mục sai"

# (6) Khóa sai: WAL-G không thoát; hạn cắt.
t0=$(now_s)
if FETCH_TIMEOUT=20 verify wrongkey -e WALG_LIBSODIUM_KEY="$(openssl rand -hex 32)"; then
  die "weekly-verify.sh với khóa sai lại đạt"
fi
wrong_seconds=$(($(now_s) - t0))
[ "$wrong_seconds" -le 90 ] || die "khóa sai: chạy ${wrong_seconds}s, hạn không cắt"
expect_fail "khóa sai" "backup-fetch"

# (7) Xóa phần dữ liệu chính (part_001) của bản sao lưu mới nhất (chỉ có một bản).
obj=$(dc run --rm -T rclone lsf -R --files-only "s3:$DRILL_BUCKET/pg/basebackups_005/" 2>/dev/null | grep 'tar_partitions/part_001' | head -1)
[ -n "$obj" ] || die "không tìm thấy object tar của bản sao lưu"
dc run --rm -T rclone deletefile "s3:$DRILL_BUCKET/pg/basebackups_005/$obj" 2>/dev/null
if FETCH_TIMEOUT=60 verify corrupt; then die "weekly-verify.sh đạt dù bản sao lưu mất một object"; fi
expect_fail "mất object" "backup-fetch"

[ "$(drill_psql -c "SELECT pg_postmaster_start_time()")" = "$pg_started" ] ||
  die "cụm PostgreSQL thật đã khởi động lại trong lúc diễn tập"

echo "VERIFY DRILL OK base_duration_seconds=$base_dur verify_duration_seconds=$verify_dur wrongkey_seconds=$wrong_seconds deleted=$obj | $ok_line"
