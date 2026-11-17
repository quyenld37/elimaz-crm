#!/usr/bin/env bash
# Chạy search-plan-probe.sql trên PostgreSQL diễn tập và kiểm máy: (a) có Seq Scan, không dùng index trigram;
# (b) câu bên trong hàm dùng index trigram. In hai kế hoạch, rồi "SEARCH PLAN OK ..." và thoát 0 khi đạt.
# Dùng: search-plan-probe.sh [tệp lưu đầu ra]
set -euo pipefail
. "$(dirname "$0")/drill-lib.sh"

out=${1:-$(mktemp "${TMPDIR:-/tmp}/crm-search-plan.XXXXXX")}
trap drill_down EXIT
drill_gen_secrets
drill_up

# stdout (kết quả EXPLAIN của (a)) và stderr (NOTICE của auto_explain cho (b)) để hai tệp riêng: hai luồng không giữ thứ tự với nhau.
dc exec -T postgres psql -X -v ON_ERROR_STOP=1 -U postgres -d postgres -f - \
  <"$(dirname "$0")/search-plan-probe.sql" >"$out" 2>"$out.notice" || {
  cat "$out" "$out.notice" >&2
  die "search-plan-probe.sql lỗi"
}
cat "$out" "$out.notice"

section() { awk -v s="$2" -v e="$3" 'index($0, s){on=1; next} index($0, e){on=0} on' "$1"; }
plan_a=$(section "$out" '==== (a)' '==== (b)')
plan_b=$(section "$out.notice" '==== (b)' '==== kiem ket qua')

printf '%s\n' "$plan_a" | grep -q 'Seq Scan on names' || die "(a) không có Seq Scan trên probe.names"
if printf '%s\n' "$plan_a" | grep -q 'names_name_search_trgm'; then die "(a) lại dùng index trigram"; fi
printf '%s\n' "$plan_b" | grep -q 'Index Scan on names_name_search_trgm' || die "(b) câu trong hàm không dùng index trigram"
if printf '%s\n' "$plan_b" | grep -q 'Seq Scan on names'; then die "(b) câu trong hàm vẫn Seq Scan"; fi
grep -q 'KIEM KET QUA OK' "$out.notice" || die "thiếu dòng KIEM KET QUA OK"

ms_a=$(printf '%s\n' "$plan_a" | sed -n 's/.*Execution Time: \([0-9.]*\) ms.*/\1/p' | head -1)
ms_b=$(printf '%s\n' "$plan_b" | sed -n 's/.*duration: \([0-9.]*\) ms.*/\1/p' | head -1)
echo "SEARCH PLAN OK direct=seq_scan direct_ms=$ms_a function=trgm_index function_ms=$ms_b output=$out $out.notice"
