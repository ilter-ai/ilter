.PHONY: build build-go build-web check check-go check-web check-strict check-diff fix \
        dev run test clean sqlc-gen services-up services-down services-logs docker-build

APP_NAME   := ilter
WEB_DIR    := web
OLLAMA_SVC := ilter-ollama-1
OLLAMA_MOD := nomic-embed-text
AIR_BIN    := $(shell go env GOPATH)/bin/air
RTK        := $(or $(shell command -v rtk 2>/dev/null),)

ILTER_ADMIN_API_KEY ?= test
export ILTER_ADMIN_API_KEY

ILTER_REDIS_URL ?= redis://localhost:6379
export ILTER_REDIS_URL

check: check-go check-web

check-go: build-go
	$(RTK) golangci-lint run ./... --out-format=tab

check-web: build-web
	cd $(WEB_DIR) && bun run astro check && bun run biome check .
check-strict: check
	$(RTK) golangci-lint run --enable=gosec,gocritic,bodyclose,contextcheck ./... --out-format=tab
	cd $(WEB_DIR) && bun run knip
	$(RTK) go test -race -count=1 ./...
check-diff:
	$(RTK) golangci-lint run --new-from-rev=origin/main ./... --out-format=tab
	cd $(WEB_DIR) && bun run biome check --changed --since=main .
fix:
	gofumpt -l -w .
	$(RTK) golangci-lint run --fix ./... --out-format=tab
	cd $(WEB_DIR) && bun run biome check --write .

build: build-web build-go
build-go:
	$(RTK) go build -o $(APP_NAME) ./cmd/ilter
build-web:
	cd $(WEB_DIR) && bun run build

services-up:
	@$(RTK) docker-compose -f docker-compose.yaml up -d --wait ollama redis
	@$(RTK) docker exec $(OLLAMA_SVC) ollama pull $(OLLAMA_MOD)
services-down:
	$(RTK) docker-compose -f docker-compose.yaml stop ollama redis

dev:
	@test -x "$(AIR_BIN)" || $(RTK) go install github.com/air-verse/air@latest
	@pkill ilter 2>/dev/null || true
	@pkill -f "astro dev" 2>/dev/null || true
	@rm -f data/*.db data/*.db-shm data/*.db-wal
	@rm -rf $(WEB_DIR)/node_modules/.vite
	$(MAKE) services-up
	@$(RTK) go build -o $(APP_NAME) ./cmd/ilter && ./$(APP_NAME) init --demo
	@echo "UI :4321  API :9191  proxy :8181  ollama :11434  key=$(ILTER_ADMIN_API_KEY)"
	@cd $(WEB_DIR) && bun run dev & \
	trap 'kill 0' EXIT INT TERM; \
	"$(AIR_BIN)"

test:
	$(RTK) go test -race -count=1 ./...

# E2E: Playwright dashboard tests (attaches to dev instance — requires `make dev`)
test-e2e:
	cd $(WEB_DIR) && $(RTK) playwright test

sqlc-gen:
	$(RTK) go tool sqlc generate

clean:
	rm -f $(APP_NAME)
	rm -rf $(WEB_DIR)/dist $(WEB_DIR)/node_modules $(WEB_DIR)/bun.lock

docker-build:
	$(RTK) docker build -t ilter-gateway -f deployments/Dockerfile .
