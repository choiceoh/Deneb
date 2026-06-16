# Deneb Build
#
# Pure Go gateway build (Rust core has been removed).

.PHONY: all \
       go go-run go-dev go-test go-vet go-fmt go-lint go-clean go-bench go-binary gateway-prod wormhole \
       test clean check check-go fmt generate generate-check \
       tool-schemas tool-schemas-check \
       data-gen data-gen-check \
       kotlin-models kotlin-models-check \
       kotlin-check kotlin-spotless kotlin-detekt \
       ci ci/fast go-test-cached \
       info

# Version from git tags (release-please format: deneb-vX.Y.Z), injected via ldflags.
# Uses the latest deneb-v* tag by version sort, regardless of current branch ancestry.
DENEB_VERSION := $(shell git tag --sort=-v:refname --list 'deneb-v*' 2>/dev/null | head -1 | sed 's/^deneb-v//')
GO_LDFLAGS := -ldflags '-s -w -X main.Version=$(DENEB_VERSION)'

# DGX Spark unified-memory guard for the Go toolchain.
#
# On GB10 the GPU shares system RAM, so resident sidecar models (vLLM, OCR, ASR)
# eat into the headroom the Go toolchain can use. `go build`/`test`/`vet` default
# to one build action per CPU (20 on this box), and each compile/link/test binary
# can peak at a few GB — enough to OOM the host when free memory is already low.
#
# GO_PAR caps the parallel build/test actions, budgeting ~4 GB per action against
# current MemAvailable, clamped to [2, NPROC]. So a busy box (little free RAM)
# falls back toward `-p 2`, while an idle one uses every core. Override explicitly
# with `make go GO_PAR=4`, or export GOGC=50 to trade build speed for a smaller
# heap. CI runs on a dedicated 16-vCPU runner and calls `go` directly (not make),
# so this guard only ever affects local DGX builds — never CI timing.
GO_PAR ?= $(shell \
	mem_gb=$$(awk '/MemAvailable/ {print int($$2/1024/1024)}' /proc/meminfo 2>/dev/null || echo 8); \
	cpu=$$(nproc 2>/dev/null || echo 4); \
	par=$$((mem_gb / 4)); \
	[ $$par -gt $$cpu ] && par=$$cpu; \
	[ $$par -lt 2 ] && par=2; \
	echo $$par)

# Fix NO_PROXY for Claude Code web containers: Go module proxy uses googleapis.com,
# but NO_PROXY includes *.googleapis.com which makes Go bypass the egress proxy and
# attempt direct UDP DNS (blocked). Strip those entries so Go traffic routes through proxy.
ifneq ($(CLAUDE_CODE_PROXY_RESOLVES_HOSTS),)
_CLEAN_NO_PROXY := $(shell echo "$(NO_PROXY)" | sed 's/\*\.googleapis\.com//g; s/\*\.google\.com//g' | sed 's/,,*/,/g; s/^,//; s/,$$//')
GO_ENV := NO_PROXY="$(_CLEAN_NO_PROXY)" no_proxy="$(_CLEAN_NO_PROXY)"
else
GO_ENV :=
endif

# Ensure Go toolchain binaries (golangci-lint, etc.) are on PATH.
export PATH := $(HOME)/go/bin:$(PATH)

# Android SDK location for the native client's gradle gates (spotless/detekt).
# Mirrors the scripts/dev convention (default ~/android-sdk) and is exported so
# the gradle wrapper picks it up — `make ci` / `make kotlin-check` then run the
# Kotlin lint gate with no manual env setup (the gap that let gofmt/spotless CI
# failures slip past local checks). Override with `make kotlin-check ANDROID_HOME=...`.
ANDROID_HOME ?= $(HOME)/android-sdk
export ANDROID_HOME

# Default: build Go gateway.
all: go

# --- Go gateway ---

go:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go build -p $(GO_PAR) $(GO_LDFLAGS) ./...

go-run: go
	cd gateway-go && $(GO_ENV) go run ./cmd/gateway/

# Dev mode: build and run gateway with auto-restart on SIGUSR1 (exit code 75).
# Uses go build instead of go run to avoid signal forwarding issues.
go-dev:
	@echo "Starting Go gateway in dev mode (auto-restart on SIGUSR1)..."
	@while true; do \
		if ! $(GO_ENV) CGO_ENABLED=0 go build -C gateway-go -p $(GO_PAR) $(GO_LDFLAGS) -o /tmp/deneb-gateway-dev ./cmd/gateway/; then \
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
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go test -p $(GO_PAR) -count=1 ./...

