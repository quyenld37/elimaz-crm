#!/usr/bin/env bash
# Bài thử tải M4 (kế hoạch M4 Task 180, HT17 lặp lại cho ca live, ADR-42): ba vai trò CRM chạy như tiến trình trên
# máy thử, máy Meta giả tools/fakegraph (2 Page, 2.000 PSID, luồng SSE live), k6 fb_live.js (10× ca live) cùng
# fb_staff.js (nhân viên), 50 client SSE; in bảng chấm.
#   loadtest/fb_run.sh live      tải 15 phút
#   loadtest/fb_run.sh chaos     như trên + fb_chaos.sh: giết worker, giết api, ngắt PostgreSQL 60 s, cách 3 phút
#   DURATION=1m loadtest/fb_run.sh live   chạy khói
# Biến (mặc định): CRM_PG_PORT (5442), PG_DB (crm), CRM_S3_PORT (8333), COMPOSE_PROJECT (crm; chỉ bài chaos dùng để
# dừng PostgreSQL của dự án compose đó — không bao giờ là bộ chung crm-shared), API_PORT (4290), INGEST_PORT (4291),
# GRAPH_PORT (4293), ACCESS_PORT (24322), RATE (100 sự kiện/giây), LOADTEST_OUT (loadtest/out).
# Cần: go, k6, psql, python3, curl; PostgreSQL, SeaweedFS của dự án compose đang chạy (make db-up), database PG_DB
# chưa có dữ liệu thật (bài thử migrate, thêm tenant "Thử tải").
# Kết quả ở $LOADTEST_OUT/fb-<bài>-<giờ>/: report.md (bảng chấm), live.json, staff.json (k6), sse.json, series.jsonl
# (fbstats trước và sau), stats.csv (CPU, bộ nhớ mỗi 15 s), chaos.csv, live.csv, log từng vai trò. Thoát 1 khi có
# tiêu chí KHÔNG ĐẠT.
set -euo pipefail
MODE="${1:?cách dùng: loadtest/fb_run.sh live|chaos}"
case "$MODE" in live | chaos) ;; *) echo "bài lạ: $MODE" >&2; exit 2 ;; esac

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PG_HOST=127.0.0.1 CRM_PG_PORT="${CRM_PG_PORT:-5442}" PG_DB="${PG_DB:-crm}" CRM_S3_PORT="${CRM_S3_PORT:-8333}"
export COMPOSE_PROJECT="${COMPOSE_PROJECT:-crm}"
export API_PORT="${API_PORT:-4290}" INGEST_PORT="${INGEST_PORT:-4291}" GRAPH_PORT="${GRAPH_PORT:-4293}"
export ACCESS_PORT="${ACCESS_PORT:-24322}"
# Khóa ứng dụng Meta giả (chuỗi dev công khai của tools/fakegraph, không phải bí mật thật).
export META_SECRET="fa4e9a9f00000000000000000000de00" META_APP="1626301872631379"
STAFF=50
STAMP="$(date +%Y%m%d%H%M%S)"
export RUN="fb${STAMP}"
[ "$MODE" = chaos ] && RUN="fbc${STAMP}"
export OUT="${LOADTEST_OUT:-$ROOT/loadtest/out}/fb-${MODE}-$(date +%Y%m%d-%H%M%S)"
export BIN="$OUT/bin"
OWNER_URL="postgres://crm_owner:crm_owner_dev@${PG_HOST}:${CRM_PG_PORT}/${PG_DB}?sslmode=disable"
API="http://127.0.0.1:${API_PORT}"
INGEST="http://127.0.0.1:${INGEST_PORT}"
export GRAPH="http://127.0.0.1:${GRAPH_PORT}"
# shellcheck source=loadtest/fb_lib.sh
. "$ROOT/loadtest/fb_lib.sh"
mkdir -p "$OUT/access" "$BIN"
log() { echo "fb_run.sh: $(date '+%H:%M:%S') $*" >&2; }
iso() { date -u +%Y-%m-%dT%H:%M:%SZ; }

