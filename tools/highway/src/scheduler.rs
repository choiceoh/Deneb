//! Optimal parallel test scheduler using timing-aware bin-packing.
//!
//! Given a set of test files to run and their estimated durations, produces
//! an optimal assignment of tests to worker slots that minimizes total wall-clock
//! time. Uses the Longest Processing Time (LPT) algorithm which gives a 4/3
//! approximation to optimal makespan, then applies local search refinement.

use std::collections::HashMap;
use std::path::{Path, PathBuf};

use anyhow::Result;
use serde::{Deserialize, Serialize};

use crate::cache::CacheStore;

/// Default duration estimate for unknown tests (ms).
const DEFAULT_DURATION_MS: u64 = 250;

/// Minimum number of workers.
const MIN_WORKERS: usize = 2;

/// A scheduled shard of tests to run in one Vitest invocation.
#[derive(Debug, Clone, Serialize)]
pub struct Shard {
    /// Shard index (0-based)
    pub index: usize,
    /// Test files assigned to this shard
    pub test_files: Vec<PathBuf>,
    /// Estimated total duration of this shard (ms)
    pub estimated_duration_ms: u64,
}

/// Full schedule output.
#[derive(Debug, Serialize)]
pub struct Schedule {
    /// Number of parallel workers/shards
    pub num_shards: usize,
    /// Ordered shards (longest first for progress indication)
    pub shards: Vec<Shard>,
    /// Estimated wall-clock time (= max shard duration)
    pub estimated_wall_time_ms: u64,
    /// Total test execution time across all shards
    pub total_execution_time_ms: u64,
    /// Parallelism efficiency (total / (wall * shards))
    pub efficiency: f64,
}

/// Timing data source: combines historical cache data with external timing file.
pub struct TimingSource {
    /// Historical durations from cache
    cache_timings: HashMap<String, u64>,
    /// External timing baseline (from test-timings.unit.json)
    baseline_timings: HashMap<String, u64>,
}

impl TimingSource {
    /// Create from cache and optional external timings file.
    pub fn new(cache: &CacheStore, timings_file: Option<&Path>) -> Self {
        let cache_timings: HashMap<String, u64> = cache
            .entries
            .iter()
            .map(|(k, v)| (k.clone(), v.duration_ms))
            .collect();

        let baseline_timings = timings_file
            .and_then(|path| std::fs::read_to_string(path).ok())
            .and_then(|data| serde_json::from_str::<HashMap<String, f64>>(&data).ok())
            .map(|m| m.into_iter().map(|(k, v)| (k, v as u64)).collect())
            .unwrap_or_default();

        Self {
            cache_timings,
            baseline_timings,
        }
    }

    /// Get estimated duration for a test file (cache > baseline > default).
    pub fn estimate(&self, test_path: &str) -> u64 {
        if let Some(&dur) = self.cache_timings.get(test_path) {
            return dur;
        }
        if let Some(&dur) = self.baseline_timings.get(test_path) {
            return dur;
        }
        DEFAULT_DURATION_MS
    }
}

/// Configuration for test isolation behavior.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IsolationBehavior {
    /// Tests that must run alone (one per shard)
    pub isolated: Vec<String>,
    /// Tests that need singleton process
    pub singleton_isolated: Vec<String>,
    /// Tests for thread-only pool
    pub thread_singleton: Vec<String>,
}

impl Default for IsolationBehavior {
    fn default() -> Self {
        Self {
            isolated: Vec::new(),
            singleton_isolated: Vec::new(),
            thread_singleton: Vec::new(),
        }
    }
}

/// Load isolation behavior from the test-parallel.behavior.json file.
pub fn load_isolation_behavior(root: &Path) -> IsolationBehavior {
    let behavior_path = root.join("test/fixtures/test-parallel.behavior.json");
    std::fs::read_to_string(&behavior_path)
        .ok()
        .and_then(|data| serde_json::from_str(&data).ok())
        .unwrap_or_default()
}

