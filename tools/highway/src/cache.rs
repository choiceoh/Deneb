//! Content-addressed test cache using xxHash3 for ultra-fast file hashing.
//!
//! Each test file's "cache key" is a hash of:
//! 1. The test file's own content
//! 2. All transitive dependencies' content (from the import graph)
//! 3. The vitest config hash (invalidates all on config change)
//!
//! If the cache key matches the previous run, the test is skipped entirely.

use std::collections::{HashMap, HashSet};
use std::path::{Path, PathBuf};
use std::time::SystemTime;

use anyhow::{Context, Result};
use dashmap::DashMap;
use rayon::prelude::*;
use serde::{Deserialize, Serialize};
use xxhash_rust::xxh3::xxh3_128;

use crate::analyzer::ImportGraph;

/// Cache entry for a single test file.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CacheEntry {
    /// Combined hash of test + all transitive deps
    pub content_hash: u128,
    /// Last test result: pass/fail
    pub result: TestResult,
    /// Execution duration in milliseconds
    pub duration_ms: u64,
    /// Timestamp of the cache entry
    pub timestamp: u64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum TestResult {
    Pass,
    Fail,
}

/// The persistent cache store.
#[derive(Debug, Serialize, Deserialize)]
pub struct CacheStore {
    /// Version for cache invalidation on schema changes
    pub version: u32,
    /// Hash of the vitest config (global invalidation key)
    pub config_hash: u128,
    /// test file path (relative to root) -> cache entry
    pub entries: HashMap<String, CacheEntry>,
}

impl Default for CacheStore {
    fn default() -> Self {
        Self {
            version: 1,
            config_hash: 0,
            entries: HashMap::new(),
        }
    }
}

const CACHE_FILE: &str = ".highway-cache.json";

impl CacheStore {
    /// Load cache from disk, returning default if not found or incompatible.
    pub fn load(root: &Path) -> Self {
        let cache_path = root.join(CACHE_FILE);
        match std::fs::read_to_string(&cache_path) {
            Ok(data) => serde_json::from_str(&data).unwrap_or_default(),
            Err(_) => Self::default(),
        }
    }

    /// Save cache to disk.
    pub fn save(&self, root: &Path) -> Result<()> {
        let cache_path = root.join(CACHE_FILE);
        let data = serde_json::to_string_pretty(self)?;
        std::fs::write(&cache_path, data).context("write cache file")?;
        Ok(())
    }

    /// Check if a test can be skipped (cache hit with passing result).
    pub fn is_cached(&self, test_path: &str, current_hash: u128) -> bool {
        if let Some(entry) = self.entries.get(test_path) {
            entry.content_hash == current_hash && entry.result == TestResult::Pass
        } else {
            false
        }
    }

