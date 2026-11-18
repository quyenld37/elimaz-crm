#!/usr/bin/env bash
# Hỏng giữa tải (kế hoạch M0 Task 22, ADR-42). run.sh gọi script này song song với heavy.js. Lần lượt, mỗi lần cách
# nhau CHAOS_GAP giây (mặc định 180): giết crm-api, giết crm-worker, ngắt PostgreSQL CHAOS_PG_DOWN giây (mặc định 60).
# Tiến trình bị giết được khởi động lại ngay (vai trò của bộ giám sát ở máy thật). Suốt bài, thêm 2 lệnh
# command.selftest mỗi giây (dedupe_key lt.<run>.cmd.<n>) để đếm lệnh gửi hai lần.
#
# Ghi $OUT/chaos.csv: fault,start,recovered (epoch giây). "Trở lại" là lúc cả ba vai trò healthy và không còn việc
# pending nào quá hạn 2 s; tính từ lúc giết (khởi động lại ngay sau đó) hoặc lúc bật lại PostgreSQL. Quá CHAOS_WAIT giây
# (mặc định 300) thì cột recovered để trống.
#
# Biến bắt buộc: PROJECT (dự án compose), RUN, OUT, TENANT_KEY. Không bao giờ chạy trên máy có dữ liệu thật.
set -uo pipefail
: "${PROJECT:?}" "${RUN:?}" "${OUT:?}" "${TENANT_KEY:?}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GAP="${CHAOS_GAP:-180}"
PG_DOWN="${CHAOS_PG_DOWN:-60}"
WAIT="${CHAOS_WAIT:-300}"
CSV="$OUT/chaos.csv"

dc() { docker compose -p "$PROJECT" -f "$ROOT/docker/compose.dev.yml" -f "$ROOT/loadtest/compose.loadtest.yml" --profile app "$@"; }
psql_q() { dc exec -T postgres psql -U postgres -d crm -tAq -c "$1" 2>/dev/null; }
now() { perl -MTime::HiRes=time -e 'printf "%.1f\n", time'; }
log() { echo "chaos: $(date '+%H:%M:%S') $*" >&2; }

healthy() {
	local s id st
	for s in crm-ingest crm-api crm-worker; do
		id="$(dc ps -q "$s" 2>/dev/null)"
		[ -n "$id" ] || return 1
		st="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$id" 2>/dev/null)"
		[ "$st" = healthy ] || return 1
	done
}

backlog_ok() {
	local n
	n="$(psql_q "SELECT count(*) FROM platform.jobs WHERE status = 'pending' AND run_at < now() - interval '2 seconds'")"
	[ "$n" = 0 ]
}

# wait_recovered <tên lần hỏng> <lúc gỡ hỏng>: chờ tới khi trở lại hoặc quá WAIT giây, ghi một dòng CSV.
wait_recovered() {
	local fault="$1" start="$2" t
	while :; do
		t="$(now)"
		if healthy && backlog_ok; then
			echo "$fault,$start,$t" >>"$CSV"
			log "$fault: trở lại sau $(perl -e "printf '%.1f', $t - $start") s"
			return
		fi
		if perl -e "exit(!($t - $start > $WAIT))"; then
			echo "$fault,$start," >>"$CSV"
			log "$fault: chưa trở lại sau $WAIT s"
			return
		fi
		sleep 1
	done
}

commands_loop() {
	local tenant n=0
	tenant="$(psql_q "SELECT tenant_id FROM platform.tenant_routes WHERE kind = 'selftest' AND external_key = '$TENANT_KEY'")"
	[ -n "$tenant" ] || { log "không tìm thấy tenant của $TENANT_KEY"; return 1; }
	while :; do
		n=$((n + 1))
		psql_q "INSERT INTO platform.jobs (tenant_id, kind, mode, dedupe_key, on_lease_expired, max_attempts, expires_at)
			VALUES ('$tenant', 'command.selftest', 'command', 'lt.$RUN.cmd.$n', 'in_doubt', 8, now() + interval '3 minutes')" >/dev/null
		sleep 0.5
	done
}

# sleep_until <epoch>
sleep_until() { perl -MTime::HiRes=time,sleep -e "my \$d = $1 - time; sleep(\$d) if \$d > 0"; }

echo "fault,start,recovered" >"$CSV"
T0="$(now)"
commands_loop &
CMD_PID=$!
trap 'kill $CMD_PID 2>/dev/null' EXIT

sleep_until "$(perl -e "print $T0 + $GAP")"
log "giết crm-api"
S="$(now)"
dc kill crm-api >/dev/null 2>&1
dc start crm-api >/dev/null 2>&1
wait_recovered "giết crm-api" "$S"

sleep_until "$(perl -e "print $T0 + 2 * $GAP")"
log "giết crm-worker"
S="$(now)"
dc kill crm-worker >/dev/null 2>&1
dc start crm-worker >/dev/null 2>&1
wait_recovered "giết crm-worker" "$S"

sleep_until "$(perl -e "print $T0 + 3 * $GAP")"
log "ngắt PostgreSQL $PG_DOWN s"
dc stop postgres >/dev/null 2>&1
sleep "$PG_DOWN"
dc start postgres >/dev/null 2>&1
wait_recovered "ngắt PostgreSQL ${PG_DOWN} s" "$(now)"
log "xong"
