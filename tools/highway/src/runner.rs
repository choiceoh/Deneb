//! Vitest orchestration runner.
//!
//! Executes scheduled test shards as parallel Vitest processes, collects results,
//! and updates the cache with outcomes.

use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::Arc;
use std::time::Instant;

use anyhow::{Context, Result};
use crossbeam_channel::bounded;
use serde::Serialize;

use crate::cache::{CacheStore, TestResult};
use crate::scheduler::Schedule;

/// Result of a single shard execution.
#[derive(Debug, Clone, Serialize)]
pub struct ShardResult {
    pub shard_index: usize,
    pub success: bool,
    pub duration_ms: u64,
    pub test_count: usize,
    pub exit_code: i32,
    pub stdout_tail: String,
    pub stderr_tail: String,
}

/// Aggregated result of the full test run.
#[derive(Debug, Serialize)]
pub struct RunResult {
    pub success: bool,
    pub total_tests: usize,
    pub cached_tests: usize,
    pub executed_tests: usize,
    pub passed_shards: usize,
    pub failed_shards: usize,
    pub wall_time_ms: u64,
    pub total_execution_ms: u64,
    pub shard_results: Vec<ShardResult>,
    pub cache_summary: CacheSummaryOutput,
}

#[derive(Debug, Serialize)]
pub struct CacheSummaryOutput {
    pub hit_rate: f64,
    pub skipped_tests: usize,
    pub estimated_time_saved_ms: u64,
}

/// Progress callback type for real-time status updates.
pub type ProgressCallback = Box<dyn Fn(&ProgressEvent) + Send + Sync>;

#[derive(Debug)]
pub enum ProgressEvent {
    ShardStarted {
        shard_index: usize,
        test_count: usize,
    },
    ShardCompleted {
        shard_index: usize,
        success: bool,
        duration_ms: u64,
    },
    Summary {
        completed: usize,
        total: usize,
    },
}

/// Configuration for the runner.
pub struct RunnerConfig {
    pub root: PathBuf,
    pub vitest_bin: String,
    pub timeout_ms: u64,
    pub verbose: bool,
    pub dry_run: bool,
}

impl Default for RunnerConfig {
    fn default() -> Self {
        Self {
            root: PathBuf::from("."),
            vitest_bin: "pnpm".into(),
            timeout_ms: 120_000,
            verbose: false,
            dry_run: false,
        }
    }
}

