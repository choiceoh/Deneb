---
title: "Open-Source Tool Recommendations"
summary: "Recommended open-source tools for observability, testing, security, analytics, and code quality"
read_when:
  - You want to evaluate which open-source tools to integrate next
  - You need implementation guidance for a specific recommended tool
  - You are planning a development sprint and need to prioritize tooling improvements
---

# Open-source tool recommendations

This document recommends open-source tools that fit Deneb's lean dependency philosophy and single-server (DGX Spark) deployment model. Each tool is evaluated for integration effort and includes a concrete implementation plan.

## Summary

| Tool | Category | Priority | Effort | External dep |
|------|----------|----------|--------|--------------|
| `slog` structured handler | Observability | P0 | Low | No |
| Prometheus Go client | Observability | P0 | Medium | Yes |
| `proptest` | Testing | P0 | Low | Dev-only |
| `rapid` | Testing | P0 | Low | Test-only |
| Go benchmark (stdlib) | Testing | P0 | Low | No |
| `cargo-deny` | Security | P0 | Low | CLI tool |
| DuckDB | Analytics | P1 | Medium | CLI tool |
| `cargo-machete` | Code quality | P1 | Low | CLI tool |
| `golangci-lint` | Code quality | P1 | Low | CLI tool |

<Info>
Priority levels: **P0** = immediate value, integrate first. **P1** = next sprint, meaningful but less urgent.
</Info>

---

## Observability

### slog structured handler (Go stdlib)

Go 1.21+ standard library structured logging. No external dependency required.

**What it does.** Replaces default `slog` text output with a JSON handler that automatically injects contextual fields (request ID, session ID, RPC method) into every log line. This makes logs machine-parseable and searchable without adding any dependency.

**Why it fits Deneb.** The gateway already uses `slog` throughout. Switching to `slog.NewJSONHandler` with `slog.With()` context propagation is a minimal change that dramatically improves log quality for a single-server deployment where `grep` and `jq` are the primary log analysis tools.

**Integration effort:** Low

<Steps>
<Step title="Configure JSON handler at startup">

In `gateway-go/cmd/gateway/main.go`, replace the default logger with a JSON handler:

```go
handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelInfo,
})
slog.SetDefault(slog.New(handler))
```

</Step>
<Step title="Add contextual fields to RPC dispatch">

In `gateway-go/internal/rpc/`, use `slog.With()` to attach `request_id` and `method` to the logger before dispatching:

```go
logger := slog.With(
    "request_id", req.ID,
    "method", req.Method,
)
```

</Step>
<Step title="Propagate session context">

In `gateway-go/internal/session/`, attach `session_id` and `session_status` to the logger for all session-scoped operations.

</Step>
</Steps>

**Files to modify:** `gateway-go/cmd/gateway/main.go`, `gateway-go/internal/rpc/`, `gateway-go/internal/session/`

---

### Prometheus Go client

Metrics collection library for Go applications.

**What it does.** Exposes a `/metrics` HTTP endpoint with structured counters, histograms, and gauges. Tracks RPC request rates, response latencies, LLM API call durations, memory store sizes, and system resource usage.

**Why it fits Deneb.** On a single DGX Spark server, a `/metrics` endpoint provides instant visibility without external infrastructure. Query with `curl localhost:PORT/metrics` or optionally connect Grafana later. The Prometheus data model (labels, histograms) is the industry standard and works well with single-instance deployments.

**Integration effort:** Medium — adds `prometheus/client_golang` as a Go dependency.

<Steps>
<Step title="Add dependency">

```bash
cd gateway-go && go get github.com/prometheus/client_golang/prometheus
```

</Step>
<Step title="Create metrics package">

Create `gateway-go/internal/metrics/metrics.go` with core metric definitions:

```go
var (
    RPCRequestsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "deneb_rpc_requests_total",
            Help: "Total RPC requests by method and status",
        },
        []string{"method", "status"},
    )
    RPCDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "deneb_rpc_duration_seconds",
            Help:    "RPC request duration in seconds",
            Buckets: prometheus.DefBuckets,
        },
        []string{"method"},
    )
    LLMRequestDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "deneb_llm_request_duration_seconds",
            Help:    "LLM API request duration in seconds",
            Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
        },
        []string{"provider", "model"},
    )
)
```