    /// Record a test result in the cache.
    pub fn record(
        &mut self,
        test_path: String,
        content_hash: u128,
        result: TestResult,
        duration_ms: u64,
    ) {
        let timestamp = SystemTime::now()
            .duration_since(SystemTime::UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();

        self.entries.insert(
            test_path,
            CacheEntry {
                content_hash,
                result,
                duration_ms,
                timestamp,
            },
        );
    }
}

/// Compute content hashes for all test files considering their full dependency tree.
pub fn compute_test_hashes(
    graph: &ImportGraph,
    root: &Path,
    config_hash: u128,
) -> Result<HashMap<PathBuf, u128>> {
    // Phase 1: Hash all individual files in parallel
    let file_hashes: DashMap<PathBuf, u128> = DashMap::new();

    let all_files: HashSet<&PathBuf> = graph.forward.keys().chain(graph.reverse.keys()).collect();

    all_files.par_iter().for_each(|file_path| {
        if let Ok(hash) = hash_file(file_path) {
            file_hashes.insert((*file_path).clone(), hash);
        }
    });

    // Phase 2: For each test file, compute combined hash of all transitive deps
    let test_hashes: DashMap<PathBuf, u128> = DashMap::new();

    graph.test_files.par_iter().for_each(|test_file| {
        let deps = collect_transitive_deps(graph, test_file);

        // Collect all file hashes in a deterministic order
        let mut dep_hashes: Vec<(String, u128)> = deps
            .iter()
            .filter_map(|dep| {
                file_hashes.get(dep).map(|h| {
                    let rel = dep.strip_prefix(root).unwrap_or(dep);
                    (rel.to_string_lossy().to_string(), *h)
                })
            })
            .collect();

        // Sort by path for deterministic hashing
        dep_hashes.sort_by(|a, b| a.0.cmp(&b.0));

        // Combine all hashes into one
        let mut combined = Vec::with_capacity(dep_hashes.len() * 16 + 16);
        combined.extend_from_slice(&config_hash.to_le_bytes());
        for (_, hash) in &dep_hashes {
            combined.extend_from_slice(&hash.to_le_bytes());
        }

        let final_hash = xxh3_128(&combined);
        test_hashes.insert(test_file.clone(), final_hash);
    });

    Ok(test_hashes.into_iter().collect())
}

/// Collect all transitive dependencies of a file (including the file itself).
fn collect_transitive_deps(graph: &ImportGraph, file: &Path) -> HashSet<PathBuf> {
    let mut visited = HashSet::new();
    let mut stack = vec![file.to_path_buf()];

    while let Some(current) = stack.pop() {
        if !visited.insert(current.clone()) {
            continue;
        }
        if let Some(deps) = graph.forward.get(&current) {
            for dep in deps {
                if !visited.contains(dep) {
                    stack.push(dep.clone());
                }
            }
        }
    }

    visited
}

/// Hash a single file's content using xxHash3-128.
fn hash_file(path: &Path) -> Result<u128> {
    let content = std::fs::read(path).with_context(|| format!("read {}", path.display()))?;
    Ok(xxh3_128(&content))
}

/// Hash vitest config files to detect config changes.
pub fn hash_config(root: &Path) -> u128 {
    let config_files = [
        "vitest.config.ts",
        "vitest.unit.config.ts",
        "vitest.gateway.config.ts",
        "vitest.channels.config.ts",
        "vitest.extensions.config.ts",
        "tsconfig.json",
        "package.json",
    ];

    let mut combined = Vec::new();
    for name in &config_files {
        let path = root.join(name);
        if let Ok(content) = std::fs::read(&path) {
            combined.extend_from_slice(&xxh3_128(&content).to_le_bytes());
        }
    }

    xxh3_128(&combined)
}

/// Summary of cache analysis.
#[derive(Debug, Serialize)]
pub struct CacheSummary {
    pub total_tests: usize,
    pub cached_tests: usize,
    pub tests_to_run: usize,
    pub estimated_skip_time_ms: u64,
    pub cache_hit_rate: f64,
}

/// Analyze cache state and determine which tests need to run.
pub fn analyze_cache(
    cache: &CacheStore,
    test_hashes: &HashMap<PathBuf, u128>,
    root: &Path,
) -> (Vec<PathBuf>, CacheSummary) {
    let mut to_run = Vec::new();
    let mut cached_count = 0;
    let mut skip_time = 0u64;

    for (test_path, hash) in test_hashes {
        let rel_path = test_path
            .strip_prefix(root)
            .unwrap_or(test_path)
            .to_string_lossy()
            .to_string();

        if cache.is_cached(&rel_path, *hash) {
            cached_count += 1;
            if let Some(entry) = cache.entries.get(&rel_path) {
                skip_time += entry.duration_ms;
            }
        } else {
            to_run.push(test_path.clone());
        }
    }

    let total = test_hashes.len();
    let summary = CacheSummary {
        total_tests: total,
        cached_tests: cached_count,
        tests_to_run: to_run.len(),
        estimated_skip_time_ms: skip_time,
        cache_hit_rate: if total > 0 {
            cached_count as f64 / total as f64
        } else {
            0.0
        },
    };

    (to_run, summary)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::analyzer::ImportGraph;
    use std::collections::{HashMap, HashSet};

    // ── CacheStore defaults ─────────────────────────────────────────────────

    #[test]
    fn cache_store_default_values() {
        let store = CacheStore::default();
        assert_eq!(store.version, 1);
        assert_eq!(store.config_hash, 0);
        assert!(store.entries.is_empty());
    }

    // ── is_cached ───────────────────────────────────────────────────────────

    #[test]
    fn is_cached_returns_true_for_matching_pass() {
        let mut store = CacheStore::default();
        store.record("test.ts".into(), 42, TestResult::Pass, 100);
        assert!(store.is_cached("test.ts", 42));
    }

    #[test]
    fn is_cached_returns_false_for_hash_mismatch() {
        let mut store = CacheStore::default();
        store.record("test.ts".into(), 42, TestResult::Pass, 100);
        assert!(!store.is_cached("test.ts", 99));
    }

    #[test]
    fn is_cached_returns_false_for_fail_result() {
        let mut store = CacheStore::default();
        store.record("test.ts".into(), 42, TestResult::Fail, 100);
        assert!(!store.is_cached("test.ts", 42));
    }

    #[test]
    fn is_cached_returns_false_for_missing_entry() {
        let store = CacheStore::default();
        assert!(!store.is_cached("nonexistent.ts", 1));
    }

    // ── record ──────────────────────────────────────────────────────────────

    #[test]
    fn record_stores_entry_correctly() {
        let mut store = CacheStore::default();
        store.record("a.test.ts".into(), 123, TestResult::Pass, 500);

        let entry = store.entries.get("a.test.ts").unwrap();
        assert_eq!(entry.content_hash, 123);
        assert_eq!(entry.result, TestResult::Pass);
        assert_eq!(entry.duration_ms, 500);
        assert!(entry.timestamp > 0);
    }

    #[test]
    fn record_overwrites_previous_entry() {
        let mut store = CacheStore::default();
        store.record("a.test.ts".into(), 1, TestResult::Pass, 100);
        store.record("a.test.ts".into(), 2, TestResult::Fail, 200);

        let entry = store.entries.get("a.test.ts").unwrap();
        assert_eq!(entry.content_hash, 2);
        assert_eq!(entry.result, TestResult::Fail);
        assert_eq!(entry.duration_ms, 200);
    }

    // ── save / load round-trip ──────────────────────────────────────────────

    #[test]
    fn save_and_load_round_trip() {
        let dir = tempfile::tempdir().unwrap();
        let root = dir.path();

        let mut store = CacheStore::default();
        store.config_hash = 999;
        store.record("x.test.ts".into(), 77, TestResult::Pass, 300);
        store.save(root).unwrap();

        let loaded = CacheStore::load(root);
        assert_eq!(loaded.config_hash, 999);
        assert_eq!(loaded.entries.len(), 1);
        assert!(loaded.is_cached("x.test.ts", 77));
    }

    #[test]
    fn load_returns_default_when_no_file() {
        let dir = tempfile::tempdir().unwrap();
        let store = CacheStore::load(dir.path());
        assert_eq!(store.version, 1);
        assert!(store.entries.is_empty());
    }

    #[test]
    fn load_returns_default_for_corrupt_json() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(dir.path().join(".highway-cache.json"), "not json").unwrap();
        let store = CacheStore::load(dir.path());
        assert_eq!(store.version, 1);
        assert!(store.entries.is_empty());
    }

