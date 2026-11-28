#!/usr/bin/env bash
# Kịch bản chạy thử "một đơn đi trọn" (kế hoạch M1, Task 33; Điểm dừng M1-A): tạo mẫu 1 biến thể giá 250000 → phiếu nhập
# 5 → ghi sổ → đơn 2 cái ship 30000 → vận đơn ở hãng giả → hãng báo picked_up → hãng báo delivered. In từng bước, thời
# gian và số dư sau bước đó. Mỗi lệnh ghi mang một Idempotency-Key mới.
#
# Dùng: scripts/demo/mot-don-di-tron.sh <base-url>     (ví dụ http://localhost:4190)
#
# Biến môi trường (không ghi bí mật vào tệp):
#   WAREHOUSE_ID, CARRIER_ACCOUNT_ID  bắt buộc: kho MAIN và tài khoản hãng giả fake-1 (seed-dev tạo; RUNBOOK có câu SQL)
#   Xác thực, chọn một:
#     CRM_ACCESS_JWT                  thẻ Cloudflare Access (header Cf-Access-Jwt-Assertion), máy dev, test
#     CF_ACCESS_CLIENT_ID, CF_ACCESS_CLIENT_SECRET   service token chủ shop cấp (staging, đi qua Cloudflare)
#   CRM_ORIGIN       Origin của màn hình (mặc định http://localhost:4192, phải nằm trong CONSOLE_ORIGINS)
#   FAKECARRIER_URL  hãng giả (mặc định http://127.0.0.1:4194; trang /admin chỉ nghe máy nội bộ)
#   PRODUCT_CODE     mã mẫu (mặc định DEMO<giờ phút giây>)
#   WAIT_SECONDS     hạn chờ mỗi bước sau commit (mặc định 10)
#   PROVINCE_CODE, WARD_CODE  mã tỉnh, phường của địa chỉ giao (mặc định 79, 26734: hãng giả có cùng mã; tenant phải
#                    có hai mã đã khớp cho hãng giả trong carrier_locations, xem syncCarrierLocations)
#
# Cần: bash, curl, jq. Không in SĐT, địa chỉ, thẻ.
set -euo pipefail

base="${1:?dùng: $0 <base-url>}"
base="${base%/}"
: "${WAREHOUSE_ID:?thiếu WAREHOUSE_ID}"
: "${CARRIER_ACCOUNT_ID:?thiếu CARRIER_ACCOUNT_ID}"
origin="${CRM_ORIGIN:-http://localhost:4192}"
fc="${FAKECARRIER_URL:-http://127.0.0.1:4194}"
fc="${fc%/}"
code="${PRODUCT_CODE:-DEMO$(date +%H%M%S)}"
wait_s="${WAIT_SECONDS:-10}"
province="${PROVINCE_CODE:-79}"
ward="${WARD_CODE:-26734}"

for c in curl jq; do
  command -v "$c" >/dev/null || { echo "thiếu lệnh $c" >&2; exit 2; }
done

# Header xác thực ghi vào tệp tạm quyền 600 (curl -H @tệp), không đặt trên dòng lệnh curl (ps thấy được).
auth_file="$(mktemp)"
chmod 600 "$auth_file"
trap 'rm -f "$auth_file"' EXIT
if [[ -n "${CRM_ACCESS_JWT:-}" ]]; then
  printf 'Cf-Access-Jwt-Assertion: %s\n' "$CRM_ACCESS_JWT" >"$auth_file"
elif [[ -n "${CF_ACCESS_CLIENT_ID:-}" && -n "${CF_ACCESS_CLIENT_SECRET:-}" ]]; then
  printf 'CF-Access-Client-Id: %s\nCF-Access-Client-Secret: %s\n' "$CF_ACCESS_CLIENT_ID" "$CF_ACCESS_CLIENT_SECRET" >"$auth_file"
else
  echo "thiếu xác thực: đặt CRM_ACCESS_JWT hoặc CF_ACCESS_CLIENT_ID và CF_ACCESS_CLIENT_SECRET" >&2
  exit 2
