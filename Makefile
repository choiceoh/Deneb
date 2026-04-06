# Deneb Multi-Language Build
#
# Orchestrates Rust (core-rs workspace), Go (gateway-go), and CLI (cli-rs) builds.

.PHONY: all rust rust-ml rust-dgx rust-all rust-debug rust-test rust-fmt rust-clippy rust-bench rust-clean \
       go go-ffi go-dgx go-pure go-run go-dev go-test go-test-pure go-test-fuzz go-vet go-fmt go-lint go-clean go-bench go-binary mcp-server gateway-prod gateway-dgx \
       cli cli-debug cli-test cli-fmt cli-clippy cli-bench cli-clean \
       cli-cross-linux-arm64 \
       deny machete \
       test clean check check/fast check-rust check-cli check-go fmt generate generate-check \
       proto proto-go proto-rust proto-check proto-lint proto-watch \
       tool-schemas tool-schemas-check \
       model-caps model-caps-check \
       error-codes-gen error-codes-gen-check \
       data-gen data-gen-check \
       ffi-gen ffi-gen-check proto-error-codes-gen proto-error-codes-gen-check error-code-sync \
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

# Auto-detect GCC include path for bindgen (llama-cpp-sys-2 needs stdbool.h).
# libclang used by bindgen may not find GCC-provided headers without explicit paths.
ifndef BINDGEN_EXTRA_CLANG_ARGS
_GCC_INCLUDE := $(shell gcc -print-file-name=include 2>/dev/null)
_GCC_MACHINE := $(shell gcc -dumpmachine 2>/dev/null)
ifneq ($(_GCC_INCLUDE),include)
export BINDGEN_EXTRA_CLANG_ARGS := -I$(_GCC_INCLUDE) -I/usr/include/$(_GCC_MACHINE) -I/usr/include
endif
endif

# Default: build Rust first (produces .a), then Go (links it via CGo), then CLI.
all: rust go cli

# --- Rust core library (workspace) ---

# Build core crate for CGo static linking (minimal — no ml).
# --no-default-features disables "napi_binding" (Node.js addon), producing
# only the staticlib (.a) and rlib needed by the Go gateway via CGo.
rust:
	cd core-rs && cargo build --release -p deneb-core --no-default-features

# Build core with local ML inference (CPU).
rust-ml:
	cd core-rs && cargo build --release -p deneb-core --no-default-features --features ml

# Build core with ML + CUDA GPU acceleration (DGX Spark production).
rust-dgx:
	cd core-rs && cargo build --release -p deneb-core --no-default-features --features ml,cuda

# Build all workspace crates.
rust-all:
	cd core-rs && cargo build --release --workspace

rust-debug:
	cd core-rs && cargo build --workspace

rust-test:
	cd core-rs && cargo test --workspace

rust-bench:
	cd core-rs && cargo bench --workspace

rust-fmt:
	cd core-rs && cargo fmt --all -- --check

rust-clippy:
	cd core-rs && cargo clippy --workspace --all-targets -- -D warnings

rust-clean:
	cd core-rs && cargo clean

# --- Go gateway ---

# Default go: CGo build linking Rust static lib (requires `make rust` first).
go: go-ffi

go-ffi:
	cd gateway-go && $(GO_ENV) go build $(GO_LDFLAGS) ./...

# CGo build with CUDA libraries (requires `make rust-dgx` first).
# The "cuda" build tag pulls in LDFLAGS from core_cgo_cuda.go.
# Set CUDA_LIBDIR to add a custom library search path for non-standard installs.
CUDA_LIBDIR ?=
go-dgx:
	cd gateway-go && $(if $(CUDA_LIBDIR),CGO_LDFLAGS="-L$(CUDA_LIBDIR)",) $(GO_ENV) go build -tags cuda $(GO_LDFLAGS) ./...

# Pure-Go build with fallback implementations (no Rust required).
go-pure:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go build $(GO_LDFLAGS) -tags no_ffi ./...

go-run: go
	cd gateway-go && $(GO_ENV) go run ./cmd/gateway/

# Dev mode: build and run gateway with auto-restart on SIGUSR1 (exit code 75).
# Uses go build instead of go run to avoid signal forwarding issues.
go-dev:
	@echo "Starting Go gateway in dev mode (auto-restart on SIGUSR1)..."
	@while true; do \
		if ! $(GO_ENV) go build -C gateway-go $(GO_LDFLAGS) -o /tmp/deneb-gateway-dev ./cmd/gateway/; then \
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
	cd gateway-go && $(GO_ENV) go test -race -count=1 ./...

