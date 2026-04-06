# Deneb Build
#
# Pure Go gateway build (Rust core has been removed).

.PHONY: all \
       go go-run go-dev go-test go-test-fuzz go-vet go-fmt go-lint go-clean go-bench go-binary mcp-server gateway-prod \
       test clean check check-go fmt generate generate-check \
       tool-schemas tool-schemas-check \
       model-caps model-caps-check \
       data-gen data-gen-check \
       info

# Version from git tags (release-please format: deneb-vX.Y.Z), injected via ldflags.
# Uses the latest deneb-v* tag by version sort, regardless of current branch ancestry.
DENEB_VERSION := $(shell git tag --sort=-v:refname --list 'deneb-v*' 2>/dev/null | head -1 | sed 's/^deneb-v//')
GO_LDFLAGS := -ldflags '-s -w -X main.Version=$(DENEB_VERSION)'

# Fix NO_PROXY for Claude Code web containers: Go module proxy uses googleapis.com,
# but NO_PROXY includes *.googleapis.com which makes Go bypass the egress proxy and
# attempt direct UDP DNS (blocked). Strip those entries so Go traffic routes through proxy.
ifneq ($(CLAUDE_CODE_PROXY_RESOLVES_HOSTS),)
_CLEAN_NO_PROXY := $(shell echo "$(NO_PROXY)" | sed 's/\*\.googleapis\.com//g; s/\*\.google\.com//g' | sed 's/,,*/,/g; s/^,//; s/,$$//')
GO_ENV := NO_PROXY="$(_CLEAN_NO_PROXY)" no_proxy="$(_CLEAN_NO_PROXY)"
else
GO_ENV :=
endif

# Default: build Go gateway.
all: go

# --- Go gateway ---

go:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go build $(GO_LDFLAGS) ./...

go-run: go
	cd gateway-go && $(GO_ENV) go run ./cmd/gateway/

# Dev mode: build and run gateway with auto-restart on SIGUSR1 (exit code 75).
# Uses go build instead of go run to avoid signal forwarding issues.
go-dev:
	@echo "Starting Go gateway in dev mode (auto-restart on SIGUSR1)..."
	@while true; do \
		if ! $(GO_ENV) CGO_ENABLED=0 go build -C gateway-go $(GO_LDFLAGS) -o /tmp/deneb-gateway-dev ./cmd/gateway/; then \
			echo "[go-dev] Build failed, aborting."; \
			exit 1; \
		fi; \
		/tmp/deneb-gateway-dev $(ARGS); \
		EXIT=$$?; \
		if [ $$EXIT -eq 75 ]; then \
			echo "[go-dev] Restarting gateway (SIGUSR1)..."; \
			sleep 0.5; \
			continue; \
		fi; \
		echo "[go-dev] Gateway exited with code $$EXIT"; \
		exit $$EXIT; \
	done

go-test:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go test -count=1 ./...

go-test-fuzz:
	cd gateway-go && $(GO_ENV) go test ./internal/bridge/ -fuzz=FuzzParseRequestFrame -fuzztime=10s

go-vet:
	cd gateway-go && $(GO_ENV) go vet ./...

go-fmt:
	@cd gateway-go && test -z "$$(gofmt -l .)" || (echo "Go files need formatting:"; gofmt -l .; exit 1)

# Lint only new/changed Go code (safe for CI gate on existing codebases).
go-lint:
	cd gateway-go && golangci-lint run --new ./...

# Full lint audit (all existing code). Use for periodic cleanup.
go-lint-all:
	cd gateway-go && golangci-lint run ./...

go-binary:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go build -trimpath $(GO_LDFLAGS) -o ../dist/deneb-gateway ./cmd/gateway/

# Build MCP server binary (pure Go, thin bridge to gateway HTTP RPC).
mcp-server:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go build -trimpath $(GO_LDFLAGS) -o ../bin/deneb-mcp ./cmd/mcp-server/

# Build production gateway binary to dist/.
gateway-prod: go-binary
	@echo "Production gateway ready: dist/deneb-gateway"

go-clean:
	cd gateway-go && go clean ./...

# Run Go benchmarks with memory allocation stats.
go-bench:
	cd gateway-go && $(GO_ENV) go test -bench=. -benchmem -run='^$$' ./...

# --- Combined operations ---

test: go-test
	@echo "Go tests passed"

clean: go-clean
	@echo "Cleaned Go build artifacts"

