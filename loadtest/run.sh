#!/usr/bin/env bash
# Chạy một bài thử tải M0 (kế hoạch M0 Task 22, ADR-42) trên compose dev và in bảng chấm ngưỡng.
#   loadtest/run.sh normal|heavy|chaos|soak
# Thời lượng mặc định theo kế hoạch (30 phút, 15 phút, 15 phút, 8 giờ); DURATION=5m ghi đè (chạy khói, ngâm rút ngắn).
#
# Biến (mặc định trong ngoặc): COMPOSE_PROJECT (crm), CRM_PG_PORT (5442), CRM_API_PORT (4190), CRM_INGEST_PORT (4191),
# LOADTEST_ACCESS_PORT (24222), LOADTEST_ACCESS_LISTEN (127.0.0.1; trên Linux đặt địa chỉ bridge của Docker để
# host-gateway tới được), CRM_IMAGE (crm:loadtest; LOADTEST_BUILD=1 dựng lại), STAFF (50), LOADTEST_OUT
# (loadtest/out). Cần: docker, k6, go, perl. PostgreSQL, SeaweedFS, Jaeger của dự án phải đang chạy (make db-up).
# Kết quả: $LOADTEST_OUT/<mode>-<giờ>/ (report.md, k6.json, sse.json, db.json, series.jsonl, stats.csv, chaos.csv);
# tokens.json bị xóa khi bài kết thúc.
set -euo pipefail
MODE="${1:?cách dùng: loadtest/run.sh normal|heavy|chaos|soak}"
case "$MODE" in normal | heavy | chaos | soak) ;; *) echo "bài lạ: $MODE" >&2; exit 2 ;; esac

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PROJECT="${COMPOSE_PROJECT:-crm}"
export CRM_PG_PORT="${CRM_PG_PORT:-5442}" CRM_API_PORT="${CRM_API_PORT:-4190}" CRM_INGEST_PORT="${CRM_INGEST_PORT:-4191}"
export LOADTEST_ACCESS_PORT="${LOADTEST_ACCESS_PORT:-24222}" CRM_IMAGE="${CRM_IMAGE:-crm:loadtest}"
export TENANT_KEY="loadtest"
STAFF="${STAFF:-50}"
OWNER_URL="postgres://crm_owner:crm_owner_dev@127.0.0.1:${CRM_PG_PORT}/crm?sslmode=disable"
SECRET="dev-0123456789abcdef0123456789abcdef" # SELFTEST_WEBHOOK_SECRET giả của docker/compose.dev.yml
export RUN="${MODE}$(date +%Y%m%d%H%M%S)"
export OUT="${LOADTEST_OUT:-$ROOT/loadtest/out}/${MODE}-$(date +%Y%m%d-%H%M%S)"
export LOADTEST_ACCESS_DIR="$OUT/access"
mkdir -p "$LOADTEST_ACCESS_DIR"
chmod 755 "$OUT" "$LOADTEST_ACCESS_DIR"

dc() { docker compose -p "$PROJECT" -f "$ROOT/docker/compose.dev.yml" -f "$ROOT/loadtest/compose.loadtest.yml" --profile app "$@"; }
log() { echo "run.sh: $(date '+%H:%M:%S') $*" >&2; }
iso() { date -u +%Y-%m-%dT%H:%M:%SZ; }

PIDS=()
cleanup() {
	for p in ${PIDS[@]+"${PIDS[@]}"}; do kill "$p" 2>/dev/null || true; done
	wait 2>/dev/null || true
	rm -f "$LOADTEST_ACCESS_DIR/tokens.json" # thẻ thử chỉ dùng trong lúc chạy; gitleaks quét cả thư mục làm việc
}
trap cleanup EXIT

log "máy lúc bắt đầu: $(uptime)"
echo "$(uptime)" >"$OUT/uptime-truoc.txt"

if [ "${LOADTEST_BUILD:-0}" = 1 ] || ! docker image inspect "$CRM_IMAGE" >/dev/null 2>&1; then
	log "dựng image $CRM_IMAGE"
	docker build -q -t "$CRM_IMAGE" "$ROOT" >/dev/null
fi
(cd "$ROOT" && go build -o "$OUT/loadkit" ./tools/loadkit)

# Máy chủ khóa Access giả (khóa, CA mới mỗi lượt) → ca.pem, tokens.json; api tạo lại để tin CA mới.
"$OUT/loadkit" access -listen "${LOADTEST_ACCESS_LISTEN:-127.0.0.1}:${LOADTEST_ACCESS_PORT}" -team "host.docker.internal:${LOADTEST_ACCESS_PORT}" \
	-out "$LOADTEST_ACCESS_DIR" -n "$STAFF" -ttl 10h 2>"$OUT/access.log" &
PIDS+=($!)
for _ in $(seq 50); do [ -s "$LOADTEST_ACCESS_DIR/tokens.json" ] && break; sleep 0.2; done