go-test-pure:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go test -tags no_ffi -count=1 ./...

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

go-binary: rust go
	cd gateway-go && $(GO_ENV) go build -trimpath $(GO_LDFLAGS) -o ../dist/deneb-gateway ./cmd/gateway/

# Build MCP server binary (pure Go, no FFI — thin bridge to gateway HTTP RPC).
mcp-server:
	cd gateway-go && $(GO_ENV) CGO_ENABLED=0 go build -trimpath $(GO_LDFLAGS) -tags no_ffi -o ../bin/deneb-mcp ./cmd/mcp-server/

# Build full DGX Spark production: Rust (Vega+ML+CUDA) + Go (CUDA linked) + CLI.
gateway-dgx: rust-dgx go-dgx cli
	cp cli-rs/target/release/deneb dist/deneb-rs 2>/dev/null || true
	@echo "DGX gateway ready: gateway-go/deneb-gateway (Vega+ML+CUDA)"

# Build production gateway: Go binary + CLI, copies both to dist/.
gateway-prod: go-binary cli
	cp cli-rs/target/release/deneb dist/deneb-rs 2>/dev/null || true
	@echo "Production gateway ready: dist/deneb-gateway + dist/deneb-rs"

go-clean:
	cd gateway-go && go clean ./...

# --- Rust CLI ---

cli:
	cd cli-rs && cargo build --release

cli-debug:
	cd cli-rs && cargo build

cli-test:
	cd cli-rs && cargo test

cli-fmt:
	cd cli-rs && cargo fmt -- --check

cli-clippy:
	cd cli-rs && cargo clippy --all-targets -- -D warnings

cli-bench:
	cd cli-rs && cargo test --test startup_bench -- --nocapture

cli-clean:
	cd cli-rs && cargo clean

# Cross-compilation for DGX Spark (requires rustup target aarch64-unknown-linux-gnu)
cli-cross-linux-arm64:
	cd cli-rs && cargo build --release --target aarch64-unknown-linux-gnu

cli-install: cli
	./cli-rs/scripts/install.sh

# --- Audit / quality tools ---

# Run cargo-deny to check Rust dependencies for vulnerabilities, license issues, and bans.
deny:
	cargo deny check

# Run cargo-machete to detect unused Rust dependencies.
machete:
	cd core-rs && cargo machete

# Run Go benchmarks with memory allocation stats.
go-bench:
	cd gateway-go && $(GO_ENV) go test -bench=. -benchmem -run='^$$' ./...

# --- Combined operations ---

test: rust-test go-test cli-test
	@echo "Rust, Go, and CLI tests passed"

clean: rust-clean go-clean cli-clean
	@echo "Cleaned Rust, Go, and CLI build artifacts"

# Per-language check groups (used as parallel units).
check-rust: rust-fmt rust-clippy rust-test
check-cli: cli-fmt cli-clippy cli-test
check-go: go-fmt go-vet go-test

# Full check: generate-check first (sequential), then Rust/CLI/Go in parallel.
check: generate-check
	@$(MAKE) -j3 check-rust check-cli check-go
	@echo "All checks passed"

# Fast check: format + lint only (no tests). Good for pre-commit gate.
check/fast: rust-fmt rust-clippy cli-fmt cli-clippy go-fmt go-vet
	@echo "Fast checks passed (fmt + lint, no tests)"

# Run all code generation pipelines in dependency order.
generate: proto tool-schemas model-caps error-codes-gen data-gen
	@echo "All code generation pipelines completed"

# Verify generated sources are up to date.
# Runs each generation domain independently so failures name the broken group.
generate-check:
	@echo "==> [1/5] proto types (proto → Go + Rust)"
	@$(MAKE) proto-check
	@echo "==> [2/5] error codes (gateway.proto → Rust + Go)"
	@$(MAKE) error-codes-gen-check
	@echo "==> [3/5] tool schemas (tool_schemas.yaml → tool_schemas_gen.go)"
	@$(MAKE) tool-schemas-check
	@echo "==> [4/5] model capabilities (model_caps.yaml → model_caps_gen.go)"
	@$(MAKE) model-caps-check
	@echo "==> [5/5] data tables (*.yaml → *_gen.go)"
	@$(MAKE) data-gen-check
	@echo "All generation checks passed"

