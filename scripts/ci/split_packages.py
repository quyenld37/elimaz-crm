#!/usr/bin/env python3
"""Chia danh sách gói Go thành tối đa N phần cân bằng cho ma trận job test của test-remote.yml (Task 493).

Đầu vào (stdin): đường dẫn nhập của gói, mỗi dòng một gói (dòng trống, trùng bị bỏ).
Trọng số: số giây trong tệp thời gian (--times, dòng "<gói> <giây>", '#' là chú thích); gói chưa có trong tệp thì ước
theo số tệp *_test.go của thư mục gói × giây trung bình mỗi tệp (từ tệp thời gian, không có thì 5 s), tối thiểu 1 s.
Số phần: min(--max-parts, số gói, trần(tổng / --min-seconds)), ít nhất 1 khi có gói. Mỗi job tốn ~2 phút dựng
(checkout, Go, compose) nên phần quá nhẹ là phí phút. Chia tham lam: gói nặng trước vào phần nhẹ nhất (LPT).
Đầu ra (stdout): JSON [{"part": 1, "race": "<gói…>", "plain": "<gói…>", "seconds": 123}, …]; --race là danh sách gói
chạy -race (cách bằng khoảng trắng) hoặc ALL.
"""
import argparse
import json
import math
import os
import sys

MOD = "elimaz.com.vn/crm/"
DEFAULT_PER_FILE = 5.0


def read_times(path):
    times = {}
    if not path or not os.path.exists(path):
        return times
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.split("#", 1)[0].strip()
            if not line:
                continue
            parts = line.split()
            if len(parts) >= 2:
                try:
                    times[parts[0]] = float(parts[1])
                except ValueError:
                    pass
    return times


def test_files(root, pkg):
    rel = pkg[len(MOD):] if pkg.startswith(MOD) else pkg
    d = os.path.join(root, rel)
    try:
        return sum(1 for n in os.listdir(d) if n.endswith("_test.go"))
    except OSError:
        return 0


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--times", default="")
    ap.add_argument("--max-parts", type=int, default=6)
    ap.add_argument("--min-seconds", type=float, default=120)
    ap.add_argument("--race", default="")
    ap.add_argument("--root", default=os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
    a = ap.parse_args()

    pkgs = []
    for tok in sys.stdin.read().split():
        if tok not in pkgs:
            pkgs.append(tok)
    if not pkgs:
        print("[]")
        return

    times = read_times(a.times)
    known_files = sum(test_files(a.root, p) for p in times)
    per_file = (sum(times.values()) / known_files) if times and known_files else DEFAULT_PER_FILE
    weight = {}
    for p in pkgs:
        weight[p] = times[p] if p in times else max(1.0, test_files(a.root, p) * per_file)

    total = sum(weight.values())
    k = max(1, min(a.max_parts, len(pkgs), math.ceil(total / max(a.min_seconds, 1))))
    bins = [{"pkgs": [], "w": 0.0} for _ in range(k)]
    for p in sorted(pkgs, key=lambda p: (-weight[p], p)):
        b = min(bins, key=lambda b: b["w"])
        b["pkgs"].append(p)
        b["w"] += weight[p]

    race_all = a.race.strip() == "ALL"
    race_set = set(a.race.split())
    out = []
    for i, b in enumerate(sorted(bins, key=lambda b: -b["w"]), 1):
        ps = sorted(b["pkgs"])
        out.append({
            "part": i,
            "race": " ".join(p for p in ps if race_all or p in race_set),
            "plain": " ".join(p for p in ps if not (race_all or p in race_set)),
            "seconds": int(round(b["w"])),
        })
    print(json.dumps(out))


if __name__ == "__main__":
    main()
