#!/usr/bin/env bash
# Bài thử tải M5 (kế hoạch M5 Task 219 / M5-36, ADR-42): ba vai trò CRM chạy như tiến trình trên máy thử (fb_lib.sh),
# api nối hai Valkey của compose dev; k6 gọi thẳng API web của CRM bằng khóa dịch vụ (loadtest/k6/m5). Mỗi bài một đợt
# flash sale mới; sau bài: chờ chỉ số phút cuối được ghi, crm verify-flash, loadkit m5report chấm ngưỡng.
#   loadtest/m5_run.sh all                      sáu bài lần lượt
#   loadtest/m5_run.sh flash_spike,web_read     một số bài
#   DURATION=20s loadtest/m5_run.sh flash_spike chạy khói (rút thời lượng phần giữ tải)
# Bài: flash_spike, flash_per_customer, flash_sustained, flash_abandon, flash_close_with_holds, web_read.
# Biến (mặc định): CRM_PG_PORT (5442), PG_DB (crm), CRM_S3_PORT (8333), CRM_VALKEY_CACHE_PORT (6390),
# CRM_VALKEY_GATE_PORT (6391), API_PORT (4290), INGEST_PORT (4291), GRAPH_PORT (4293, không dùng), ACCESS_PORT (24322),
# LOADTEST_OUT (loadtest/out).
# Cần: go, k6, psql, python3; PostgreSQL, SeaweedFS, Valkey của dự án compose đang chạy (make dev hay
# `make db-up` + profile valkey), database PG_DB không có dữ liệu thật.
# Kết quả ở $LOADTEST_OUT/m5-<giờ>/: report.md, <bài>/{k6.json,before.json,after.json,verify.txt,k6.txt}, stats.csv
# (CPU, bộ nhớ của từng vai trò mỗi 15 s), log từng vai trò. Thoát 1 khi có bài KHÔNG ĐẠT.
set -euo pipefail
SPEC="${1:?cách dùng: loadtest/m5_run.sh all|<bài>[,<bài>…]}"
ALL=(flash_spike flash_per_customer flash_sustained flash_abandon flash_close_with_holds web_read)
if [ "$SPEC" = all ]; then
	TESTS=("${ALL[@]}")
else
	IFS=, read -r -a TESTS <<<"$SPEC"
fi
for t in "${TESTS[@]}"; do
	case " ${ALL[*]} " in *" $t "*) ;; *) echo "bài lạ: $t" >&2; exit 2 ;; esac
done

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PG_HOST=127.0.0.1 CRM_PG_PORT="${CRM_PG_PORT:-5442}" PG_DB="${PG_DB:-crm}" CRM_S3_PORT="${CRM_S3_PORT:-8333}"
export API_PORT="${API_PORT:-4290}" INGEST_PORT="${INGEST_PORT:-4291}" GRAPH_PORT="${GRAPH_PORT:-4293}"
export ACCESS_PORT="${ACCESS_PORT:-24322}"
# Khóa ứng dụng Meta giả (chuỗi dev công khai của tools/fakegraph): fb_lib.sh đòi; bài M5 không gọi Meta.
export META_SECRET="fa4e9a9f00000000000000000000de00" META_APP="1626301872631379"
# Valkey của compose dev (mật khẩu dev công khai của docker/compose.dev.yml) và khóa HMAC giả chỉ cho máy thử.
VC_PORT="${CRM_VALKEY_CACHE_PORT:-6390}" VG_PORT="${CRM_VALKEY_GATE_PORT:-6391}"
export VALKEY_CACHE_URL="valkey://crm_cache:crm-cache-dev-0123456789abcdef@127.0.0.1:${VC_PORT}/0"
export VALKEY_GATE_URL="valkey://crm_gate:crm-gate-dev-0123456789abcdef@127.0.0.1:${VG_PORT}/0"
export CACHE_KEY_SECRET="loadtest-cache-0123456789abcdef0123456789abcdef"
# QĐ23: giới hạn web_key mặc định 1.200/phút chặn tải flash sale qua một khóa (ghi ở "Việc phát hiện thêm" Task 219);
# bài thử nâng để đo CRM chứ không đo giới hạn của khóa. Giới hạn theo SĐT, IP, cổng nhận vào giữ mặc định.
export RATE_LIMITS_JSON='{"web_key": {"max": 10000000, "window_s": 60, "fallback_max": 5000000}}'
STAFF=2
STAMP="$(date +%Y%m%d%H%M%S)"
export OUT="${LOADTEST_OUT:-$ROOT/loadtest/out}/m5-$(date +%Y%m%d-%H%M%S)"
export BIN="$OUT/bin"
OWNER_URL="postgres://crm_owner:crm_owner_dev@${PG_HOST}:${CRM_PG_PORT}/${PG_DB}?sslmode=disable"
API="http://127.0.0.1:${API_PORT}"
# shellcheck source=loadtest/fb_lib.sh
. "$ROOT/loadtest/fb_lib.sh"
mkdir -p "$OUT/access" "$BIN"
log() { echo "m5_run.sh: $(date '+%H:%M:%S') $*" >&2; }
iso() { date -u +%Y-%m-%dT%H:%M:%SZ; }

