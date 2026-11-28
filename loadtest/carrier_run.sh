#!/usr/bin/env bash
# Bài thử tải webhook hãng của M3 (kế hoạch M3 Task M3-48, ADR-42): ba vai trò CRM chạy như tiến trình trên máy thử,
# hãng giả tools/fakecarrier (tra kiện trả sau 200 ms, không tự gửi webhook), k6 carrier_webhooks.js (20 tin/giây
# trong 15 phút vào /hooks/fake/<route>, mỗi vận đơn 6 tin); in bảng chấm.
#   loadtest/carrier_run.sh              tải 15 phút, 3.000 vận đơn
#   DURATION=1m SHIPMENTS=200 loadtest/carrier_run.sh   chạy khói (SHIPMENTS = RATE × giây / 6 để mỗi vận đơn đủ 6 bước)
# Biến (mặc định): CRM_PG_PORT (5442), PG_DB (crm), CRM_S3_PORT (8333), API_PORT (4290), INGEST_PORT (4291),
# FAKECARRIER_PORT (4294), ACCESS_PORT (24322), RATE (20), DURATION (15m), SHIPMENTS (3000), LOADTEST_OUT (loadtest/out).
# Cần: go, k6, psql, python3, curl; PostgreSQL, SeaweedFS của dự án compose đang chạy (make db-up), database PG_DB chưa
# có dữ liệu thật (bài thử migrate, thêm tenant "Thử tải").
# Kết quả ở $LOADTEST_OUT/carrier-<giờ>/: report.md (bảng chấm), k6.json, log từng vai trò. Thoát 1 khi có tiêu chí
# KHÔNG ĐẠT.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PG_HOST=127.0.0.1 CRM_PG_PORT="${CRM_PG_PORT:-5442}" PG_DB="${PG_DB:-crm}" CRM_S3_PORT="${CRM_S3_PORT:-8333}"
export API_PORT="${API_PORT:-4290}" INGEST_PORT="${INGEST_PORT:-4291}" GRAPH_PORT="${GRAPH_PORT:-4293}"
export ACCESS_PORT="${ACCESS_PORT:-24322}" FAKECARRIER_PORT="${FAKECARRIER_PORT:-4294}"
export META_SECRET="fa4e9a9f00000000000000000000de00" META_APP="1626301872631379" # máy Meta giả: không dùng ở bài này
# Bí mật webhook, Bearer của hãng giả trong bài thử (chuỗi công khai của máy thử, không phải bí mật thật).
export LT_FAKECARRIER_URL="http://127.0.0.1:${FAKECARRIER_PORT}" LT_FAKECARRIER_SECRET="loadtest-fakecarrier-0123456789abcdef"
RATE="${RATE:-20}" DURATION="${DURATION:-15m}" SHIPMENTS="${SHIPMENTS:-3000}"
STAFF=50
export RUN="c$(date +%Y%m%d%H%M%S)"
export OUT="${LOADTEST_OUT:-$ROOT/loadtest/out}/carrier-$(date +%Y%m%d-%H%M%S)"
export BIN="$OUT/bin"
OWNER_URL="postgres://crm_owner:crm_owner_dev@${PG_HOST}:${CRM_PG_PORT}/${PG_DB}?sslmode=disable"
API="http://127.0.0.1:${API_PORT}"
INGEST="http://127.0.0.1:${INGEST_PORT}"
# shellcheck source=loadtest/fb_lib.sh
. "$ROOT/loadtest/fb_lib.sh"
mkdir -p "$OUT/access" "$BIN"
log() { echo "carrier_run.sh: $(date '+%H:%M:%S') $*" >&2; }
iso() { date -u +%Y-%m-%dT%H:%M:%SZ; }

PIDS=()
cleanup() {
	set +e
	stop_roles
	for p in ${PIDS[@]+"${PIDS[@]}"}; do kill "$p" 2>/dev/null || true; done
	wait 2>/dev/null || true
	rm -f "$OUT/access/tokens.json" # thẻ thử chỉ dùng trong lúc chạy; gitleaks quét cả thư mục làm việc
}
trap cleanup EXIT