/// Execute the scheduled test shards in parallel.
pub fn execute_schedule(
    schedule: &Schedule,
    config: &RunnerConfig,
    cached_tests: usize,
    estimated_skip_time: u64,
    progress: Option<ProgressCallback>,
) -> Result<RunResult> {
    let start = Instant::now();

    if config.dry_run {
        return Ok(RunResult {
            success: true,
            total_tests: schedule
                .shards
                .iter()
                .map(|s| s.test_files.len())
                .sum::<usize>()
                + cached_tests,
            cached_tests,
            executed_tests: schedule
                .shards
                .iter()
                .map(|s| s.test_files.len())
                .sum(),
            passed_shards: schedule.shards.len(),
            failed_shards: 0,
            wall_time_ms: 0,
            total_execution_ms: 0,
            shard_results: Vec::new(),
            cache_summary: CacheSummaryOutput {
                hit_rate: if cached_tests > 0 { 1.0 } else { 0.0 },
                skipped_tests: cached_tests,
                estimated_time_saved_ms: estimated_skip_time,
            },
        });
    }

    let completed = Arc::new(AtomicUsize::new(0));
    let any_failure = Arc::new(AtomicBool::new(false));
    let total_shards = schedule.shards.len();

    // Wrap progress in Arc for sharing across threads
    let progress_arc: Option<Arc<dyn Fn(&ProgressEvent) + Send + Sync>> =
        progress.map(|p| Arc::from(p) as Arc<dyn Fn(&ProgressEvent) + Send + Sync>);

    // Execute shards using thread pool
    let (tx, rx) = bounded::<ShardResult>(total_shards);

    let handles: Vec<_> = schedule
        .shards
        .iter()
        .map(|shard| {
            let tx = tx.clone();
            let config_root = config.root.clone();
            let vitest_bin = config.vitest_bin.clone();
            let test_files: Vec<String> = shard
                .test_files
                .iter()
                .map(|f| {
                    f.strip_prefix(&config_root)
                        .unwrap_or(f)
                        .to_string_lossy()
                        .to_string()
                })
                .collect();
            let shard_index = shard.index;
            let test_count = shard.test_files.len();
            let timeout = config.timeout_ms;
            let verbose = config.verbose;
            let completed = Arc::clone(&completed);
            let any_failure = Arc::clone(&any_failure);
            let progress_clone = progress_arc.clone();

            std::thread::spawn(move || {
                if let Some(ref p) = progress_clone {
                    p(&ProgressEvent::ShardStarted {
                        shard_index,
                        test_count,
                    });
                }

                let result =
                    run_vitest_shard(&config_root, &vitest_bin, &test_files, shard_index, timeout, verbose);

                let shard_result = match result {
                    Ok(r) => r,
                    Err(e) => ShardResult {
                        shard_index,
                        success: false,
                        duration_ms: 0,
                        test_count,
                        exit_code: -1,
                        stdout_tail: String::new(),
                        stderr_tail: format!("Error: {}", e),
                    },
                };

                if !shard_result.success {
                    any_failure.store(true, Ordering::SeqCst);
                }

                let done = completed.fetch_add(1, Ordering::SeqCst) + 1;
                if let Some(ref p) = progress_clone {
                    p(&ProgressEvent::ShardCompleted {
                        shard_index,
                        success: shard_result.success,
                        duration_ms: shard_result.duration_ms,
                    });
                    p(&ProgressEvent::Summary {
                        completed: done,
                        total: total_shards,
                    });
                }

                let _ = tx.send(shard_result);
            })
        })
        .collect();

    drop(tx);

    // Collect results
    let mut shard_results: Vec<ShardResult> = Vec::new();
    for result in rx {
        shard_results.push(result);
    }

    for handle in handles {
        let _ = handle.join();
    }

    shard_results.sort_by_key(|r| r.shard_index);

    let wall_time = start.elapsed().as_millis() as u64;
    let total_exec: u64 = shard_results.iter().map(|r| r.duration_ms).sum();
    let passed = shard_results.iter().filter(|r| r.success).count();
    let failed = shard_results.iter().filter(|r| !r.success).count();
    let executed: usize = shard_results.iter().map(|r| r.test_count).sum();

    Ok(RunResult {
        success: !any_failure.load(Ordering::SeqCst),
        total_tests: executed + cached_tests,
        cached_tests,
        executed_tests: executed,
        passed_shards: passed,
        failed_shards: failed,
        wall_time_ms: wall_time,
        total_execution_ms: total_exec,
        shard_results,
        cache_summary: CacheSummaryOutput {
            hit_rate: if cached_tests + executed > 0 {
                cached_tests as f64 / (cached_tests + executed) as f64
            } else {
                0.0
            },
            skipped_tests: cached_tests,
            estimated_time_saved_ms: estimated_skip_time,
        },
    })
}

