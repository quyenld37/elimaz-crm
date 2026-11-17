# Lệnh dev của CRM (kế hoạch M0 bản 5). Task 3 thêm `ci`, Task 4 và 6 thêm `gen`.
#
# Cổng mặc định đúng kế hoạch (PostgreSQL 5442, Jaeger UI 16686, OTLP 4318). Nhiều phiên chạy song song
# trên một máy thì mỗi phiên đặt tên dự án compose và cổng riêng, ví dụ:
#   make db-up COMPOSE_PROJECT=crm-t02 CRM_PG_PORT=5452
#   make spike-test CRM_PG_PORT=5452
# Test đọc chuỗi kết nối từ CRM_TEST_DATABASE_URL (crm_app); mặc định dựng từ CRM_PG_PORT.

include tools/versions.mk

COMPOSE_PROJECT ?= crm
CRM_PG_PORT ?= 5442
export CRM_PG_PORT
CRM_TEST_DATABASE_URL ?= postgres://crm_app:crm_app_dev@127.0.0.1:$(CRM_PG_PORT)/crm?sslmode=disable
export CRM_TEST_DATABASE_URL

COMPOSE := docker compose -p $(COMPOSE_PROJECT) -f docker/compose.dev.yml

.PHONY: db-up db-down db-reset spike-test

db-up: ## Dựng PostgreSQL 18 và Jaeger dev, chờ tới khi sẵn sàng
	$(COMPOSE) up -d --wait

db-down: ## Dừng container, giữ dữ liệu
	$(COMPOSE) down

db-reset: ## Xóa container và volume dữ liệu của dự án compose này
	$(COMPOSE) down -v

spike-test: ## Test phép đo flash sale trên PostgreSQL dev (module riêng spikes/flashsale)
	cd spikes/flashsale && go test -race -count=1 ./...

# ---- Cổng kiểm (Task 3). `make ci` là lưới cuối; CI trên máy chủ git chạy đúng lệnh này.
# Sinh lại mã (ogen, sqlc, bảng route), sqlc vet, oasdiff thêm vào đây ở Task 4 và Task 6.
.PHONY: ci build vet lint test vuln catalog catalog-check jscpd test-guard gitleaks

ci: build vet lint test vuln catalog-check jscpd test-guard gitleaks ## Toàn bộ cổng kiểm phần đã có

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

gitleaks: ## Không có bí mật trong lịch sử git và thư mục làm việc
	gitleaks git --no-banner --redact .
	gitleaks dir --no-banner --redact .
