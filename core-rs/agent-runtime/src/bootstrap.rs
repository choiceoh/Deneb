//! Bootstrap file cache — pure-logic cache for workspace bootstrap files.
//!
//! Mirrors `src/agents/bootstrap-cache.ts`. Keep in sync.
//!
//! The host (TS/Go) loads files from disk and stores them here.
//! This module provides the cache data structure and lookup logic.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// A cached workspace bootstrap file entry.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct WorkspaceBootstrapFile {
    pub path: String,
    pub content: String,
}

/// Pure-logic cache for workspace bootstrap files, keyed by session key.
#[derive(Debug, Default)]
pub struct BootstrapCache {
    cache: HashMap<String, Vec<WorkspaceBootstrapFile>>,
}

impl BootstrapCache {
    pub fn new() -> Self {
        Self::default()
    }

    /// Get cached files for a session key.
    pub fn get(&self, session_key: &str) -> Option<&Vec<WorkspaceBootstrapFile>> {
        self.cache.get(session_key)
    }

    /// Store files for a session key.
    pub fn set(&mut self, session_key: String, files: Vec<WorkspaceBootstrapFile>) {
        self.cache.insert(session_key, files);
    }

    /// Clear cache entry for a session key.
    pub fn clear_snapshot(&mut self, session_key: &str) {
        self.cache.remove(session_key);
    }

    /// Conditional clear on session rollover: clears if the session key changed
    /// or if a previous session ID was provided (indicating rollover).
    /// Empty strings are treated as absent (matching TS truthy check behavior).
    pub fn clear_on_session_rollover(
        &mut self,
        session_key: Option<&str>,
        previous_session_id: Option<&str>,
    ) {
        if let Some(key) = session_key {
            if !key.is_empty() && previous_session_id.filter(|s| !s.is_empty()).is_some() {
                self.cache.remove(key);
            }
        }
    }

    /// Clear the entire cache.
    pub fn clear_all(&mut self) {
        self.cache.clear();
    }

    /// Number of cached session entries.
    pub fn len(&self) -> usize {
        self.cache.len()
    }

    pub fn is_empty(&self) -> bool {
        self.cache.is_empty()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn get_set_clear() {
        let mut cache = BootstrapCache::new();
        let files = vec![WorkspaceBootstrapFile {
            path: "CLAUDE.md".to_string(),
            content: "# Rules".to_string(),
        }];
        cache.set("agent:bot:main".to_string(), files);
        assert_eq!(cache.len(), 1);
        assert!(cache.get("agent:bot:main").is_some());
        cache.clear_snapshot("agent:bot:main");
        assert!(cache.get("agent:bot:main").is_none());
    }

    #[test]
    fn clear_on_rollover() {
        let mut cache = BootstrapCache::new();
        cache.set("agent:bot:main".to_string(), vec![]);
        cache.clear_on_session_rollover(Some("agent:bot:main"), Some("prev-id"));
        assert!(cache.is_empty());
    }

    #[test]
    fn clear_on_rollover_no_previous() {
        let mut cache = BootstrapCache::new();
        cache.set("agent:bot:main".to_string(), vec![]);
        cache.clear_on_session_rollover(Some("agent:bot:main"), None);
        // No previous session ID = no rollover = no clear.
        assert_eq!(cache.len(), 1);
    }

    #[test]
    fn clear_all() {
        let mut cache = BootstrapCache::new();
        cache.set("s1".to_string(), vec![]);
        cache.set("s2".to_string(), vec![]);
        cache.clear_all();
        assert!(cache.is_empty());
    }
}
