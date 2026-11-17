#!/usr/bin/env bash
# Hook PreToolUse (Bash): phân tích lệnh bằng bashguard.py (chặn push, commit sai cách, sửa tệp được bảo vệ, reviewer chạy lệnh ghi).
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 0
exec python3 scripts/agent/bashguard.py
