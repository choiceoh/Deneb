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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::cache::CacheStore;

    // ── compute_optimal_shard_count ─────────────────────────────────────────

    #[test]
    fn shard_count_minimum_is_2() {
        // Even with 1 test, we get at least MIN_WORKERS (2)
        assert_eq!(compute_optimal_shard_count(1, 8), 2);
    }

    #[test]
    fn shard_count_capped_at_16() {
        // With 1000 tests and 32 CPUs, sqrt(1000) ≈ 32, but hard cap is 16
        assert_eq!(compute_optimal_shard_count(1000, 32), 16);
    }

    #[test]
    fn shard_count_capped_at_cpus() {
        // sqrt(100) = 10, but only 4 CPUs available
        assert_eq!(compute_optimal_shard_count(100, 4), 4);
    }

    #[test]
    fn shard_count_sqrt_based() {
        // sqrt(16) = 4, with 8 CPUs available -> 4
        assert_eq!(compute_optimal_shard_count(16, 8), 4);
    }

    #[test]
    fn shard_count_rounds_up() {
        // sqrt(10) ≈ 3.16 -> ceil = 4
        assert_eq!(compute_optimal_shard_count(10, 8), 4);
    }

    // ── TimingSource ────────────────────────────────────────────────────────

    #[test]
    fn timing_source_returns_default_for_unknown() {
        let cache = CacheStore::default();
        let timing = TimingSource::new(&cache, None);
        assert_eq!(timing.estimate("unknown.test.ts"), DEFAULT_DURATION_MS);
    }

    #[test]
    fn timing_source_prefers_cache_over_baseline() {
        let mut cache = CacheStore::default();
        cache.record("a.test.ts".into(), 1, crate::cache::TestResult::Pass, 999);

        // Write a baseline file with a different value
        let dir = tempfile::tempdir().unwrap();
        let baseline = dir.path().join("timings.json");
        std::fs::write(&baseline, r#"{"a.test.ts": 100}"#).unwrap();

        let timing = TimingSource::new(&cache, Some(baseline.as_path()));
        assert_eq!(timing.estimate("a.test.ts"), 999);
    }

    #[test]
    fn timing_source_falls_back_to_baseline() {
        let cache = CacheStore::default();
        let dir = tempfile::tempdir().unwrap();
        let baseline = dir.path().join("timings.json");
        std::fs::write(&baseline, r#"{"b.test.ts": 500.0}"#).unwrap();

        let timing = TimingSource::new(&cache, Some(baseline.as_path()));
        assert_eq!(timing.estimate("b.test.ts"), 500);
    }

    // ── IsolationBehavior ───────────────────────────────────────────────────

    #[test]
    fn isolation_behavior_default_empty() {
        let b = IsolationBehavior::default();
        assert!(b.isolated.is_empty());
        assert!(b.singleton_isolated.is_empty());
        assert!(b.thread_singleton.is_empty());
    }

    #[test]
    fn load_isolation_behavior_missing_file() {
        let dir = tempfile::tempdir().unwrap();
        let b = load_isolation_behavior(dir.path());
        assert!(b.isolated.is_empty());
    }

    #[test]
    fn load_isolation_behavior_from_file() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(dir.path().join("test/fixtures")).unwrap();
        std::fs::write(
            dir.path().join("test/fixtures/test-parallel.behavior.json"),
            r#"{"isolated": ["gateway"], "singleton_isolated": [], "thread_singleton": []}"#,
        )
        .unwrap();

        let b = load_isolation_behavior(dir.path());
        assert_eq!(b.isolated, vec!["gateway"]);
    }

    // ── create_schedule ─────────────────────────────────────────────────────

    #[test]
    fn create_schedule_empty_input() {
        let cache = CacheStore::default();
        let timing = TimingSource::new(&cache, None);
        let behavior = IsolationBehavior::default();

        let schedule = create_schedule(&[], Path::new("/root"), 4, &timing, &behavior).unwrap();
        assert_eq!(schedule.num_shards, 0);
        assert!(schedule.shards.is_empty());
        assert_eq!(schedule.estimated_wall_time_ms, 0);
        assert!((schedule.efficiency - 1.0).abs() < f64::EPSILON);
    }

    #[test]
    fn create_schedule_single_test() {
        let cache = CacheStore::default();
        let timing = TimingSource::new(&cache, None);
        let behavior = IsolationBehavior::default();
        let tests = vec![PathBuf::from("/root/a.test.ts")];

        let schedule = create_schedule(&tests, Path::new("/root"), 4, &timing, &behavior).unwrap();
        assert!(schedule.num_shards >= 1);
        // The single test should appear in exactly one shard
        let total_files: usize = schedule.shards.iter().map(|s| s.test_files.len()).sum();
        assert_eq!(total_files, 1);
    }

    #[test]
    fn create_schedule_distributes_tests() {
        let cache = CacheStore::default();
        let timing = TimingSource::new(&cache, None);
        let behavior = IsolationBehavior::default();

        let tests: Vec<PathBuf> = (0..20)
            .map(|i| PathBuf::from(format!("/root/test{}.test.ts", i)))
            .collect();

        let schedule = create_schedule(&tests, Path::new("/root"), 8, &timing, &behavior).unwrap();
        assert!(schedule.num_shards >= 2);
        let total_files: usize = schedule.shards.iter().map(|s| s.test_files.len()).sum();
        assert_eq!(total_files, 20);
    }

    #[test]
    fn create_schedule_isolates_tests() {
        let cache = CacheStore::default();
        let timing = TimingSource::new(&cache, None);
        let behavior = IsolationBehavior {
            isolated: vec!["gateway".into()],
            singleton_isolated: Vec::new(),
            thread_singleton: Vec::new(),
        };

        let tests = vec![
            PathBuf::from("/root/gateway.test.ts"),
            PathBuf::from("/root/utils.test.ts"),
            PathBuf::from("/root/config.test.ts"),
        ];

        let schedule = create_schedule(&tests, Path::new("/root"), 4, &timing, &behavior).unwrap();

        // The gateway test should be alone in its shard
        let gateway_shard = schedule
            .shards
            .iter()
            .find(|s| {
                s.test_files
                    .iter()
                    .any(|f| f.to_string_lossy().contains("gateway"))
            })
            .expect("gateway test should be in a shard");
        assert_eq!(
            gateway_shard.test_files.len(),
            1,
            "isolated test should be alone in its shard"
        );
    }

    #[test]
    fn create_schedule_indices_are_sequential() {
        let cache = CacheStore::default();
        let timing = TimingSource::new(&cache, None);
        let behavior = IsolationBehavior::default();

        let tests: Vec<PathBuf> = (0..10)
            .map(|i| PathBuf::from(format!("/root/t{}.test.ts", i)))
            .collect();

        let schedule = create_schedule(&tests, Path::new("/root"), 4, &timing, &behavior).unwrap();
        for (i, shard) in schedule.shards.iter().enumerate() {
            assert_eq!(shard.index, i, "shard indices should be sequential");
        }
    }

    #[test]
    fn create_schedule_shards_sorted_by_duration_desc() {
        let mut cache = CacheStore::default();
        // Give different durations to force ordering
        cache.record("fast.test.ts".into(), 1, crate::cache::TestResult::Pass, 100);
        cache.record("slow.test.ts".into(), 2, crate::cache::TestResult::Pass, 5000);

        let timing = TimingSource::new(&cache, None);
        let behavior = IsolationBehavior::default();

        let tests = vec![
            PathBuf::from("/root/fast.test.ts"),
            PathBuf::from("/root/slow.test.ts"),
            PathBuf::from("/root/other1.test.ts"),
            PathBuf::from("/root/other2.test.ts"),
        ];

        let schedule = create_schedule(&tests, Path::new("/root"), 4, &timing, &behavior).unwrap();
        for i in 1..schedule.shards.len() {
            assert!(
                schedule.shards[i - 1].estimated_duration_ms
                    >= schedule.shards[i].estimated_duration_ms,
                "shards should be sorted by duration descending"
            );
        }
    }

    // ── refine_schedule ─────────────────────────────────────────────────────

    #[test]
    fn refine_schedule_balances_loads() {
        let a = PathBuf::from("a");
        let b = PathBuf::from("b");
        let c = PathBuf::from("c");
        let timed = vec![(a.clone(), 1000), (b.clone(), 100), (c.clone(), 100)];

        // Start with imbalanced assignment: shard0 has all, shard1 empty
        let mut shard_loads = vec![
            (vec![a.clone(), b.clone(), c.clone()], 1200),
            (vec![], 0),
        ];

        refine_schedule(&mut shard_loads, &timed);

        // After refinement, loads should be more balanced
        let diff = (shard_loads[0].1 as i64 - shard_loads[1].1 as i64).unsigned_abs();
        assert!(diff < 1200, "loads should be more balanced after refinement");
    }

    #[test]
    fn refine_schedule_noop_when_balanced() {
        let a = PathBuf::from("a");
        let b = PathBuf::from("b");
        let timed = vec![(a.clone(), 500), (b.clone(), 500)];

        let mut shard_loads = vec![(vec![a.clone()], 500), (vec![b.clone()], 500)];

        refine_schedule(&mut shard_loads, &timed);

        // Should remain the same
        assert_eq!(shard_loads[0].1, 500);
        assert_eq!(shard_loads[1].1, 500);
    }

    // ── to_vitest_specs ─────────────────────────────────────────────────────

    #[test]
    fn to_vitest_specs_strips_root_prefix() {
        let schedule = Schedule {
            num_shards: 1,
            shards: vec![Shard {
                index: 0,
                test_files: vec![PathBuf::from("/root/src/a.test.ts")],
                estimated_duration_ms: 100,
            }],
            estimated_wall_time_ms: 100,
            total_execution_time_ms: 100,
            efficiency: 1.0,
        };

        let specs = schedule.to_vitest_specs(Path::new("/root"));
        assert_eq!(specs.len(), 1);
        assert_eq!(specs[0].test_files, vec!["src/a.test.ts"]);
        assert_eq!(specs[0].estimated_ms, 100);
    }

    #[test]
    fn to_vitest_specs_preserves_ordering() {
        let schedule = Schedule {
            num_shards: 2,
            shards: vec![
                Shard {
                    index: 0,
                    test_files: vec![PathBuf::from("/r/a.test.ts")],
                    estimated_duration_ms: 500,
                },
                Shard {
                    index: 1,
                    test_files: vec![PathBuf::from("/r/b.test.ts"), PathBuf::from("/r/c.test.ts")],
                    estimated_duration_ms: 400,
                },
            ],
            estimated_wall_time_ms: 500,
            total_execution_time_ms: 900,
            efficiency: 0.9,
        };

        let specs = schedule.to_vitest_specs(Path::new("/r"));
        assert_eq!(specs[0].shard_index, 0);
        assert_eq!(specs[1].shard_index, 1);
        assert_eq!(specs[1].test_files.len(), 2);
    }
}
