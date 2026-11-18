#!/usr/bin/env bash
# Bản sao lưu đầy đủ bằng WAL-G (ADR-37). Chạy trong container PostgreSQL, bằng người dùng postgres. Lịch: mỗi đêm
# 02:00 giờ Việt Nam (RUNBOOK, mục "Sao lưu theo lịch").
# Cần biến WAL-G: WALG_S3_PREFIX, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_ENDPOINT, AWS_S3_FORCE_PATH_STYLE,
# WALG_LIBSODIUM_KEY, WALG_LIBSODIUM_KEY_TRANSFORM; và PGDATA. Database ghi kết quả: CRM_BACKUP_RUNS_DB (mặc định crm).
# Mọi lần chạy ghi một dòng platform.backup_runs (kind = 'base', ok, duration_seconds) khi bảng đó có — kể cả khi thiếu
# biến (thiếu khóa mã hóa phải ra ok = false để luật backup-age có dữ liệu). Thoát khác 0 khi lỗi.
# Mỗi lệnh WAL-G có hạn (CRM_BACKUP_TIMEOUT giây, mặc định 4 giờ): WAL-G có thể treo thay vì thoát (Task 2).
# Không ghi nội dung lỗi của WAL-G vào backup_runs: chỉ ghi bước hỏng.
set -uo pipefail
. "$(dirname "$0")/runs-lib.sh"

push_timeout=${CRM_BACKUP_TIMEOUT:-14400}

fail() {
  echo "base-backup: $1" >&2
  runs_record base false "$1" || echo "base-backup: không ghi được backup_runs" >&2
  exit 1
}

[ -n "${PGDATA:-}" ] || fail "thiếu PGDATA"
[ -n "${WALG_LIBSODIUM_KEY:-}" ] || fail "thiếu WALG_LIBSODIUM_KEY: không sao lưu khi chưa có khóa mã hóa"

# timeout trả 124 khi quá hạn (137 nếu phải giết bằng KILL sau 30 s).
rc=0
timeout -k 30 "$push_timeout" wal-g backup-push "$PGDATA" || rc=$?
case $rc in
0) ;;
124 | 137) fail "quá hạn ${push_timeout}s ở bước backup-push" ;;
*) fail "lỗi ở bước backup-push" ;;
esac
timeout -k 30 "$push_timeout" wal-g delete retain FULL 14 --confirm || fail "lỗi ở bước delete retain FULL 14"

runs_record base true "ok" || {
  echo "base-backup: sao lưu xong nhưng không ghi được backup_runs" >&2
  exit 1
}
echo "base-backup: xong"
