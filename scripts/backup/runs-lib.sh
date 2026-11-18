# Ghi một dòng platform.backup_runs (bảng của 0010_ops.sql) cho base-backup.sh và weekly-verify.sh (Task 21).
# Chạy trong container PostgreSQL bằng người dùng postgres, qua socket; database: CRM_BACKUP_RUNS_DB (mặc định crm).
# Bảng chưa có (cụm chưa migrate) thì không ghi gì. detail chỉ mang tên bước hỏng và số đếm, không bao giờ có nội
# dung lỗi của WAL-G hay dòng dữ liệu.
# shellcheck shell=bash

runs_t0=$(date +%s)

# runs_record <kind: base|restore_verify> <ok: true|false> <detail>. duration_seconds tính từ lúc nạp tệp này.
runs_record() {
  local dur=$(($(date +%s) - runs_t0))
  psql -X -q -v ON_ERROR_STOP=1 -d "${CRM_BACKUP_RUNS_DB:-crm}" \
    -v kind="$1" -v ok="$2" -v detail="$3" -v dur="$dur" <<'SQL'
SELECT to_regclass('platform.backup_runs') IS NOT NULL AS has_table \gset
\if :has_table
INSERT INTO platform.backup_runs (kind, ok, duration_seconds, detail)
VALUES (:'kind', :'ok'::boolean, :'dur'::int, :'detail');
\endif
SQL
}