PIDS=()
cleanup() {
	set +e # dọn hết dù một bước lỗi (tiến trình đã chết)
	stop_roles
	for p in ${PIDS[@]+"${PIDS[@]}"}; do kill "$p" 2>/dev/null || true; done
	wait 2>/dev/null || true
	rm -f "$OUT/access/tokens.json" # thẻ thử chỉ dùng trong lúc chạy; gitleaks quét cả thư mục làm việc
}
trap cleanup EXIT

log "máy: $(nproc 2>/dev/null || sysctl -n hw.ncpu) lõi; $(uptime)"
{ echo "lõi: $(nproc 2>/dev/null || sysctl -n hw.ncpu)"; uptime; } >"$OUT/may.txt"
(cd "$ROOT" && go build -o "$BIN/crm" ./cmd/crm && go build -o "$BIN/loadkit" ./tools/loadkit &&
	go build -o "$BIN/fakegraph" ./tools/fakegraph)

"$BIN/loadkit" access -listen "127.0.0.1:${ACCESS_PORT}" -team "127.0.0.1:${ACCESS_PORT}" -out "$OUT/access" -n "$STAFF" \
	-ttl 4h 2>"$OUT/access.log" &
PIDS+=($!)
PORT="$GRAPH_PORT" FAKEGRAPH_PAGES=2 FAKEGRAPH_PSIDS=2000 FAKEGRAPH_APP_SECRET="$META_SECRET" FAKEGRAPH_APP_ID="$META_APP" \
	CRM_ENV=dev "$BIN/fakegraph" >"$OUT/fakegraph.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 50); do [ -s "$OUT/access/tokens.json" ] && curl -sf "$GRAPH/_fake/streams" >/dev/null && break; sleep 0.2; done

log "migrate, seed, chạy ba vai trò"
(role_vars migrate && "$BIN/crm") >"$OUT/migrate.log" 2>&1
"$BIN/loadkit" seed -db "$OWNER_URL" -n "$STAFF" -tenant-key loadtest 2>>"$OUT/loadkit.log"
for r in "${ROLES[@]}"; do start_role "$r"; done
wait_healthy 60
"$BIN/loadkit" fbseed -db "$OWNER_URL" -api "$API" -graph "$GRAPH" -tokens "$OUT/access/tokens.json" -out "$OUT/fb.json" \
	2>>"$OUT/loadkit.log"

START="$(iso)"
"$BIN/loadkit" fbstats -db "$OWNER_URL" -fixture "$OUT/fb.json" -run "$RUN" -from "$START" >"$OUT/series.jsonl"
"$BIN/loadkit" sse -base "$API" -tokens "$OUT/access/tokens.json" -clients "$STAFF" -run "$RUN" -out "$OUT/sse.json" \
	-fixture "$OUT/fb.json" -db "$OWNER_URL" 2>>"$OUT/loadkit.log" &
SSE_PID=$!
PIDS+=("$SSE_PID")

# CPU (%) và bộ nhớ (RSS) của từng vai trò mỗi 15 s, cùng dạng dòng với docker stats của bộ M0.
(
	while :; do
		ts="$(date +%s)"
		for r in "${ROLES[@]}"; do
			p="$(role_pid "$r")"
			[ -n "$p" ] || continue
			ps -o %cpu=,rss= -p "$p" 2>/dev/null | awk -v ts="$ts" -v n="crm-$r" '{printf "%s,%s,%s%%,%.1fMiB / 0B\n", ts, n, $1, $2/1024}'
		done >>"$OUT/stats.csv"
		sleep 15
	done
) &
PIDS+=($!)
sleep 3 # client SSE nối xong trước khi gửi

