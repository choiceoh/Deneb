//! Highway — High-performance test orchestration engine for Deneb.
//!
//! Subcommands:
//!   analyze  — Build import graph, find affected tests for changed files
//!   cache    — Show cache status, clear cache, or compute hashes
//!   schedule — Generate optimal parallel execution plan
//!   run      — Full pipeline: analyze → cache → schedule → execute
//!   graph    — Export the full import graph (JSON/DOT)

mod analyzer;
mod cache;
mod runner;
mod scheduler;

use std::path::PathBuf;
use std::time::Instant;

use anyhow::{Context, Result};
use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(name = "highway", version, about = "High-performance test orchestration engine")]
struct Cli {
    /// Project root directory
    #[arg(long, default_value = ".", global = true)]
    root: PathBuf,

    /// Output format: json or text
    #[arg(long, default_value = "text", global = true)]
    format: OutputFormat,

    #[command(subcommand)]
    command: Commands,
}

#[derive(Clone, clap::ValueEnum)]
enum OutputFormat {
    Json,
    Text,
}

#[derive(Subcommand)]
enum Commands {
    /// Analyze import graph and find affected tests for changed files
    Analyze {
        /// Changed files (if empty, uses git diff)
        #[arg(trailing_var_arg = true)]
        files: Vec<PathBuf>,

        /// Use git diff to detect changed files (against HEAD)
        #[arg(long, default_value_t = false)]
        git: bool,

        /// Git base ref for diff (default: HEAD)
        #[arg(long, default_value = "HEAD")]
        base: String,

        /// Include only staged changes
        #[arg(long, default_value_t = false)]
        staged: bool,
    },

    /// Manage the test result cache
    Cache {
        #[command(subcommand)]
        action: CacheAction,
    },

    /// Generate optimal parallel execution schedule
    Schedule {
        /// Test files to schedule (if empty, uses all tests)
        #[arg(trailing_var_arg = true)]
        files: Vec<PathBuf>,

        /// Number of CPU cores to use (0 = auto-detect)
        #[arg(long, default_value_t = 0)]
        cpus: usize,
    },

    /// Full pipeline: analyze → cache → schedule → execute
    Run {
        /// Changed files (if empty, runs all tests)
        #[arg(trailing_var_arg = true)]
        files: Vec<PathBuf>,

        /// Use git diff to detect changed files
        #[arg(long, default_value_t = false)]
        git: bool,

        /// Git base ref for diff
        #[arg(long, default_value = "HEAD")]
        base: String,

        /// Only staged changes
        #[arg(long, default_value_t = false)]
        staged: bool,

        /// Number of CPU cores (0 = auto)
        #[arg(long, default_value_t = 0)]
        cpus: usize,

        /// Dry run: show what would execute without running
        #[arg(long, default_value_t = false)]
        dry_run: bool,

        /// Skip cache (force all tests to run)
        #[arg(long, default_value_t = false)]
        no_cache: bool,

        /// Verbose output (show vitest stdout/stderr)
        #[arg(long, default_value_t = false)]
        verbose: bool,
    },

    /// Export the import graph
    Graph {
        /// Output format: json or dot
        #[arg(long, default_value = "json")]
        output: GraphFormat,

        /// Only include files matching this pattern
        #[arg(long)]
        filter: Option<String>,
    },
}

#[derive(Subcommand)]
enum CacheAction {
    /// Show cache status and statistics
    Status,
    /// Clear all cached results
    Clear,
    /// Compute and display content hashes for test files
    Hashes,
}

#[derive(Clone, clap::ValueEnum)]
enum GraphFormat {
    Json,
    Dot,
}