</Step>
<Step title="Register metrics endpoint">

In `gateway-go/internal/server/server.go`, add a `/metrics` handler using `promhttp.Handler()`.

</Step>
<Step title="Instrument RPC and LLM paths">

Add `RPCRequestsTotal.Inc()` and `RPCDuration.Observe()` calls in the RPC dispatcher. Add `LLMRequestDuration.Observe()` in `gateway-go/internal/llm/client.go`.

</Step>
</Steps>

**Files to modify:** `gateway-go/go.mod`, new `gateway-go/internal/metrics/metrics.go`, `gateway-go/internal/server/server.go`, `gateway-go/internal/rpc/`, `gateway-go/internal/llm/client.go`

---

## Testing

### proptest (Rust)

Property-based testing framework for Rust.

**What it does.** Automatically generates random inputs and verifies that invariants hold across thousands of test cases. When a failure is found, it shrinks the input to the minimal reproducing case. This catches edge cases that hand-written unit tests miss.

**Why it fits Deneb.** The Rust core has critical FFI boundaries (Go-Rust type conversions), protocol frame serialization, and SIMD-accelerated math. These are exactly the domains where property-based testing excels — verifying roundtrip correctness and numerical stability across arbitrary inputs.

**Integration effort:** Low — dev-dependency only, no runtime cost.

<Steps>
<Step title="Add dev dependency">

In `core-rs/core/Cargo.toml`:

```toml
[dev-dependencies]
proptest = "1.5"
```

</Step>
<Step title="Add FFI roundtrip tests">

In protocol/frame serialization modules, add proptest strategies:

```rust
use proptest::prelude::*;

proptest! {
    #[test]
    fn request_frame_roundtrip(method in "[a-z.]{1,64}", id in any::<u64>()) {
        let frame = RequestFrame { method, id, .. };
        let bytes = frame.encode();
        let decoded = RequestFrame::decode(&bytes).unwrap();
        prop_assert_eq!(frame, decoded);
    }
}
```

</Step>
<Step title="Add SIMD cosine similarity tests">

Verify that SIMD-accelerated cosine similarity produces results within epsilon of a naive implementation for arbitrary vector pairs.

</Step>
</Steps>

**Files to modify:** `core-rs/core/Cargo.toml`, test modules in `core-rs/core/src/protocol/`, `core-rs/core/src/memory_search/`

---

### rapid (Go)

Property-based testing library for Go.

**What it does.** The Go equivalent of proptest. Generates random typed inputs, checks properties, and shrinks failures to minimal cases. Integrates with Go's standard `testing` package.

**Why it fits Deneb.** The Go gateway has complex state machines (session lifecycle), a 130+ method RPC dispatcher, and protocol type consistency requirements. Property-based tests can verify invariants like "any valid session transition sequence ends in a terminal state" or "serialization always roundtrips."

**Integration effort:** Low — test-only dependency.

<Steps>
<Step title="Add test dependency">

```bash
cd gateway-go && go get -t pgregory.net/rapid
```

</Step>
<Step title="Add session state machine property tests">

```go
func TestSessionTransitionsAlwaysTerminate(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        status := rapid.SampledFrom(validStatuses).Draw(t, "initial")
        // Apply random valid transitions
        // Assert: final state is terminal
    })
}
```

</Step>
<Step title="Add protocol consistency property tests">

Verify that hand-written JSON types and generated protobuf types produce identical wire formats for arbitrary inputs.

</Step>
</Steps>

**Files to modify:** `gateway-go/go.mod`, new test files in `gateway-go/internal/session/`, `gateway-go/pkg/protocol/`

---

### Go benchmark (stdlib)

Go's built-in benchmarking via `testing.B`. No external dependency.

**What it does.** Measures function execution time with statistical rigor. Reports ns/op, bytes/op, and allocs/op. Benchmarks can be tracked over time to detect performance regressions.

**Why it fits Deneb.** The gateway has several performance-critical paths (FFI calls, SQLite queries, Vega search, RPC dispatch) that currently lack any performance measurement. stdlib benchmarks establish baselines with zero dependency cost.