    // ── collect_transitive_deps ─────────────────────────────────────────────

    fn make_graph_for_deps(forward: Vec<(&str, Vec<&str>)>) -> ImportGraph {
        let forward_map: HashMap<PathBuf, HashSet<PathBuf>> = forward
            .into_iter()
            .map(|(k, vs)| {
                (
                    PathBuf::from(k),
                    vs.into_iter().map(PathBuf::from).collect(),
                )
            })
            .collect();

        ImportGraph {
            forward: forward_map,
            reverse: HashMap::new(),
            test_files: Vec::new(),
        }
    }

    #[test]
    fn collect_transitive_deps_includes_self() {
        let graph = make_graph_for_deps(vec![("/a.ts", vec![])]);
        let deps = collect_transitive_deps(&graph, Path::new("/a.ts"));
        assert!(deps.contains(&PathBuf::from("/a.ts")));
    }

    #[test]
    fn collect_transitive_deps_follows_chain() {
        let graph = make_graph_for_deps(vec![
            ("/a.ts", vec!["/b.ts"]),
            ("/b.ts", vec!["/c.ts"]),
            ("/c.ts", vec![]),
        ]);
        let deps = collect_transitive_deps(&graph, Path::new("/a.ts"));
        assert_eq!(deps.len(), 3);
        assert!(deps.contains(&PathBuf::from("/a.ts")));
        assert!(deps.contains(&PathBuf::from("/b.ts")));
        assert!(deps.contains(&PathBuf::from("/c.ts")));
    }

    #[test]
    fn collect_transitive_deps_handles_cycles() {
        let graph = make_graph_for_deps(vec![
            ("/a.ts", vec!["/b.ts"]),
            ("/b.ts", vec!["/a.ts"]),
        ]);
        let deps = collect_transitive_deps(&graph, Path::new("/a.ts"));
        assert_eq!(deps.len(), 2);
    }

    #[test]
    fn collect_transitive_deps_diamond() {
        // a -> b, a -> c, b -> d, c -> d
        let graph = make_graph_for_deps(vec![
            ("/a.ts", vec!["/b.ts", "/c.ts"]),
            ("/b.ts", vec!["/d.ts"]),
            ("/c.ts", vec!["/d.ts"]),
            ("/d.ts", vec![]),
        ]);
        let deps = collect_transitive_deps(&graph, Path::new("/a.ts"));
        assert_eq!(deps.len(), 4);
    }