fmt:
	cd core-rs && cargo fmt --all
	cd cli-rs && cargo fmt
	cd gateway-go && gofmt -w .

# --- Protobuf code generation ---

proto:
	./scripts/proto-gen.sh

proto-go:
	./scripts/proto-gen.sh --go

proto-rust:
	./scripts/proto-gen.sh --rust

proto-check:
	./scripts/proto-gen.sh --check

proto-lint:
	./scripts/proto-gen.sh --lint

proto-watch:
	./scripts/proto-gen.sh --watch

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

# --- Error code generation (unified) ---
#
# proto/gateway.proto is the single source of truth for ALL error codes:
#   - ErrorCode enum → protocol-level codes (Rust enum + Go string constants)
#   - FfiErrorCode enum → C ABI return codes (Rust constants + Go int constants)
# Generated files: error_codes.rs, errors_gen.go, ffi_error_codes_gen.go.

# Regenerate all error code files from proto/gateway.proto.
error-codes-gen:
	./scripts/gen-error-codes.sh

# Verify all error code files are up to date.
error-codes-gen-check:
	./scripts/gen-error-codes.sh --check

# Legacy aliases — kept for backward compatibility.
ffi-gen: error-codes-gen
ffi-gen-check: error-codes-gen-check
proto-error-codes-gen: error-codes-gen
proto-error-codes-gen-check: error-codes-gen-check
error-code-sync: error-codes-gen-check

# --- Data table code generation ---
#
# Universal YAML → Go var generator for data tables (tool classification).
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
	@echo "Deneb Multi-Language Build"
	@echo ""
	@echo "  make rust       - Build Rust core crate (release, CGo, minimal)"
	@echo "  make  - Build Rust core + Vega search (FTS-only)"
	@echo "  make rust-ml    - Build Rust core + Vega + ML inference (CPU)"
	@echo "  make rust-dgx   - Build Rust core + Vega + ML + CUDA (DGX Spark)"
	@echo "  make rust-all   - Build all Rust workspace crates"
	@echo "  make go         - Build Go gateway"
	@echo "  make go-dgx     - Build Go gateway with CUDA linking (DGX Spark)"
	@echo "  make go-dev     - Run Go gateway in dev mode (auto-restart on SIGUSR1)"
	@echo "  make cli        - Build Rust CLI (release)"
	@echo "  make go-binary  - Build Go gateway binary to dist/"
	@echo "  make gateway-dgx - Full DGX Spark build (rust-dgx + go-dgx + cli)"
	@echo "  make test       - Run Rust + Go + CLI tests"
	@echo "  make go-lint    - Run golangci-lint on Go gateway"
	@echo "  make go-fmt     - Check Go formatting"
	@echo "  make check      - Run all checks in parallel (Rust + Go + CLI)"
	@echo "  make check/fast - Fast checks: fmt + lint only, no tests"
	@echo "  make generate         - Run all code generation pipelines"
	@echo "  make generate-check   - Verify all generated files (per domain, names failing group)"
	@echo "  make tool-schemas-check  - Verify tool_schemas_gen.go is up to date"
	@echo "  make model-caps-check    - Verify model_caps_gen.go is up to date"
	@echo "  make data-gen            - Regenerate all YAML-driven data tables"
	@echo "  make data-gen-check      - Verify data table gen files are up to date"
	@echo "  make clean      - Clean Rust, Go, and CLI build artifacts"
	@echo "  make go-bench   - Run Go gateway benchmarks"
	@echo "  make deny       - Check Rust deps (security, license, bans)"
	@echo "  make machete    - Detect unused Rust dependencies"
	@echo "  make cli-bench  - Run CLI startup benchmark"
	@echo "  make cli-cross-linux-arm64 - Cross-compile CLI for Linux arm64"
	@echo "  make proto      - Generate protobuf code (Go + Rust)"
	@echo "  make proto-go   - Generate Go protobuf structs"
	@echo "  make proto-rust - Generate Rust protobuf structs"
	@echo "  make proto-check - Generate + verify no uncommitted diffs"
	@echo "  make proto-lint  - Lint proto files only"
	@echo "  make proto-watch - Watch proto files and regenerate on change"
