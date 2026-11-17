#!/usr/bin/env python3
"""Phân tích một lệnh shell (đầu vào hook PreToolUse/Bash) và quyết định chặn hay cho.
Thoát 2 + thông báo ở stderr = chặn. Không phải bộ phân tích shell hoàn chỉnh: chặn rộng tay khi không chắc.
"""
import json, re, shlex, sys

data = json.load(sys.stdin)
cmd = (data.get("tool_input") or {}).get("command") or ""
agent = data.get("agent_type") or ""

def block(msg):
    sys.stderr.write("Chặn: " + msg + "\n")
    sys.exit(2)

# Đường dẫn được bảo vệ: chỉ chủ shop (hoặc phiên cài đặt ngoài apps/crm) được sửa.
PROTECTED = [r"\.claude/", r"scripts/agent/", r"\.githooks", r"\.git/", r"docs/tasks/hien-tai\.md", r"core\.hooksPath"]
WRITE_HINT = re.compile(r"(>|\brm\b|\bmv\b|\bcp\b|\btee\b|\bsed\b[^|;&]*\s-i|\bchmod\b|\bln\b|\btruncate\b|\binstall\b|\bpython3?\b|\bperl\b|\bruby\b|\bnode\b|\bdd\b|\bunlink\b|\bgit\s+(rm|mv|checkout|restore|update-index|config)\b)")
for pat in PROTECTED:
    if re.search(pat, cmd) and WRITE_HINT.search(cmd):
        block(f"lệnh có thể sửa tệp được bảo vệ ({pat.replace(chr(92), '')}). Hook, cấu hình agent, git hook, trạng thái task chỉ chủ shop đổi. Đổi trạng thái task bằng scripts/agent/new-task.sh hoặc finish-task.sh.")

# Tách lệnh ghép thành các lệnh đơn.
try:
    lex = shlex.shlex(cmd, posix=True, punctuation_chars=";&|()\n")
    lex.whitespace_split = True
    tokens = list(lex)
except ValueError:
    block("không phân tích được lệnh (dấu nháy lệch). Viết lại lệnh đơn giản hơn.")
parts, cur = [], []
for t in tokens:
    if t and set(t) <= set(";&|()\n"):
        if cur: parts.append(cur); cur = []
    else:
        cur.append(t)
if cur: parts.append(cur)

GIT_OPTS_WITH_ARG = {"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path", "--config-env"}
subs = []
for argv in parts:
    # bỏ gán biến môi trường ở đầu (A=b git ...), env, command, sudo
    while argv and (re.match(r"^[A-Za-z_][A-Za-z0-9_]*=", argv[0]) or argv[0] in ("env", "command", "builtin", "exec", "nohup", "time")):
        argv = argv[1:]
    if not argv: continue
    prog = argv[0].rsplit("/", 1)[-1]
    if prog in ("bash", "sh", "zsh") and "-c" in argv:
        block("không chạy lệnh git hay sửa tệp qua 'bash -c'. Gọi lệnh trực tiếp.")
    if prog != "git": continue
    i, opts = 1, []
    while i < len(argv) and argv[i].startswith("-"):
        o = argv[i]; opts.append(o)
        if o in GIT_OPTS_WITH_ARG: i += 1; opts.append(argv[i] if i < len(argv) else "")
        i += 1
    sub = argv[i] if i < len(argv) else ""
    rest = argv[i+1:]
    if any("hooksPath" in o for o in opts) or (sub == "config" and any("hooksPath" in r for r in rest)):
        block("không đổi core.hooksPath.")
    if sub == "push":
        block("push cần OK riêng của chủ shop trong chat.")
    if sub == "commit":
        if any(r in ("--no-verify", "-n") for r in rest):
            block("không dùng --no-verify; cổng commit phải chạy.")
        if any(r in ("-a", "--all", "-i", "--include", "-o", "--only", "--amend") or re.match(r"^-[a-zA-Z]*a", r) for r in rest):
            block("commit chỉ những gì đã stage, không dùng -a/--all/-i/-o/--amend. Stage bằng 'git add <tệp>' ở một lệnh riêng, chạy reviewer, rồi commit.")
        # pathspec sau '--' hoặc tham số không phải cờ và không phải giá trị của -m/-F
        skip = False
        for r in rest:
            if skip: skip = False; continue
            if r in ("-m", "-F", "--message", "--file", "--author", "--date", "-C", "-c", "--fixup", "--squash", "--cleanup", "-S"): skip = True; continue
            if r == "--" or not r.startswith("-"):
                block("commit kèm đường dẫn sẽ lấy cả phần chưa review. Stage trước, commit không kèm đường dẫn.")
    subs.append(sub)
if "commit" in subs and any(s in ("add", "stage", "rm", "mv") for s in subs):
    block("tách 'git add' và 'git commit' ra hai lệnh riêng (reviewer phải review đúng phần đã stage trước khi commit).")

# Reviewer chỉ được chạy lệnh đọc.
if agent == "reviewer":
    READ_ONLY = re.compile(r"^(git (diff|log|show|status|ls-files|rev-parse|blame)|cat|ls|grep|rg|head|tail|wc|find|go (list|doc|vet)|gopls)\b")
    for argv in parts:
        line = " ".join(argv)
        if not READ_ONLY.match(line):
            block(f"reviewer chỉ được chạy lệnh đọc (git diff/log/show/status, cat, grep, ls, go list/doc/vet). Lệnh bị chặn: {line[:80]}")
sys.exit(0)