log "máy: $(nproc 2>/dev/null || sysctl -n hw.ncpu) lõi; $(uptime)"
{ echo "lõi: $(nproc 2>/dev/null || sysctl -n hw.ncpu)"; echo "lúc bắt đầu: $(uptime)"; } >"$OUT/may.txt"
(cd "$ROOT" && go build -o "$BIN/crm" ./cmd/crm && go build -o "$BIN/loadkit" ./tools/loadkit &&
	go build -o "$BIN/fakecarrier" ./tools/fakecarrier)

"$BIN/loadkit" access -listen "127.0.0.1:${ACCESS_PORT}" -team "127.0.0.1:${ACCESS_PORT}" -out "$OUT/access" -n "$STAFF" \
	-ttl 4h 2>"$OUT/access.log" &
PIDS+=($!)
PORT="$FAKECARRIER_PORT" FAKECARRIER_WEBHOOK_SECRET="$LT_FAKECARRIER_SECRET" FAKECARRIER_DATA=- FAKECARRIER_TRACK_DELAY=200ms \
	"$BIN/fakecarrier" >"$OUT/fakecarrier.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 50); do [ -s "$OUT/access/tokens.json" ] && curl -sf "$LT_FAKECARRIER_URL/health" >/dev/null && break; sleep 0.2; done

log "migrate, seed, chạy ba vai trò"
(role_vars migrate && "$BIN/crm") >"$OUT/migrate.log" 2>&1
"$BIN/loadkit" seed -db "$OWNER_URL" -n "$STAFF" -tenant-key loadtest 2>>"$OUT/loadkit.log"
for r in "${ROLES[@]}"; do start_role "$r"; done
wait_healthy 60
log "dựng $SHIPMENTS vận đơn chờ thu gom qua API"
"$BIN/loadkit" carrierseed -db "$OWNER_URL" -api "$API" -tokens "$OUT/access/tokens.json" -n "$SHIPMENTS" \
	-out "$OUT/carrier.json" 2>>"$OUT/loadkit.log"
TENANT="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["tenantId"])' "$OUT/carrier.json")"

DEAD0="$(pg_q "SELECT count(*) FROM platform.jobs WHERE status = 'dead'")"
LOCKS0="$(pg_q "SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()")"
START="$(iso)"
log "tải: $RATE tin/giây trong $DURATION"
set +e
INGEST="$INGEST" FAKECARRIER="$LT_FAKECARRIER_URL" SECRET="$LT_FAKECARRIER_SECRET" FIXTURE="$OUT/carrier.json" \
	RATE="$RATE" DURATION="$DURATION" SUMMARY_OUT="$OUT/k6.json" k6 run -q "$ROOT/loadtest/k6/carrier_webhooks.js" \
	>"$OUT/k6.txt" 2>&1
K6_RC=$?
set -e
END="$(iso)"

# Nối webhook của lượt (eventId lt.<run>.<mã hãng>.<lần đổi>) với sự kiện vận đơn CRM ghi (khóa <trạng thái>:<lần đổi>).
lat_sql="SELECT set_config('app.tenant_id', '$TENANT', false);
WITH w AS (
	SELECT e->>'tracking' AS tr, (e->>'status') || ':' || split_part(e->>'eventId', '.', 4) AS k,
		(e->>'sentAt')::timestamptz AS sent
	FROM platform.webhook_deliveries d
	CROSS JOIN LATERAL jsonb_array_elements(convert_from(d.body, 'UTF8')::jsonb -> 'events') e
	WHERE d.received_at >= '$START' AND e->>'eventId' LIKE 'lt.$RUN.%')
SELECT count(*) || ' ' || count(ev.id) || ' ' ||
	coalesce(round((percentile_cont(0.95) WITHIN GROUP (ORDER BY extract(epoch FROM ev.recorded_at - w.sent) * 1000))::numeric, 1), -1) || ' ' ||
	coalesce(round((percentile_cont(0.99) WITHIN GROUP (ORDER BY extract(epoch FROM ev.recorded_at - w.sent) * 1000))::numeric, 1), -1)
