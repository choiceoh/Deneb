# Deneb Multi-Language Build
#
# Orchestrates Rust (core-rs), Go (gateway-go), and TypeScript (pnpm) builds.

.PHONY: all rust rust-debug rust-test rust-fmt rust-clippy rust-bench rust-clean \
       go go-ffi go-pure go-run go-test go-test-pure go-test-fuzz go-vet go-clean \
       ts ts-check ts-test \
       test test-all clean check fmt \
       proto proto-go proto-rust proto-ts proto-check proto-lint proto-watch \
       info

# Default: build Rust first (produces .a), then Go (links it via CGo).
all: rust go

# --- Rust core library ---

# Build for CGo static linking (no napi-rs).
# Use `make rust-napi` for Node.js native addon builds.
rust:
	cd core-rs && cargo build --release --no-default-features

rust-napi:
	cd core-rs && cargo build --release

rust-debug:
	cd core-rs && cargo build

rust-test:
	cd core-rs && cargo test

rust-bench:
	cd core-rs && cargo bench

rust-fmt:
	cd core-rs && cargo fmt -- --check

rust-clippy:
	cd core-rs && cargo clippy --all-targets -- -D warnings

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

go-test:
	cd gateway-go && go test -race -count=1 ./...

go-test-pure:
	cd gateway-go && CGO_ENABLED=0 go test -tags no_ffi -count=1 ./...

go-test-fuzz:
	cd gateway-go && go test ./internal/bridge/ -fuzz=FuzzParseRequestFrame -fuzztime=10s

go-vet:
	cd gateway-go && go vet ./...

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

test-all: rust-test go-test ts-test
	@echo "All tests passed (Rust + Go + TypeScript)"

clean: rust-clean go-clean
	@echo "Cleaned Rust and Go build artifacts"

check: proto-check rust-fmt rust-clippy rust-test go-vet go-test ts-check
	@echo "All checks passed"

fmt:
	cd core-rs && cargo fmt
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
