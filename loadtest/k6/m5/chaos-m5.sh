#!/usr/bin/env bash
# Hỏng giữa tải của flash sale (kế hoạch M5 Task 220 / M5-37; FS10, FS11, FS12, HT27). m5_run.sh chaos gọi script này
# sau khi dựng ba vai trò; mỗi bước một đợt mới 500 suất, k6 `flash_spike.js` chạy cả bước, lỗi gây ở giây 15 sau giờ mở,
# bước cách nhau STEP_GAP giây (mặc định 60) tính từ lúc bắt đầu bước trước. Sau mỗi bước: crm verify-flash, đếm đơn
# trùng external_ref, loadkit m5report (chaos_valkey cho a-c, chaos_api cho d, chaos_pg cho e).
#   a: dừng valkey-gate 30 s   b: dừng valkey-cache 30 s   c: FLUSHALL cả hai bằng crm_admin
#   d: SIGKILL api rồi chạy lại ngay   e: dừng PostgreSQL 30 s
# Biến bắt buộc (m5_run.sh đặt): OUT, BIN, API, TOK, OWNER_URL, STAMP, ROOT, COMPOSE_PROJECT và biến của fb_lib.sh.
# CHAOS_STEPS (mặc định a,b,c,d,e) chọn bước; CHAOS_DOWN (30) giây ngừng; DURATION giữ tải (mặc định 50s).
# Không bao giờ dừng bộ chung crm-shared*: thoát 2.
set -uo pipefail
: "${OUT:?}" "${BIN:?}" "${API:?}" "${TOK:?}" "${OWNER_URL:?}" "${STAMP:?}" "${ROOT:?}" "${COMPOSE_PROJECT:?}"
case "$COMPOSE_PROJECT" in crm-shared*) echo "chaos-m5.sh: không gây lỗi trên bộ chung $COMPOSE_PROJECT" >&2; exit 2 ;; esac
# shellcheck source=loadtest/fb_lib.sh
. "$ROOT/loadtest/fb_lib.sh"
STEPS="${CHAOS_STEPS:-a,b,c,d,e}"
DOWN="${CHAOS_DOWN:-30}"
GAP="${STEP_GAP:-60}"
HOLD="${DURATION:-50s}"
FAULT_AT=15 # giây sau giờ mở
ADMIN=(--user crm_admin --pass crm-admin-dev-0123456789abcdef --no-auth-warning)
log() { echo "chaos-m5.sh: $(date '+%H:%M:%S') $*" >&2; }
dc() { docker compose -p "$COMPOSE_PROJECT" -f "$ROOT/docker/compose.dev.yml" --profile valkey "$@"; }
iso() { date -u +%Y-%m-%dT%H:%M:%SZ; }
sleep_until() { python3 -c "import time; d = $1 - time.time(); time.sleep(d) if d > 0 else None"; }

# fault <bước>: gây lỗi rồi gỡ; ghi mốc vào $dir/fault.txt (epoch giây: gây, gỡ).
fault() {
	local s="$1" dir="$2" t0 t1
	t0="$(now_s)"
	case "$s" in
	a) dc stop valkey-gate >/dev/null 2>&1; sleep "$DOWN"; dc start valkey-gate >/dev/null 2>&1 ;;
	b) dc stop valkey-cache >/dev/null 2>&1; sleep "$DOWN"; dc start valkey-cache >/dev/null 2>&1 ;;
	c) for v in valkey-cache valkey-gate; do dc exec -T "$v" valkey-cli "${ADMIN[@]}" FLUSHALL >/dev/null 2>&1; done ;;
	d) kill_role api; start_role api ;;
	e) dc stop postgres >/dev/null 2>&1; sleep "$DOWN"; dc start postgres >/dev/null 2>&1 ;;
	esac
	t1="$(now_s)"
	echo "$t0 $t1" >"$dir/fault.txt"
	log "bước $s: gây lỗi $t0, gỡ $t1"
}