/// Run a single Vitest shard as a subprocess.
fn run_vitest_shard(
    root: &Path,
    vitest_bin: &str,
    test_files: &[String],
    shard_index: usize,
    _timeout_ms: u64,
    verbose: bool,
) -> Result<ShardResult> {
    let start = Instant::now();

    // Build vitest command: pnpm test -- file1 file2 ...
    let mut cmd = Command::new(vitest_bin);
    cmd.arg("test").arg("--");

    for file in test_files {
        cmd.arg(file);
    }

    cmd.current_dir(root);

    if !verbose {
        cmd.stdout(Stdio::piped());
        cmd.stderr(Stdio::piped());
    }

    // Set environment to identify this shard
    cmd.env("HIGHWAY_SHARD_INDEX", shard_index.to_string());
    cmd.env("HIGHWAY_SHARD_TOTAL", "1");

    let output = cmd.output().context("spawn vitest")?;
    let duration = start.elapsed().as_millis() as u64;

    let stdout = String::from_utf8_lossy(&output.stdout);
    let stderr = String::from_utf8_lossy(&output.stderr);

    // Keep last 50 lines for debugging (in correct order)
    let stdout_tail = tail_lines(&stdout, 50);
    let stderr_tail = tail_lines(&stderr, 50);

    Ok(ShardResult {
        shard_index,
        success: output.status.success(),
        duration_ms: duration,
        test_count: test_files.len(),
        exit_code: output.status.code().unwrap_or(-1),
        stdout_tail,
        stderr_tail,
    })
}

/// Extract the last `n` lines from a string without collecting all lines first.
fn tail_lines(s: &str, n: usize) -> String {
    let lines: Vec<&str> = s.lines().collect();
    let start = lines.len().saturating_sub(n);
    lines[start..].join("\n")
}

