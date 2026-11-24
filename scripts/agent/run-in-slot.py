#!/usr/bin/env python3
"""Chạy một lệnh nặng (cổng kiểm, test toàn bộ, gộp nhánh) trong LƯỢT TEST DUY NHẤT của cả máy, ở mức ưu tiên thấp.

21/11 (chủ shop: máy lúc nóng lúc nguội, lag): nhiều phiên làm task chạy cổng kiểm trùng lúc (đo: 2 cổng kiểm cùng chạy,
tải máy ~15–25) nên CPU dồn thành từng đợt. Công cụ này giữ khóa độc quyền trên một tệp dùng chung mọi worktree; phiên khác
tới lượt mới chạy, trong lúc chờ vẫn suy nghĩ/viết mã bình thường. Lệnh chạy với nice +10 để ứng dụng của người dùng
được CPU trước.

Dùng: run-in-slot.py <lệnh> [tham số...]
Biến: CRM_TEST_SLOT_LOCK (đường dẫn tệp khóa, mặc định ~/Library/Caches/crm-test-slot.lock),
      CRM_TEST_SLOT_HELD=1 được đặt cho lệnh con (lệnh lồng bên trong không chờ lại chính mình).
"""
import fcntl
import os
import sys
import time

if len(sys.argv) < 2:
    sys.stderr.write("dùng: run-in-slot.py <lệnh> [tham số...]\n")
    sys.exit(2)

cmd = sys.argv[1:]
if os.environ.get("CRM_TEST_SLOT_HELD") == "1":
    os.execvp(cmd[0], cmd)  # đã ở trong lượt (lệnh lồng): chạy luôn

path = os.environ.get("CRM_TEST_SLOT_LOCK") or os.path.expanduser("~/Library/Caches/crm-test-slot.lock")
os.makedirs(os.path.dirname(path), exist_ok=True)
fd = os.open(path, os.O_RDWR | os.O_CREAT, 0o644)
start = time.monotonic()
try:
    fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
except BlockingIOError:
    try:
        holder = os.pread(fd, 200, 0).decode(errors="replace").strip()
    except OSError:
        holder = ""
    sys.stderr.write(f"== chờ lượt test của máy (đang chạy: {holder or 'không rõ'})\n")
    sys.stderr.flush()
    fcntl.flock(fd, fcntl.LOCK_EX)
    sys.stderr.write(f"== tới lượt sau {time.monotonic() - start:.0f} giây\n")
    sys.stderr.flush()
os.ftruncate(fd, 0)
os.pwrite(fd, f"pid {os.getpid()} {os.getcwd()} {' '.join(cmd)[:120]}".encode(), 0)
os.set_inheritable(fd, True)  # lệnh con giữ khóa tới khi xong, kể cả khi tiến trình này bị thay
os.environ["CRM_TEST_SLOT_HELD"] = "1"
try:
    os.nice(10)
except OSError:
    pass
# Ưu tiên thấp của macOS: lớp QoS "utility" (taskpolicy -c utility) — nhường máy cho người dùng mà không hãm đọc ghi
# đĩa. 24/11 đo: chế độ nền (-b, dùng từ 23/11) hãm cả I/O, dịch lại mã chậm 10 lần (go build -a: -b 102 s, utility
# 11 s, nice 9 s) — bước build của ci-task Task 468 mất 352 s thay vì 20–44 s.
if sys.platform == "darwin" and os.path.exists("/usr/sbin/taskpolicy"):
    cmd = ["/usr/sbin/taskpolicy", "-c", "utility", *cmd]
os.execvp(cmd[0], cmd)