run_step() {
	local s="$1" mode dir slots=500 sale start k6rc vrc dups
	case "$s" in a | b | c) mode=chaos_valkey ;; d) mode=chaos_api ;; e) mode=chaos_pg ;; *) log "bước lạ: $s"; return 2 ;; esac
	dir="$OUT/chaos_$s"
	mkdir -p "$dir"
	"$BIN/loadkit" m5sale -db "$OWNER_URL" -api "$API" -tokens "$TOK" -slots "$slots" -limit 1 -hold 10 -start-in 20s \
		-out "$dir/sale.json" 2>>"$OUT/loadkit.log" || { log "bước $s: không dựng được đợt"; return 1; }
	sale="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["saleId"])' "$dir/sale.json")"
	start="$(iso)"
	"$BIN/loadkit" m5stats -db "$OWNER_URL" -from "$start" -out "$dir/before.json" || return 1
	local open_at
	open_at="$(python3 -c 'import json,sys,datetime; print(datetime.datetime.fromisoformat(json.load(open(sys.argv[1]))["startsAt"].replace("Z","+00:00")).timestamp())' "$dir/sale.json")"
	(sleep_until "$open_at + $FAULT_AT"; fault "$s" "$dir") &
	local fpid=$!
	set +e
	API="$API" KEY="$OUT/key.json" SALE="$dir/sale.json" RUN="${STAMP}chaos$s" TOKENS="$TOK" SUMMARY_OUT="$dir/k6.json" \
		DURATION="$HOLD" PEAK="${PEAK:-500}" k6 run -q --no-color "$ROOT/loadtest/k6/m5/flash_spike.js" >"$dir/k6.txt" 2>"$dir/k6.log"
	k6rc=$?
	wait "$fpid"
	:
	log "bước $s: k6 xong (mã $k6rc); chờ ổn định"
	for r in "${ROLES[@]}"; do # vai trò chết theo lỗi (ngắt PostgreSQL) thì chạy lại như compose restart
		role_healthy "$r" || { kill_role "$r"; start_role "$r"; }
	done
	wait_healthy 120 || log "bước $s: vai trò chưa healthy sau 120 s"
	sleep 70 # chỉ số phút cuối được ghi; việc nền chạy nốt
	"$BIN/loadkit" m5stats -db "$OWNER_URL" -from "$start" -out "$dir/after.json" || return 1
	set +e
	(role_vars worker && "$BIN/crm" verify-flash --sale "$sale") >"$dir/verify.txt" 2>&1
	vrc=$?
	dups="$(pg_q "SELECT count(*) FROM (SELECT external_ref FROM ord.orders WHERE external_ref LIKE 'm5-${STAMP}chaos${s}-%' GROUP BY external_ref HAVING count(*) > 1) d")"
	:
	{
		echo
		echo "## chaos bước $s ($mode; đợt $sale; k6 mã $k6rc; verify-flash mã $vrc; đơn trùng ${dups:-?})"
		echo "gây lỗi / gỡ (epoch): $(cat "$dir/fault.txt" 2>/dev/null)"
		echo
	} >>"$OUT/report.md"
	set +e
	"$BIN/loadkit" m5report -mode "$mode" -slots "$slots" -k6 "$dir/k6.json" -before "$dir/before.json" \
		-after "$dir/after.json" -verify-exit "$vrc" -dups "${dups:-1}" >>"$OUT/report.md"
	local rc=$?
	:
	{ echo; echo '```'; tail -30 "$dir/verify.txt"; echo '```'; } >>"$OUT/report.md"
	return "$rc"
}

FAILED=()
T0="$(now_s)"
i=0
IFS=, read -r -a LIST <<<"$STEPS"
for s in "${LIST[@]}"; do
	sleep_until "$T0 + $i * $GAP"
	run_step "$s" || FAILED+=("$s")
	i=$((i + 1))
done
[ ${#FAILED[@]} -eq 0 ] || { log "KHÔNG ĐẠT ở bước: ${FAILED[*]}"; exit 1; }
