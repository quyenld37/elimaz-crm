#!/usr/bin/env bash
# Bài thử tải M2 (kế hoạch M2 Task 88, M2-31; chạy bằng `make perf-m2`, ngoài make ci): api, worker chạy như tiến trình
# trên máy thử; tools/seedperf nạp 1.000.000 khách, 1.300.000 SĐT, 5.000.000 đơn (8.000.000 dòng) vào database đo, tenant
# thứ hai 100.000 khách; rồi k6 m2-search.js (tìm khách 20 lượt/giây, 5 phút) và m2-orders.js (lên đơn 10×, 5 phút); in
# bảng chấm, câu chậm nhất (auto_explain: EXPLAIN ANALYZE, BUFFERS) và dung lượng database.
#   loadtest/m2_run.sh
#   DURATION=1m loadtest/m2_run.sh   chạy khói: tải 1 phút, đợt 20 đơn/giây 10 s, cỡ nhỏ (20.000 khách, 50.000 đơn, 2.000
#                                    khách tenant hai) trừ khi đặt SEED_*
# Biến (mặc định): CRM_PG_PORT (5442), PG_DB (crm), COMPOSE_PROJECT (crm; đọc log PostgreSQL của dự án compose đó),
# CRM_S3_PORT (8333), API_PORT (4290), ACCESS_PORT (24322), SEED_CUSTOMERS (1000000), SEED_ORDERS (5000000),
# SEED_TENANT2 (100000), AUTO_EXPLAIN_MS (50: câu của crm_app từ ngần ấy ms được ghi kế hoạch), LOADTEST_OUT (loadtest/out).
# Cần: go, k6, psql, docker, python3; PostgreSQL, SeaweedFS của dự án compose đang chạy (make db-up), database PG_DB mới
# (seedperf từ chối database có khách của tenant khác). Không bao giờ chạy trên bộ chung crm-shared hay database thật.
# Kết quả ở $LOADTEST_OUT/m2-<giờ>/: report.md (bảng chấm), search.json, orders.json (k6), series.jsonl (m2stats trước
# và sau bài lên đơn), m2-sample.json (không có SĐT: chỉ công thức và mẫu mã), slow.md, log từng vai trò. Thoát 1 khi
# có tiêu chí KHÔNG ĐẠT.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PG_HOST=127.0.0.1 CRM_PG_PORT="${CRM_PG_PORT:-5442}" PG_DB="${PG_DB:-crm}" CRM_S3_PORT="${CRM_S3_PORT:-8333}"
export COMPOSE_PROJECT="${COMPOSE_PROJECT:-crm}"
export API_PORT="${API_PORT:-4290}" INGEST_PORT="${INGEST_PORT:-4291}" GRAPH_PORT="${GRAPH_PORT:-4293}"
export ACCESS_PORT="${ACCESS_PORT:-24322}"
if [ "$COMPOSE_PROJECT" = crm-shared ] || [ "$CRM_PG_PORT" = 5600 ]; then
	echo "m2_run.sh: không chạy trên bộ chung crm-shared (đổi cấu hình role crm_app, nạp hàng triệu dòng)" >&2
	exit 2
fi
# Khóa ứng dụng Meta giả như fb_run.sh (fb_lib.sh đòi; bài M2 không nối Page nào).
export META_SECRET="fa4e9a9f00000000000000000000de00" META_APP="1626301872631379"
STAFF=50
export OUT="${LOADTEST_OUT:-$ROOT/loadtest/out}/m2-$(date +%Y%m%d-%H%M%S)"
export BIN="$OUT/bin"
OWNER_URL="postgres://crm_owner:crm_owner_dev@${PG_HOST}:${CRM_PG_PORT}/${PG_DB}?sslmode=disable"
ADMIN_URL="postgres://postgres:postgres_dev@${PG_HOST}:${CRM_PG_PORT}/${PG_DB}?sslmode=disable"
API="http://127.0.0.1:${API_PORT}"
# shellcheck source=loadtest/fb_lib.sh
. "$ROOT/loadtest/fb_lib.sh"
ROLES=(api worker) # bài M2 không nhận webhook
mkdir -p "$OUT/access" "$BIN"
log() { echo "m2_run.sh: $(date '+%H:%M:%S') $*" >&2; }
iso() { date -u +%Y-%m-%dT%H:%M:%SZ; }
admin_q() { PGPASSWORD=postgres_dev psql -h "$PG_HOST" -p "$CRM_PG_PORT" -U postgres -d "$PG_DB" -v ON_ERROR_STOP=1 -tAq -c "$1"; }

