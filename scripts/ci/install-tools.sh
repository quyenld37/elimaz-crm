#!/usr/bin/env bash
# Cài công cụ đã ghim (make ci-tools) trên runner của test-remote.yml (Task 493); bỏ qua khi cache trúng.
# Dùng: install-tools.sh <cache-hit: true|…> [khóa công cụ]. npm cài toàn cục vào ~/.npm-global để cache giữ được jscpd.
# Task 494: máy tự cài (vps) không khôi phục cache của GitHub mà giữ GOPATH/bin giữa các lần: sau khi cài ghi dấu
# GOPATH/bin/.ci-tools-<khóa>; lần sau cùng khóa (cùng phiên bản ghim) thì bỏ cài.
set -eu
npm config set prefix "$HOME/.npm-global"
echo "$HOME/.npm-global/bin" >> "$GITHUB_PATH"
bin="$(go env GOPATH)/bin"
echo "$bin" >> "$GITHUB_PATH"
stamp="$bin/.ci-tools-${2:-}"
if [ "${1:-}" = true ]; then exit 0; fi
if [ -n "${2:-}" ] && [ -f "$stamp" ]; then echo "công cụ đã cài (dấu $stamp)"; exit 0; fi
PATH="$HOME/.npm-global/bin:$PATH" make ci-tools
if [ -n "${2:-}" ]; then mkdir -p "$bin" && touch "$stamp"; fi
