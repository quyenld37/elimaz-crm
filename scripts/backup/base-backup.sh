#!/usr/bin/env bash
# Bản sao lưu đầy đủ bằng WAL-G (ADR-37). Chạy trong container PostgreSQL, bằng người dùng postgres.
# Cần biến WAL-G: WALG_S3_PREFIX, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_ENDPOINT, AWS_S3_FORCE_PATH_STYLE,
# WALG_LIBSODIUM_KEY, WALG_LIBSODIUM_KEY_TRANSFORM; và PGDATA. Database ghi kết quả: CRM_BACKUP_RUNS_DB (mặc định crm).
# Ghi platform.backup_runs (kind = 'base') khi bảng đó có (từ Task 19). Thoát khác 0 khi lỗi.
# Không ghi nội dung lỗi của WAL-G vào backup_runs: chỉ ghi bước hỏng.
set -uo pipefail
: "${PGDATA:?thiếu PGDATA}"
: "${WALG_LIBSODIUM_KEY:?thiếu WALG_LIBSODIUM_KEY: không sao lưu khi chưa có khóa mã hóa}"
db=${CRM_BACKUP_RUNS_DB:-crm}

record() { # record <ok: true|false> <bước>
  psql -X -q -v ON_ERROR_STOP=1 -d "$db" -v ok="$1" -v detail="$2" <<'SQL'
SELECT to_regclass('platform.backup_runs') IS NOT NULL AS has_table \gset
\if :has_table
INSERT INTO platform.backup_runs (kind, ok, detail) VALUES ('base', :'ok'::boolean, :'detail');
\endif
SQL
}

step=""
if ! wal-g backup-push "$PGDATA"; then
  step="backup-push"
elif ! wal-g delete retain FULL 14 --confirm; then
  step="delete retain FULL 14"
fi

if [ -n "$step" ]; then
  echo "base-backup: lỗi ở bước $step" >&2
  record false "lỗi ở bước $step" || echo "base-backup: không ghi được backup_runs" >&2
  exit 1
fi
record true "ok" || {
  echo "base-backup: sao lưu xong nhưng không ghi được backup_runs" >&2
  exit 1
}
echo "base-backup: xong"