PIDS=()
cleanup() {
	set +e # dọn hết dù một bước lỗi
	stop_roles
	for p in ${PIDS[@]+"${PIDS[@]}"}; do kill "$p" 2>/dev/null || true; done
	wait 2>/dev/null || true
	rm -f "$OUT/access/tokens.json" "$OUT/key.json" # thẻ, khóa thử chỉ sống trong lúc chạy (gitleaks quét thư mục)
}
trap cleanup EXIT

log "máy: $(nproc 2>/dev/null || sysctl -n hw.ncpu) lõi; $(uptime)"
{
	echo "lõi: $(nproc 2>/dev/null || sysctl -n hw.ncpu)"
	free -m 2>/dev/null | sed -n 2p || true
	uptime
} >"$OUT/may.txt"
(cd "$ROOT" && go build -o "$BIN/crm" ./cmd/crm && go build -o "$BIN/loadkit" ./tools/loadkit)

"$BIN/loadkit" access -listen "127.0.0.1:${ACCESS_PORT}" -team "127.0.0.1:${ACCESS_PORT}" -out "$OUT/access" -n "$STAFF" \
	-ttl 6h 2>"$OUT/access.log" &
PIDS+=($!)
for _ in $(seq 50); do [ -s "$OUT/access/tokens.json" ] && break; sleep 0.2; done

log "migrate, seed, chạy ba vai trò"
(role_vars migrate && "$BIN/crm") >"$OUT/migrate.log" 2>&1
"$BIN/loadkit" seed -db "$OWNER_URL" -n "$STAFF" -tenant-key loadtest 2>>"$OUT/loadkit.log"
for r in "${ROLES[@]}"; do start_role "$r"; done
wait_healthy 60
TOK="$OUT/access/tokens.json"
"$BIN/loadkit" m5key -db "$OWNER_URL" -api "$API" -tokens "$TOK" -out "$OUT/key.json" 2>>"$OUT/loadkit.log"

# CPU (%) và bộ nhớ (RSS) của từng vai trò mỗi 15 s; Valkey, PostgreSQL theo docker stats nếu có.
(
	while :; do
		ts="$(date +%s)"
		for r in "${ROLES[@]}"; do
			p="$(role_pid "$r")"
			[ -n "$p" ] || continue
			ps -o %cpu=,rss= -p "$p" 2>/dev/null | awk -v ts="$ts" -v n="crm-$r" '{printf "%s,%s,%s%%,%.1fMiB / 0B\n", ts, n, $1, $2/1024}'
		done >>"$OUT/stats.csv"
		docker stats --no-stream --format '{{.Name}},{{.CPUPerc}},{{.MemUsage}}' 2>/dev/null |
			grep -E 'postgres|valkey' | sed "s/^/${ts},/" >>"$OUT/stats.csv" || true
		sleep 15
	done
) &
PIDS+=($!)

