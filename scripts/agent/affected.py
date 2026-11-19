#!/usr/bin/env python3
"""In ra các gói Go cần test: gói chứa tệp vừa đổi và mọi gói phụ thuộc vào chúng (bắc cầu),
cùng toàn bộ gói của vùng rủi ro cao nếu có tệp đổi nằm trong đó.
Đầu vào: danh sách tệp đổi (đường dẫn tương đối) trên stdin. Đầu ra: mỗi dòng một import path."""
import json, os, subprocess, sys, unicodedata

# git trả đường dẫn "Thời" dạng NFD, go list dạng NFC (19/11, Task 401): chuẩn hóa NFC trước khi so, không thì không gói nào khớp.
nfc = lambda x: unicodedata.normalize("NFC", x)

changed = [nfc(l.strip()) for l in sys.stdin if l.strip()]
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

root = nfc(subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True).stdout.strip())
by_dir = {os.path.relpath(nfc(p["Dir"]), root): p["ImportPath"] for p in pkgs}
# Lan lên theo phụ thuộc thật của Go (19/11): gói X nhập Y trong mã chạy (Imports) thì mọi gói dùng X cũng dùng Y -> lan tiếp.
# Gói X chỉ nhập Y trong test (TestImports/XTestImports) thì chỉ test của X phải chạy lại; gói dùng X KHÔNG biên dịch test
# của X nên không lan tiếp. Trước đây gộp chung nên đổi một module kéo theo gần cả bộ (test của realtime nhập httpapi,
# testkit nhập realtime -> mọi gói dùng testkit).
importers, test_importers = {}, {}
for p in pkgs:
    for imp in p.get("Imports", []):
        importers.setdefault(imp, set()).add(p["ImportPath"])
    for imp in p.get("TestImports", []) + p.get("XTestImports", []):
        test_importers.setdefault(imp, set()).add(p["ImportPath"])

targets = set()
if any(d in (".",) for d in go_dirs) and any(p in ("go.mod", "go.sum") for p in changed):
    targets = {p["ImportPath"] for p in pkgs}          # đổi phụ thuộc: test hết
for d in go_dirs:
    if d in by_dir: targets.add(by_dir[d])
stack = list(targets)
reached = set(targets)          # gói có mã chạy bị ảnh hưởng (lan tiếp được)
while stack:
    cur = stack.pop()
    for up in importers.get(cur, ()):
        if up not in reached:
            reached.add(up); stack.append(up)
targets = set(reached)
for cur in reached:             # test của gói nhập gói bị ảnh hưởng: chạy lại, không lan tiếp
    targets |= test_importers.get(cur, set())
for hr in high_risk:
    if any(p == hr or p.startswith(hr.rstrip("/") + "/") for p in changed):
        base = hr if os.path.isdir(os.path.join(root, hr)) else os.path.dirname(hr)
        for dd, ip in by_dir.items():
            if dd == base or dd.startswith(base + "/"): targets.add(ip)
# Chặn hỏng âm thầm: thư mục có tệp .go đổi (còn tồn tại) mà không khớp gói nào -> thoát 4, ci-task chạy toàn bộ test.
unmatched = [d for d in go_dirs if d != "." and d not in by_dir and os.path.isdir(os.path.join(root, d))
             and any(f.endswith(".go") for f in os.listdir(os.path.join(root, d)))
             and not d.startswith(("console", "spikes")) and "testdata" not in d.split("/")]
if unmatched:
    sys.stderr.write("affected.py: không khớp gói cho " + ", ".join(sorted(unmatched)) + "\n")
    sys.exit(4)
print("\n".join(sorted(targets)))
