#!/bin/sh
# Dựng /etc/valkey/users.acl từ users.acl.tmpl rồi chạy valkey-server với tệp cấu hình $1 (cache.conf hoặc gate.conf).
# Dùng chung cho docker/compose.dev.yml và internal/testkit/valkey.go (kế hoạch M5 Task 184). Mật khẩu chỉ gồm
# A-Z a-z 0-9 _ - (thay bằng sed an toàn).
set -eu
conf=${1:?thiếu tệp cấu hình}
for v in VALKEY_CACHE_PASSWORD VALKEY_GATE_PASSWORD VALKEY_ADMIN_PASSWORD; do
  eval "p=\${$v:-}"
  case "$p" in
    "" | *[!A-Za-z0-9_-]*) echo "start.sh: $v rỗng hoặc có ký tự ngoài A-Za-z0-9_-" >&2; exit 2 ;;
  esac
done
umask 022
sed -e '/^#/d' \
  -e "s/@CRM_CACHE_PASSWORD@/$VALKEY_CACHE_PASSWORD/" \
  -e "s/@CRM_GATE_PASSWORD@/$VALKEY_GATE_PASSWORD/" \
  -e "s/@CRM_ADMIN_PASSWORD@/$VALKEY_ADMIN_PASSWORD/" \
  /etc/valkey-src/users.acl.tmpl > /etc/valkey/users.acl
exec docker-entrypoint.sh valkey-server "/etc/valkey-src/$conf"
