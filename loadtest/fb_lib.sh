# shellcheck shell=bash
# Phần chung của fb_run.sh và fb_chaos.sh (bộ thử tải M4, kế hoạch M4 Task 180): cấu hình và vòng đời ba vai trò CRM
# (ingest, api, worker) chạy như tiến trình trên máy thử, cùng một tệp nhị phân `crm` dựng từ mã đang thử.
# Chỉ dùng biến do fb_run.sh đặt: BIN, OUT, PG_HOST, CRM_PG_PORT, PG_DB, CRM_S3_PORT, API_PORT, INGEST_PORT,
# GRAPH_PORT, ACCESS_PORT, META_SECRET, META_APP. Không bao giờ chạy với database có dữ liệu thật.

ROLES=(ingest api worker)
# Khóa mã hóa trường giả (chỉ máy thử; cùng dạng keyspec với test của config).
FB_FIELD_KEYS="k1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="

# role_vars <vai trò>: xuất biến môi trường của vai trò vào shell hiện tại (gọi trong subshell).
role_vars() {
	export CRM_ENV=dev CRM_ROLE="$1" OTEL_ENABLED=false
	export DATABASE_URL="postgres://crm_app:crm_app_dev@${PG_HOST}:${CRM_PG_PORT}/${PG_DB}?sslmode=disable"
	export META_APP_SECRET="$META_SECRET"
	local graph="http://127.0.0.1:${GRAPH_PORT}"
	local s3=(S3_ENDPOINT="http://127.0.0.1:${CRM_S3_PORT}" S3_ACCESS_KEY_ID=crm-app-dev
		S3_SECRET_ACCESS_KEY=crm-app-dev-only-not-a-secret FILES_PUBLIC_BASE_URL="http://127.0.0.1:${CRM_S3_PORT}/crm-public")
	case "$1" in
	migrate)
		export DATABASE_OWNER_URL="postgres://crm_owner:crm_owner_dev@${PG_HOST}:${CRM_PG_PORT}/${PG_DB}?sslmode=disable"
		unset META_APP_SECRET
		;;
	ingest)
		export PORT="$INGEST_PORT" SPOOL_DIR="$OUT/spool" GOMEMLIMIT=230MiB SELFTEST_WEBHOOK_SECRET=dev-0123456789abcdef0123456789abcdef
		export META_VERIFY_TOKEN="loadtest-verify-0123456789abcdef0123456789" HOOKS_PUBLIC_BASE_URL="http://127.0.0.1:${INGEST_PORT}"
		;;
	api)
		export "${s3[@]}"
		export PORT="$API_PORT" GOMEMLIMIT=460MiB HEALTH_TOKEN=dev-0123456789abcdef0123456789abcdef
		export ACCESS_TEAM_DOMAIN="127.0.0.1:${ACCESS_PORT}" ACCESS_AUD=loadtest-aud ACCESS_MFA_CLAIM=amr ACCESS_MFA_VALUES=mfa
		export SSL_CERT_FILE="$OUT/access/ca.pem" CONSOLE_ORIGINS=http://localhost:4192 FIELD_ENC_KEYS="$FB_FIELD_KEYS"
		export META_APP_ID="$META_APP" META_LOGIN_CONFIG_ID=1000000000000001
		export META_OAUTH_REDIRECT_URL=http://localhost:4192/kenh/facebook/xong
		export META_GRAPH_BASE_URL="$graph" META_STREAMING_BASE_URL="$graph"
		;;
	worker)
		export "${s3[@]}"
		export GOMEMLIMIT=460MiB IMAGER_URL=http://127.0.0.1:9 FIELD_ENC_KEYS="$FB_FIELD_KEYS" META_APP_ID="$META_APP"
		export META_GRAPH_BASE_URL="$graph" META_STREAMING_BASE_URL="$graph"
		;;
	*)
		echo "vai trò lạ: $1" >&2
		return 2
		;;
	esac
}

# start_role <vai trò>: chạy vai trò ở nền, log vào $OUT/<vai trò>.log, mã tiến trình ở $OUT/pids/<vai trò>.
start_role() {
	mkdir -p "$OUT/pids" "$OUT/spool"
	(
		role_vars "$1"
		exec "$BIN/crm"
	) >>"$OUT/$1.log" 2>&1 &
	echo $! >"$OUT/pids/$1"
}

role_pid() { cat "$OUT/pids/$1" 2>/dev/null; }

# kill_role <vai trò>: giết đúng tiến trình vai trò mà bộ thử đã chạy (SIGKILL: như sập máy, không tắt êm).
kill_role() {
	local p
	p="$(role_pid "$1")" || true
	if [ -n "$p" ]; then kill -9 "$p" 2>/dev/null || true; fi
	return 0
}

# stop_roles: tắt êm mọi vai trò (SIGTERM), chờ tối đa 30 s rồi SIGKILL. Không dừng giữa chừng dù set -e (vai trò đã
# chết thì kill lỗi).
stop_roles() {
	local r p
	for r in "${ROLES[@]}"; do
		p="$(role_pid "$r")" || true
		[ -n "$p" ] && { kill "$p" 2>/dev/null || true; }
	done
	for _ in $(seq 30); do
		local alive=0
		for r in "${ROLES[@]}"; do
			p="$(role_pid "$r")" || true
			if [ -n "$p" ] && kill -0 "$p" 2>/dev/null; then alive=1; fi
		done
		[ "$alive" = 0 ] && return 0
		sleep 1
	done
	for r in "${ROLES[@]}"; do kill_role "$r"; done
}

# role_healthy <vai trò>: tiến trình còn sống và `crm healthcheck` của vai trò qua (ingest, api: /health/live;
# worker: nhịp sống trong database).
role_healthy() {
	local p
	p="$(role_pid "$1")" || return 1
	[ -n "$p" ] && kill -0 "$p" 2>/dev/null || return 1
	(
		role_vars "$1"
		"$BIN/crm" healthcheck
	) >/dev/null 2>&1
}

all_healthy() {
	local r
	for r in "${ROLES[@]}"; do role_healthy "$r" || return 1; done
}

# wait_healthy <giây>: chờ cả ba vai trò healthy; quá hạn thì in đuôi log và trả 1.
wait_healthy() {
	local i
	for i in $(seq "$1"); do
		all_healthy && return 0
		sleep 1
	done
	for r in "${ROLES[@]}"; do
		echo "== $r.log" >&2
		tail -20 "$OUT/$r.log" >&2
	done
	return 1
}

# pg_q <câu>: chạy một câu chỉ đọc bằng chủ bảng (psql trên máy; số đo, không ghi).
pg_q() {
	PGPASSWORD=crm_owner_dev psql -h "$PG_HOST" -p "$CRM_PG_PORT" -U crm_owner -d "$PG_DB" -tAq -c "$1" 2>/dev/null
}

# now_s: giờ hiện tại, giây Unix có phần thập phân.
now_s() { python3 -c 'import time; print(f"{time.time():.1f}")'; }
