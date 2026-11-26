#!/usr/bin/env python3
"""Bộ điều phối máy chạy test (Task 494, chủ shop 26/11: "các task nào làm song song mà cần test thì cứ chia cho các máy
này chạy song song, test song song, làm sao task làm nhanh nhất có thể"; Task 495, chủ shop 26/11: "máy t sử dụng để xử
lý các task, còn chạy test thì dùng trong bể").

Năm bể, mỗi bể có số chỗ; một lượt (ci-task, merge-test) giữ một chỗ tới khi xong:
  blacksmith  4 chỗ  runner Blacksmith 4 vCPU (toàn bộ test ~6,5 phút một máy); 3.000 phút/tháng, tính ×2 → hết ở 2.700
  ubicloud    3 chỗ  runner ubicloud-standard-2 (2 vCPU); chủ shop không muốn trả tiền (cutoff 1 USD/tháng ở console
                     không chặn cứng) → tính ×1, hết ở 1.100
  github      3 chỗ  ubuntu-24.04 2 lõi; 2.000 phút/tháng chung với website → hết ở 1.600
  vps         1 chỗ  runner tự cài elimaz-vps, 3 lõi (toàn bộ 40 phút): chỉ nhận ≤ 15 gói và không ALL; không tính phút
  mac         0 chỗ  máy dev: KHÔNG chạy test trong lượt chọn bình thường; chỉ là DỰ PHÒNG CUỐI (1 chỗ, CHUNG tệp khóa với
                     scripts/agent/run-in-slot.py) khi --local-only (không chạy được từ xa) hay mọi bể từ xa đều bị loại
                     (tạm hỏng, hết phút, không nhận lượt đó, vừa lỗi hạ tầng). Khi đó in đúng một dòng
                     "== DỰ PHÒNG CUỐI: chạy trên máy Mac (lý do: …)" để điều phối viên báo chủ shop.
Chọn chỗ TRỐNG theo thứ tự blacksmith → ubicloud → github → vps; hết chỗ thì chờ chỗ đầu tiên trống ở bất kỳ bể nào
(bể từ xa chỉ đầy, không hỏng: chờ, không rơi về mac).
Giữ chỗ bằng flock trên <dir>/crm-runner-<bể>.<n>.lock (mac: CRM_TEST_SLOT_LOCK, mặc định ~/Library/Caches/
crm-test-slot.lock); lệnh con kế thừa khóa, hệ điều hành nhả khi mọi tiến trình giữ nó đã thoát (kể cả bị giết).
Lệnh con thoát 3 trên bể từ xa (không chạy được: run không bắt đầu sau 3 phút, gh lỗi) → bể tạm hỏng 30 phút
(<dir>/crm-runner-<bể>.down) và chọn lại bể khác ngay. Thoát 1 (đỏ thật) hay 2 trả nguyên.

Dùng:
  pool.py run [--count N | --all] [--no-mac] [--local-only] -- <lệnh> [tham số…]
      Lệnh con nhận CRM_CI_RUNNER=<bể>, CRM_CI_POOL_HELD=1 (mac: thêm CRM_TEST_SLOT_HELD=1, nice +10, QoS utility).
      --count: số gói sẽ test (vps chỉ nhận ≤ 15); --all: toàn bộ (không vps); không biết số gói: không vps.
      --no-mac: bản cần test không phải thư mục làm việc này (không cả dự phòng); --local-only: chỉ mac (dự phòng cuối).
  pool.py record --runner <bể> --run <id> --minutes <phút máy>   ghi sổ phút (remote-test.sh gọi sau mỗi run)
  pool.py status                                                 chỗ bận, phút tháng này, hạn tạm hỏng của từng bể
Biến: CRM_CI_RUNNER ép một bể (bỏ qua tạm hỏng, sổ phút, luật vps; chờ đúng bể đó; ép mac không phải dự phòng nên
không in dòng đó); CRM_CI_POOL_DIR (mặc định ~/Library/Caches); CRM_CI_POOL_SLOTS (đổi số chỗ, vd "blacksmith=1";
"mac=1" cho mac vào lượt chọn bình thường, sau mọi bể từ xa; "mac=0" tắt hẳn cả dự phòng cuối); CRM_CI_POOL_POLL (giây giữa hai lần dò chỗ trống, mặc định 15); CRM_CI_DOWN_MINUTES (mặc định 30).
Mã thoát: của lệnh con; 2 sai tham số; 3 không bể nào dùng được.
"""
import argparse
import datetime
import fcntl
import os
import subprocess
import sys
import time

