# Lệnh dev của CRM (kế hoạch M0 bản 5). Task 3 thêm `ci`, Task 4 và 6 thêm `gen`.
#
# Cổng mặc định đúng kế hoạch (PostgreSQL 5442, Jaeger UI 16686, OTLP 4318). Nhiều phiên chạy song song
# trên một máy thì mỗi phiên đặt tên dự án compose và cổng riêng, ví dụ:
#   make db-up COMPOSE_PROJECT=crm-t02 CRM_PG_PORT=5452 CRM_S3_PORT=8440 CRM_S3_OFFSITE_PORT=8441
#   make spike-test CRM_PG_PORT=5452
# Test đọc chuỗi kết nối từ CRM_TEST_DATABASE_URL (crm_app); mặc định dựng từ CRM_PG_PORT.

include tools/versions.mk

COMPOSE_PROJECT ?= crm
CRM_PG_PORT ?= 5442
export CRM_PG_PORT
CRM_TEST_DATABASE_URL ?= postgres://crm_app:crm_app_dev@127.0.0.1:$(CRM_PG_PORT)/crm?sslmode=disable
export CRM_TEST_DATABASE_URL

CRM_S3_PORT ?= 8333
CRM_S3_OFFSITE_PORT ?= 8334
CRM_IMAGER_PORT ?= 4193
export CRM_S3_PORT CRM_S3_OFFSITE_PORT CRM_IMAGER_PORT

# Image chính của compose dev theo dự án compose (việc treo Task 395): phiên song song không ghi đè image của nhau.
# CI đặt CRM_IMAGE=crm:<mã commit>.
CRM_IMAGE ?= crm:$(COMPOSE_PROJECT)
export CRM_IMAGE

COMPOSE := docker compose -p $(COMPOSE_PROJECT) -f docker/compose.dev.yml

# Dịch vụ M1 có profile riêng; `dev` chỉ bật profile nào đã có Dockerfile (Task M1-42, M1-31, M1-45 thêm).
DEV_PROFILES := $(if $(wildcard docker/imager/Dockerfile),--profile imager) \
	$(if $(wildcard tools/fakecarrier/Dockerfile),--profile fakecarrier) \
	$(if $(wildcard docker/filesync/Dockerfile),--profile filesync)

.PHONY: db-up db-down db-reset dev probe-test spike-test test-imager test-imager-compose image app-up app-down image-check verify-drill demo-mot-don

db-up: ## Dựng PostgreSQL 18, Jaeger, SeaweedFS (kho chính, kho ngoài giả, tạo bucket), chờ tới khi sẵn sàng
	$(COMPOSE) up -d --wait

dev: ## Như db-up, cộng các dịch vụ M1 đã có mã (imager, fakecarrier, filesync)
	$(COMPOSE) $(DEV_PROFILES) up -d --wait --build

probe-test: ## Phép thử SeaweedFS (Task M1-40) trên compose dev: make dev trước; ghi số đo vào docs/do-luong/seaweedfs-m1.md
	go test -tags probe -count=1 -v ./tests/probe/...

# Test của imager cần libvips: chạy trong stage `test` của docker/imager/Dockerfile (cùng bản libvips với image chạy).
# --init: tini gặt tiến trình con mồ côi để test nhóm tiến trình bị giết thấy đúng. IMAGER_REQUIRE_VIPS=1: ca cần
# libvips, prlimit không được tự bỏ qua.
test-imager: ## Test của internal/imager, cmd/imager với libvips thật (Task M1-42)
	docker build -q -f docker/imager/Dockerfile --target test -t crm-imager-test . >/dev/null
	docker run --rm --init -e IMAGER_REQUIRE_VIPS=1 crm-imager-test \
		go test -race -count=1 $(TEST_IMAGER_FLAGS) ./internal/imager/... ./cmd/imager/...

test-imager-compose: ## Container imager của compose dev: không biến bí mật, không tới PostgreSQL, chỉ đọc (dựng lại imager)
	$(COMPOSE) --profile imager up -d --wait --build imager imager-port postgres seaweedfs
	COMPOSE_PROJECT=$(COMPOSE_PROJECT) CRM_IMAGER_PORT=$(CRM_IMAGER_PORT) go test -tags imagercompose -count=1 -run '^TestImagerCompose_' -v ./internal/imager/

# ---- Image chính và ba vai trò trên compose dev (Task 21). Cổng máy: CRM_API_PORT (4190), CRM_INGEST_PORT (4191).
image: ## Dựng image chính $(CRM_IMAGE) (mặc định crm:<dự án compose>; Go tĩnh, distroless, không root)
	docker build -t $(CRM_IMAGE) .

app-up: ## Migrate một lần rồi chạy crm-ingest, crm-api, crm-worker từ $(CRM_IMAGE) (make db-up và make image trước)
	$(COMPOSE) --profile app run --rm -T crm-migrate
	$(COMPOSE) --profile app up -d --wait --no-deps crm-ingest crm-api crm-worker