**Integration effort:** Low

<Steps>
<Step title="Add RPC dispatch benchmarks">

Create `gateway-go/internal/rpc/dispatch_bench_test.go`:

```go
func BenchmarkRPCDispatch(b *testing.B) {
    registry := setupTestRegistry()
    req := makeTestRequest("session.list")
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        registry.Dispatch(req)
    }
}
```

</Step>
<Step title="Add FFI call overhead benchmarks">

Create benchmarks in `gateway-go/internal/ffi/` measuring CGo call overhead for key functions like `deneb_validate_frame`, `deneb_cosine_similarity`.

</Step>
<Step title="Add Makefile target">

Add `make bench` target: `cd gateway-go && go test -bench=. -benchmem ./...`

</Step>
</Steps>

**Files to modify:** new `*_bench_test.go` files in `gateway-go/internal/rpc/`, `gateway-go/internal/ffi/`, `Makefile`

---

## Security and auditing

### cargo-deny

Cargo plugin for linting dependencies against security, license, and policy rules.

**What it does.** Checks all Rust dependencies for:
- **Advisories** — known vulnerabilities from the RustSec advisory database
- **Licenses** — ensures all dependencies use allowed licenses (MIT, Apache-2.0, etc.)
- **Bans** — blocks specific crates or duplicate versions
- **Sources** — restricts dependency sources to crates.io (blocks unknown registries)

**Why it fits Deneb.** The Rust core links as a static library into the Go gateway via FFI. A supply-chain vulnerability in any Rust dependency directly compromises the gateway binary. `cargo-deny` provides a single configuration file (`deny.toml`) that enforces security policy automatically.

**Integration effort:** Low — CLI tool + config file, no code changes.

<Steps>
<Step title="Install cargo-deny">

```bash
cargo install cargo-deny
```

</Step>
<Step title="Create deny.toml">

Create `deny.toml` at project root:

```toml
[advisories]
vulnerability = "deny"
unmaintained = "warn"

[licenses]
allow = ["MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "ISC", "Unicode-3.0"]

[bans]
multiple-versions = "warn"
wildcards = "deny"

[sources]
unknown-registry = "deny"
unknown-git = "deny"
```

</Step>
<Step title="Add to Makefile and CI">

Add `make deny` target: `cargo deny check`

Add to `.github/workflows/` as a CI check that runs on PRs touching `core-rs/` or `Cargo.lock`.

</Step>
</Steps>

**Files to modify:** new `deny.toml` (project root), `Makefile`, `.github/workflows/`

---

## Data and analytics

### DuckDB

Embedded analytical database optimized for OLAP queries.

**What it does.** Reads JSONL, CSV, and Parquet files directly with SQL — no data loading step required. Designed for analytical queries (aggregations, window functions, pivots) on local data. Runs as a CLI tool or embeddable library.

**Why it fits Deneb.** Session logs are stored as JSONL files under `~/.deneb/agents/*/sessions/*.jsonl`. Currently there is no way to query these analytically (token usage trends, cost breakdowns, skill usage patterns). DuckDB can query these files in-place with SQL, leveraging the DGX Spark's abundant memory for fast aggregation.

**Integration effort:** Medium — CLI tool for ad-hoc analysis, optional Go binding for in-gateway analytics.

<Steps>
<Step title="Install DuckDB CLI">

```bash
# DGX Spark (aarch64 Linux)
curl -LO https://github.com/duckdb/duckdb/releases/latest/download/duckdb_cli-linux-amd64.zip
unzip duckdb_cli-linux-amd64.zip -d /usr/local/bin/
```

</Step>
<Step title="Create analysis SQL scripts">

Create `scripts/analytics/` with reusable SQL queries:

```sql
-- scripts/analytics/token-usage.sql
-- Token usage summary by model and day
SELECT
    json_extract_string(line, '$.model') AS model,
    date_trunc('day', json_extract_string(line, '$.timestamp')::timestamp) AS day,
    sum(json_extract(line, '$.usage.input_tokens')::int) AS input_tokens,
    sum(json_extract(line, '$.usage.output_tokens')::int) AS output_tokens
FROM read_json_auto('~/.deneb/agents/*/sessions/*.jsonl')
WHERE json_extract_string(line, '$.type') = 'llm_response'
GROUP BY model, day
ORDER BY day DESC;
```

