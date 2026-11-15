#!/usr/bin/env python3
"""In ra các gói Go cần test: gói chứa tệp vừa đổi và mọi gói phụ thuộc vào chúng (bắc cầu),
cùng toàn bộ gói của vùng rủi ro cao nếu có tệp đổi nằm trong đó.
Đầu vào: danh sách tệp đổi (đường dẫn tương đối) trên stdin. Đầu ra: mỗi dòng một import path."""
import json, os, subprocess, sys

changed = [l.strip() for l in sys.stdin if l.strip()]
go_dirs = {os.path.dirname(p) or "." for p in changed if p.endswith(".go") or p.endswith("go.mod") or p.endswith("go.sum")}
if not go_dirs:
    sys.exit(0)
high_risk = os.environ.get("HIGH_RISK_DIRS", "").split()

out = subprocess.run(["go", "list", "-json", "./..."], capture_output=True, text=True)
if out.returncode != 0:
    sys.stderr.write(out.stderr)
    sys.exit(3)
dec = json.JSONDecoder(); s = out.stdout; i = 0; pkgs = []
while i < len(s):
    while i < len(s) and s[i].isspace(): i += 1
    if i >= len(s): break
    obj, i = dec.raw_decode(s, i); pkgs.append(obj)

root = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True).stdout.strip()
by_dir = {os.path.relpath(p["Dir"], root): p["ImportPath"] for p in pkgs}
importers = {}
for p in pkgs:
    for imp in p.get("Imports", []) + p.get("TestImports", []) + p.get("XTestImports", []):
        importers.setdefault(imp, set()).add(p["ImportPath"])

targets = set()
if any(d in (".",) for d in go_dirs) and any(p in ("go.mod", "go.sum") for p in changed):
    targets = {p["ImportPath"] for p in pkgs}          # đổi phụ thuộc: test hết
for d in go_dirs:
    if d in by_dir: targets.add(by_dir[d])
stack = list(targets)
while stack:
    cur = stack.pop()
    for up in importers.get(cur, ()):
        if up not in targets:
            targets.add(up); stack.append(up)
for hr in high_risk:
    if any(p == hr or p.startswith(hr.rstrip("/") + "/") for p in changed):
        base = hr if os.path.isdir(os.path.join(root, hr)) else os.path.dirname(hr)
        for dd, ip in by_dir.items():
            if dd == base or dd.startswith(base + "/"): targets.add(ip)
print("\n".join(sorted(targets)))
