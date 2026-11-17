#!/usr/bin/env bash
# Hook SubagentStop (reviewer): ghi dấu duyệt gắn với phần mã đã stage. Xem review_stamp.py.
cd "${CLAUDE_PROJECT_DIR:-$(pwd)}" || exit 0
. scripts/agent/lib.sh
REVIEW_DIR=$(review_dir) CODE_HASH=$(staged_code_hash) exec python3 scripts/agent/review_stamp.py
