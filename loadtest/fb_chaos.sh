#!/usr/bin/env bash
# Hỏng giữa tải của bài M4 (kế hoạch M4 Task 180; LV10, HT01, HT02). fb_run.sh chaos gọi script này song song với
# fb_live.js, fb_staff.js. Lần lượt, mỗi lần cách CHAOS_GAP giây (mặc định 180) tính từ lúc bắt đầu:
#   1. giết crm-worker (đang giữ luồng comment live của cả hai phiên) bằng SIGKILL rồi chạy lại ngay;
#   2. giết crm-api rồi chạy lại ngay;
#   3. dừng PostgreSQL của dự án compose COMPOSE_PROJECT CHAOS_PG_DOWN giây (mặc định 60) rồi bật lại; CHAOS_PG_DOWN=0
#      bỏ bước này (chỉ để chạy khói trên bộ compose dùng chung, không phải bài đo).
# Ghi $OUT/chaos.csv (fault,start,recovered — epoch giây): "trở lại" là lúc ba vai trò healthy và không còn việc pending
# quá hạn 2 s; quá CHAOS_WAIT giây (mặc định 300) thì recovered để trống. Ghi $OUT/live.csv (fault,video,start,regained):
# sau mỗi lần hỏng, lúc máy Meta giả thấy luồng /{video}/live_comments mở lại (sổ GET /_fake/streams); quá CHAOS_WAIT thì
# regained để trống.
# Biến bắt buộc (fb_run.sh đặt): OUT, BIN, GRAPH, COMPOSE_PROJECT và biến của fb_lib.sh. Không bao giờ dừng bộ chung
# crm-shared hay database có dữ liệu thật.
set -uo pipefail
: "${OUT:?}" "${BIN:?}" "${GRAPH:?}" "${COMPOSE_PROJECT:?}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=loadtest/fb_lib.sh
. "$ROOT/loadtest/fb_lib.sh"
GAP="${CHAOS_GAP:-180}"
PG_DOWN="${CHAOS_PG_DOWN:-60}"
WAIT="${CHAOS_WAIT:-300}"
CSV="$OUT/chaos.csv"
LIVE="$OUT/live.csv"
if [ "$PG_DOWN" != 0 ]; then
	case "$COMPOSE_PROJECT" in crm-shared*)
		echo "fb_chaos.sh: không dừng PostgreSQL của bộ chung $COMPOSE_PROJECT (đặt CHAOS_PG_DOWN=0 để chạy khói)" >&2
		exit 2
		;;
	esac
fi
log() { echo "fb_chaos: $(date '+%H:%M:%S') $*" >&2; }
dc() { docker compose -p "$COMPOSE_PROJECT" -f "$ROOT/docker/compose.dev.yml" "$@"; }
sleep_until() { python3 -c "import time; d = $1 - time.time(); time.sleep(d) if d > 0 else None"; }
gt() { python3 -c "import sys; sys.exit(0 if $1 > $2 else 1)"; }

backlog_ok() {
	local n
	n="$(pg_q "SELECT count(*) FROM platform.jobs WHERE status = 'pending' AND run_at < now() - interval '2 seconds'")"
	[ "$n" = 0 ]
}

# wait_recovered <lần hỏng> <lúc gỡ hỏng>: kiểm từ 3 s sau lúc gỡ hỏng (trước đó việc nền mới chưa kịp quá hạn 2 s
# nên "không còn việc chờ quá hạn" chưa chứng minh được worker đã chạy lại); mốc trở lại lấy sau khi cả hai kiểm qua.
wait_recovered() {
	local fault="$1" start="$2" t
	sleep_until "$start + 3"
	while :; do
		if all_healthy && backlog_ok; then
			t="$(now_s)"
			echo "$fault,$start,$t" >>"$CSV"
			log "$fault: trở lại sau $(python3 -c "print(round($t - $start, 1))") s"
			return
		fi
		t="$(now_s)"
		if gt "$t - $start" "$WAIT"; then
			echo "$fault,$start," >>"$CSV"
			log "$fault: chưa trở lại sau $WAIT s"
			return
		fi
		sleep 1
	done
}

# regained <video> <từ lúc> <keep>: in lúc luồng của video được nhận lại sau lúc đó: lúc mở của luồng đầu tiên mở sau
# lúc đó; keep=1 (ngắt PostgreSQL, worker không chết) thì luồng mở từ trước mà vẫn chưa đóng tính là nhận lại ngay lúc
# đó. Rỗng khi chưa có.
regained() {
	curl -sf --max-time 5 "$GRAPH/_fake/streams" | python3 -c '
import json, sys, datetime
video, since, keep = sys.argv[1], float(sys.argv[2]), sys.argv[3] == "1"
best = None
for l in json.load(sys.stdin) or []:
    if l["video"] != video:
        continue
    at = datetime.datetime.fromisoformat(l["opened"].replace("Z", "+00:00")).timestamp()
    if at < since and keep and l.get("closed") is None:
        at = since
    if at >= since and (best is None or at < best):
        best = at
print("" if best is None else f"{best:.1f}")' "$1" "$2" "$3" 2>/dev/null
}

# wait_streams <lần hỏng> <lúc gỡ hỏng> <keep>: mỗi video của fb.json, chờ luồng được nhận lại sau lúc đó.
wait_streams() {
	local fault="$1" start="$2" keep="$3" v at
	for v in $(python3 -c 'import json,sys; print(" ".join(p["videoId"] for p in json.load(open(sys.argv[1]))["pages"]))' "$OUT/fb.json"); do
		while :; do
			at="$(regained "$v" "$start" "$keep")"
			if [ -n "$at" ]; then
				echo "$fault,$v,$start,$at" >>"$LIVE"
				log "$fault: luồng video $v mở lại sau $(python3 -c "print(round($at - $start, 1))") s"
				break
			fi
			if gt "$(now_s) - $start" "$WAIT"; then
				echo "$fault,$v,$start," >>"$LIVE"
				log "$fault: luồng video $v chưa mở lại sau $WAIT s"
				break
			fi
			sleep 0.5
		done
	done
}


echo "fault,start,recovered" >"$CSV"
echo "fault,video,start,regained" >"$LIVE"
T0="$(now_s)"

sleep_until "$T0 + $GAP"
log "giết crm-worker"
S="$(now_s)"
kill_role worker
start_role worker
wait_streams "giết crm-worker" "$S" 0 &
STREAMS=$!
wait_recovered "giết crm-worker" "$S"
wait "$STREAMS"

sleep_until "$T0 + 2 * $GAP"
log "giết crm-api"
S="$(now_s)"
kill_role api
start_role api
wait_recovered "giết crm-api" "$S"

if [ "$PG_DOWN" = 0 ]; then
	log "xong (bỏ bước ngắt PostgreSQL: CHAOS_PG_DOWN=0)"
	exit 0
fi
sleep_until "$T0 + 3 * $GAP"
log "ngắt PostgreSQL $PG_DOWN s"
dc stop postgres >/dev/null 2>&1
sleep "$PG_DOWN"
dc start postgres >/dev/null 2>&1
S="$(now_s)"
wait_streams "ngắt PostgreSQL ${PG_DOWN} s" "$S" 1 &
STREAMS=$!
wait_recovered "ngắt PostgreSQL ${PG_DOWN} s" "$S"
wait "$STREAMS"
log "xong"
