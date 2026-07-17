# Japanese Kanji Reading Trainer (JKRT)
# Run `make help` for targets. Prefer `make run-dev` for first-time local use.

.DEFAULT_GOAL := help

GO       ?= go
DB_PATH  ?= ./jkrt.db
ADDR_DEV ?= :8080
ADDR_TUN ?= 127.0.0.1:8080

# Load .env into the shell when present (does not override already-exported vars
# if you use `export` before make — sourcing applies file values for this process).
define WITH_ENV
	set -a; \
	if [ -f .env ]; then . ./.env; fi; \
	set +a;
endef

.PHONY: help
help: ## Show this help
	@echo "JKRT — common commands"
	@echo ""
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "First time:  make env   # then edit .env"
	@echo "Local only:  make run-dev"
	@echo "Phone/HTTPS: make run-auth   (other terminal: make tunnel-quick)"
	@echo "Docs:        README.md  |  docs/auth-and-tunnel.md"

.PHONY: env
env: ## Create .env from .env.example (does not overwrite)
	@if [ -f .env ]; then \
		echo ".env already exists — not overwriting"; \
		exit 0; \
	fi
	@cp .env.example .env
	@# Fill a real session secret so auth-on starts without editing that field.
	@secret=$$(openssl rand -hex 32); \
	if command -v sed >/dev/null 2>&1; then \
		sed -i.bak "s/^JKRT_SESSION_SECRET=.*/JKRT_SESSION_SECRET=$$secret/" .env && rm -f .env.bak; \
	else \
		echo "Set JKRT_SESSION_SECRET=$$secret in .env"; \
	fi
	@echo "Created .env — change JKRT_PASSWORD from change-me before any tunnel."
	@echo "Session secret was generated. Do not commit .env."

.PHONY: secret
secret: ## Print a random JKRT_SESSION_SECRET (32+ bytes hex)
	@openssl rand -hex 32

.PHONY: run-dev
run-dev: ## Start server with auth OFF (local only — never with a tunnel)
	JKRT_AUTH=off JKRT_ADDR=$(ADDR_DEV) $(GO) run ./cmd/server

.PHONY: run-auth
run-auth: ## Start server with auth ON (requires .env or exported secrets)
	@if [ ! -f .env ] && [ -z "$$JKRT_SESSION_SECRET" ]; then \
		echo "Missing secrets. Run: make env   then edit JKRT_PASSWORD in .env"; \
		exit 1; \
	fi
	@$(WITH_ENV) \
	export JKRT_AUTH=on; \
	export JKRT_ADDR="$${JKRT_ADDR:-$(ADDR_TUN)}"; \
	if [ -z "$$JKRT_SESSION_SECRET" ] || [ "$${#JKRT_SESSION_SECRET}" -lt 32 ]; then \
		echo "JKRT_SESSION_SECRET must be at least 32 characters (see make env / make secret)"; \
		exit 1; \
	fi; \
	echo "Starting with auth=on addr=$$JKRT_ADDR (never use auth off with a tunnel)"; \
	$(GO) run ./cmd/server

.PHONY: run
run: run-auth ## Alias for run-auth

.PHONY: build
build: ## Build binary to ./bin/jkrt
	@mkdir -p bin
	$(GO) build -o bin/jkrt ./cmd/server
	@echo "Built bin/jkrt"

.PHONY: test
test: ## Run all tests (no live network)
	$(GO) test ./... -count=1

.PHONY: test-v
test-v: ## Run all tests verbose
	$(GO) test ./... -count=1 -v

.PHONY: setpassword
setpassword: ## Rotate login password (does not wipe Cards)
	$(GO) run ./cmd/setpassword -db $(DB_PATH)

.PHONY: scrape
scrape: ## POST /api/scrape (server must already be running; works when JKRT_AUTH=off)
	@curl -sS -X POST "http://127.0.0.1:8080/api/scrape"; echo

.PHONY: health
health: ## GET /health
	@curl -sS "http://127.0.0.1:8080/health"; echo

.PHONY: tunnel-quick
tunnel-quick: ## Ephemeral HTTPS URL via cloudflared (server must be on 127.0.0.1:8080 with AUTH ON)
	@command -v cloudflared >/dev/null 2>&1 || { \
		echo "cloudflared not found. Install:"; \
		echo "  https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation/"; \
		exit 1; \
	}
	@echo "WARNING: Only use with JKRT_AUTH=on (make run-auth)."
	@echo "Pointing tunnel at http://127.0.0.1:8080 …"
	cloudflared tunnel --url http://127.0.0.1:8080

.PHONY: tidy
tidy: ## go mod tidy
	$(GO) mod tidy

.PHONY: clean
clean: ## Remove local build artifacts (not your database)
	rm -rf bin/