# one <bài>: (gọi kèm ||, nên set -e không áp trong hàm: mọi bước cần dừng có `|| return 1`) dựng đợt, đo trước, k6, chờ, đo sau, verify-flash, chấm. In bảng vào report.md; trả 1 khi không đạt.
one() {
	local t="$1" dir="$OUT/$1" slots=500 limit=1 hold=10 expect extra=() k6env=() wait_after=65
	mkdir -p "$dir"
	case "$t" in
	flash_sustained) slots=15000 ;;
	flash_abandon) hold=5 ;;
	flash_close_with_holds) hold=5 ;;
	web_read) slots=100 extra=(-catalog 30) ;;
	esac
	expect="$slots"
	[ "$t" = flash_close_with_holds ] && expect=200 # số người giữ suất lúc đóng
	"$BIN/loadkit" m5sale -db "$OWNER_URL" -api "$API" -tokens "$TOK" -slots "$slots" -limit "$limit" -hold "$hold" \
		-start-in 20s ${extra[@]+"${extra[@]}"} -out "$dir/sale.json" 2>>"$OUT/loadkit.log" ||
		{ log "bài $t: không dựng được đợt"; tail -5 "$OUT/loadkit.log" >&2; return 1; }
	local sale start
	sale="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["saleId"])' "$dir/sale.json")"
	start="$(iso)"
	"$BIN/loadkit" m5stats -db "$OWNER_URL" -from "$start" -out "$dir/before.json" || return 1
	log "bài $t (đợt $sale, $slots suất) bắt đầu"
	k6env=(API="$API" KEY="$OUT/key.json" SALE="$dir/sale.json" RUN="${STAMP}${t}" TOKENS="$TOK"
		SUMMARY_OUT="$dir/k6.json")
	[ -n "${DURATION:-}" ] && k6env+=(DURATION="$DURATION")
	set +e
	env "${k6env[@]}" k6 run -q --no-color "$ROOT/loadtest/k6/m5/$t.js" >"$dir/k6.txt" 2>"$dir/k6.log"
	local k6rc=$?
	set -e
	log "bài $t: k6 xong (mã $k6rc)"
	# Bài đóng đợt: người số lẻ để hết hạn giữ 5 phút (tính từ lúc mua, ~ đầu bài) trước khi kiểm.
	[ "$t" = flash_close_with_holds ] && wait_after=260
	sleep "$wait_after" # chỉ số theo phút của phút cuối được ghi; nhịp hủy đơn nháp chạy
	"$BIN/loadkit" m5stats -db "$OWNER_URL" -from "$start" -out "$dir/after.json" || return 1
	local vrc=0
	if [ "$t" != web_read ]; then
		set +e
		(role_vars worker && "$BIN/crm" verify-flash --sale "$sale") >"$dir/verify.txt" 2>&1
		vrc=$?
		set -e
	fi
	{
		echo
		echo "## $t (đợt $sale; k6 mã $k6rc; verify-flash mã $vrc)"
		echo
	} >>"$OUT/report.md"
	set +e
	"$BIN/loadkit" m5report -mode "$t" -slots "$expect" -k6 "$dir/k6.json" -before "$dir/before.json" \
		-after "$dir/after.json" -verify-exit "$vrc" >>"$OUT/report.md"
	local rc=$?
	set -e
	if [ "$t" != web_read ]; then
		{ echo; echo '```'; tail -30 "$dir/verify.txt"; echo '```'; } >>"$OUT/report.md"
	fi
	return "$rc"
}

{
	echo "# Thử tải M5 ($(date -u '+%Y-%m-%d %H:%M UTC'))"
	echo
	echo "Máy: $(head -1 "$OUT/may.txt"); $(sed -n 2p "$OUT/may.txt"); lúc bắt đầu: $(tail -1 "$OUT/may.txt")"
} >"$OUT/report.md"
FAILED=()
for t in "${TESTS[@]}"; do
	one "$t" || FAILED+=("$t")
done
{
	echo
	echo "Lúc kết thúc: $(uptime)"
	echo
	echo "CPU, bộ nhớ lớn nhất theo tiến trình (stats.csv):"
	echo
	awk -F, '{gsub(/%/,"",$3); split($4,m," "); v=m[1]+0; u=m[1]; gsub(/[0-9.]/,"",u)
		if ($3+0 > c[$2]) c[$2]=$3+0
		if (v > mm[$2]) {mm[$2]=v; mu[$2]=u}}
		END {for (k in c) printf "- %s: CPU %.0f%%, bộ nhớ %s%s\n", k, c[k], mm[k], mu[k]}' "$OUT/stats.csv" 2>/dev/null | sort || true
	echo
	if [ ${#FAILED[@]} -eq 0 ]; then echo "KẾT QUẢ: mọi bài ĐẠT"; else echo "KẾT QUẢ: KHÔNG ĐẠT ở ${FAILED[*]}"; fi
} >>"$OUT/report.md"
cat "$OUT/report.md"
log "kết quả ở $OUT"
[ ${#FAILED[@]} -eq 0 ]