fn main() -> Result<()> {
    let cli = Cli::parse();
    let root = cli
        .root
        .canonicalize()
        .context("canonicalize project root")?;

    match cli.command {
        Commands::Analyze {
            files,
            git,
            base,
            staged,
        } => cmd_analyze(&root, files, git, &base, staged, &cli.format),
        Commands::Cache { action } => cmd_cache(&root, action, &cli.format),
        Commands::Schedule { files, cpus } => cmd_schedule(&root, files, cpus, &cli.format),
        Commands::Run {
            files,
            git,
            base,
            staged,
            cpus,
            dry_run,
            no_cache,
            verbose,
        } => cmd_run(
            &root,
            files,
            git,
            &base,
            staged,
            cpus,
            dry_run,
            no_cache,
            verbose,
            &cli.format,
        ),
        Commands::Graph { output, filter } => cmd_graph(&root, output, filter, &cli.format),
    }
}

// ─── analyze ───────────────────────────────────────────────────────────────────

fn cmd_analyze(
    root: &PathBuf,
    files: Vec<PathBuf>,
    git: bool,
    base: &str,
    staged: bool,
    format: &OutputFormat,
) -> Result<()> {
    let start = Instant::now();

    eprintln!("⚡ Building import graph...");
    let config = analyzer::AnalyzerConfig {
        root: root.clone(),
        ..Default::default()
    };
    let graph = analyzer::build_import_graph(&config)?;
    let graph_time = start.elapsed();
    eprintln!(
        "  Graph: {} nodes, {} edges in {:.0?}",
        graph.forward.len(),
        graph.forward.values().map(|v| v.len()).sum::<usize>(),
        graph_time
    );

    // Determine changed files
    let changed = if git || files.is_empty() {
        detect_git_changes(root, base, staged)?
    } else {
        files
            .into_iter()
            .map(|f| {
                if f.is_absolute() {
                    f
                } else {
                    root.join(&f)
                }
            })
            .collect()
    };

    eprintln!("  Changed files: {}", changed.len());

    let affected = analyzer::find_affected_tests(&graph, &changed)?;
    let elapsed = start.elapsed().as_millis() as u64;

    let result = analyzer::AnalysisResult {
        changed_files: changed
            .iter()
            .map(|f| {
                f.strip_prefix(root)
                    .unwrap_or(f)
                    .to_string_lossy()
                    .to_string()
            })
            .collect(),
        affected_tests: affected
            .iter()
            .map(|f| {
                f.strip_prefix(root)
                    .unwrap_or(f)
                    .to_string_lossy()
                    .to_string()
            })
            .collect(),
        total_tests: graph.test_files.len(),
        graph_nodes: graph.forward.len(),
        graph_edges: graph.forward.values().map(|v| v.len()).sum(),
        elapsed_ms: elapsed,
    };

    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&result)?),
        OutputFormat::Text => {
            eprintln!(
                "\n✅ {} affected tests (of {} total) in {}ms\n",
                result.affected_tests.len(),
                result.total_tests,
                result.elapsed_ms
            );
            for test in &result.affected_tests {
                println!("{}", test);
            }
        }
    }

    Ok(())
}

// ─── cache ─────────────────────────────────────────────────────────────────────

fn cmd_cache(root: &PathBuf, action: CacheAction, format: &OutputFormat) -> Result<()> {
    match action {
        CacheAction::Status => {
            let store = cache::CacheStore::load(root);
            let total = store.entries.len();
            let pass = store
                .entries
                .values()
                .filter(|e| e.result == cache::TestResult::Pass)
                .count();
            let fail = total - pass;

            #[derive(serde::Serialize)]
            struct Status {
                total_entries: usize,
                pass: usize,
                fail: usize,
                config_hash: String,
            }

            let status = Status {
                total_entries: total,
                pass,
                fail,
                config_hash: format!("{:032x}", store.config_hash),
            };

            match format {
                OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&status)?),
                OutputFormat::Text => {
                    eprintln!("Cache: {} entries ({} pass, {} fail)", total, pass, fail);
                    eprintln!("Config hash: {:032x}", store.config_hash);
                }
            }
        }
        CacheAction::Clear => {
            let path = root.join(".highway-cache.json");
            if path.exists() {
                std::fs::remove_file(&path)?;
                eprintln!("Cache cleared.");
            } else {
                eprintln!("No cache file found.");
            }
        }
        CacheAction::Hashes => {
            let start = Instant::now();
            eprintln!("⚡ Building import graph for hash computation...");

            let config = analyzer::AnalyzerConfig {
                root: root.clone(),
                ..Default::default()
            };
            let graph = analyzer::build_import_graph(&config)?;
            let config_hash = cache::hash_config(root);
            let hashes = cache::compute_test_hashes(&graph, root, config_hash)?;

            eprintln!(
                "  Computed {} hashes in {:.0?}",
                hashes.len(),
                start.elapsed()
            );

            #[derive(serde::Serialize)]
            struct HashEntry {
                test: String,
                hash: String,
            }

            let mut entries: Vec<HashEntry> = hashes
                .iter()
                .map(|(path, hash)| HashEntry {
                    test: path
                        .strip_prefix(root)
                        .unwrap_or(path)
                        .to_string_lossy()
                        .to_string(),
                    hash: format!("{:032x}", hash),
                })
                .collect();
            entries.sort_by(|a, b| a.test.cmp(&b.test));

            match format {
                OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&entries)?),
                OutputFormat::Text => {
                    for entry in &entries {
                        println!("{}  {}", &entry.hash[..16], entry.test);
                    }
                }
            }
        }
    }
    Ok(())
}

