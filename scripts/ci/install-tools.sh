#!/usr/bin/env bash
# Cài công cụ đã ghim (make ci-tools) trên runner của test-remote.yml (Task 493); bỏ qua khi cache trúng.
# Dùng: install-tools.sh <cache-hit: true|…>. npm cài toàn cục vào ~/.npm-global để cache giữ được jscpd.
set -eu
npm config set prefix "$HOME/.npm-global"
echo "$HOME/.npm-global/bin" >> "$GITHUB_PATH"
echo "$(go env GOPATH)/bin" >> "$GITHUB_PATH"
if [ "${1:-}" != true ]; then PATH="$HOME/.npm-global/bin:$PATH" make ci-tools; fi
