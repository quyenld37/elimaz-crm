#!/usr/bin/env python3
"""Cổng commit: kiểm một dấu duyệt có thật do reviewer tạo ra không.
Dùng: stamp_check.py <tệp dấu> <mã băm phần mã đã stage>. Thoát 0 = hợp lệ.
Dấu (do hook SubagentStop ghi) là JSON trỏ tới bản ghi hội thoại của subagent reviewer. Cổng đòi:
bản ghi nằm trong ~/.claude/projects/**/subagents/, tệp .meta.json cạnh nó ghi agentType=reviewer,
reviewer đã thật sự chạy 'git diff --cached', và câu trả lời cuối có dòng '## Kết luận: ĐẠT'.
Không chống được người cố tình làm giả cả bản ghi hội thoại; việc đó cần CI trên máy khác (đề xuất 4)."""
import json, os, sys

def fail(msg):
    sys.stderr.write("Cổng commit: dấu duyệt không hợp lệ: " + msg + "\n")
    sys.exit(1)

stamp, code_hash = sys.argv[1], sys.argv[2]
try:
    d = json.load(open(stamp, encoding="utf-8"))
except Exception:
    fail("không đọc được tệp dấu (dấu phải do hook ghi khi reviewer kết luận).")
if d.get("code") != code_hash:
    fail("dấu không khớp phần mã đang stage.")
tp = os.path.realpath(d.get("transcript") or "")
root = os.path.realpath(os.path.expanduser("~/.claude/projects")) + os.sep
if not tp.startswith(root) or "/subagents/" not in tp or not os.path.isfile(tp):
    fail("không tìm thấy bản ghi hội thoại của reviewer.")
meta = tp[:-len(".jsonl")] + ".meta.json" if tp.endswith(".jsonl") else ""
try:
    if json.load(open(meta, encoding="utf-8")).get("agentType") != "reviewer":
        fail("bản ghi không phải của agent reviewer.")
except (OSError, ValueError):
    fail("thiếu thông tin agent của bản ghi.")
ran_diff, last = False, ""
for line in open(tp, encoding="utf-8"):
    try:
        e = json.loads(line)
    except ValueError:
        continue
    m = e.get("message") or {}
    if m.get("role") != "assistant" or not isinstance(m.get("content"), list):
        continue
    text = ""
    for b in m["content"]:
        if b.get("type") == "tool_use" and "git diff --cached" in json.dumps(b.get("input"), ensure_ascii=False):
            ran_diff = True
        if b.get("type") == "text":
            text += b.get("text", "")
    if text.strip():
        last = text
if not ran_diff:
    fail("reviewer chưa chạy 'git diff --cached' nên chưa xem phần đã stage.")
if not any(l.strip().startswith("## Kết luận: ĐẠT") for l in last.splitlines()):
    fail("câu trả lời cuối của reviewer không có '## Kết luận: ĐẠT'.")
sys.exit(0)
