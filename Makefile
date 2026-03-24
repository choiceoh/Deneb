# Deneb Multi-Language Build
#
# Orchestrates Rust (core-rs), Go (gateway-go), and TypeScript (pnpm) builds.

.PHONY: all rust go ts clean test fmt check

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

check: rust-test go-test ts-check
	@echo "All checks passed"

# --- Protobuf (requires protoc + plugins) ---

proto:
	@echo "Protobuf compilation requires protoc. Install with:"
	@echo "  apt install protobuf-compiler"
	@echo "  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
	@echo "  cargo install protobuf-codegen"
	@echo ""
	@echo "Then run:"
	@echo "  protoc --go_out=gateway-go/pkg/protocol --go_opt=paths=source_relative proto/*.proto"

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
	@echo "  make proto      - Show protobuf compilation instructions"