app-down: ## Gỡ ba vai trò (giữ database)
	$(COMPOSE) --profile app rm -sf crm-migrate crm-ingest crm-api crm-worker

demo-mot-don: ## Kịch bản "một đơn đi trọn" trên compose dev: hãng giả, thẻ Access giả, khóa sinh lúc chạy (Task 395; make db-up, make image trước)
	COMPOSE_PROJECT=$(COMPOSE_PROJECT) scripts/demo/mot-don-compose.sh

image-check: ## Kiểm image: health 200, healthy, uid khác 0, không .env*, kích thước (Task 21 Step 1; make db-up trước)
	COMPOSE_PROJECT=$(COMPOSE_PROJECT) scripts/ops/image-check.sh

verify-drill: ## Diễn tập sao lưu và kiểm bản sao lưu hằng tuần trên compose diễn tập crm-t02 (Task 21 Step 2)
	scripts/backup/verify-drill.sh

db-down: ## Dừng container, giữ dữ liệu
	$(COMPOSE) down

db-reset: ## Xóa container và volume dữ liệu của dự án compose này
	$(COMPOSE) down -v

spike-test: ## Test phép đo flash sale trên PostgreSQL dev (module riêng spikes/flashsale)
	cd spikes/flashsale && go test -race -count=1 ./...

fuzz-xlsx: ## Fuzz xlsx.Read 60 s trên byte ngẫu nhiên: không panic, không treo (Task M1-38; chạy khi đổi gói xlsx hay nâng excelize)
	go test -run '^$$' -fuzz '^FuzzRead$$' -fuzztime 60s -fuzzminimizetime 5s ./internal/platform/xlsx/

# ---- Cổng kiểm (Task 3). `make ci` là lưới cuối; CI trên máy chủ git chạy đúng lệnh này.
# Task 4 thêm sinh mã sqlc và sqlc vet; Task 6 thêm ogen, bảng route, oasdiff.
.PHONY: mutation ci-tools ci build vet lint test vuln catalog catalog-check jscpd test-guard gitleaks gen gen-check sqlc-vet oasdiff console-deps console-check

ci: build vet lint gen-check sqlc-vet test vuln oasdiff catalog-check jscpd test-guard gitleaks console-check ## Toàn bộ cổng kiểm phần đã có

gen: console-deps ## Sinh lại mã từ nguồn: sqlc (queries/ -> internal/**/internal/store), ogen, bảng route và client màn hình (openapi/openapi.yaml -> console/src/api)
	sqlc generate
	ogen -config openapi/ogen.yml -target internal/httpapi/gen -package gen -clean openapi/openapi.yaml
	go run ./tools/routegen -in openapi/openapi.yaml -out internal/httpapi/routes_gen.go
	cd console && npm run --silent gen

gen-check: gen ## Mã sinh phải khớp bản trong git (kể cả tệp sinh mới chưa commit)
	git diff --exit-code -- internal console/src/api
	git ls-files --others --exclude-standard -- 'internal/*/internal/store/*' 'internal/httpapi/gen/*' internal/httpapi/routes_gen.go 'console/src/api/*' | (! grep .)

# ---- Màn hình (Task 17). Phiên bản ghim trong console/package-lock.json; lý do chọn: docs/chon-cong-cu-console.md.
console-deps: ## Cài đúng phụ thuộc đã ghim của console (bỏ qua khi lock không đổi)
	@if [ ! -f console/node_modules/.package-lock.json ] || [ console/package-lock.json -nt console/node_modules/.package-lock.json ]; then \
		cd console && npm ci --no-audit --no-fund; fi

console-check: console-deps ## tsc (TypeScript 7), eslint, prettier, vitest, bản dựng, gitleaks trên console/dist
	cd console && npx tsc --noEmit
	cd console && npx eslint .
	cd console && npx prettier --check .
	cd console && npx vitest run
	cd console && npx vite build
	gitleaks dir --no-banner --redact console/dist

console-e2e: console-deps ## Test trình duyệt của màn hình (Playwright: Chromium mọi spec, WebKit spec .phone.spec.ts), API giả; cổng CRM_E2E_PORT (5148)
	cd console && npx playwright install chromium chromium-headless-shell webkit
	cd console && npx playwright test

# Thay đổi phá vỡ của hợp đồng so với nhánh main phải có dòng trong breaking-approved.yaml (tools/apicheck gọi
# oasdiff). Thử với tệp mẫu: make oasdiff OASDIFF_BASE=-base=<tệp gốc> OASDIFF_REV=<tệp mới>
OASDIFF_BASE ?= -base-ref=main
OASDIFF_REV ?= openapi/openapi.yaml
OASDIFF_APPROVED ?= openapi/breaking-approved.yaml
oasdiff: ## Chặn thay đổi phá vỡ openapi.yaml chưa được duyệt (so với main; lần đầu main chưa có tệp thì bỏ qua)
	go run ./tools/apicheck $(OASDIFF_BASE) -rev $(OASDIFF_REV) -approved $(OASDIFF_APPROVED)