// ─── schedule ──────────────────────────────────────────────────────────────────

fn cmd_schedule(
    root: &PathBuf,
    files: Vec<PathBuf>,
    cpus: usize,
    format: &OutputFormat,
) -> Result<()> {
    let test_files = if files.is_empty() {
        eprintln!("⚡ Discovering test files...");
        let config = analyzer::AnalyzerConfig {
            root: root.clone(),
            ..Default::default()
        };
        let graph = analyzer::build_import_graph(&config)?;
        graph.test_files
    } else {
        files
            .into_iter()
            .map(|f| {
                if f.is_absolute() {
                    f
                } else {
                    root.join(f)
                }
            })
            .collect()
    };

    let num_cpus = if cpus == 0 {
        num_cpus_detect()
    } else {
        cpus
    };

    let cache_store = cache::CacheStore::load(root);
    let timings_path = root.join("test/fixtures/test-timings.unit.json");
    let timing = scheduler::TimingSource::new(
        &cache_store,
        if timings_path.exists() {
            Some(timings_path.as_path())
        } else {
            None
        },
    );
    let behavior = scheduler::load_isolation_behavior(root);

    let schedule = scheduler::create_schedule(&test_files, root, num_cpus, &timing, &behavior)?;

    match format {
        OutputFormat::Json => {
            let specs = schedule.to_vitest_specs(root);
            #[derive(serde::Serialize)]
            struct Output {
                schedule: scheduler::Schedule,
                vitest_specs: Vec<scheduler::VitestShardSpec>,
            }
            println!(
                "{}",
                serde_json::to_string_pretty(&Output {
                    schedule,
                    vitest_specs: specs,
                })?
            );
        }
        OutputFormat::Text => {
            eprintln!(
                "\n📋 Schedule: {} shards, ~{}ms wall time, {:.0}% efficiency\n",
                schedule.num_shards,
                schedule.estimated_wall_time_ms,
                schedule.efficiency * 100.0
            );
            for shard in &schedule.shards {
                eprintln!(
                    "  Shard {}: {} tests, ~{}ms",
                    shard.index,
                    shard.test_files.len(),
                    shard.estimated_duration_ms
                );
                if shard.test_files.len() <= 5 {
                    for f in &shard.test_files {
                        let rel = f.strip_prefix(root).unwrap_or(f);
                        eprintln!("    {}", rel.display());
                    }
                }
            }
        }
    }

    Ok(())
}

// ─── run (full pipeline) ───────────────────────────────────────────────────────

