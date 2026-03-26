# Deneb Multi-Language Build
#
# Orchestrates Rust (core-rs workspace), Go (gateway-go), and TypeScript (pnpm) builds.

.PHONY: all rust rust-vega rust-dgx rust-all rust-debug rust-test rust-fmt rust-clippy rust-bench rust-clean \
       go go-ffi go-pure go-run go-dev go-test go-test-pure go-test-fuzz go-vet go-clean go-binary go-binary-dgx gateway-prod gateway-dgx \
       cli cli-debug cli-test cli-fmt cli-clippy cli-bench cli-clean \
       cli-cross-linux-x64 cli-cross-linux-arm64 cli-cross-darwin-x64 cli-cross-darwin-arm64 \
       cli-cross-win-x64 cli-cross-all \
       ts ts-check ts-test \
       test test-all clean check fmt \
       proto proto-go proto-rust proto-ts proto-check proto-lint proto-watch \
       error-code-sync \
       info

# Default: build Rust first (produces .a), then Go (links it via CGo), then CLI.
all: rust go cli

# --- Rust core library (workspace) ---

# Build core crate for CGo static linking (minimal — no vega/ml).
# --no-default-features disables "napi_binding" (Node.js addon), producing
# only the staticlib (.a) and rlib needed by the Go gateway via CGo.
# Use `make rust-napi` for Node.js native addon builds (includes cdylib).
# Use `make rust-dgx` for DGX Spark production builds (includes vega + ml + CUDA).
rust:
	cd core-rs && cargo build --release -p deneb-core --no-default-features

# Build core with Vega search engine enabled (FTS-only, no ML).
rust-vega:
	cd core-rs && cargo build --release -p deneb-core --no-default-features --features vega

# Build core with Vega + ML + CUDA for DGX Spark (GGUF inference).
# Requires: llama.cpp build deps + CUDA toolkit on the host.
rust-dgx:
	cd core-rs && cargo build --release -p deneb-core --no-default-features --features dgx

# Build all workspace crates (core + vega + ml).
rust-all:
	cd core-rs && cargo build --release --workspace

rust-napi:
	cd core-rs && cargo build --release -p deneb-core

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
	cd gateway-go && go build ./...

# Pure-Go build with fallback implementations (no Rust required).
go-pure:
	cd gateway-go && CGO_ENABLED=0 go build -tags no_ffi ./...

go-run: go
	cd gateway-go && go run ./cmd/gateway/

# Dev mode: build and run gateway with auto-restart on SIGUSR1 (exit code 75).
# Uses go build instead of go run to avoid signal forwarding issues.
go-dev:
	@echo "Starting Go gateway in dev mode (auto-restart on SIGUSR1)..."
	@while true; do \
		cd gateway-go && go build -o /tmp/deneb-gateway-dev ./cmd/gateway/ && /tmp/deneb-gateway-dev $(ARGS); \
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

go-binary: rust go
	cd gateway-go && go build -o ../dist/deneb-gateway ./cmd/gateway/

# Build DGX Spark production binary (Rust with vega + ml + CUDA, then Go).
go-binary-dgx: rust-dgx go
	cd gateway-go && go build -o ../dist/deneb-gateway ./cmd/gateway/

# Build production gateway: Go binary + CLI, copies both to dist/.
gateway-prod: go-binary cli
	cp cli-rs/target/release/deneb dist/deneb-rs 2>/dev/null || true
	@echo "Production gateway ready: dist/deneb-gateway + dist/deneb-rs"

# Build DGX Spark production gateway (with Vega + ML + CUDA).
gateway-dgx: go-binary-dgx cli
	cp cli-rs/target/release/deneb dist/deneb-rs 2>/dev/null || true
	@echo "DGX Spark production gateway ready: dist/deneb-gateway + dist/deneb-rs (vega+ml+cuda)"

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

# Cross-compilation targets (requires cross or appropriate rustup targets)
cli-cross-linux-x64:
	cd cli-rs && cargo build --release --target x86_64-unknown-linux-gnu

cli-cross-linux-arm64:
	cd cli-rs && cargo build --release --target aarch64-unknown-linux-gnu

cli-cross-darwin-x64:
	cd cli-rs && cargo build --release --target x86_64-apple-darwin

cli-cross-darwin-arm64:
	cd cli-rs && cargo build --release --target aarch64-apple-darwin

cli-cross-win-x64:
	cd cli-rs && cargo build --release --target x86_64-pc-windows-msvc

cli-cross-all: cli-cross-linux-x64 cli-cross-linux-arm64 cli-cross-darwin-x64 cli-cross-darwin-arm64 cli-cross-win-x64

cli-install: cli
	./cli-rs/scripts/install.sh

# --- TypeScript (existing) ---

ts:
	pnpm build

ts-check:
	pnpm check

ts-test:
	pnpm test:fast

# --- Combined operations ---

test: rust-test go-test cli-test
	@echo "Rust, Go, and CLI tests passed"

test-all: rust-test go-test cli-test ts-test
	@echo "All tests passed (Rust + Go + CLI + TypeScript)"

clean: rust-clean go-clean cli-clean
	@echo "Cleaned Rust, Go, and CLI build artifacts"

check: proto-check error-code-sync rust-fmt rust-clippy rust-test cli-fmt cli-clippy cli-test go-vet go-test ts-check
	@echo "All checks passed"

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

proto-ts:
	./scripts/proto-gen.sh --ts

proto-check:
	./scripts/proto-gen.sh --check

proto-lint:
	./scripts/proto-gen.sh --lint

proto-watch:
	./scripts/proto-gen.sh --watch

# --- Error code sync ---

error-code-sync:
	./scripts/error-code-sync-check.sh

# --- Info ---

info:
	@echo "Deneb Multi-Language Build"
	@echo ""
	@echo "  make rust       - Build Rust core crate (release, CGo, minimal)"
	@echo "  make rust-vega  - Build Rust core + Vega search (FTS-only)"
	@echo "  make rust-dgx   - Build Rust core + Vega + ML + CUDA (DGX Spark)"
	@echo "  make rust-all   - Build all Rust workspace crates"
	@echo "  make go         - Build Go gateway"
	@echo "  make go-dev     - Run Go gateway in dev mode (auto-restart on SIGUSR1)"
	@echo "  make cli        - Build Rust CLI (release)"
	@echo "  make go-binary  - Build Go gateway binary to dist/"
	@echo "  make gateway-dgx - Build DGX Spark production gateway (vega+ml+cuda)"
	@echo "  make ts         - Build TypeScript (pnpm)"
	@echo "  make test       - Run Rust + Go + CLI tests"
	@echo "  make check      - Run all checks (Rust + Go + CLI + TS)"
	@echo "  make clean      - Clean Rust, Go, and CLI build artifacts"
	@echo "  make cli-bench  - Run CLI startup benchmark"
	@echo "  make cli-cross-all - Cross-compile CLI for all platforms"
	@echo "  make proto      - Generate protobuf code (Go + Rust + TS)"
	@echo "  make proto-go   - Generate Go protobuf structs"
	@echo "  make proto-rust - Generate Rust protobuf structs"
	@echo "  make proto-ts   - Generate TypeScript protobuf types"
	@echo "  make proto-check - Generate + verify no uncommitted diffs"
	@echo "  make proto-lint  - Lint proto files only"
	@echo "  make proto-watch - Watch proto files and regenerate on change"
