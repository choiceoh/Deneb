---
name: autoresearch
version: "1.1.0"
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
     metric_cmd="python train.py" metric_name="val_bpb" \
     metric_direction="minimize" time_budget_sec=300 branch_tag="mar31" \
     metric_pattern="val_bpb:\s*([\d.]+)"

2. autoresearch action=start workdir=/path/to/project

3. autoresearch action=status workdir=/path/to/project   (check progress)

4. autoresearch action=stop workdir=/path/to/project      (halt the loop)

5. autoresearch action=results workdir=/path/to/project    (view full log)
```

## Actions

### `init` — Initialize experiment workspace

Sets up the experiment configuration in `<workdir>/.autoresearch/config.json`.
Automatically runs the baseline measurement to establish a reference point.

**Required parameters:**
- `workdir` — Path to the project directory (must be a git repo with clean working tree)
- `target_files` — Array of files the agent may modify (relative to workdir)
- `metric_cmd` — Shell command that runs the experiment and prints the metric
- `metric_name` — Human-readable name for the metric (e.g., "val_bpb", "accuracy", "build_size_kb")
- `metric_direction` — `"minimize"` or `"maximize"`
- `branch_tag` — Tag for the experiment branch (`autoresearch/<tag>`)

**Optional parameters:**
- `time_budget_sec` — Time budget per experiment in seconds (default: 300)
- `model` — LLM model for hypothesis generation (default: claude-sonnet-4-20250514)
- `metric_pattern` — Regex with one capture group to extract the metric from output.
  Example: `val_bpb:\s*([\d.]+)` extracts `1.087` from `val_bpb: 1.087`.
  If omitted, uses default heuristic (last number on last stdout line).

### `start` — Begin autonomous loop

Creates an isolated experiment branch (`autoresearch/<tag>`) and launches the
background experiment loop. Requires a clean git working tree.

Each iteration:
1. Reads target file contents and full experiment history
2. Analyzes trends: success rate, improvement velocity, plateau detection
3. Feeds trend analysis + kept/failed history to LLM for pattern-aware hypothesis generation
4. Applies code changes and git commits
5. Runs the experiment within the time budget
6. Extracts metric (via `metric_pattern` or heuristic), validates (NaN/Inf rejection)
7. Keeps if improved, reverts if not
8. Records to results.tsv with delta_from_best and best_so_far tracking
9. Saves full stdout/stderr to per-iteration log files
10. Sends Telegram notification with outcome and improvement %

### `stop` — Halt the loop

Gracefully stops the running experiment loop and returns a final summary
including trend analysis, top improvements, and overall improvement from baseline.

### `status` — Check progress

Returns current state (RUNNING/STOPPED) with experiment summary: iterations,
best metric, improvement %, success rate, trend analysis, consecutive failures.

### `results` — View experiment log

Returns the full experiment history. Use `format="tsv"` for raw TSV data.

## Metric Command Guidelines

The `metric_cmd` should produce output containing the metric value.
Use `metric_pattern` for structured output, or ensure the metric is the
last number printed to stdout.

Examples:
```bash
# ML training: structured output + pattern extraction
# metric_cmd: "python train.py"
# metric_pattern: "val_bpb:\s*([\d.]+)"
python train.py
# Output: "epoch 10 | val_bpb: 1.087 | train_loss: 0.95"

# Test coverage (heuristic extraction — last number)
go test -cover ./... 2>&1 | grep 'coverage:' | grep -oP '[\d.]+'

# Build size
make build && du -b ./build/output | cut -f1

# Benchmark score with pattern
# metric_cmd: "cargo bench"
# metric_pattern: "time:\s*(\d+\.?\d*)"
cargo bench
```

The `TIME_BUDGET` environment variable is set to the configured seconds,
so experiments can self-limit if they support it.

## Branch Isolation

On `start`, autoresearch automatically:
1. Checks that the working tree is clean (no uncommitted changes)
2. Records the current branch (for potential return)
3. Creates `autoresearch/<tag>` branch from current HEAD (or switches to it if it exists)
4. All experiment commits happen on this isolated branch

This keeps your main branch clean. The experiment branch contains
the full git history of all kept changes.

## Stuck Recovery

The runner automatically detects when the agent is stuck and progressively
escalates its strategy guidance:
- **3+ consecutive failures**: suggests changing strategy (e.g., switch from hyperparameter tuning to architecture changes)
- **5+ consecutive failures**: demands abandoning the current approach entirely
- **8+ consecutive failures**: forces reversion to simplest known-working configuration

## LLM Strategy Phases

The prompt automatically adapts to the experiment phase:
- **Early (1-10)**: Explore broadly — try different approaches
- **Exploration (11-30)**: Balance new ideas with refining winners
- **Exploitation (30+)**: Focus on fine-tuning the best-performing approaches

## Constants Override Mode

Instead of rewriting entire files, you can define **named constants** that the agent tunes.
The original source files are never permanently modified — only override values are proposed and tested.

### Setup

Add `constants` to the init call:

```
autoresearch action=init workdir=/path/to/project target_files=["train.py"] \
  metric_cmd="python train.py" metric_name="val_bpb" \
  metric_direction="minimize" time_budget_sec=300 branch_tag="constants-exp" \
  constants=[
    {"name": "LEARNING_RATE", "file": "train.py", "pattern": "lr\\s*=\\s*([\\d.]+)", "type": "float", "min": 0.0001, "max": 0.1},
    {"name": "BATCH_SIZE", "file": "train.py", "pattern": "batch_size\\s*=\\s*(\\d+)", "type": "int", "min": 8, "max": 256},
    {"name": "HIDDEN_DIM", "file": "train.py", "pattern": "hidden_dim\\s*=\\s*(\\d+)", "type": "int"}
  ]
```

Each constant requires:
- `name` — identifier shown to the LLM
- `file` — which target file contains it (must be in `target_files`)
- `pattern` — regex with one capture group for the value
- `type` — `float`, `int`, or `string`
- `min`/`max` — optional bounds (float/int only)

### How It Works

1. Constants are extracted from original files using the regex pattern
2. LLM proposes new values (not file rewrites)
3. Overrides are applied temporarily for the experiment
4. Original files are always restored after the experiment
5. Best overrides are saved to `.autoresearch/overrides.json`

### Applying Best Overrides

After the experiment loop finds optimal values:

```
autoresearch action=apply_overrides workdir=/path/to/project
```

This permanently bakes the best-found values into the source files using the same regex patterns.
Commit when ready.

## Data Tracking

```
<workdir>/.autoresearch/
  config.json      — experiment config + mutable state (best, baseline, consecutive failures)
  results.tsv      — iteration log with delta_from_best, best_so_far tracking
  overrides.json   — best-found constant override values (constants mode only)
  runs/
    0000.log       — baseline stdout/stderr
    0001.log       — iteration 1 stdout/stderr
    ...            — full experiment output preserved for debugging
```