PIDS=()
cleanup() {
	set +e # dọn hết dù một bước lỗi
	stop_roles
	for p in ${PIDS[@]+"${PIDS[@]}"}; do kill "$p" 2>/dev/null || true; done
	wait 2>/dev/null || true
	rm -f "$OUT/access/tokens.json" # thẻ thử chỉ dùng trong lúc chạy; gitleaks quét cả thư mục làm việc
}
trap cleanup EXIT

log "máy: $(nproc 2>/dev/null || sysctl -n hw.ncpu) lõi; $(uptime)"
{ echo "lõi: $(nproc 2>/dev/null || sysctl -n hw.ncpu)"; uptime; } >"$OUT/may.txt"
(cd "$ROOT" && go build -o "$BIN/crm" ./cmd/crm && go build -o "$BIN/loadkit" ./tools/loadkit &&
	go build -o "$BIN/seedperf" ./tools/seedperf)

"$BIN/loadkit" access -listen "127.0.0.1:${ACCESS_PORT}" -team "127.0.0.1:${ACCESS_PORT}" -out "$OUT/access" -n "$STAFF" \
	-ttl 4h 2>"$OUT/access.log" &
PIDS+=($!)
for _ in $(seq 50); do [ -s "$OUT/access/tokens.json" ] && break; sleep 0.2; done

log "migrate, seed nhân viên"
(role_vars migrate && "$BIN/crm") >"$OUT/migrate.log" 2>&1
"$BIN/loadkit" seed -db "$OWNER_URL" -n "$STAFF" -tenant-key loadtest 2>>"$OUT/loadkit.log"
# Câu chậm: auto_explain cho crm_app trong database đo (kết nối mở sau lệnh này, tức là api, worker chạy dưới đây).
# log_timing off: chỉ đếm dòng, buffers (không đo giờ từng nút) để phần đo không làm chậm câu; không ghi giá trị tham số.
admin_q "DO \$\$ DECLARE s text[]; BEGIN FOREACH s SLICE 1 IN ARRAY ARRAY[
	['session_preload_libraries', 'auto_explain'], ['auto_explain.log_min_duration', '${AUTO_EXPLAIN_MS:-50}'],
	['auto_explain.log_analyze', 'on'], ['auto_explain.log_buffers', 'on'], ['auto_explain.log_timing', 'off'],
	['auto_explain.log_nested_statements', 'on'], ['auto_explain.log_parameter_max_length', '0'],
	['auto_explain.log_format', 'text'], ['auto_explain.log_level', 'log']] LOOP
	EXECUTE format('ALTER ROLE crm_app IN DATABASE %I SET %s = %L', current_database(), s[1], s[2]); END LOOP; END \$\$"
for r in "${ROLES[@]}"; do start_role "$r"; done
wait_healthy 60
"$BIN/loadkit" m2seed -db "$OWNER_URL" -api "$API" -tokens "$OUT/access/tokens.json" -out "$OUT/m2.json" 2>>"$OUT/loadkit.log"

log "seedperf: nạp dữ liệu đo"
SIZE=(1000000 5000000 100000)
[ -n "${DURATION:-}" ] && SIZE=(20000 50000 2000) # chạy khói
"$BIN/seedperf" -admin "$ADMIN_URL" -out "$OUT/m2-sample.json" -customers "${SEED_CUSTOMERS:-${SIZE[0]}}" \
	-orders "${SEED_ORDERS:-${SIZE[1]}}" -tenant2 "${SEED_TENANT2:-${SIZE[2]}}" 2>&1 | tee "$OUT/seedperf.log" >&2
