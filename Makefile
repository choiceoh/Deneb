# Deneb Multi-Language Build
#
# Orchestrates Rust (core-rs workspace), Go (gateway-go), and CLI (cli-rs) builds.

.PHONY: all rust rust-vega rust-all rust-debug rust-test rust-fmt rust-clippy rust-bench rust-clean \
       go go-ffi go-pure go-run go-dev go-test go-test-pure go-test-fuzz go-vet go-fmt go-lint go-clean go-bench go-binary gateway-prod \
       cli cli-debug cli-test cli-fmt cli-clippy cli-bench cli-clean \
       cli-cross-linux-arm64 \
       deny machete \
       test clean check fmt generate generate-check \
       proto proto-go proto-rust proto-check proto-lint proto-watch \
       tool-schemas tool-schemas-check \
       model-caps model-caps-check \
       ffi-gen ffi-gen-check \
       proto-error-codes-gen proto-error-codes-gen-check error-code-sync \
       info

# Version from git tags (release-please format: deneb-vX.Y.Z), injected via ldflags.
# Uses the latest deneb-v* tag by version sort, regardless of current branch ancestry.
DENEB_VERSION := $(shell git tag --sort=-v:refname --list 'deneb-v*' 2>/dev/null | head -1 | sed 's/^deneb-v//')
GO_LDFLAGS := -ldflags '-X main.Version=$(DENEB_VERSION)'

# Default: build Rust first (produces .a), then Go (links it via CGo), then CLI.
all: rust go cli

# --- Rust core library (workspace) ---

# Build core crate for CGo static linking (minimal — no vega/ml).
# --no-default-features disables "napi_binding" (Node.js addon), producing
# only the staticlib (.a) and rlib needed by the Go gateway via CGo.
rust:
	cd core-rs && cargo build --release -p deneb-core --no-default-features

# Build core with Vega search engine enabled (FTS-only, no ML).
rust-vega:
	cd core-rs && cargo build --release -p deneb-core --no-default-features --features vega

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
	cd gateway-go && go build $(GO_LDFLAGS) ./...

# Pure-Go build with fallback implementations (no Rust required).
go-pure:
	cd gateway-go && CGO_ENABLED=0 go build $(GO_LDFLAGS) -tags no_ffi ./...

go-run: go
	cd gateway-go && go run ./cmd/gateway/

# Dev mode: build and run gateway with auto-restart on SIGUSR1 (exit code 75).
# Uses go build instead of go run to avoid signal forwarding issues.
go-dev:
	@echo "Starting Go gateway in dev mode (auto-restart on SIGUSR1)..."
	@while true; do \
		if ! go build -C gateway-go $(GO_LDFLAGS) -o /tmp/deneb-gateway-dev ./cmd/gateway/; then \
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
	cd gateway-go && go test -race -count=1 ./...

go-test-pure:
	cd gateway-go && CGO_ENABLED=0 go test -tags no_ffi -count=1 ./...

go-test-fuzz:
	cd gateway-go && go test ./internal/bridge/ -fuzz=FuzzParseRequestFrame -fuzztime=10s

go-vet:
	cd gateway-go && go vet ./...

go-fmt:
	@cd gateway-go && test -z "$$(gofmt -l .)" || (echo "Go files need formatting:"; gofmt -l .; exit 1)

# Lint only new/changed Go code (safe for CI gate on existing codebases).
go-lint:
	cd gateway-go && golangci-lint run --new ./...

# Full lint audit (all existing code). Use for periodic cleanup.
go-lint-all:
	cd gateway-go && golangci-lint run ./...

go-binary: rust go
	cd gateway-go && go build $(GO_LDFLAGS) -o ../dist/deneb-gateway ./cmd/gateway/

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
	cd gateway-go && go test -bench=. -benchmem -run='^$$' ./...

# --- Combined operations ---

test: rust-test go-test cli-test
	@echo "Rust, Go, and CLI tests passed"

clean: rust-clean go-clean cli-clean
	@echo "Cleaned Rust, Go, and CLI build artifacts"

check: generate-check rust-fmt rust-clippy rust-test cli-fmt cli-clippy cli-test go-fmt go-vet go-test
	@echo "All checks passed"

# Run all code generation pipelines in dependency order.
generate: proto tool-schemas model-caps ffi-gen proto-error-codes-gen
	@echo "All code generation pipelines completed"