# Cached test variant for the fast inner-loop gate (make ci/fast): drops
# -count=1 so Go's test cache serves unchanged packages and only re-runs what a
# change actually invalidated. Not for the authoritative gate — the cache can
# mask flakes — so plain `go-test` (with -count=1) stays the one CI mirrors.
go-test-cached:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go test -p $(GO_PAR) ./...

go-vet:
	cd gateway-go && $(GO_ENV) go vet -p $(GO_PAR) ./...

go-fmt:
	@cd gateway-go && test -z "$$(gofmt -l .)" || (echo "Go files need formatting:"; gofmt -l .; exit 1)

# Lint only new/changed Go code (safe for CI gate on existing codebases).
go-lint:
	cd gateway-go && golangci-lint run --new ./...

# Full lint audit (all existing code). Use for periodic cleanup.
go-lint-all:
	cd gateway-go && golangci-lint run ./...

go-binary:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go build -trimpath -p $(GO_PAR) $(GO_LDFLAGS) -o ../dist/deneb-gateway ./cmd/gateway/


# Build production gateway binary to dist/.
gateway-prod:
	$(MAKE) go-binary
	@echo "Production gateway ready: dist/deneb-gateway"

# Build the wormhole model router binary to dist/ (cmd/wormhole). Managed as a
# sibling service (scripts/deploy/start-wormhole.sh, wormhole.service).
wormhole:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go build -trimpath -p $(GO_PAR) $(GO_LDFLAGS) -o ../dist/wormhole ./cmd/wormhole/
	@echo "wormhole router ready: dist/wormhole"

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

check-go: go-fmt go-vet go-lint go-test

# Full check: generate-check first (sequential), then Go checks.
check: generate-check check-go
	@echo "All checks passed"

# Fast check: format + vet + lint only (no tests). Good for pre-commit gate.
check/fast: go-fmt go-vet go-lint
	@echo "Fast checks passed (fmt + vet + lint, no tests)"

# Run all code generation pipelines in dependency order.
generate: tool-schemas data-gen kotlin-models
	@echo "All code generation pipelines completed"

# Verify generated sources are up to date.
# Runs each generation domain independently so failures name the broken group.
generate-check:
	@echo "==> [1/3] tool schemas (tool_schemas.json -> tool_schemas_gen.go)"
	@$(MAKE) tool-schemas-check
	@echo "==> [2/3] data tables (*.json -> *_gen.go)"
	@$(MAKE) data-gen-check
	@echo "==> [3/3] kotlin wire models (Go //deneb:wire -> MiniappWireTypes.kt)"
	@$(MAKE) kotlin-models-check
	@echo "All generation checks passed"

fmt:
	cd gateway-go && gofmt -w .

# --- Tool schema code generation ---

# Regenerate gateway-go/internal/pipeline/chat/toolreg/tool_schemas_gen.go from tool_schemas.json.
tool-schemas:
	cd gateway-go && go run cmd/tool-schema-gen/main.go \
		-json internal/pipeline/chat/toolreg/tool_schemas.json \
		-out  internal/pipeline/chat/toolreg/tool_schemas_gen.go \
		-pkg  toolreg

# Verify tool_schemas_gen.go is up to date (fails if json and Go are out of sync).
tool-schemas-check:
	cd gateway-go && go run cmd/tool-schema-gen/main.go \
		-json internal/pipeline/chat/toolreg/tool_schemas.json \
		-out  internal/pipeline/chat/toolreg/tool_schemas_gen.go \
		-pkg  toolreg
	@git diff --exit-code -- gateway-go/internal/pipeline/chat/toolreg/tool_schemas_gen.go

# --- Data table code generation ---
#
# Universal JSON -> Go var generator for data tables (tool classification).
# Source JSON files live next to their generated Go counterparts.

DATA_GEN = go run cmd/data-gen/main.go
DATA_GEN_TARGETS = \
	internal/pipeline/chat/tool_classification

data-gen:
	@cd gateway-go && for t in $(DATA_GEN_TARGETS); do \
		$(DATA_GEN) -json $${t}.json -out $${t}_gen.go; \
	done

data-gen-check:
	@cd gateway-go && for t in $(DATA_GEN_TARGETS); do \
		$(DATA_GEN) -json $${t}.json -out $${t}_gen.go; \
	done
	@git diff --exit-code -- $(addprefix gateway-go/,$(addsuffix _gen.go,$(DATA_GEN_TARGETS)))