</Step>
<Step title="Add Makefile target">

Add `make analytics` target: `duckdb < scripts/analytics/token-usage.sql`

</Step>
</Steps>

**Files to modify:** new `scripts/analytics/` directory, `Makefile`

---

## Code quality

### cargo-machete

Detects unused dependencies in Rust projects.

**What it does.** Scans Rust source files and compares against `Cargo.toml` declarations. Reports dependencies that are listed but never referenced in code. Fast — uses simple text matching rather than full compilation.

**Why it fits Deneb.** Maintaining a lean dependency tree is a core project principle. As the Rust workspace grows across 4 crates, unused dependencies can accumulate during refactors. `cargo-machete` catches these automatically.

**Integration effort:** Low — CLI tool, no code changes.

<Steps>
<Step title="Install cargo-machete">

```bash
cargo install cargo-machete
```

</Step>
<Step title="Run and verify">

```bash
cd core-rs && cargo machete
```

Review results — some false positives may occur for proc-macro or build-script dependencies. Add exceptions to `.cargo-machete.toml` if needed.

</Step>
<Step title="Add to Makefile">

Add `make machete` target: `cd core-rs && cargo machete`

Optionally add to `make check` pipeline.

</Step>
</Steps>

**Files to modify:** `Makefile`, optionally `.cargo-machete.toml`

---

### golangci-lint

Meta-linter that runs 50+ Go linters in a single pass.

**What it does.** Aggregates linters for unused code (`unused`), error handling (`errcheck`), performance anti-patterns (`gocritic`), style (`revive`), and security (`gosec`) into one tool. Respects a `.golangci.yml` configuration file for enabling/disabling specific linters.

**Why it fits Deneb.** The project already has a `.golangci.yml` configuration file but it is not yet integrated into the build pipeline. Activating it catches bugs early (unchecked errors, shadowed variables, inefficient patterns) without manual code review overhead.

**Integration effort:** Low — configuration file already exists.

<Steps>
<Step title="Install golangci-lint">

```bash
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b /usr/local/bin
```

</Step>
<Step title="Verify existing configuration">

Review `.golangci.yml` to confirm enabled linters match project needs. Key linters to enable:

- `errcheck` — unchecked error returns
- `govet` — suspicious constructs
- `staticcheck` — advanced static analysis
- `unused` — unused code detection
- `gosec` — security-focused checks

</Step>
<Step title="Add to Makefile and CI">

Add `make lint` target:

```makefile
lint:
	cd gateway-go && golangci-lint run ./...
```

Add to `.github/workflows/` as a CI check on PRs touching `gateway-go/`.

</Step>
</Steps>

**Files to modify:** `.golangci.yml` (update if needed), `Makefile`, `.github/workflows/`

---

## Implementation roadmap

<Steps>
<Step title="Phase 1: Zero-dependency wins (P0, Low effort)">

Start with tools that require no new runtime dependencies:

1. **slog structured handler** — immediate log quality improvement
2. **Go benchmarks** — establish performance baselines
3. **cargo-deny** — security policy enforcement
4. **cargo-machete** — dependency hygiene

All four can be integrated in parallel with no conflicts.

</Step>
<Step title="Phase 2: Test infrastructure (P0, Low effort)">

Add property-based testing frameworks:

1. **proptest** (Rust dev-dependency)
2. **rapid** (Go test-dependency)

Write initial property tests for FFI boundaries, protocol roundtrips, and session state machines.

</Step>
<Step title="Phase 3: Observability and quality (P0-P1, Medium effort)">

Add runtime observability and lint enforcement:

1. **Prometheus Go client** — `/metrics` endpoint with RPC and LLM metrics
2. **golangci-lint** — CI-enforced linting

</Step>
<Step title="Phase 4: Analytics (P1, Medium effort)">

Set up analytical capability:

1. **DuckDB** — CLI install + SQL scripts for session log analysis

</Step>
</Steps>