# Verify generated sources are up to date.
# Runs each generation domain independently so failures name the broken group.
generate-check:
	@echo "==> [1/5] proto types (proto → Go + Rust)"
	@$(MAKE) proto-check
	@echo "==> [2/5] proto error codes (gateway.proto → error_codes.rs)"
	@$(MAKE) proto-error-codes-gen-check
	@echo "==> [3/5] ffi error codes (ffi_utils.rs → ffi_error_codes_gen.go)"
	@$(MAKE) ffi-gen-check
	@echo "==> [4/5] tool schemas (tool_schemas.yaml → tool_schemas_gen.go)"
	@$(MAKE) tool-schemas-check
	@echo "==> [5/5] model capabilities (model_caps.yaml → model_caps_gen.go)"
	@$(MAKE) model-caps-check
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
# Requires: python3, pyyaml, gofmt
tool-schemas:
	cd gateway-go && python3 cmd/tool-schema-gen/gen.py \
		-yaml internal/chat/toolreg/tool_schemas.yaml \
		-out  internal/chat/toolreg/tool_schemas_gen.go \
		-pkg  toolreg

# Verify tool_schemas_gen.go is up to date (fails if yaml and Go are out of sync).
tool-schemas-check:
	cd gateway-go && python3 cmd/tool-schema-gen/gen.py \
		-yaml internal/chat/toolreg/tool_schemas.yaml \
		-out  internal/chat/toolreg/tool_schemas_gen.go \
		-pkg  toolreg
	@git diff --exit-code -- gateway-go/internal/chat/toolreg/tool_schemas_gen.go

# Regenerate gateway-go/internal/autoreply/model_caps_gen.go from model_caps.yaml.
# Requires: python3, pyyaml, gofmt
model-caps:
	cd gateway-go && python3 cmd/model-caps-gen/gen.py \
		-yaml internal/autoreply/thinking/model_caps.yaml \
		-out  internal/autoreply/thinking/model_caps_gen.go

# Verify model_caps_gen.go is up to date (fails if yaml and Go are out of sync).
model-caps-check:
	cd gateway-go && python3 cmd/model-caps-gen/gen.py \
		-yaml internal/autoreply/thinking/model_caps.yaml \
		-out  internal/autoreply/thinking/model_caps_gen.go
	@git diff --exit-code -- gateway-go/internal/autoreply/thinking/model_caps_gen.go

# --- FFI error code generation ---
#
# Rust ffi_utils.rs is the single source of truth for FFI error codes.
# ffi_error_codes_gen.go is generated from it — never edit it by hand.

# Regenerate gateway-go/internal/ffi/ffi_error_codes_gen.go from ffi_utils.rs.
ffi-gen:
	./scripts/gen-ffi-errors.sh

# Verify ffi_error_codes_gen.go is up to date (fails if Rust and Go are out of sync).
ffi-gen-check:
	./scripts/gen-ffi-errors.sh --check

# --- Protocol error code generation ---
#
# proto/gateway.proto is the single source of truth for the ErrorCode enum.
# error_codes.rs is generated from it — never edit it by hand.

# Regenerate core-rs/core/src/protocol/error_codes.rs from proto/gateway.proto.
proto-error-codes-gen:
	./scripts/gen-proto-error-codes.sh

# Verify error_codes.rs is up to date (fails if proto and Rust are out of sync).
proto-error-codes-gen-check:
	./scripts/gen-proto-error-codes.sh --check

# Legacy alias — kept for compatibility with external scripts.
error-code-sync: proto-error-codes-gen-check

# --- Info ---

info:
	@echo "Deneb Multi-Language Build"
	@echo ""
	@echo "  make rust       - Build Rust core crate (release, CGo, minimal)"
	@echo "  make rust-vega  - Build Rust core + Vega search (FTS-only)"
	@echo "  make rust-all   - Build all Rust workspace crates"
	@echo "  make go         - Build Go gateway"
	@echo "  make go-dev     - Run Go gateway in dev mode (auto-restart on SIGUSR1)"
	@echo "  make cli        - Build Rust CLI (release)"
	@echo "  make go-binary  - Build Go gateway binary to dist/"
	@echo "  make test       - Run Rust + Go + CLI tests"
	@echo "  make go-lint    - Run golangci-lint on Go gateway"
	@echo "  make go-fmt     - Check Go formatting"
	@echo "  make check      - Run all checks (Rust + Go + CLI)"
	@echo "  make generate         - Run all code generation pipelines"
	@echo "  make generate-check   - Verify all generated files (per domain, names failing group)"
	@echo "  make tool-schemas-check  - Verify tool_schemas_gen.go is up to date"
	@echo "  make model-caps-check    - Verify model_caps_gen.go is up to date"
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
