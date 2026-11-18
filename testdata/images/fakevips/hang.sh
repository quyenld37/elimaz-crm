#!/bin/sh
# vips giả bị treo (Task 42): ghi số tiến trình (cũng là số nhóm, vì imager đặt Setpgid) vào $FAKEVIPS_PIDFILE nếu có,
# rồi ngủ 60 s trong một tiến trình con, để test thấy imager giết cả nhóm chứ không chỉ tiến trình đầu.
if [ -n "${FAKEVIPS_PIDFILE:-}" ]; then echo $$ > "$FAKEVIPS_PIDFILE"; fi
sleep 60 &
wait