sqlc-vet: ## sqlc vet (luật sqlc/db-prepare) trên database thử đã migrate; câu mẫu sai phải đỏ (tests/arch)
	go test -race -count=1 -run '^TestSqlcVet$$' ./tests/arch

build: ## Dựng mọi gói, tệp chạy không cgo
	CGO_ENABLED=0 go build -trimpath ./...

vet:
	go vet ./...

lint: ## golangci-lint theo .golangci.yml
	golangci-lint run ./...

test: ## Mọi test, có -race, với PostgreSQL dev (make db-up trước)
	go test -race -count=1 ./...

vuln:
	govulncheck ./...

catalog: ## Sinh lại docs/danh-muc-dung-chung.md
	go run ./tools/catalog

catalog-check: catalog ## Danh mục dùng chung phải khớp mã
	git diff --exit-code -- docs/danh-muc-dung-chung.md

jscpd: ## Chỉ báo đoạn trùng mới so với mốc nền jscpd-baseline.json
	jscpd --config .jscpd.json --similarity --baseline jscpd-baseline.json --fail-on-new-clones=0 .

test-guard: ## Test không bị xóa, tắt, nới mà không ghi lý do (so với main)
	scripts/agent/test-guard.sh main

# ---- Mutation testing vùng rủi ro cao (Task 23; ADR-25). Không nằm trong `ci` (vài giờ trên máy dev); CI chạy theo lịch.
# GOFLAGS=-count=1: gremlins đặt hạn mỗi lượt theo thời gian chạy test đo lúc đầu; kết quả test lấy từ cache thì hạn gần 0
# và mọi đột biến thành TIMED OUT (không tính vào hiệu quả, điểm 100% giả).
# Một nguồn danh sách (Task 392): đọc thẳng HIGH_RISK_DIRS của hook trong scripts/agent/lib.sh, không chép lại ở đây.
# Mục là thư mục: đột biến cả gói. Mục là tệp .go (guard.go của internal/httpapi): đột biến gói chứa nó nhưng loại mọi thư
# mục con và mọi tệp .go khác của gói (tính lúc chạy, nên tệp thêm sau này cũng bị loại).
MUTATION_HIGH_RISK := $(shell sed -n 's/^HIGH_RISK_DIRS="\(.*\)"$$/\1/p' scripts/agent/lib.sh)
MUTATION_DIRS := $(filter-out %.go,$(MUTATION_HIGH_RISK))
MUTATION_FILES := $(filter %.go,$(MUTATION_HIGH_RISK))
mutation_exclude = -E '/' $(foreach g,$(filter-out $(1) %_test.go,$(wildcard $(dir $(1))*.go)),-E '^$(subst .,\.,$(notdir $(g)))$$')
MUTATION_OUT ?= .mutation
mutation: ## Đột biến mã vùng cao bằng gremlins (.gremlins.yaml, ngưỡng 80%); JSON trong $(MUTATION_OUT)/; make db-up trước
	@test -n "$(MUTATION_DIRS)" || { echo "không đọc được HIGH_RISK_DIRS trong scripts/agent/lib.sh"; exit 1; }
	@mkdir -p $(MUTATION_OUT)
	rc=0; for d in $(MUTATION_DIRS); do \
		echo "== $$d"; GOFLAGS=-count=1 gremlins unleash --config .gremlins.yaml -o $(MUTATION_OUT)/$$(basename $$d).json ./$$d || rc=1; \
	done; \
	$(foreach f,$(MUTATION_FILES),echo "== $(f)"; \
	GOFLAGS=-count=1 gremlins unleash --config .gremlins.yaml $(call mutation_exclude,$(f)) -o $(MUTATION_OUT)/$(basename $(notdir $(f))).json ./$(patsubst %/,%,$(dir $(f))) || rc=1; ) \
	exit $$rc

# ---- Công cụ cho máy CI (Task 23): đúng phiên bản ghim ở tools/versions.mk.
ci-tools: ## Cài golangci-lint, govulncheck, sqlc, ogen, gitleaks, gremlins, jscpd đã ghim (máy CI; máy dev đã có)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(strip $(GOLANGCI_VERSION))
	go install golang.org/x/vuln/cmd/govulncheck@$(strip $(GOVULNCHECK_VERSION))
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(strip $(SQLC_VERSION))
	go install github.com/ogen-go/ogen/cmd/ogen@$(strip $(OGEN_VERSION))
	go install github.com/zricethezav/gitleaks/v8@$(strip $(GITLEAKS_VERSION))
	go install github.com/go-gremlins/gremlins/cmd/gremlins@$(strip $(GREMLINS_VERSION))
	npm install -g --no-audit --no-fund jscpd@$(strip $(JSCPD_VERSION))

gitleaks: ## Không có bí mật trong lịch sử git và thư mục làm việc
	gitleaks git --no-banner --redact .
	gitleaks dir --no-banner --redact .