check-go: go-fmt go-vet go-test

# Full check: generate-check first (sequential), then Go checks.
check: generate-check check-go
	@echo "All checks passed"

# Fast check: format + lint only (no tests). Good for pre-commit gate.
check/fast: go-fmt go-vet
	@echo "Fast checks passed (fmt + lint, no tests)"

# Run all code generation pipelines in dependency order.
generate: tool-schemas model-caps data-gen
	@echo "All code generation pipelines completed"

# Verify generated sources are up to date.
# Runs each generation domain independently so failures name the broken group.
generate-check:
	@echo "==> [1/3] tool schemas (tool_schemas.yaml -> tool_schemas_gen.go)"
	@$(MAKE) tool-schemas-check
	@echo "==> [2/3] model capabilities (model_caps.yaml -> model_caps_gen.go)"
	@$(MAKE) model-caps-check
	@echo "==> [3/3] data tables (*.yaml -> *_gen.go)"
	@$(MAKE) data-gen-check
	@echo "All generation checks passed"

fmt:
	cd gateway-go && gofmt -w .

# --- Tool schema code generation ---

# Regenerate gateway-go/internal/chat/toolreg/tool_schemas_gen.go from tool_schemas.yaml.
tool-schemas:
	cd gateway-go && go run cmd/tool-schema-gen/main.go \
		-yaml internal/chat/toolreg/tool_schemas.yaml \
		-out  internal/chat/toolreg/tool_schemas_gen.go \
		-pkg  toolreg

# Verify tool_schemas_gen.go is up to date (fails if yaml and Go are out of sync).
tool-schemas-check:
	cd gateway-go && go run cmd/tool-schema-gen/main.go \
		-yaml internal/chat/toolreg/tool_schemas.yaml \
		-out  internal/chat/toolreg/tool_schemas_gen.go \
		-pkg  toolreg
	@git diff --exit-code -- gateway-go/internal/chat/toolreg/tool_schemas_gen.go

# Regenerate gateway-go/internal/autoreply/model_caps_gen.go from model_caps.yaml.
model-caps:
	cd gateway-go && go run cmd/model-caps-gen/main.go \
		-yaml internal/autoreply/thinking/model_caps.yaml \
		-out  internal/autoreply/thinking/model_caps_gen.go

# Verify model_caps_gen.go is up to date (fails if yaml and Go are out of sync).
model-caps-check:
	cd gateway-go && go run cmd/model-caps-gen/main.go \
		-yaml internal/autoreply/thinking/model_caps.yaml \
		-out  internal/autoreply/thinking/model_caps_gen.go
	@git diff --exit-code -- gateway-go/internal/autoreply/thinking/model_caps_gen.go

# --- Data table code generation ---
#
# Universal YAML -> Go var generator for data tables (tool classification).
# Source YAML files live next to their generated Go counterparts.

DATA_GEN = go run cmd/data-gen/main.go
DATA_GEN_TARGETS = \
	internal/chat/tool_classification

data-gen:
	@cd gateway-go && for t in $(DATA_GEN_TARGETS); do \
		$(DATA_GEN) -yaml $${t}.yaml -out $${t}_gen.go; \
	done

data-gen-check:
	@cd gateway-go && for t in $(DATA_GEN_TARGETS); do \
		$(DATA_GEN) -yaml $${t}.yaml -out $${t}_gen.go; \
	done
	@git diff --exit-code -- $(addprefix gateway-go/,$(addsuffix _gen.go,$(DATA_GEN_TARGETS)))

# --- Info ---

info:
	@echo "Deneb Build (Pure Go)"
	@echo ""
	@echo "  make go         - Build Go gateway"
	@echo "  make go-dev     - Run Go gateway in dev mode (auto-restart on SIGUSR1)"
	@echo "  make go-binary  - Build Go gateway binary to dist/"
	@echo "  make gateway-prod - Production gateway build"
	@echo "  make test       - Run Go tests"
	@echo "  make go-lint    - Run golangci-lint on Go gateway"
	@echo "  make go-fmt     - Check Go formatting"
	@echo "  make check      - Run all checks (generate + fmt + vet + test)"
	@echo "  make check/fast - Fast checks: fmt + vet only, no tests"
	@echo "  make generate         - Run all code generation pipelines"
	@echo "  make generate-check   - Verify all generated files"
	@echo "  make clean      - Clean Go build artifacts"
	@echo "  make go-bench   - Run Go gateway benchmarks"