POOLS = [  # thứ tự chọn
    ("blacksmith", 4),
    ("ubicloud", 3),
    ("github", 3),
    ("vps", 1),
    ("mac", 0),  # máy dev không chạy test trong lượt chọn bình thường (Task 495); chỉ dự phòng cuối, MAC_LAST_RESORT chỗ
]
SLOTS = dict(POOLS)  # CRM_CI_POOL_SLOTS="blacksmith=1,github=2" đổi số chỗ (đo, bớt tải một bể); 0 là tắt bể đó
MAC_LAST_RESORT = 1  # số chỗ mac khi dự phòng cuối / --local-only / ép mac; CRM_CI_POOL_SLOTS có "mac=N" thì là N
REMOTE = ("blacksmith", "ubicloud", "github", "vps")
MULTIPLIER = {"blacksmith": 2, "ubicloud": 1, "github": 1, "vps": 0}  # phút tính trên mỗi phút máy
BUDGET = {"blacksmith": 2700, "ubicloud": 1100, "github": 1600}
LAST_RESORT_LINE = "== DỰ PHÒNG CUỐI: chạy trên máy Mac (lý do: {})"  # điều phối viên dò đúng chữ này
VPS_MAX_PACKAGES = 15
LEDGER = "crm-runner-minutes.tsv"


def load_slots():
    global MAC_LAST_RESORT  # đọc cấu hình một lần lúc khởi động
    for item in (os.environ.get("CRM_CI_POOL_SLOTS") or "").split(","):
        name, _, n = item.strip().partition("=")
        if name in SLOTS and n.strip().isdigit():
            SLOTS[name] = int(n)
            if name == "mac":
                MAC_LAST_RESORT = int(n)


def slots(pool, last_resort=False):
    """Số chỗ của bể; mac khi dự phòng cuối (hay --local-only, ép mac) có MAC_LAST_RESORT chỗ dù lượt thường là 0."""
    return MAC_LAST_RESORT if pool == "mac" and last_resort else SLOTS[pool]


def say(msg):
    sys.stderr.write(msg + "\n")
    sys.stderr.flush()


def pool_dir():
    d = os.environ.get("CRM_CI_POOL_DIR") or os.path.expanduser("~/Library/Caches")
    os.makedirs(d, exist_ok=True)
    return d


def lock_path(pool, n):
    if pool == "mac":
        p = os.environ.get("CRM_TEST_SLOT_LOCK") or os.path.expanduser("~/Library/Caches/crm-test-slot.lock")
        os.makedirs(os.path.dirname(p), exist_ok=True)
        return p
    return os.path.join(pool_dir(), f"crm-runner-{pool}.{n}.lock")


def month():
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m")


def minutes_this_month():
    used = {}
    path = os.path.join(pool_dir(), LEDGER)
    if not os.path.exists(path):
        return used
    m = month()
    with open(path, encoding="utf-8") as f:
        for line in f:
            parts = line.rstrip("\n").split("\t")
            if len(parts) >= 5 and parts[0] == m:
                try:
                    used[parts[1]] = used.get(parts[1], 0) + float(parts[4])
                except ValueError:
                    pass
    return used


def down_path(pool):
    return os.path.join(pool_dir(), f"crm-runner-{pool}.down")


def down_until(pool):
    try:
        with open(down_path(pool), encoding="utf-8") as f:
            return int(f.read().split()[0])
    except (OSError, ValueError, IndexError):
        return 0


def mark_down(pool, reason):
    mins = float(os.environ.get("CRM_CI_DOWN_MINUTES") or 30)
    until = int(time.time() + mins * 60)
    with open(down_path(pool), "w", encoding="utf-8") as f:
        f.write(f"{until} {reason}\n")
    say(f"== bể {pool} tạm hỏng {mins:g} phút ({reason}); chọn bể khác")


def try_slot(pool, n):
    """Giữ chỗ n của bể nếu trống: trả fd đang giữ khóa, không thì None."""
    fd = os.open(lock_path(pool, n), os.O_RDWR | os.O_CREAT, 0o644)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        os.close(fd)
        return None
    return fd


def holder(pool, n):
    try:
        with open(lock_path(pool, n), encoding="utf-8", errors="replace") as f:
            return f.read(200).strip()
    except OSError:
        return ""