CHAOS_PID=""
if [ "$MODE" = chaos ]; then
	"$ROOT/loadtest/fb_chaos.sh" 2>>"$OUT/chaos.log" &
	CHAOS_PID=$!
	PIDS+=("$CHAOS_PID")
fi
log "k6 fb_live.js + fb_staff.js bắt đầu (lượt $RUN)"
K6ENV=(INGEST="$INGEST" API="$API" GRAPH="$GRAPH" SECRET="$META_SECRET" RUN="$RUN" TOKENS="$OUT/access/tokens.json"
	FIXTURE="$OUT/fb.json" PSIDS=2000)
set +e
env "${K6ENV[@]}" SUMMARY_OUT="$OUT/staff.json" k6 run -q --no-color "$ROOT/loadtest/k6/fb_staff.js" \
	>"$OUT/k6-staff.txt" 2>"$OUT/k6-staff.log" &
STAFF_PID=$!
env "${K6ENV[@]}" SUMMARY_OUT="$OUT/live.json" k6 run -q --no-color "$ROOT/loadtest/k6/fb_live.js" \
	>"$OUT/k6-live.txt" 2>"$OUT/k6-live.log"
LIVE_EXIT=$?
wait "$STAFF_PID"
STAFF_EXIT=$?
set -e
log "k6 xong (fb_live mã $LIVE_EXIT, fb_staff mã $STAFF_EXIT)"
[ -n "$CHAOS_PID" ] && { wait "$CHAOS_PID" || true; }
END_LOAD="$(iso)"

# Kết thúc phiên live (đối chiếu cuối phiên), chờ hàng đợi cạn (tối đa 5 phút), rồi qua một phút để histogram của
# phút cuối được ghi.
"$BIN/loadkit" fbend -api "$API" -graph "$GRAPH" -tokens "$OUT/access/tokens.json" -fixture "$OUT/fb.json" \
	2>>"$OUT/loadkit.log" || log "không kết thúc được phiên live"
for _ in $(seq 60); do
	backlog="$("$BIN/loadkit" fbstats -db "$OWNER_URL" -fixture "$OUT/fb.json" -run "$RUN" -from "$START" 2>/dev/null |
		python3 -c 'import json,sys; print(json.load(sys.stdin)["db"]["backlog"])' 2>/dev/null || true)"
	[ "$backlog" = 0 ] && break
	sleep 5
done
sleep 65
kill -INT "$SSE_PID" 2>/dev/null || true
wait "$SSE_PID" 2>/dev/null || true
"$BIN/loadkit" fbstats -db "$OWNER_URL" -fixture "$OUT/fb.json" -run "$RUN" -from "$START" -graph "$GRAPH" >>"$OUT/series.jsonl"

{
	echo "Lượt $RUN: tải từ $START tới $END_LOAD (UTC); k6 fb_live mã $LIVE_EXIT, fb_staff mã $STAFF_EXIT."
	echo "Máy ($(head -1 "$OUT/may.txt")); lúc bắt đầu: $(tail -1 "$OUT/may.txt"); lúc kết thúc: $(uptime)"
	echo
} >"$OUT/report.md"
CHAOS_ARG=()
[ "$MODE" = chaos ] && CHAOS_ARG=(-chaos "$OUT/chaos.csv" -regain "$OUT/live.csv")
REPORT_MODE=fb
[ "$MODE" = chaos ] && REPORT_MODE=fbchaos
set +e
"$BIN/loadkit" fbreport -mode "$REPORT_MODE" -start "$START" -live "$OUT/live.json" -staff "$OUT/staff.json" \
	-sse "$OUT/sse.json" -series "$OUT/series.jsonl" -stats "$OUT/stats.csv" ${CHAOS_ARG[@]+"${CHAOS_ARG[@]}"} >>"$OUT/report.md"
REPORT_EXIT=$?
set -e
cat "$OUT/report.md"
log "kết quả ở $OUT"
exit "$REPORT_EXIT"