/// Create an optimal schedule for the given test files.
pub fn create_schedule(
    test_files: &[PathBuf],
    root: &Path,
    num_cpus: usize,
    timing: &TimingSource,
    behavior: &IsolationBehavior,
) -> Result<Schedule> {
    if test_files.is_empty() {
        return Ok(Schedule {
            num_shards: 0,
            shards: Vec::new(),
            estimated_wall_time_ms: 0,
            total_execution_time_ms: 0,
            efficiency: 1.0,
        });
    }

    let num_shards = compute_optimal_shard_count(test_files.len(), num_cpus);

    // Separate isolated tests from regular ones
    let (isolated, regular): (Vec<_>, Vec<_>) = test_files.iter().partition(|f| {
        let rel = f
            .strip_prefix(root)
            .unwrap_or(f)
            .to_string_lossy()
            .to_string();
        behavior.isolated.iter().any(|p| rel.contains(p))
            || behavior.singleton_isolated.iter().any(|p| rel.contains(p))
    });

    // Build timed entries for regular tests
    let mut timed: Vec<(PathBuf, u64)> = regular
        .into_iter()
        .map(|f| {
            let rel = f
                .strip_prefix(root)
                .unwrap_or(f)
                .to_string_lossy()
                .to_string();
            let dur = timing.estimate(&rel);
            (f.clone(), dur)
        })
        .collect();

    // Sort by duration descending (LPT algorithm)
    timed.sort_by(|a, b| b.1.cmp(&a.1));

    // LPT bin-packing: assign each test to the shard with the least total load
    let effective_shards = num_shards.saturating_sub(isolated.len()).max(1);
    let mut shard_loads: Vec<(Vec<PathBuf>, u64)> = vec![(Vec::new(), 0); effective_shards];

    for (file, duration) in &timed {
        // Find shard with minimum load (greedy LPT)
        let min_idx = shard_loads
            .iter()
            .enumerate()
            .min_by_key(|(_, (_, load))| *load)
            .map(|(i, _)| i)
            .unwrap_or(0);

        shard_loads[min_idx].0.push(file.clone());
        shard_loads[min_idx].1 += duration;
    }

    // Local search refinement: try moving tests between shards to balance
    refine_schedule(&mut shard_loads, &timed);

    // Build final shards
    let mut shards: Vec<Shard> = shard_loads
        .into_iter()
        .enumerate()
        .filter(|(_, (files, _))| !files.is_empty())
        .map(|(i, (files, dur))| Shard {
            index: i,
            test_files: files,
            estimated_duration_ms: dur,
        })
        .collect();

    // Add isolated tests as separate shards
    for (i, isolated_file) in isolated.into_iter().enumerate() {
        let rel = isolated_file
            .strip_prefix(root)
            .unwrap_or(isolated_file)
            .to_string_lossy()
            .to_string();
        let dur = timing.estimate(&rel);
        shards.push(Shard {
            index: shards.len() + i,
            test_files: vec![isolated_file.clone()],
            estimated_duration_ms: dur,
        });
    }

    // Sort shards by duration descending (start longest first)
    shards.sort_by(|a, b| b.estimated_duration_ms.cmp(&a.estimated_duration_ms));

    // Reassign indices after sorting
    for (i, shard) in shards.iter_mut().enumerate() {
        shard.index = i;
    }

    let wall_time = shards
        .iter()
        .map(|s| s.estimated_duration_ms)
        .max()
        .unwrap_or(0);
    let total_time: u64 = shards.iter().map(|s| s.estimated_duration_ms).sum();
    let efficiency = if wall_time > 0 && !shards.is_empty() {
        total_time as f64 / (wall_time as f64 * shards.len() as f64)
    } else {
        1.0
    };

    Ok(Schedule {
        num_shards: shards.len(),
        shards,
        estimated_wall_time_ms: wall_time,
        total_execution_time_ms: total_time,
        efficiency,
    })
}

/// Compute optimal number of shards based on test count and CPU count.
fn compute_optimal_shard_count(num_tests: usize, num_cpus: usize) -> usize {
    let max_workers = num_cpus.min(16); // Hard cap from project rules
    let natural = (num_tests as f64).sqrt().ceil() as usize;
    natural.clamp(MIN_WORKERS, max_workers)
}

/// Local search refinement: try moving tasks from the heaviest shard to the lightest.
fn refine_schedule(shard_loads: &mut [(Vec<PathBuf>, u64)], timed: &[(PathBuf, u64)]) {
    let timing_map: HashMap<PathBuf, u64> = timed.iter().cloned().collect();

    for _ in 0..50 {
        // Max iterations to prevent infinite loops
        let (max_idx, max_load) = shard_loads
            .iter()
            .enumerate()
            .max_by_key(|(_, (_, load))| *load)
            .map(|(i, (_, l))| (i, *l))
            .unwrap_or((0, 0));

        let (min_idx, min_load) = shard_loads
            .iter()
            .enumerate()
            .min_by_key(|(_, (_, load))| *load)
            .map(|(i, (_, l))| (i, *l))
            .unwrap_or((0, 0));

        if max_idx == min_idx || max_load <= min_load {
            break;
        }

        let imbalance = max_load - min_load;

        // Find the best test to move (one that reduces imbalance most)
        let mut best_move: Option<(usize, u64)> = None;
        for (i, file) in shard_loads[max_idx].0.iter().enumerate() {
            if let Some(&dur) = timing_map.get(file) {
                // Moving this test would change loads:
                // max_load - dur, min_load + dur
                // New imbalance: |(max_load - dur) - (min_load + dur)| = |imbalance - 2*dur|
                let new_imbalance = if 2 * dur > imbalance {
                    2 * dur - imbalance
                } else {
                    imbalance - 2 * dur
                };
                if new_imbalance < imbalance {
                    if best_move.is_none() || new_imbalance < best_move.unwrap().1 {
                        best_move = Some((i, new_imbalance));
                    }
                }
            }
        }

        if let Some((move_idx, _)) = best_move {
            let file = shard_loads[max_idx].0.remove(move_idx);
            let dur = timing_map.get(&file).copied().unwrap_or(0);
            shard_loads[max_idx].1 -= dur;
            shard_loads[min_idx].0.push(file);
            shard_loads[min_idx].1 += dur;
        } else {
            break;
        }
    }
}

/// Output format for schedule as Vitest-compatible shard specs.
#[derive(Debug, Serialize)]
pub struct VitestShardSpec {
    pub shard_index: usize,
    pub test_files: Vec<String>,
    pub estimated_ms: u64,
}

impl Schedule {
    /// Convert schedule to Vitest-compatible shard specs.
    pub fn to_vitest_specs(&self, root: &Path) -> Vec<VitestShardSpec> {
        self.shards
            .iter()
            .map(|shard| VitestShardSpec {
                shard_index: shard.index,
                test_files: shard
                    .test_files
                    .iter()
                    .map(|f| {
                        f.strip_prefix(root)
                            .unwrap_or(f)
                            .to_string_lossy()
                            .to_string()
                    })
                    .collect(),
                estimated_ms: shard.estimated_duration_ms,
            })
            .collect()
    }
}