admin_q "SELECT pg_size_pretty(pg_database_size(current_database()))" >"$OUT/db-size.txt"
log "dung lượng database sau khi nạp: $(cat "$OUT/db-size.txt")"

K6ENV=(API="$API" TOKENS="$OUT/access/tokens.json" SAMPLE="$OUT/m2-sample.json" M2_FIXTURE="$OUT/m2.json")
if [ -n "${DURATION:-}" ]; then K6ENV+=(BURST_AT=10s BURST_FOR=10s); fi
START_SEARCH="$(iso)"
log "k6 m2-search.js"
set +e
env "${K6ENV[@]}" SUMMARY_OUT="$OUT/search.json" k6 run -q --no-color "$ROOT/loadtest/k6/m2-search.js" \
	>"$OUT/k6-search.txt" 2>"$OUT/k6-search.log"
SEARCH_EXIT=$?
set -e
START_ORDERS="$(iso)"
"$BIN/loadkit" m2stats -db "$OWNER_URL" -fixture "$OUT/m2.json" -from "$START_ORDERS" >"$OUT/series.jsonl"
log "k6 m2-orders.js"
set +e
env "${K6ENV[@]}" SUMMARY_OUT="$OUT/orders.json" k6 run -q --no-color "$ROOT/loadtest/k6/m2-orders.js" \
	>"$OUT/k6-orders.txt" 2>"$OUT/k6-orders.log"
ORDERS_EXIT=$?
set -e
END_LOAD="$(iso)"
log "k6 xong (search mã $SEARCH_EXIT, orders mã $ORDERS_EXIT); chờ hàng đợi cạn"
for _ in $(seq 24); do
	backlog="$("$BIN/loadkit" m2stats -db "$OWNER_URL" -fixture "$OUT/m2.json" -from "$START_ORDERS" 2>/dev/null |
		python3 -c 'import json,sys; print(json.load(sys.stdin)["db"]["backlog"])' 2>/dev/null || true)"
	[ "$backlog" = 0 ] && break
	sleep 5
done
sleep 65 # chỉ số tx_retry của phút cuối được ghi vào metrics_minute
"$BIN/loadkit" m2stats -db "$OWNER_URL" -fixture "$OUT/m2.json" -from "$START_ORDERS" >>"$OUT/series.jsonl"

docker compose -p "$COMPOSE_PROJECT" -f "$ROOT/docker/compose.dev.yml" logs --no-color --since "$START_SEARCH" postgres \
	>"$OUT/postgres.log" 2>&1 || log "không đọc được log PostgreSQL của dự án $COMPOSE_PROJECT"
"$BIN/loadkit" m2slow -log "$OUT/postgres.log" -top 3 >"$OUT/slow.md"
rm -f "$OUT/postgres.log" # log có câu SQL của mọi vai trò; kế hoạch câu chậm đã ở slow.md

{
	echo "Lượt M2: tìm khách từ $START_SEARCH, lên đơn từ $START_ORDERS tới $END_LOAD (UTC); k6 search mã $SEARCH_EXIT, orders mã $ORDERS_EXIT."
	echo "Máy ($(head -1 "$OUT/may.txt")); lúc bắt đầu: $(tail -1 "$OUT/may.txt"); lúc kết thúc: $(uptime)"
	echo "Số đo tạm trên máy thử tham khảo: đo lại trên server thử trước chạy thật."
	echo
} >"$OUT/report.md"
set +e
"$BIN/loadkit" m2report -search "$OUT/search.json" -orders "$OUT/orders.json" -series "$OUT/series.jsonl" \
	-sample "$OUT/m2-sample.json" -slow "$OUT/slow.md" >>"$OUT/report.md"
REPORT_EXIT=$?
set -e
cat "$OUT/report.md"
log "kết quả ở $OUT"
exit "$REPORT_EXIT"
