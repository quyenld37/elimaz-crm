#!/usr/bin/env python3
"""Hook SubagentStop (reviewer): đọc câu trả lời cuối của reviewer; nếu có dòng '## Kết luận: ĐẠT' thì ghi dấu duyệt
gắn với mã băm phần mã đã stage (biến môi trường CODE_HASH) vào .git/crm-review/. Agent không ghi dấu này."""
import datetime, json, os, sys
d = json.load(sys.stdin)
msg = d.get("last_assistant_message") or ""
tp = d.get("agent_transcript_path")
if not msg and tp and os.path.exists(tp):
    for line in open(tp, encoding="utf-8"):
        try:
            e = json.loads(line)
        except Exception:
            continue
        m = e.get("message") or {}
        if m.get("role") != "assistant":
            continue
        c = m.get("content")
        t = "".join(x.get("text", "") for x in c if isinstance(x, dict) and x.get("type") == "text") if isinstance(c, list) else (c or "")
        if t.strip():
            msg = t
code_hash = os.environ.get("CODE_HASH", "")
rd = os.environ.get("REVIEW_DIR") or ".git/crm-review"
os.makedirs(rd, exist_ok=True)
ok = any(l.strip().startswith("## Kết luận: ĐẠT") for l in msg.splitlines())
with open(os.path.join(rd, "log"), "a", encoding="utf-8") as log:
    log.write(f"{datetime.datetime.now().isoformat(timespec='seconds')} code={code_hash} verdict={'DAT' if ok else 'KHONG'} msg_chars={len(msg)}\n")
if ok and code_hash:
    with open(os.path.join(rd, code_hash), "w", encoding="utf-8") as f:
        json.dump({"code": code_hash, "transcript": tp or ""}, f, ensure_ascii=False)
