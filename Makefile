.DEFAULT_GOAL = run

COMPOSE = docker compose --env-file .env.local

# Runs all services in detached mode.
.PHONY: run
run:
	$(COMPOSE) up -d --build

# Runs all services without detached mode (for debugging).
.PHONY: rund
rund:
	$(COMPOSE) up --build

# Shows all service statuses.
.PHONY: services
services:
	$(COMPOSE) ps

# Stops all running services.
.PHONY: stop
stop:
	$(COMPOSE) down

# Cleans up all resources including volumes.
.PHONY: clean
clean:
	$(COMPOSE) down -v