fi
auth=(-H "@${auth_file}")

new_key() {
  if command -v uuidgen >/dev/null; then
    uuidgen | tr -d '\n'
  elif [[ -r /proc/sys/kernel/random/uuid ]]; then
    tr -d '\n' </proc/sys/kernel/random/uuid
  else
    printf 'k%s%s%s' "$(date +%s)" "$RANDOM" "$RANDOM"
  fi
}

now_ms() {
  local s
  s="$(date +%s%N 2>/dev/null)"
  if [[ "$s" == *N ]]; then # date của macOS không có %N
    echo $(($(date +%s) * 1000))
  else
    echo $((s / 1000000))
  fi
}

# api <method> <path> [thân JSON] <mã mong đợi>: in thân trả lời; mã khác thì báo lỗi (chỉ mã lỗi, không thân) và dừng.
api() {
  local method="$1" path="$2" body="" want
  if [[ $# -eq 4 ]]; then body="$3"; want="$4"; else want="$3"; fi
  local args=(-sS -o /dev/stdout -w '\n%{http_code}' -X "$method" "${auth[@]}")
  if [[ "$method" != GET ]]; then
    args+=(-H "Origin: ${origin}" -H "Content-Type: application/json" -H "Idempotency-Key: $(new_key)" --data "$body")
  fi
  local out status
  out="$(curl "${args[@]}" "${base}${path}")"
  status="${out##*$'\n'}"
  out="${out%$'\n'*}"
  if [[ "$status" != "$want" ]]; then
    echo "LỖI: $method $path → $status (mong $want): $(jq -r '.error // "?"' <<<"$out" 2>/dev/null)" >&2
    exit 1
  fi
  printf '%s' "$out"
}

available() {
  api GET "/api/v1/catalog/products?warehouseId=${WAREHOUSE_ID}&q=${code}" 200 | jq -r '.items[0].available'
}

# wait_until <mô tả> <lệnh in giá trị> <giá trị mong>: hỏi lại mỗi 200 ms tới hạn WAIT_SECONDS.
wait_until() {
  local what="$1" cmd="$2" want="$3" got deadline
  deadline=$(($(date +%s) + wait_s))
  while :; do
    got="$(eval "$cmd")"
    [[ "$got" == "$want" ]] && return 0
    if (($(date +%s) > deadline)); then
      echo "LỖI: quá ${wait_s} s chờ ${what} (đang là ${got})" >&2
      exit 1
    fi
    sleep 0.2
  done
}

t0=0
step() { t0=$(now_ms); echo "$1"; }
done_step() { echo "   ($(($(now_ms) - t0)) ms) $1"; }

step "1. Tạo mẫu ${code} (1 biến thể, giá 250000)"
product="$(api POST /api/v1/catalog/products "$(jq -nc --arg c "$code" \
  '{code:$c,name:("Đầm thử "+$c),status:"active",variants:[{sku:($c+"-DO-S"),color:"Đỏ",size:"S",price:250000}]}')" 201)"
variant="$(jq -r '.variants[0].id' <<<"$product")"
done_step "biến thể ${variant}, available $(available)"

step "2. Phiếu nhập 5 cái, ghi sổ"
doc="$(api POST /api/v1/inventory/documents "$(jq -nc --arg w "$WAREHOUSE_ID" '{warehouseId:$w,kind:"import"}')" 201)"
doc_id="$(jq -r .id <<<"$doc")"
doc="$(api PUT "/api/v1/inventory/documents/${doc_id}/lines" "$(jq -nc --argjson v "$(jq .version <<<"$doc")" \
  --arg var "$variant" '{version:$v,lines:[{variantId:$var,quantity:5,unitCost:120000}]}')" 200)"
doc="$(api POST "/api/v1/inventory/documents/${doc_id}/post" "$(jq -nc --argjson v "$(jq .version <<<"$doc")" '{version:$v}')" 200)"
done_step "phiếu $(jq -r .code <<<"$doc") $(jq -r .status <<<"$doc"), available $(available)"

step "3. Lên đơn 2 cái, ship 30000"
order="$(api POST /api/v1/orders "$(jq -nc --arg var "$variant" --arg province "$province" --arg ward "$ward" '{customer:{fullName:"Khách thử"},
  items:[{variantId:$var,qty:2}],shipAddress:{name:"Khách thử",phone:"+84900000000",line:"Địa chỉ thử",provinceCode:$province,wardCode:$ward},shippingFee:30000}')" 201)"
order_id="$(jq -r .id <<<"$order")"
done_step "đơn $(jq -r .code <<<"$order") $(jq -r .status <<<"$order"), grandTotal $(jq -r .grandTotal <<<"$order"), available $(available)"

# Hãng cần mã địa giới của hãng cho địa chỉ giao: đồng bộ (việc nền) rồi chờ hai mã hết "chưa khớp" (Task 526).
# unmapped_ours: số mã của địa chỉ giao còn chưa khớp với hãng giả; đi hết mọi trang (còn nextCursor thì chưa phải hết).
unmapped_ours() {
  local cursor="" page n=0
  while :; do
    page="$(api GET "/api/v1/carrier-locations/unmapped?carrier=fake&limit=100${cursor:+&cursor=$cursor}" 200)"
    n=$((n + $(jq -r --arg p "$province" --arg w "$ward" '[.items[] | select(.ourCode == $p or .ourCode == $w)] | length' <<<"$page")))
    cursor="$(jq -r '.nextCursor // empty | @uri' <<<"$page")"
    [[ -n "$cursor" ]] || break
  done
  echo "$n"
}
api POST "/api/v1/carrier-accounts/${CARRIER_ACCOUNT_ID}/locations/sync" 202 >/dev/null
wait_until "địa giới khớp với hãng" "unmapped_ours" 0

step "4. Tạo vận đơn ở hãng giả"
shipment="$(api POST "/api/v1/orders/${order_id}/shipments" "$(jq -nc --arg a "$CARRIER_ACCOUNT_ID" '{carrierAccountId:$a}')" 202)"
shipment_id="$(jq -r .id <<<"$shipment")"
wait_until "vận đơn awaiting_pickup" "api GET /api/v1/shipments/${shipment_id} 200 | jq -r .status" awaiting_pickup
shipment="$(api GET "/api/v1/shipments/${shipment_id}" 200)"
tracking="$(jq -r .trackingCode <<<"$shipment")"
done_step "vận đơn $(jq -r .code <<<"$shipment") awaiting_pickup, mã hãng ${tracking}, COD $(jq -r .codAmount <<<"$shipment")"

advance() {
  local to="$1" res
  res="$(curl -sS -X POST -H "Content-Type: application/json" --data "{\"to\":\"${to}\"}" \
    "${fc}/admin/parcels/${tracking}/advance")"
  if [[ "$(jq -r .webhookStatus <<<"$res")" != 200 ]]; then
    echo "LỖI: hãng giả không gửi được webhook (${to}): $(jq -c '{webhookStatus}' <<<"$res" 2>/dev/null)" >&2
    exit 1
  fi
}

step "5. Hãng báo picked_up"
advance picked_up
wait_until "vận đơn shipping" "api GET /api/v1/shipments/${shipment_id} 200 | jq -r .status" shipping
done_step "vận đơn shipping, available $(available) (sổ COD: to_collect = COD, xem RUNBOOK)"

step "6. Hãng báo delivered"
advance delivered
wait_until "đơn delivered" "api GET /api/v1/orders/${order_id} 200 | jq -r .status" delivered
done_step "đơn delivered, vận đơn $(api GET "/api/v1/shipments/${shipment_id}" 200 | jq -r .status), available $(available)"

echo "Xong: đơn ${order_id} đi trọn (hãng giả). Sổ COD đọc bằng SQL ở RUNBOOK mục \"Chạy lát mỏng\"."