log "migrate, seed, dựng ba vai trò"
dc up -d --wait postgres >/dev/null 2>&1 # dựng lại với init của lớp phủ nếu chưa có
dc run --rm -T crm-migrate >/dev/null
"$OUT/loadkit" seed -db "$OWNER_URL" -n "$STAFF" -tenant-key "$TENANT_KEY" 2>>"$OUT/loadkit.log"
dc up -d --wait --no-deps --force-recreate crm-ingest crm-api crm-worker

API="http://127.0.0.1:${CRM_API_PORT}"
INGEST="http://127.0.0.1:${CRM_INGEST_PORT}"
"$OUT/loadkit" sse -base "$API" -tokens "$LOADTEST_ACCESS_DIR/tokens.json" -clients "$STAFF" -run "$RUN" \
	-out "$OUT/sse.json" 2>>"$OUT/loadkit.log" &
SSE_PID=$!
PIDS+=("$SSE_PID")

# Lấy mẫu: docker stats mỗi 15 s; số đo database mỗi 5 phút (bài ngâm cần đường theo thời gian).
START="$(iso)"
(
	n=0
	while :; do
		ts="$(date +%s)"
		docker stats --no-stream --format '{{.Name}},{{.CPUPerc}},{{.MemUsage}}' $(dc ps -q 2>/dev/null) 2>/dev/null |
			sed "s/^/$ts,/" >>"$OUT/stats.csv" || true
		if [ $((n % 20)) = 0 ]; then
			"$OUT/loadkit" stats -db "$OWNER_URL" -run "$RUN" -from "$START" >>"$OUT/series.jsonl" 2>>"$OUT/loadkit.log" || true
		fi
		n=$((n + 1))
		sleep 15
	done
) &
SAMPLER_PID=$!
PIDS+=("$SAMPLER_PID")
sleep 3 # client SSE nối xong trước khi gửi

SCRIPT="$MODE"
[ "$MODE" = chaos ] && SCRIPT=heavy
CHAOS_PID=""
if [ "$MODE" = chaos ]; then
	"$ROOT/loadtest/k6/chaos.sh" 2>>"$OUT/chaos.log" &
	CHAOS_PID=$!
	PIDS+=("$CHAOS_PID")
fi
log "k6 $SCRIPT.js bắt đầu (run $RUN)"
set +e
INGEST="$INGEST" API="$API" SECRET="$SECRET" TENANT_KEY="$TENANT_KEY" RUN="$RUN" \
	TOKENS="$LOADTEST_ACCESS_DIR/tokens.json" SUMMARY_OUT="$OUT/k6.json" \
	k6 run -q --no-color "$ROOT/loadtest/k6/$SCRIPT.js" 2>"$OUT/k6.log" | tee "$OUT/k6.txt"
K6_EXIT=${PIPESTATUS[0]}
set -e
log "k6 xong (mã $K6_EXIT)"
[ -n "$CHAOS_PID" ] && { wait "$CHAOS_PID" || true; }
END_LOAD="$(iso)"

# Chờ hàng đợi cạn (tối đa 5 phút), rồi chờ qua một phút để Flusher ghi histogram của phút cuối.
for _ in $(seq 60); do
	other="$("$OUT/loadkit" stats -db "$OWNER_URL" -run "$RUN" -from "$START" 2>/dev/null |
		sed -n 's/.*"inbound_other":\([0-9]*\).*/\1/p' || true)" # lỗi tạm của database: thử lại, không thoát
	[ "$other" = 0 ] && break
	sleep 5
done
sleep 65
kill -INT "$SSE_PID" 2>/dev/null || true
wait "$SSE_PID" 2>/dev/null || true
{ kill "$SAMPLER_PID" && wait "$SAMPLER_PID"; } 2>/dev/null || true
"$OUT/loadkit" stats -db "$OWNER_URL" -run "$RUN" -from "$START" >"$OUT/db.json"
echo "$(uptime)" >"$OUT/uptime-sau.txt"

{
	echo "Lượt $RUN: tải từ $START tới $END_LOAD (UTC); k6 thoát mã $K6_EXIT."
	echo "Máy lúc bắt đầu: $(cat "$OUT/uptime-truoc.txt")"
	echo "Máy lúc kết thúc: $(cat "$OUT/uptime-sau.txt")"
	echo
} >"$OUT/report.md"
CHAOS_ARG=()
[ "$MODE" = chaos ] && CHAOS_ARG=(-chaos "$OUT/chaos.csv")
set +e
"$OUT/loadkit" report -mode "$MODE" -start "$START" -k6 "$OUT/k6.json" -sse "$OUT/sse.json" -db "$OUT/db.json" \
	-series "$OUT/series.jsonl" -stats "$OUT/stats.csv" -project "$PROJECT" ${CHAOS_ARG[@]+"${CHAOS_ARG[@]}"} >>"$OUT/report.md"
REPORT_EXIT=$?
set -e
cat "$OUT/report.md"
log "kết quả ở $OUT"
exit "$REPORT_EXIT"
