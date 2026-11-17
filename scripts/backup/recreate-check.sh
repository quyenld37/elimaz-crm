#!/usr/bin/env bash
# Phép thử ngắn số 4 (ADR-42): xóa hẳn container PostgreSQL 18 rồi tạo lại với cùng volume, dữ liệu phải còn.
# Image bản 18 để dữ liệu ở /var/lib/postgresql/18/docker; volume gắn ở /var/lib/postgresql (04 mục 13).
# In "RECREATE OK" và thoát 0 khi đạt.
set -euo pipefail
. "$(dirname "$0")/drill-lib.sh"

trap drill_down EXIT
drill_gen_secrets
drill_up

pgdata=$(dc exec -T postgres sh -c 'printf %s "$PGDATA"')
case "$pgdata" in
  /var/lib/postgresql/*) ;;
  *) die "PGDATA=$pgdata không nằm trong volume /var/lib/postgresql" ;;
esac
# Chính thư mục dữ liệu phải nằm trên volume gắn vào, không trên lớp ghi của container.
dc exec -T postgres sh -c 'grep -q " /var/lib/postgresql " /proc/self/mountinfo' ||
  die "/var/lib/postgresql không phải điểm gắn volume"

marker="recreate-$(openssl rand -hex 6)"
drill_psql -c "CREATE TABLE recreate_check(marker text NOT NULL); INSERT INTO recreate_check VALUES ('$marker');" >/dev/null
old_id=$(dc ps -q postgres)

dc rm -sf postgres >&2
[ -z "$(dc ps -aq postgres)" ] || die "container cũ vẫn còn"
dc up -d postgres >&2
drill_wait_ready postgres 120
new_id=$(dc ps -q postgres)
[ -n "$new_id" ] && [ "$new_id" != "$old_id" ] || die "container không được tạo mới (id cũ $old_id, id mới $new_id)"

got=$(drill_psql -c "SELECT count(*) FROM recreate_check WHERE marker = '$marker'")
[ "$got" = 1 ] || die "dòng đã chèn không còn sau khi tạo lại container (đếm được $got)"

echo "RECREATE OK pgdata=$pgdata old=${old_id:0:12} new=${new_id:0:12}"
