# Deneb Multi-Language Build
#
# Orchestrates Rust (core-rs), Go (gateway-go), and TypeScript (pnpm) builds.

.PHONY: all rust rust-debug rust-test rust-bench rust-clean \
       go go-run go-test go-clean \
       ts ts-check ts-test \
       test clean check \
       proto proto-go proto-rust proto-ts proto-check proto-lint proto-watch \
       info

# Default: build everything
all: rust go

# --- Rust core library ---

rust:
	cd core-rs && cargo build --release

rust-debug:
	cd core-rs && cargo build

rust-test:
	cd core-rs && cargo test

rust-bench:
	cd core-rs && cargo bench

rust-clean:
	cd core-rs && cargo clean

# --- Go gateway ---

go:
	cd gateway-go && go build ./...

go-run:
	cd gateway-go && go run ./cmd/gateway/

go-test:
	cd gateway-go && go test ./...

go-clean:
	cd gateway-go && go clean ./...

# --- TypeScript (existing) ---

ts:
	pnpm build

ts-check:
	pnpm check

ts-test:
	pnpm test:fast

# --- Combined operations ---

test: rust-test go-test
	@echo "Rust and Go tests passed"

clean: rust-clean go-clean
	@echo "Cleaned Rust and Go build artifacts"

check: proto-check rust-test go-test ts-check
	@echo "All checks passed"

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

# --- Info ---

info:
	@echo "Deneb Multi-Language Build"
	@echo ""
	@echo "  make rust       - Build Rust core library (release)"
	@echo "  make go         - Build Go gateway"
	@echo "  make ts         - Build TypeScript (pnpm)"
	@echo "  make test       - Run Rust + Go tests"
	@echo "  make check      - Run all checks (Rust + Go + TS)"
	@echo "  make clean      - Clean Rust + Go build artifacts"
	@echo "  make proto      - Generate protobuf code (Go + Rust + TS)"
	@echo "  make proto-go   - Generate Go protobuf structs"
	@echo "  make proto-rust - Generate Rust protobuf structs"
	@echo "  make proto-ts   - Generate TypeScript protobuf types"
	@echo "  make proto-check - Generate + verify no uncommitted diffs"
	@echo "  make proto-lint  - Lint proto files only"
	@echo "  make proto-watch - Watch proto files and regenerate on change"