def eligible(args, forced, tried):
    """Danh sách bể được xét theo thứ tự nhanh nhất, kèm lý do bỏ của bể bị loại. tried: bể đã thoát 3 ở lần gọi này
    (không thử lại dù hạn tạm hỏng đã hết, để vòng chọn lại luôn dừng)."""
    if args.local_only:
        return ["mac"], {}
    if forced:
        return [forced], {}
    used = minutes_this_month()
    now = time.time()
    out, skipped = [], {}
    for pool, _ in POOLS:
        if pool in tried:
            skipped[pool] = "vừa lỗi hạ tầng ở lượt này"
            continue
        if SLOTS[pool] == 0:
            if pool != "mac":  # mac 0 chỗ là mặc định (chỉ dự phòng cuối), không báo
                skipped[pool] = "tắt (CRM_CI_POOL_SLOTS)"
            continue
        if pool == "mac" and args.no_mac:
            skipped[pool] = "bản cần test không phải thư mục này"
            continue
        if pool == "vps" and (args.all or args.count is None or args.count > VPS_MAX_PACKAGES):
            skipped[pool] = f"chỉ nhận ≤ {VPS_MAX_PACKAGES} gói, không ALL"
            continue
        if down_until(pool) > now:
            skipped[pool] = "tạm hỏng"
            continue
        if pool in BUDGET and used.get(pool, 0) >= BUDGET[pool]:
            skipped[pool] = f"hết phút tháng này ({used[pool]:g} ≥ {BUDGET[pool]})"
            continue
        out.append(pool)
    return out, skipped


def acquire(pools, last_resort=False):
    """Chờ tới khi giữ được một chỗ trống, xét bể theo thứ tự nhanh nhất ở mỗi lượt dò."""
    poll = float(os.environ.get("CRM_CI_POOL_POLL") or 15)
    start, waited = time.monotonic(), False
    while True:
        for pool in pools:
            for n in range(1, slots(pool, last_resort) + 1):
                fd = try_slot(pool, n)
                if fd is not None:
                    if waited:
                        say(f"== tới lượt sau {time.monotonic() - start:.0f} giây")
                    return pool, n, fd
        if not waited:
            busy = "; ".join(f"{p}: {slots(p, last_resort)} chỗ bận" for p in pools)
            say(f"== chờ lượt test của máy (mọi bể đầy: {busy})")
            waited = True
        time.sleep(poll)


def run_child(pool, n, fd, cmd, last_resort=False):
    os.ftruncate(fd, 0)
    os.pwrite(fd, f"pid {os.getpid()} {os.getcwd()} {pool}.{n} {' '.join(cmd)[:120]}".encode(), 0)
    env = dict(os.environ, CRM_CI_RUNNER=pool, CRM_CI_POOL_HELD="1")
    preexec = None
    if pool == "mac":
        env["CRM_TEST_SLOT_HELD"] = "1"
        preexec = lambda: os.nice(10)  # noqa: E731 — như run-in-slot.py
        if sys.platform == "darwin" and os.path.exists("/usr/sbin/taskpolicy"):
            cmd = ["/usr/sbin/taskpolicy", "-c", "utility", *cmd]
    say(f"== bể máy: {pool} (chỗ {n}/{slots(pool, last_resort)})")
    try:
        p = subprocess.Popen(cmd, env=env, pass_fds=(fd,), preexec_fn=preexec)
    except OSError as e:
        os.close(fd)
        say(f"không chạy được lệnh: {e}")
        return 2
    while True:
        try:
            rc = p.wait()
            break
        except KeyboardInterrupt:
            continue  # Ctrl-C tới cả nhóm: chờ lệnh con dọn (hủy run, xóa nhánh) rồi mới thoát
    os.close(fd)
    return 128 - rc if rc < 0 else rc


