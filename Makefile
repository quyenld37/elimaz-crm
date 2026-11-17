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
