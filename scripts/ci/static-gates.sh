#!/usr/bin/env bash
# Cổng tĩnh của một task (Task 493): một danh sách dùng chung cho job static của .github/workflows/test-remote.yml và
# scripts/agent/ci-task.sh (bản vá scripts/ci/ci-task-tu-xa.patch). Giống phần cổng tĩnh ci-task chạy từ 18/11: build,
# vet, lint, gen-check và sqlc-vet khi đổi nguồn của chúng, catalog-check, jscpd, gitleaks, oasdiff và test-guard so với
# BASE, vuln khi đổi go.mod, console-check khi đổi console, test kiến trúc.
# Dùng: static-gates.sh BASE    In "STATIC XANH" (thoát 0) hoặc "STATIC ĐỎ: <cổng…>" (thoát 1).
# CI_STATIC_DRY=1: chỉ in "RUN <lệnh>" từng cổng sẽ chạy (test của tests/deploy).
set -u
base=${1:?thiếu BASE}
cd "$(git rev-parse --show-toplevel)" || exit 1
fail=""
step() {
  local name=$1 t0=$SECONDS; shift
  echo "== $name"
  if [ "${CI_STATIC_DRY:-0}" = 1 ]; then echo "RUN $*"; return; fi
  if ! "$@"; then fail="$fail $name"; echo "== $name: ĐỎ"; fi
  echo "== $name: $((SECONDS-t0)) giây"
}
changed=$( { git diff --name-only "$base"; git ls-files --others --exclude-standard; } | sort -u )
has() { printf '%s\n' "$changed" | grep -E -q "$1"; }
step build make build
step vet make vet
step lint make lint
# Bỏ bước khi đầu vào của nó không đổi (kết quả chắc chắn giống base, nơi nhánh gốc đã xanh), 19/11.
gen_in='^(go\.mod|go\.sum|tools/versions\.mk|openapi/|queries/|migrations/|sqlc\.yaml|tools/routegen/|internal/httpapi/gen/|internal/httpapi/routes_gen\.go|console/src/api/|console/package|.*/internal/store/)'
if has "$gen_in"; then step gen-check make gen-check; else echo "== gen-check: bỏ (không đổi nguồn sinh hay mã sinh)"; fi
if has '^(queries/|migrations/|sqlc\.yaml)'; then step sqlc-vet make sqlc-vet; else echo "== sqlc-vet: bỏ (không đổi SQL, schema, sqlc.yaml)"; fi
step catalog-check make catalog-check
step jscpd make jscpd
step gitleaks make gitleaks
step oasdiff make oasdiff OASDIFF_BASE=-base-ref="$base"
step test-guard scripts/agent/test-guard.sh "$base"
if has '^(go\.mod|go\.sum)$'; then step vuln make vuln; fi
if has '^console/'; then step console-check make console-check; fi
# Test kiến trúc (luật module, SQL, khóa…) là kiểm tĩnh trên mã nguồn (24/11, Task 197).
# Qua make (make test-part) để có biến test như make test (CRM_TEST_DATABASE_URL: sqlc-vet, views, routes dùng database thử).
step "test kiến trúc" make --no-print-directory test-part RACE=./tests/arch/...
if [ -n "$fail" ]; then echo "STATIC ĐỎ:$fail"; exit 1; fi
echo "STATIC XANH"