fn cmd_run(
    root: &PathBuf,
    files: Vec<PathBuf>,
    git: bool,
    base: &str,
    staged: bool,
    cpus: usize,
    dry_run: bool,
    no_cache: bool,
    verbose: bool,
    format: &OutputFormat,
) -> Result<()> {
    let pipeline_start = Instant::now();

    // Phase 1: Build import graph
    eprintln!("⚡ Phase 1: Building import graph...");
    let phase1_start = Instant::now();
    let config = analyzer::AnalyzerConfig {
        root: root.clone(),
        ..Default::default()
    };
    let graph = analyzer::build_import_graph(&config)?;
    eprintln!(
        "  {} nodes, {} edges in {:.0?}",
        graph.forward.len(),
        graph.forward.values().map(|v| v.len()).sum::<usize>(),
        phase1_start.elapsed()
    );

    // Determine test files to consider
    let candidate_tests = if files.is_empty() && !git {
        // Run all tests
        graph.test_files.clone()
    } else {
        let changed = if git || files.is_empty() {
            detect_git_changes(root, base, staged)?
        } else {
            files
                .into_iter()
                .map(|f| {
                    if f.is_absolute() {
                        f
                    } else {
                        root.join(f)
                    }
                })
                .collect()
        };
        eprintln!("  {} changed files", changed.len());
        analyzer::find_affected_tests(&graph, &changed)?
    };

    eprintln!("  {} candidate tests", candidate_tests.len());

    // Phase 2: Cache filtering
    eprintln!("⚡ Phase 2: Cache analysis...");
    let phase2_start = Instant::now();
    let config_hash = cache::hash_config(root);
    let mut cache_store = cache::CacheStore::load(root);

    // Invalidate cache if config changed
    if cache_store.config_hash != config_hash {
        eprintln!("  Config changed, invalidating cache");
        cache_store = cache::CacheStore::default();
        cache_store.config_hash = config_hash;
    }

    let test_hashes = cache::compute_test_hashes(&graph, root, config_hash)?;

    let (tests_to_run, cache_summary) = if no_cache {
        (
            candidate_tests.clone(),
            cache::CacheSummary {
                total_tests: candidate_tests.len(),
                cached_tests: 0,
                tests_to_run: candidate_tests.len(),
                estimated_skip_time_ms: 0,
                cache_hit_rate: 0.0,
            },
        )
    } else {
        // Filter to only candidate tests
        let candidate_hashes: std::collections::HashMap<_, _> = test_hashes
            .iter()
            .filter(|(k, _)| candidate_tests.contains(k))
            .map(|(k, v)| (k.clone(), *v))
            .collect();
        cache::analyze_cache(&cache_store, &candidate_hashes, root)
    };

    eprintln!(
        "  {} to run, {} cached ({:.0}% hit rate) in {:.0?}",
        cache_summary.tests_to_run,
        cache_summary.cached_tests,
        cache_summary.cache_hit_rate * 100.0,
        phase2_start.elapsed()
    );

    if cache_summary.estimated_skip_time_ms > 0 {
        eprintln!(
            "  ~{}ms saved from cache",
            cache_summary.estimated_skip_time_ms
        );
    }

    if tests_to_run.is_empty() {
        eprintln!("\n✅ All tests cached and passing. Nothing to run!");
        let result = runner::RunResult {
            success: true,
            total_tests: cache_summary.total_tests,
            cached_tests: cache_summary.cached_tests,
            executed_tests: 0,
            passed_shards: 0,
            failed_shards: 0,
            wall_time_ms: pipeline_start.elapsed().as_millis() as u64,
            total_execution_ms: 0,
            shard_results: Vec::new(),
            cache_summary: runner::CacheSummaryOutput {
                hit_rate: cache_summary.cache_hit_rate,
                skipped_tests: cache_summary.cached_tests,
                estimated_time_saved_ms: cache_summary.estimated_skip_time_ms,
            },
        };
        match format {
            OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&result)?),
            OutputFormat::Text => {}
        }
        return Ok(());
    }

    // Phase 3: Optimal scheduling
    eprintln!("⚡ Phase 3: Scheduling...");
    let num_cpus = if cpus == 0 {
        num_cpus_detect()
    } else {
        cpus
    };

    let timings_path = root.join("test/fixtures/test-timings.unit.json");
    let timing = scheduler::TimingSource::new(
        &cache_store,
        if timings_path.exists() {
            Some(timings_path.as_path())
        } else {
            None
        },
    );
    let behavior = scheduler::load_isolation_behavior(root);

    let schedule =
        scheduler::create_schedule(&tests_to_run, root, num_cpus, &timing, &behavior)?;

    eprintln!(
        "  {} shards, ~{}ms estimated wall time, {:.0}% efficiency",
        schedule.num_shards,
        schedule.estimated_wall_time_ms,
        schedule.efficiency * 100.0
    );

    // Phase 4: Execute
    eprintln!("⚡ Phase 4: Executing...\n");

    let runner_config = runner::RunnerConfig {
        root: root.clone(),
        dry_run,
        verbose,
        ..Default::default()
    };

    let progress: Option<runner::ProgressCallback> = if !dry_run {
        Some(Box::new(|event: &runner::ProgressEvent| match event {
            runner::ProgressEvent::ShardStarted {
                shard_index,
                test_count,
            } => {
                eprintln!("  ▶ Shard {} ({} tests)", shard_index, test_count);
            }
            runner::ProgressEvent::ShardCompleted {
                shard_index,
                success,
                duration_ms,
            } => {
                let icon = if *success { "✓" } else { "✗" };
                eprintln!(
                    "  {} Shard {} completed in {}ms",
                    icon, shard_index, duration_ms
                );
            }
            runner::ProgressEvent::Summary { completed, total } => {
                eprintln!("  [{}/{}]", completed, total);
            }
        }))
    } else {
        None
    };

    let run_result = runner::execute_schedule(
        &schedule,
        &runner_config,
        cache_summary.cached_tests,
        cache_summary.estimated_skip_time_ms,
        progress,
    )?;

    // Phase 5: Update cache
    if !dry_run {
        runner::update_cache_from_results(
            &mut cache_store,
            &run_result,
            &schedule,
            &test_hashes,
            root,
        );
        cache_store.config_hash = config_hash;
        cache_store.save(root)?;
    }

    let total_time = pipeline_start.elapsed().as_millis() as u64;

    // Output results
    match format {
        OutputFormat::Json => println!("{}", serde_json::to_string_pretty(&run_result)?),
        OutputFormat::Text => {
            eprintln!("\n{}", if run_result.success { "═══ ✅ ALL TESTS PASSED ═══" } else { "═══ ❌ TESTS FAILED ═══" });
            eprintln!(
                "  Total: {} tests ({} executed, {} cached)",
                run_result.total_tests, run_result.executed_tests, run_result.cached_tests
            );
            eprintln!(
                "  Shards: {} passed, {} failed",
                run_result.passed_shards, run_result.failed_shards
            );
            eprintln!(
                "  Time: {}ms wall / {}ms total execution",
                total_time, run_result.total_execution_ms
            );
            if run_result.cache_summary.estimated_time_saved_ms > 0 {
                eprintln!(
                    "  Cache: ~{}ms saved ({:.0}% hit rate)",
                    run_result.cache_summary.estimated_time_saved_ms,
                    run_result.cache_summary.hit_rate * 100.0
                );
            }

            // Show failed shard details
            for shard in &run_result.shard_results {
                if !shard.success {
                    eprintln!("\n  ❌ Shard {} (exit code {}):", shard.shard_index, shard.exit_code);
                    if !shard.stderr_tail.is_empty() {
                        for line in shard.stderr_tail.lines().take(10) {
                            eprintln!("    {}", line);
                        }
                    }
                }
            }
        }
    }

    if !run_result.success {
        std::process::exit(1);
    }

    Ok(())
}

