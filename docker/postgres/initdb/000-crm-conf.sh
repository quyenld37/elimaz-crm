#!/usr/bin/env bash
# docker-entrypoint chạy tệp này một lần, khi khởi tạo cụm mới. Tên bắt đầu bằng 000 để chạy trước mọi tệp init khác.
set -eu
grep -q "^include_dir = '/etc/postgresql/conf.d'" "$PGDATA/postgresql.conf" ||
  echo "include_dir = '/etc/postgresql/conf.d'" >>"$PGDATA/postgresql.conf"
