---
name: autoresearch
version: "1.0.0"
category: coding
description: "Autonomous AI experiment loop: iteratively modify code, run experiments with a fixed time budget, evaluate a scalar metric, and keep improvements or revert failures — all without human intervention. Inspired by karpathy/autoresearch. Use when: optimizing ML training (loss, accuracy), improving benchmark scores, reducing build size, increasing test coverage, or any codebase with a measurable fitness metric. NOT for: tasks without a clear numeric metric, UI/UX work, creative writing, or one-off code changes."
metadata:
  { "deneb": { "emoji": "🔬", "requires": { "bins": ["git"] } } }
---

# Autoresearch — Autonomous Experiment Loop

Autoresearch runs an **infinite modify → commit → experiment → evaluate → keep/revert** loop.
An AI agent proposes hypotheses, edits code, runs a fixed-time experiment, and keeps
improvements automatically. Designed for overnight/unattended execution on DGX Spark.

## Quick Start

```
1. autoresearch action=init workdir=/path/to/project target_files=["train.py"] \
     metric_cmd="python train.py 2>&1 | tail -1" metric_name="val_bpb" \
     metric_direction="minimize" time_budget_sec=300 branch_tag="mar31"

2. autoresearch action=start workdir=/path/to/project

3. autoresearch action=status workdir=/path/to/project   (check progress)

4. autoresearch action=stop workdir=/path/to/project      (halt the loop)

5. autoresearch action=results workdir=/path/to/project    (view full log)
```

## Actions

### `init` — Initialize experiment workspace

Sets up the experiment configuration in `<workdir>/.autoresearch/config.json`.

**Required parameters:**
- `workdir` — Path to the project directory
- `target_files` — Array of files the agent may modify (relative to workdir)
- `metric_cmd` — Shell command that runs the experiment and prints the metric value as its last output line
- `metric_name` — Human-readable name for the metric (e.g., "val_bpb", "accuracy", "build_size_kb")
- `metric_direction` — `"minimize"` or `"maximize"`
- `branch_tag` — Tag for the experiment branch (`autoresearch/<tag>`)

**Optional parameters:**
- `time_budget_sec` — Time budget per experiment in seconds (default: 300)
- `model` — LLM model for hypothesis generation (default: claude-sonnet-4-20250514)

### `start` — Begin autonomous loop

Launches the background experiment loop. The loop runs indefinitely until stopped.
Each iteration:
1. Reads current target file contents and experiment history
2. Asks LLM for a hypothesis and code modifications
3. Applies changes and git commits
4. Runs the experiment within the time budget
5. Evaluates the metric — keeps if improved, reverts if not
6. Records results to `.autoresearch/results.tsv`
7. Sends Telegram notification with iteration outcome

### `stop` — Halt the loop

Gracefully stops the running experiment loop and returns a final summary.

### `status` — Check progress

Returns current state (RUNNING/STOPPED) with experiment summary: iterations,
best metric, improvement percentage, consecutive failures.

### `results` — View experiment log

Returns the full experiment history. Use `format="tsv"` for raw TSV data.

## Metric Command Guidelines

The `metric_cmd` must:
- Print a single numeric value as the last non-empty line of stdout
- Complete within `time_budget_sec` (the runner enforces a timeout)
- Use the `TIME_BUDGET` environment variable if the experiment can self-limit

Examples:
```bash
# ML training: run for N seconds, print validation loss
python train.py 2>&1 | tail -1

# Test coverage
go test -cover ./... 2>&1 | grep 'coverage:' | grep -oP '[\d.]+'

# Build size
make build && du -b ./build/output | cut -f1

# Benchmark score
cargo bench 2>&1 | grep 'time:' | grep -oP '[\d.]+'
```

## Stuck Recovery

The runner automatically detects when the agent is stuck:
- **3+ consecutive failures**: prompts the LLM to change strategy
- **5+ consecutive failures**: prompts a fundamental rethinking of approach

## File Structure

```
<workdir>/.autoresearch/
  config.json    — experiment configuration + mutable state
  results.tsv    — iteration log (TSV format)
```