# --- Kotlin wire model code generation ---
#
# Generates the native client's @Serializable wire types from the Go miniapp
# handler structs marked //deneb:wire, so the client and the gateway share one
# source of truth for RPC response shapes. The check target is non-mutating
# (compares against the committed file) and gates schema drift in CI.

KOTLIN_MODELS_SRC = internal/runtime/rpc/handler/handlerminiapp
KOTLIN_MODELS_OUT = ../client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb/generated/MiniappWireTypes.kt
KOTLIN_MODELS_PKG = ai.deneb.deneb.generated

kotlin-models:
	cd gateway-go && go run cmd/kotlin-models-gen/main.go \
		-src $(KOTLIN_MODELS_SRC) \
		-out $(KOTLIN_MODELS_OUT) \
		-pkg $(KOTLIN_MODELS_PKG)

kotlin-models-check:
	cd gateway-go && go run cmd/kotlin-models-gen/main.go \
		-src $(KOTLIN_MODELS_SRC) \
		-out $(KOTLIN_MODELS_OUT) \
		-pkg $(KOTLIN_MODELS_PKG) \
		-check

# --- Kotlin client lint gates (native client) ---
#
# Mirror the kotlin-lint.yml CI gate locally: spotlessCheck = ktlint formatting,
# detekt = bug-focused static analysis (config/detekt.yml, baseline in
# config/detekt-baseline.xml). These are GATES — never auto-edit the detekt
# baseline to silence findings (.claude/rules/testing.md). Until now they had no
# make target, so the only way to check the native client before push was a manual
# `ANDROID_HOME=... ./gradlew ...`; that gap is what `make ci` closes.
#
# Local runs keep the gradle daemon (faster on repeat); CI uses --no-daemon on
# fresh runners. The daemon only affects speed, not the pass/fail outcome.
KOTLIN_APP_DIR = client-android/app

kotlin-spotless:
	cd $(KOTLIN_APP_DIR) && ./gradlew spotlessCheck --console=plain

kotlin-detekt:
	cd $(KOTLIN_APP_DIR) && ./gradlew detekt --console=plain

# Native client gate: formatting + bug-lint (matches kotlin-lint.yml).
kotlin-check: kotlin-spotless kotlin-detekt
	@echo "Kotlin client checks passed"

# --- CI gate mirror (single pre-push command) ---
#
# One command that runs every gate CI enforces — Go (generate-check, fmt, vet,
# lint, test) AND the native client (spotless, detekt) — and reports a per-gate
# PASS/FAIL summary with offender detail for failures. Unlike `make check` it
# continues past the first failure, so a single run surfaces *everything* that
# would fail CI (the recurring "fix one, rerun, discover the next" trap). The Go
# and Kotlin suites run in parallel since gradle startup is the long pole.
#
#   make ci                  # all gates (Go + Kotlin)
#   make ci ARGS=--go        # Go gates only (skip the gradle/Kotlin lane)
#   make ci ARGS=--kotlin    # Kotlin gates only
#
# This mirrors CI's *fast* gates only — no -race, coverage threshold, or
# integration-tagged tests; run those in CI or via `make go-test` variants.
ci:
	@scripts/dev/ci-check.sh $(ARGS)

# Fast inner-loop gate: path-gates the lanes (skips the Go or Kotlin side when
# its tree is untouched vs origin/main, mirroring CI's own path-gating) and uses
# the Go test cache. Much faster on single-side edits. NOT authoritative — run
# the full `make ci` before the actual push. Override the diff base with
# CI_CHECK_BASE=<ref>.
ci/fast:
	@scripts/dev/ci-check.sh --fast

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
	@echo "  make ci         - PRE-PUSH GATE: every CI check (Go + Kotlin), pass/fail summary"
	@echo "                    (ARGS=--go / ARGS=--kotlin to run one lane)"
	@echo "  make ci/fast    - Inner-loop gate: only the changed side (Go/Kotlin), cached tests"
	@echo "  make check      - Go-only checks (generate + fmt + vet + lint + test)"
	@echo "  make check/fast - Fast Go checks: fmt + vet + lint, no tests"
	@echo "  make kotlin-check - Native client gate (spotless + detekt)"
	@echo "  make generate         - Run all code generation pipelines"
	@echo "  make generate-check   - Verify all generated files"
	@echo "  make clean      - Clean Go build artifacts"
	@echo "  make go-bench   - Run Go gateway benchmarks"
	@echo ""
	@echo "  GO_PAR=$(GO_PAR)  - parallel build/test actions (auto from free RAM; override: make go GO_PAR=4)"