// ─── graph ─────────────────────────────────────────────────────────────────────

fn cmd_graph(
    root: &PathBuf,
    output: GraphFormat,
    filter: Option<String>,
    _format: &OutputFormat,
) -> Result<()> {
    eprintln!("⚡ Building import graph...");
    let config = analyzer::AnalyzerConfig {
        root: root.clone(),
        ..Default::default()
    };
    let graph = analyzer::build_import_graph(&config)?;

    match output {
        GraphFormat::Json => {
            #[derive(serde::Serialize)]
            struct Edge {
                from: String,
                to: String,
            }
            #[derive(serde::Serialize)]
            struct GraphOutput {
                nodes: Vec<String>,
                edges: Vec<Edge>,
                test_files: Vec<String>,
            }

            let mut nodes: Vec<String> = Vec::new();
            let mut edges: Vec<Edge> = Vec::new();

            for (file, deps) in &graph.forward {
                let from = file
                    .strip_prefix(root)
                    .unwrap_or(file)
                    .to_string_lossy()
                    .to_string();

                if let Some(ref f) = filter {
                    if !from.contains(f) {
                        continue;
                    }
                }

                nodes.push(from.clone());
                for dep in deps {
                    let to = dep
                        .strip_prefix(root)
                        .unwrap_or(dep)
                        .to_string_lossy()
                        .to_string();
                    edges.push(Edge {
                        from: from.clone(),
                        to,
                    });
                }
            }

            nodes.sort();
            nodes.dedup();

            let test_files: Vec<String> = graph
                .test_files
                .iter()
                .map(|f| {
                    f.strip_prefix(root)
                        .unwrap_or(f)
                        .to_string_lossy()
                        .to_string()
                })
                .collect();

            let output = GraphOutput {
                nodes,
                edges,
                test_files,
            };
            println!("{}", serde_json::to_string_pretty(&output)?);
        }
        GraphFormat::Dot => {
            println!("digraph imports {{");
            println!("  rankdir=LR;");
            println!("  node [shape=box, fontsize=8];");

            for (file, deps) in &graph.forward {
                let from = file
                    .strip_prefix(root)
                    .unwrap_or(file)
                    .to_string_lossy()
                    .to_string();

                if let Some(ref f) = filter {
                    if !from.contains(f) {
                        continue;
                    }
                }

                for dep in deps {
                    let to = dep
                        .strip_prefix(root)
                        .unwrap_or(dep)
                        .to_string_lossy()
                        .to_string();
                    println!("  \"{}\" -> \"{}\";", from, to);
                }
            }

            // Highlight test files
            for test in &graph.test_files {
                let name = test
                    .strip_prefix(root)
                    .unwrap_or(test)
                    .to_string_lossy()
                    .to_string();
                println!("  \"{}\" [style=filled, fillcolor=lightblue];", name);
            }

            println!("}}");
        }
    }

    Ok(())
}