FROM w JOIN fulfillment.shipments s ON s.tracking_code = w.tr
LEFT JOIN fulfillment.shipment_events ev ON ev.shipment_id = s.id AND ev.event_key = w.k"
log "chờ hàng đợi cạn (tối đa 120 s)"
for _ in $(seq 60); do
	read -r SENT APPLIED P95 P99 <<<"$(pg_q "$lat_sql" | tail -1)"
	[ "${SENT:-0}" -gt 0 ] && [ "$SENT" = "$APPLIED" ] && break
	sleep 2
done
read -r SENT APPLIED P95 P99 <<<"$(pg_q "$lat_sql" | tail -1)"
DEAD1="$(pg_q "SELECT count(*) FROM platform.jobs WHERE status = 'dead'")"
LOCKS1="$(pg_q "SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()")"
EXHAUSTED="$(pg_q "SELECT coalesce(sum(count), 0) FROM platform.metrics_minute WHERE name = 'tx_retry_exhausted' AND bucket >= '$START'::timestamptz - interval '1 minute'")"
read -r WH_P95 WH_OK WH_BAD ADV_BAD <<<"$(python3 - "$OUT/k6.json" <<'PY'
import json, sys
m = json.load(open(sys.argv[1]))["metrics"]
v = lambda k, f: m.get(k, {}).get("values", {}).get(f, 0)
print(round(v("webhook_ms", "p(95)"), 1), int(v("webhook_ok", "count")), int(v("webhook_bad", "count")), int(v("advance_bad", "count")))
PY
)"
echo "lúc kết thúc: $(uptime)" >>"$OUT/may.txt"

fail=0
row() { # tên, ngưỡng, đo được, đạt (0/1)
	local verdict="ĐẠT"
	[ "$4" = 1 ] || { verdict="KHÔNG ĐẠT"; fail=1; }
	echo "| $1 | $2 | $3 | $verdict |"
}
lt() { python3 -c "import sys; print(1 if 0 <= float(sys.argv[1]) < float(sys.argv[2]) else 0)" "$1" "$2"; }
{
	echo "Lượt $RUN: tải từ $START tới $END (UTC); $RATE tin/giây, $DURATION, $SHIPMENTS vận đơn; k6 mã $K6_RC."
	cat "$OUT/may.txt"
	echo
	echo "| Tiêu chí | Ngưỡng | Đo được | Kết quả |"
	echo "|---|---|---|---|"
	row "Nhận webhook p95" "< 300 ms" "$WH_P95 ms ($WH_OK tin 200)" "$(lt "$WH_P95" 300)"
	row "Không webhook nào khác 200" "0" "$WH_BAD (trang quản trị hãng giả lỗi: $ADV_BAD)" "$([ "$WH_BAD" = 0 ] && [ "$ADV_BAD" = 0 ] && echo 1 || echo 0)"
	row "Webhook tới vận đơn đổi trạng thái p95" "< 5000 ms" "$P95 ms (p99 $P99 ms)" "$(lt "$P95" 5000)"
	row "Mỗi tin hãng được áp" "đã áp = đã gửi" "gửi $SENT, áp $APPLIED" "$([ "${SENT:-0}" -gt 0 ] && [ "$SENT" = "$APPLIED" ] && echo 1 || echo 0)"
	row "deadlocks không tăng" "cuối − đầu = 0" "$LOCKS0 → $LOCKS1" "$([ "$LOCKS0" = "$LOCKS1" ] && echo 1 || echo 0)"
	row "tx_retry_exhausted" "0" "$EXHAUSTED" "$([ "$EXHAUSTED" = 0 ] && echo 1 || echo 0)"
	row "Việc dead mới" "0" "$DEAD0 → $DEAD1" "$([ "$DEAD0" = "$DEAD1" ] && echo 1 || echo 0)"
} >"$OUT/report.md"
cat "$OUT/report.md"
[ "$fail" = 0 ] && [ "$K6_RC" = 0 ]
