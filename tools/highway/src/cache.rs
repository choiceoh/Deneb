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

    let all_files: Vec<&PathBuf> = graph
        .forward
        .keys()
        .chain(graph.reverse.keys())
        .collect::<HashSet<_>>()
        .into_iter()
        .collect();

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