// ─── helpers ───────────────────────────────────────────────────────────────────

/// Detect changed files using git diff.
fn detect_git_changes(root: &PathBuf, base: &str, staged: bool) -> Result<Vec<PathBuf>> {
    let mut cmd = std::process::Command::new("git");
    cmd.current_dir(root);

    if staged {
        cmd.args(["diff", "--cached", "--name-only", "--diff-filter=ACMR"]);
    } else {
        cmd.args(["diff", "--name-only", "--diff-filter=ACMR", base]);
    }

    let output = cmd.output().context("run git diff")?;
    let stdout = String::from_utf8_lossy(&output.stdout);

    let files: Vec<PathBuf> = stdout
        .lines()
        .filter(|l| !l.trim().is_empty())
        .map(|l| root.join(l.trim()))
        .collect();

    // Also include untracked files
    let mut untracked_cmd = std::process::Command::new("git");
    untracked_cmd.current_dir(root);
    untracked_cmd.args(["ls-files", "--others", "--exclude-standard"]);

    if let Ok(output) = untracked_cmd.output() {
        let stdout = String::from_utf8_lossy(&output.stdout);
        let untracked: Vec<PathBuf> = stdout
            .lines()
            .filter(|l| !l.trim().is_empty())
            .map(|l| root.join(l.trim()))
            .collect();
        let mut all = files;
        all.extend(untracked);
        return Ok(all);
    }

    Ok(files)
}

/// Detect number of CPU cores.
fn num_cpus_detect() -> usize {
    std::thread::available_parallelism()
        .map(|n| n.get())
        .unwrap_or(4)
}
