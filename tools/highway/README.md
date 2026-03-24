# Highway — High-Performance Test Orchestration Engine

Rust-native test orchestration engine that replaces slow JavaScript-based test tooling with native-speed alternatives.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Highway CLI (Rust)                     │
├──────────────┬──────────────┬──────────────┬────────────┤
│  oxc_parser  │   xxHash3    │  LPT + Local │  Parallel  │
│  Import      │  Content     │  Search      │  Vitest    │
│  Graph       │  Addressed   │  Bin-Packing │  Shard     │
│  Analyzer    │  Cache       │  Scheduler   │  Runner    │
└──────────────┴──────────────┴──────────────┴────────────┘
```

### Components

1. **Import Graph Analyzer** (`analyzer.rs`)
   - Parses all TS/JS files using `oxc_parser` (native speed)
   - Builds forward + reverse dependency graph in parallel via `rayon`
   - Resolves imports using `oxc_resolver` (TypeScript-aware)
   - Finds affected tests via BFS on reverse edges

2. **Content-Addressed Cache** (`cache.rs`)
   - Hashes files using `xxHash3-128` (fastest non-crypto hash)
   - Each test's cache key = hash(test + all transitive deps + config)
   - Skips tests with unchanged dependency trees
   - Persists to `.highway-cache.json`

3. **Optimal Scheduler** (`scheduler.rs`)
   - Longest Processing Time (LPT) bin-packing algorithm
   - Local search refinement for load balancing
   - Respects test isolation behaviors (isolated, singleton, thread-only)
   - Uses historical timing data + baseline timings

4. **Parallel Runner** (`runner.rs`)
   - Spawns Vitest shards as parallel subprocesses
   - Real-time progress reporting
   - Automatic cache updates with results

## Usage

```bash
# Build
cd tools/highway && cargo build --release

# Analyze: find affected tests for changed files
highway analyze --git
highway analyze src/gateway/server.ts src/channels/registry.ts

# Cache management
highway cache status
highway cache clear
highway cache hashes

# Generate optimal schedule
highway schedule --cpus 8

# Full pipeline: analyze → cache → schedule → execute
highway run --git
highway run --git --dry-run     # preview without executing
highway run src/foo.ts          # run tests affected by specific files
highway run --no-cache          # force all tests to run

# Export import graph
highway graph --output json
highway graph --output dot --filter src/gateway/
```

## Integration with pnpm

```bash
# From project root
alias highway='./tools/highway/target/release/highway --root .'

# Replace dev-affected.ts
highway analyze --git --format json

# Smart test run
highway run --git
```

## Performance

| Operation                 | JS (current) | Highway (Rust) | Speedup  |
| ------------------------- | ------------ | -------------- | -------- |
| Import graph build        | ~3-5s        | ~50-200ms      | 15-100x  |
| File hashing (1000 files) | ~500ms       | ~10ms          | 50x      |
| Affected test analysis    | ~2-4s        | ~1-5ms         | 400-800x |
| Schedule generation       | ~100ms       | ~1ms           | 100x     |
| Cache check               | N/A          | ~5ms           | ∞ (new)  |