def cmd_run(args):
    cmd = args.cmd[1:] if args.cmd and args.cmd[0] == "--" else args.cmd
    if not cmd:
        say("pool.py run: thiếu lệnh")
        return 2
    if os.environ.get("CRM_CI_POOL_HELD") == "1":
        os.execvp(cmd[0], cmd)  # đã giữ chỗ (lệnh lồng): chạy luôn ở bể đã chọn
    forced = os.environ.get("CRM_CI_RUNNER", "")
    if forced and forced not in SLOTS:
        say(f"CRM_CI_RUNNER={forced}: không có bể này ({', '.join(p for p, _ in POOLS)})")
        return 2
    if os.environ.get("CRM_TEST_SLOT_HELD") == "1" and (args.local_only or forced == "mac"):
        # Gọi bên trong lượt máy đã giữ (run-in-slot.py): chạy luôn ở mac, không chờ chính khóa của tiến trình cha.
        if args.local_only:
            say(LAST_RESORT_LINE.format("không chạy được từ xa (--local-only)"))
        os.environ.update(CRM_CI_RUNNER="mac", CRM_CI_POOL_HELD="1")
        os.execvp(cmd[0], cmd)
    if args.local_only and MAC_LAST_RESORT == 0:
        say("POOL: chỉ chạy được ở máy nhưng bể mac đang tắt (CRM_CI_POOL_SLOTS)")
        return 3
    if forced and args.local_only and forced != "mac":
        say(f"== CRM_CI_RUNNER={forced} bị bỏ: chỉ chạy được ở máy")
    if forced and not args.local_only and slots(forced, forced == "mac") == 0:
        say(f"CRM_CI_RUNNER={forced}: bể đang tắt (CRM_CI_POOL_SLOTS)")
        return 2
    if forced == "mac" and args.no_mac and not args.local_only:
        say("CRM_CI_RUNNER=mac: bản cần test không phải thư mục làm việc này")
        return 2
    tried = set()
    while True:
        pools, skipped = eligible(args, forced, tried)
        for p, why in skipped.items():
            say(f"== bỏ bể {p}: {why}")
        last_resort = args.local_only or forced == "mac"
        if args.local_only:
            say(LAST_RESORT_LINE.format("không chạy được từ xa (--local-only)"))
        elif not pools:
            # Mọi bể từ xa bị loại: máy dev là dự phòng cuối (trừ khi bản cần test không ở đây hay mac tắt hẳn).
            if args.no_mac or MAC_LAST_RESORT == 0:
                say("POOL: không bể nào dùng được")
                return 3
            why = "; ".join(f"{p}: {w}" for p, w in skipped.items() if p != "mac") or "không bể từ xa nào"
            say(LAST_RESORT_LINE.format(f"mọi bể từ xa không dùng được — {why}"))
            pools, last_resort = ["mac"], True
        pool, n, fd = acquire(pools, last_resort)
        rc = run_child(pool, n, fd, cmd, last_resort)
        tried.add(pool)
        if rc == 3 and pool in REMOTE and not forced and not args.local_only:
            mark_down(pool, "không chạy được từ xa (mã 3)")
            continue
        return rc


def cmd_record(args):
    if args.runner not in MULTIPLIER:
        say(f"record: bể {args.runner} không tính phút")
        return 2
    billed = args.minutes * MULTIPLIER[args.runner]
    now = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    line = f"{month()}\t{args.runner}\t{args.run}\t{args.minutes:g}\t{billed:g}\t{now}\n"
    with open(os.path.join(pool_dir(), LEDGER), "a", encoding="utf-8") as f:
        fcntl.flock(f.fileno(), fcntl.LOCK_EX)
        f.write(line)
    return 0


def cmd_status(_args):
    used = minutes_this_month()
    now = time.time()
    for pool, _ in POOLS:
        n_slots = slots(pool, pool == "mac")
        busy = []
        for n in range(1, n_slots + 1):
            fd = try_slot(pool, n)
            if fd is None:
                busy.append(f"{n}: {holder(pool, n) or 'không rõ'}")
            else:
                os.close(fd)
        line = f"{pool}: {len(busy)}/{n_slots} chỗ bận"
        if pool == "mac" and SLOTS["mac"] == 0:
            line += " (dự phòng cuối, không chạy test trong lượt chọn bình thường)" if n_slots else " (tắt hẳn)"
        if pool in BUDGET:
            line += f"; tháng {month()}: {used.get(pool, 0):g}/{BUDGET[pool]} phút tính"
        elif pool in MULTIPLIER:
            line += "; không tính phút"
        if down_until(pool) > now:
            line += f"; tạm hỏng tới {time.strftime('%H:%M', time.localtime(down_until(pool)))}"
        print(line)
        for b in busy:
            print(f"   {b}")
    return 0


def main():
    ap = argparse.ArgumentParser(prog="pool.py")
    sub = ap.add_subparsers(dest="sub", required=True)
    r = sub.add_parser("run")
    g = r.add_mutually_exclusive_group()
    g.add_argument("--count", type=int)
    g.add_argument("--all", action="store_true")
    r.add_argument("--no-mac", action="store_true")
    r.add_argument("--local-only", action="store_true")
    r.add_argument("cmd", nargs=argparse.REMAINDER)
    rec = sub.add_parser("record")
    rec.add_argument("--runner", required=True)
    rec.add_argument("--run", required=True)
    rec.add_argument("--minutes", type=float, required=True)
    sub.add_parser("status")
    args = ap.parse_args()
    load_slots()
    return {"run": cmd_run, "record": cmd_record, "status": cmd_status}[args.sub](args)


if __name__ == "__main__":
    sys.exit(main())
