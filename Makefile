# Bifröst developer commands. `make check` is what CI runs and never needs Ollama.

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

# DB is a working copy; the testdata/ fixtures are never written. WRITE=1 disables --dry-run.
DB       ?= tmp/catalog.work.db
INPUT    ?= testdata/ProductEntry.json
MODEL    ?= nomic-embed-text
WRITE    ?=
FUZZ     ?= FuzzSanitize
FUZZPKG  ?= ./internal/ingest
FUZZTIME ?= 60s

GO_RUN  := go run ./cmd/bifrost
HAS_SQL := $(wildcard internal/catalog/queries/*.sql)

.PHONY: help run reset-db migrate generate sqlc-diff schema lint test test-go test-scripts \
        fuzz bench e2e vuln tidy-check check

help: ## List the targets
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-13s %s\n", $$1, $$2}'

# --- run -------------------------------------------------------------------------------------

$(DB):
	@mkdir -p $(dir $(DB))
	cp testdata/catalog.db $(DB)
	chmod u+w $(DB)

run: migrate ## Migrate DB, then ingest INPUT into it (dry run unless WRITE=1)
	$(GO_RUN) ingest --db $(DB) --input $(INPUT) --model $(MODEL) $(if $(WRITE),,--dry-run)

reset-db: ## Replace DB with a fresh copy of testdata/catalog.db
	rm -f $(DB)
	@$(MAKE) --no-print-directory $(DB)

migrate: | $(DB) ## Apply the migrations to DB
	$(GO_RUN) migrate --db $(DB)

# --- sqlc (skipped until internal/catalog/queries has .sql files) -----------------------------

generate: ## Regenerate internal/catalog/db with sqlc
ifeq ($(HAS_SQL),)
	@echo "sqlc: no queries in internal/catalog/queries yet, skipping"
else
	go tool sqlc generate
endif

sqlc-diff: ## Fail if internal/catalog/db is out of date
ifeq ($(HAS_SQL),)
	@echo "sqlc: no queries in internal/catalog/queries yet, skipping"
else
	go tool sqlc diff
endif

schema: ## Dump the migrated fixture schema to internal/catalog/schema.sql (sqlc fallback)
	mkdir -p tmp && rm -f tmp/schema.db && cp testdata/catalog.db tmp/schema.db
	chmod u+w tmp/schema.db
	$(GO_RUN) migrate --db tmp/schema.db
	sqlite3 -readonly tmp/schema.db .schema > internal/catalog/schema.sql

# --- quality ---------------------------------------------------------------------------------

lint: ## golangci-lint (depguard, gosec, ...)
	golangci-lint run ./...

test: test-go test-scripts ## Unit tests (never calls Ollama)

test-go:
	go test -race -count=1 ./...

test-scripts:
	bash scripts/eng-issue_test.sh

fuzz: ## Fuzz FUZZ in FUZZPKG for FUZZTIME
	go test $(FUZZPKG) -run '^$$' -fuzz '^$(FUZZ)$$' -fuzztime $(FUZZTIME)

bench: ## Benchmarks with allocations
	go test ./... -run '^$$' -bench . -benchmem

e2e: ## Integration tests against a real Ollama (needs the model pulled)
	go test -tags integration -count=1 ./...

vuln: ## Known-vulnerability scan
	go tool govulncheck ./...

tidy-check:
	go mod tidy -diff

check: lint sqlc-diff tidy-check test vuln ## Everything CI runs
