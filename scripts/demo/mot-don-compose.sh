#!/usr/bin/env bash
# Chạy kịch bản "một đơn đi trọn" (scripts/demo/mot-don-di-tron.sh, Task 33) trên compose dev (kế hoạch M1, Task 395):
#   1. sinh khóa webhook hãng giả mới (openssl rand), chỉ nằm trong biến môi trường của lần chạy này;
#   2. bật devaccess (thẻ Access giả) và fakecarrier; migrate, seed-dev; dựng lại crm-ingest, crm-api, crm-worker cùng
#      khóa đó (ingest, worker nối hãng giả);
#   3. lấy mã kho MAIN, tài khoản hãng fake-1 (câu SQL của RUNBOOK) và thẻ của chủ shop dev từ devaccess;
#   4. chạy đủ 6 bước, rồi in sổ COD, tồn kho của đơn vừa chạy (đọc bằng chủ bảng trong ngữ cảnh tenant).
#
# Dùng: make demo-mot-don   (cần make db-up và make image trước; nhiều phiên song song: đặt COMPOSE_PROJECT và cổng)
# Biến: COMPOSE_PROJECT (mặc định crm), CRM_API_PORT (4190), CRM_FAKECARRIER_PORT (4194), CRM_DEVACCESS_PORT (4195),
#       DEMO_EMAIL (mặc định chu-shop@elimaz.dev). Cần: docker, bash, curl, jq, openssl.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
PROJECT=${COMPOSE_PROJECT:-crm}
API_PORT=${CRM_API_PORT:-4190}
FC_PORT=${CRM_FAKECARRIER_PORT:-4194}
DA_PORT=${CRM_DEVACCESS_PORT:-4195}
EMAIL=${DEMO_EMAIL:-chu-shop@elimaz.dev}

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
die() {
  log "LỖI: $*"
  exit 1
}
dc() { docker compose -p "$PROJECT" -f "$ROOT/docker/compose.dev.yml" --profile app --profile fakecarrier --profile devaccess "$@"; }
# sql: chạy câu SQL bằng chủ bảng trong transaction có ngữ cảnh tenant ELIMAZ (RLS ép cả chủ bảng); in dòng kết quả.
sql() {
  dc exec -T postgres psql -U postgres -d crm -v ON_ERROR_STOP=1 -At -F ' ' -q <<SQL | grep -v -E '^(BEGIN|COMMIT)$' || true
BEGIN;
SELECT set_config('app.tenant_id', (SELECT id::text FROM platform.tenants WHERE name = 'ELIMAZ'), true) \\g /dev/null
$1
COMMIT;
SQL
}

for c in docker curl jq openssl; do
  command -v "$c" >/dev/null || die "thiếu lệnh $c"
done
docker image inspect crm:dev >/dev/null 2>&1 || die "chưa có image crm:dev: chạy make image"
dc ps --status running --services 2>/dev/null | grep -qx postgres || die "compose $PROJECT chưa chạy: make db-up"

# (1) Khóa hãng giả của lần chạy này: compose đọc CRM_FAKECARRIER_SECRET, không ghi ra tệp.
CRM_FAKECARRIER_SECRET=$(openssl rand -hex 32)
export CRM_FAKECARRIER_SECRET

# (2) devaccess trước api: api nạp bộ CA (SSL_CERT_DIR) lúc gọi HTTPS đầu tiên, chứng chỉ phải có sẵn.
log "bật devaccess, fakecarrier (dựng image công cụ nếu cần)"
dc up -d --wait --build devaccess fakecarrier >&2
log "migrate, seed-dev ${EMAIL}"
dc run --rm -T crm-migrate >&2
dc run --rm -T crm-migrate seed-dev "$EMAIL" >&2
log "dựng lại crm-ingest, crm-api, crm-worker với khóa hãng giả mới"
dc up -d --wait --no-deps --force-recreate crm-ingest crm-api crm-worker >&2

# (3) Mã kho, tài khoản hãng, thẻ.
read -r WAREHOUSE_ID CARRIER_ACCOUNT_ID <<<"$(sql "SELECT w.id, a.id FROM inventory.warehouses w
  JOIN fulfillment.carrier_accounts a ON a.warehouse_id = w.id WHERE w.code = 'MAIN' AND a.external_account_ref = 'fake-1';")"
[[ -n "${CARRIER_ACCOUNT_ID:-}" ]] || die "không thấy kho MAIN / tài khoản fake-1 (seed-dev?)"
CRM_ACCESS_JWT=$(curl -fsS "http://127.0.0.1:${DA_PORT}/token?email=${EMAIL}") || die "devaccess không phát thẻ"
export WAREHOUSE_ID CARRIER_ACCOUNT_ID CRM_ACCESS_JWT
export FAKECARRIER_URL="http://127.0.0.1:${FC_PORT}"

# (4) Kịch bản 6 bước.
out=$(mktemp)
trap 'rm -f "$out"' EXIT
"$ROOT/scripts/demo/mot-don-di-tron.sh" "http://127.0.0.1:${API_PORT}" | tee "$out"
order_id=$(sed -n 's/^Xong: đơn \([0-9a-f-]*\) .*/\1/p' "$out")
[[ -n "$order_id" ]] || die "không đọc được mã đơn từ kết quả kịch bản"

echo "Sổ COD của vận đơn (account balance):"
sql "SELECT b.account, b.balance FROM cash.cod_balances_v b JOIN fulfillment.shipments s ON s.id = b.shipment_id
  WHERE s.order_id = '${order_id}' ORDER BY b.account;" | sed 's/^/   /'
echo "Tổng mọi dòng sổ COD (phải 0): $(sql 'SELECT coalesce(sum(amount), 0) FROM cash.cod_ledger_entries;')"
echo "Tồn của biến thể (on_hand held in_transit):"
sql "SELECT b.on_hand, b.held, b.in_transit FROM inventory.stock_balances b
  JOIN ord.order_items l ON l.variant_id = b.variant_id WHERE l.order_id = '${order_id}';" | sed 's/^/   /'
