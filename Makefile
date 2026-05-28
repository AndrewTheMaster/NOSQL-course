.DEFAULT_GOAL = help

COMPOSE = docker compose --env-file .env.local
APP_PORT ?= $(shell grep '^APP_PORT=' .env.local 2>/dev/null | cut -d= -f2)
APP_PORT ?= 8080
BASE_URL = http://localhost:$(APP_PORT)

.PHONY: help
help:
	@echo "EventHub — команды Makefile"
	@echo ""
	@echo "  make run        — запуск всех сервисов в фоне (docker compose up -d --build)"
	@echo "  make rund       — запуск с выводом логов в консоль"
	@echo "  make stop       — остановка сервисов"
	@echo "  make clean      — остановка и удаление volumes"
	@echo "  make restart    — stop + run"
	@echo "  make services   — статус контейнеров"
	@echo "  make logs       — логи всех сервисов"
	@echo "  make logs-app   — логи приложения (go_app)"
	@echo "  make health     — проверка GET /health"
	@echo "  make session    — POST /session (сохраняет cookie в /tmp/eventhub-cookies.txt)"
	@echo ""
	@echo "Конфигурация: .env.local | API: api/eventhub.postman_collection.json"

.PHONY: run
run:
	$(COMPOSE) up -d --build

.PHONY: rund
rund:
	$(COMPOSE) up --build

.PHONY: services
services:
	$(COMPOSE) ps

.PHONY: stop
stop:
	$(COMPOSE) down

.PHONY: clean
clean:
	$(COMPOSE) down -v

.PHONY: restart
restart: stop run

.PHONY: logs
logs:
	$(COMPOSE) logs -f --tail=100

.PHONY: logs-app
logs-app:
	$(COMPOSE) logs -f --tail=100 app

.PHONY: health
health:
	@curl -sf "$(BASE_URL)/health" | (command -v jq >/dev/null && jq . || cat)
	@echo

.PHONY: session
session:
	@curl -sf -c /tmp/eventhub-cookies.txt -b /tmp/eventhub-cookies.txt -X POST "$(BASE_URL)/session" -o /dev/null -w "HTTP %{http_code}\n"