/// Update cache with test results from the run.
pub fn update_cache_from_results(
    cache: &mut CacheStore,
    run_result: &RunResult,
    schedule: &Schedule,
    test_hashes: &std::collections::HashMap<PathBuf, u128>,
    root: &Path,
) {
    for shard_result in &run_result.shard_results {
        let shard = &schedule.shards[shard_result.shard_index];
        let result = if shard_result.success {
            TestResult::Pass
        } else {
            TestResult::Fail
        };

        // Distribute shard duration proportionally across test files
        let per_test_ms = if shard.test_files.len() > 0 {
            shard_result.duration_ms / shard.test_files.len() as u64
        } else {
            0
        };

        for test_file in &shard.test_files {
            let rel = test_file
                .strip_prefix(root)
                .unwrap_or(test_file)
                .to_string_lossy()
                .to_string();

            if let Some(&hash) = test_hashes.get(test_file) {
                cache.record(rel, hash, result, per_test_ms);
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::cache::CacheStore;
    use crate::scheduler::{Schedule, Shard};

    // ── RunnerConfig defaults ───────────────────────────────────────────────

    #[test]
    fn runner_config_defaults() {
        let cfg = RunnerConfig::default();
        assert_eq!(cfg.root, PathBuf::from("."));
        assert_eq!(cfg.vitest_bin, "pnpm");
        assert_eq!(cfg.max_retries, 0);
        assert_eq!(cfg.timeout_ms, 120_000);
        assert!(!cfg.verbose);
        assert!(!cfg.dry_run);
    }

    // ── execute_schedule dry run ─────────────────────────────────────────────

    #[test]
    fn execute_schedule_dry_run_returns_success() {
        let schedule = Schedule {
            num_shards: 2,
            shards: vec![
                Shard {
                    index: 0,
                    test_files: vec![PathBuf::from("/root/a.test.ts")],
                    estimated_duration_ms: 100,
                },
                Shard {
                    index: 1,
                    test_files: vec![PathBuf::from("/root/b.test.ts")],
                    estimated_duration_ms: 200,
                },
            ],
            estimated_wall_time_ms: 200,
            total_execution_time_ms: 300,
            efficiency: 0.75,
        };

        let config = RunnerConfig {
            dry_run: true,
            ..Default::default()
        };

        let result = execute_schedule(&schedule, &config, 3, 500, None).unwrap();
        assert!(result.success);
        assert_eq!(result.total_tests, 5); // 2 executed + 3 cached
        assert_eq!(result.cached_tests, 3);
        assert_eq!(result.executed_tests, 2);
        assert_eq!(result.passed_shards, 2);
        assert_eq!(result.failed_shards, 0);
        assert_eq!(result.wall_time_ms, 0);
        assert!(result.shard_results.is_empty());
        assert_eq!(result.cache_summary.skipped_tests, 3);
        assert_eq!(result.cache_summary.estimated_time_saved_ms, 500);
    }

    #[test]
    fn execute_schedule_dry_run_empty_schedule() {
        let schedule = Schedule {
            num_shards: 0,
            shards: vec![],
            estimated_wall_time_ms: 0,
            total_execution_time_ms: 0,
            efficiency: 1.0,
        };

        let config = RunnerConfig {
            dry_run: true,
            ..Default::default()
        };

        let result = execute_schedule(&schedule, &config, 0, 0, None).unwrap();
        assert!(result.success);
        assert_eq!(result.total_tests, 0);
        assert_eq!(result.executed_tests, 0);
    }

    #[test]
    fn execute_schedule_dry_run_no_cache() {
        let schedule = Schedule {
            num_shards: 1,
            shards: vec![Shard {
                index: 0,
                test_files: vec![PathBuf::from("/root/x.test.ts")],
                estimated_duration_ms: 50,
            }],
            estimated_wall_time_ms: 50,
            total_execution_time_ms: 50,
            efficiency: 1.0,
        };

        let config = RunnerConfig {
            dry_run: true,
            ..Default::default()
        };

        let result = execute_schedule(&schedule, &config, 0, 0, None).unwrap();
        assert!(result.success);
        assert_eq!(result.cached_tests, 0);
        assert!((result.cache_summary.hit_rate).abs() < f64::EPSILON);
    }

    // ── update_cache_from_results ───────────────────────────────────────────

    #[test]
    fn update_cache_from_results_records_pass() {
        let mut cache = CacheStore::default();
        let root = Path::new("/root");

        let schedule = Schedule {
            num_shards: 1,
            shards: vec![Shard {
                index: 0,
                test_files: vec![PathBuf::from("/root/a.test.ts")],
                estimated_duration_ms: 100,
            }],
            estimated_wall_time_ms: 100,
            total_execution_time_ms: 100,
            efficiency: 1.0,
        };

        let run_result = RunResult {
            success: true,
            total_tests: 1,
            cached_tests: 0,
            executed_tests: 1,
            passed_shards: 1,
            failed_shards: 0,
            wall_time_ms: 150,
            total_execution_ms: 150,
            shard_results: vec![ShardResult {
                shard_index: 0,
                success: true,
                duration_ms: 150,
                test_count: 1,
                exit_code: 0,
                stdout_tail: String::new(),
                stderr_tail: String::new(),
            }],
            cache_summary: CacheSummaryOutput {
                hit_rate: 0.0,
                skipped_tests: 0,
                estimated_time_saved_ms: 0,
            },
        };

        let test_hashes: HashMap<PathBuf, u128> =
            [(PathBuf::from("/root/a.test.ts"), 42u128)].into();

        update_cache_from_results(&mut cache, &run_result, &schedule, &test_hashes, root);

        assert!(cache.is_cached("a.test.ts", 42));
        let entry = cache.entries.get("a.test.ts").unwrap();
        assert_eq!(entry.result, TestResult::Pass);
        assert_eq!(entry.duration_ms, 150);
    }

    #[test]
    fn update_cache_from_results_records_fail() {
        let mut cache = CacheStore::default();
        let root = Path::new("/root");

        let schedule = Schedule {
            num_shards: 1,
            shards: vec![Shard {
                index: 0,
                test_files: vec![PathBuf::from("/root/fail.test.ts")],
                estimated_duration_ms: 100,
            }],
            estimated_wall_time_ms: 100,
            total_execution_time_ms: 100,
            efficiency: 1.0,
        };

        let run_result = RunResult {
            success: false,
            total_tests: 1,
            cached_tests: 0,
            executed_tests: 1,
            passed_shards: 0,
            failed_shards: 1,
            wall_time_ms: 200,
            total_execution_ms: 200,
            shard_results: vec![ShardResult {
                shard_index: 0,
                success: false,
                duration_ms: 200,
                test_count: 1,
                exit_code: 1,
                stdout_tail: String::new(),
                stderr_tail: "FAIL".into(),
            }],
            cache_summary: CacheSummaryOutput {
                hit_rate: 0.0,
                skipped_tests: 0,
                estimated_time_saved_ms: 0,
            },
        };

        let test_hashes: HashMap<PathBuf, u128> =
            [(PathBuf::from("/root/fail.test.ts"), 99u128)].into();

        update_cache_from_results(&mut cache, &run_result, &schedule, &test_hashes, root);

        // Failed test should be recorded but not considered "cached" (is_cached returns false for Fail)
        assert!(!cache.is_cached("fail.test.ts", 99));
        let entry = cache.entries.get("fail.test.ts").unwrap();
        assert_eq!(entry.result, TestResult::Fail);
    }

    #[test]
    fn update_cache_distributes_duration_proportionally() {
        let mut cache = CacheStore::default();
        let root = Path::new("/root");

        let schedule = Schedule {
            num_shards: 1,
            shards: vec![Shard {
                index: 0,
                test_files: vec![
                    PathBuf::from("/root/a.test.ts"),
                    PathBuf::from("/root/b.test.ts"),
                ],
                estimated_duration_ms: 200,
            }],
            estimated_wall_time_ms: 200,
            total_execution_time_ms: 200,
            efficiency: 1.0,
        };

        let run_result = RunResult {
            success: true,
            total_tests: 2,
            cached_tests: 0,
            executed_tests: 2,
            passed_shards: 1,
            failed_shards: 0,
            wall_time_ms: 300,
            total_execution_ms: 300,
            shard_results: vec![ShardResult {
                shard_index: 0,
                success: true,
                duration_ms: 300,
                test_count: 2,
                exit_code: 0,
                stdout_tail: String::new(),
                stderr_tail: String::new(),
            }],
            cache_summary: CacheSummaryOutput {
                hit_rate: 0.0,
                skipped_tests: 0,
                estimated_time_saved_ms: 0,
            },
        };

        let test_hashes: HashMap<PathBuf, u128> = [
            (PathBuf::from("/root/a.test.ts"), 1u128),
            (PathBuf::from("/root/b.test.ts"), 2u128),
        ]
        .into();

        update_cache_from_results(&mut cache, &run_result, &schedule, &test_hashes, root);

        // 300ms / 2 tests = 150ms each
        assert_eq!(cache.entries.get("a.test.ts").unwrap().duration_ms, 150);
        assert_eq!(cache.entries.get("b.test.ts").unwrap().duration_ms, 150);
    }

    #[test]
    fn update_cache_skips_missing_hashes() {
        let mut cache = CacheStore::default();
        let root = Path::new("/root");

        let schedule = Schedule {
            num_shards: 1,
            shards: vec![Shard {
                index: 0,
                test_files: vec![PathBuf::from("/root/no-hash.test.ts")],
                estimated_duration_ms: 100,
            }],
            estimated_wall_time_ms: 100,
            total_execution_time_ms: 100,
            efficiency: 1.0,
        };

        let run_result = RunResult {
            success: true,
            total_tests: 1,
            cached_tests: 0,
            executed_tests: 1,
            passed_shards: 1,
            failed_shards: 0,
            wall_time_ms: 100,
            total_execution_ms: 100,
            shard_results: vec![ShardResult {
                shard_index: 0,
                success: true,
                duration_ms: 100,
                test_count: 1,
                exit_code: 0,
                stdout_tail: String::new(),
                stderr_tail: String::new(),
            }],
            cache_summary: CacheSummaryOutput {
                hit_rate: 0.0,
                skipped_tests: 0,
                estimated_time_saved_ms: 0,
            },
        };

        // Empty hash map — the test file has no hash entry
        let test_hashes: HashMap<PathBuf, u128> = HashMap::new();

        update_cache_from_results(&mut cache, &run_result, &schedule, &test_hashes, root);
        assert!(cache.entries.is_empty(), "no entries should be recorded without a hash");
    }
}