    // ── hash_file ───────────────────────────────────────────────────────────

    #[test]
    fn hash_file_deterministic() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("f.ts");
        std::fs::write(&path, "export const x = 1;").unwrap();

        let h1 = hash_file(&path).unwrap();
        let h2 = hash_file(&path).unwrap();
        assert_eq!(h1, h2, "same content should produce same hash");
    }

    #[test]
    fn hash_file_different_content() {
        let dir = tempfile::tempdir().unwrap();
        let p1 = dir.path().join("a.ts");
        let p2 = dir.path().join("b.ts");
        std::fs::write(&p1, "aaa").unwrap();
        std::fs::write(&p2, "bbb").unwrap();

        assert_ne!(hash_file(&p1).unwrap(), hash_file(&p2).unwrap());
    }

    #[test]
    fn hash_file_missing_returns_error() {
        assert!(hash_file(Path::new("/nonexistent/file.ts")).is_err());
    }

    // ── hash_config ─────────────────────────────────────────────────────────

    #[test]
    fn hash_config_stable_for_same_files() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(dir.path().join("package.json"), r#"{"name":"test"}"#).unwrap();

        let h1 = hash_config(dir.path());
        let h2 = hash_config(dir.path());
        assert_eq!(h1, h2);
    }

    #[test]
    fn hash_config_changes_when_file_changes() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(dir.path().join("package.json"), r#"{"v":1}"#).unwrap();
        let h1 = hash_config(dir.path());

        std::fs::write(dir.path().join("package.json"), r#"{"v":2}"#).unwrap();
        let h2 = hash_config(dir.path());
        assert_ne!(h1, h2);
    }

    #[test]
    fn hash_config_empty_dir() {
        let dir = tempfile::tempdir().unwrap();
        let h = hash_config(dir.path());
        // No config files means hashing empty bytes — still deterministic
        let h2 = hash_config(dir.path());
        assert_eq!(h, h2);
    }

    // ── analyze_cache ───────────────────────────────────────────────────────

    #[test]
    fn analyze_cache_all_cached() {
        let mut store = CacheStore::default();
        store.record("a.test.ts".into(), 10, TestResult::Pass, 500);
        store.record("b.test.ts".into(), 20, TestResult::Pass, 300);

        let root = Path::new("/project");
        let hashes: HashMap<PathBuf, u128> = [
            (PathBuf::from("/project/a.test.ts"), 10u128),
            (PathBuf::from("/project/b.test.ts"), 20u128),
        ]
        .into();

        let (to_run, summary) = analyze_cache(&store, &hashes, root);
        assert!(to_run.is_empty());
        assert_eq!(summary.cached_tests, 2);
        assert_eq!(summary.tests_to_run, 0);
        assert!((summary.cache_hit_rate - 1.0).abs() < f64::EPSILON);
        assert_eq!(summary.estimated_skip_time_ms, 800);
    }

    #[test]
    fn analyze_cache_none_cached() {
        let store = CacheStore::default();
        let root = Path::new("/project");
        let hashes: HashMap<PathBuf, u128> =
            [(PathBuf::from("/project/a.test.ts"), 10u128)].into();

        let (to_run, summary) = analyze_cache(&store, &hashes, root);
        assert_eq!(to_run.len(), 1);
        assert_eq!(summary.cached_tests, 0);
        assert_eq!(summary.tests_to_run, 1);
        assert!((summary.cache_hit_rate).abs() < f64::EPSILON);
    }

    #[test]
    fn analyze_cache_partial() {
        let mut store = CacheStore::default();
        store.record("a.test.ts".into(), 10, TestResult::Pass, 200);

        let root = Path::new("/project");
        let hashes: HashMap<PathBuf, u128> = [
            (PathBuf::from("/project/a.test.ts"), 10u128),
            (PathBuf::from("/project/b.test.ts"), 20u128),
        ]
        .into();

        let (to_run, summary) = analyze_cache(&store, &hashes, root);
        assert_eq!(to_run.len(), 1);
        assert_eq!(summary.cached_tests, 1);
        assert_eq!(summary.tests_to_run, 1);
        assert!((summary.cache_hit_rate - 0.5).abs() < f64::EPSILON);
    }

    #[test]
    fn analyze_cache_empty_input() {
        let store = CacheStore::default();
        let hashes: HashMap<PathBuf, u128> = HashMap::new();
        let (to_run, summary) = analyze_cache(&store, &hashes, Path::new("/"));
        assert!(to_run.is_empty());
        assert_eq!(summary.total_tests, 0);
        assert!((summary.cache_hit_rate).abs() < f64::EPSILON);
    }
}
