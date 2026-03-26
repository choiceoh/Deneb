//! Subagent registry — lifecycle state machine for parent-child agent runs.
//!
//! Mirrors `src/agents/subagent/subagent-registry.ts`. Keep in sync.
//!
//! This is a pure-logic state machine. The host (TS/Go) handles I/O
//! (message delivery, session creation, timers) and feeds events into this registry.
//!
//! **Thread safety:** This struct is NOT thread-safe. The caller (Go FFI or
//! Node.js napi) must serialize all access. In Go, wrap in a sync.Mutex.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Status of a subagent run.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum SubagentRunStatus {
    Pending,
    Running,
    Completed,
    Failed,
    Killed,
    Timeout,
}

impl SubagentRunStatus {
    /// Whether this status represents a terminal state.
    pub fn is_terminal(self) -> bool {
        matches!(
            self,
            Self::Completed | Self::Failed | Self::Killed | Self::Timeout
        )
    }
}

/// Spawn mode for subagent runs.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum SpawnSubagentMode {
    Spawn,
    Resume,
}

/// Cleanup policy for subagent runs.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum CleanupPolicy {
    Delete,
    Keep,
}

/// A registered subagent run record.
/// Full field set matching TS `SubagentRunRecord` type.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SubagentRunRecord {
    pub run_id: String,
    pub child_session_key: String,
    pub requester_session_key: String,
    pub controller_session_key: String,
    pub status: SubagentRunStatus,
    pub model: Option<String>,
    pub provider: Option<String>,
    pub prompt: Option<String>,
    pub started_at_ms: Option<i64>,
    pub ended_at_ms: Option<i64>,
    pub output: Option<String>,
    // Extended fields matching TS SubagentRunRecord.
    pub task: Option<String>,
    pub label: Option<String>,
    pub workspace_dir: Option<String>,
    pub run_timeout_seconds: Option<f64>,
    pub spawn_mode: Option<SpawnSubagentMode>,
    pub cleanup: Option<CleanupPolicy>,
    pub session_started_at: Option<i64>,
    pub accumulated_runtime_ms: Option<f64>,
    pub ended_reason: Option<String>,
    pub suppress_announce_reason: Option<String>,
    pub expects_completion_message: Option<bool>,
    pub announce_retry_count: Option<u32>,
    pub last_announce_retry_at: Option<i64>,
    pub frozen_result_text: Option<String>,
    pub frozen_result_captured_at: Option<i64>,
    pub fallback_frozen_result_text: Option<String>,
    pub fallback_frozen_result_captured_at: Option<i64>,
    pub attachments_dir: Option<String>,
    pub attachments_root_dir: Option<String>,
    pub retain_attachments_on_keep: Option<bool>,
}

/// Pure-logic subagent registry. Manages run lifecycle state without I/O.
#[derive(Debug, Default)]
pub struct SubagentRegistry {
    runs: HashMap<String, SubagentRunRecord>,
    /// Index: child session key -> run ID (latest).
    by_child_key: HashMap<String, String>,
}

impl SubagentRegistry {
    pub fn new() -> Self {
        Self::default()
    }

    /// Register a new subagent run.
    pub fn register(&mut self, record: SubagentRunRecord) {
        let run_id = record.run_id.clone();
        let child_key = record.child_session_key.clone();
        self.runs.insert(run_id.clone(), record);
        self.by_child_key.insert(child_key, run_id);
    }

    /// Release (remove) a subagent run by ID.
    pub fn release(&mut self, run_id: &str) -> Option<SubagentRunRecord> {
        let record = self.runs.remove(run_id)?;
        // Only remove child key index if it still points to this run.
        if self
            .by_child_key
            .get(&record.child_session_key)
            .map(|s| s.as_str())
            == Some(run_id)
        {
            self.by_child_key.remove(&record.child_session_key);
        }
        Some(record)
    }

    /// Update the status of a run. Returns false if the run doesn't exist.
    pub fn update_status(
        &mut self,
        run_id: &str,
        status: SubagentRunStatus,
        ended_at_ms: Option<i64>,
    ) -> bool {
        if let Some(record) = self.runs.get_mut(run_id) {
            record.status = status;
            if status.is_terminal() {
                record.ended_at_ms = ended_at_ms;
            }
            true
        } else {
            false
        }
    }

    /// Set the output text for a completed run.
    pub fn set_output(&mut self, run_id: &str, output: String) -> bool {
        if let Some(record) = self.runs.get_mut(run_id) {
            record.output = Some(output);
            true
        } else {
            false
        }
    }

    /// List runs for a given requester session key.
    pub fn list_for_requester(&self, requester_session_key: &str) -> Vec<&SubagentRunRecord> {
        self.runs
            .values()
            .filter(|r| r.requester_session_key == requester_session_key)
            .collect()
    }

    /// List runs for a given controller session key.
    pub fn list_for_controller(&self, controller_session_key: &str) -> Vec<&SubagentRunRecord> {
        self.runs
            .values()
            .filter(|r| r.controller_session_key == controller_session_key)
            .collect()
    }

    /// Get a run by child session key (latest registered).
    pub fn get_by_child_session_key(&self, child_session_key: &str) -> Option<&SubagentRunRecord> {
        let run_id = self.by_child_key.get(child_session_key)?;
        self.runs.get(run_id)
    }

    /// Check if any run for a child session key is still active (non-terminal).
    pub fn is_session_run_active(&self, child_session_key: &str) -> bool {
        self.get_by_child_session_key(child_session_key)
            .map(|r| !r.status.is_terminal())
            .unwrap_or(false)
    }

    /// Count active (non-terminal) runs for a requester session.
    pub fn count_active_for_session(&self, requester_session_key: &str) -> usize {
        self.runs
            .values()
            .filter(|r| r.requester_session_key == requester_session_key && !r.status.is_terminal())
            .count()
    }

    /// Count pending descendant runs (recursive: runs whose requester contains the root key).
    pub fn count_pending_descendants(&self, root_session_key: &str) -> usize {
        self.runs
            .values()
            .filter(|r| {
                !r.status.is_terminal()
                    && r.requester_session_key != root_session_key
                    && r.requester_session_key.contains(root_session_key)
            })
            .count()
    }

    /// Check if post-completion announce should be ignored for a child session.
    /// True if the run is already terminal (completed/failed/killed).
    pub fn should_ignore_post_completion_announce(&self, child_session_key: &str) -> bool {
        self.get_by_child_session_key(child_session_key)
            .map(|r| r.status.is_terminal())
            .unwrap_or(true)
    }

    /// Get all runs (for serialization/debugging).
    pub fn all_runs(&self) -> Vec<&SubagentRunRecord> {
        self.runs.values().collect()
    }

    /// Total number of registered runs.
    pub fn len(&self) -> usize {
        self.runs.len()
    }

    pub fn is_empty(&self) -> bool {
        self.runs.is_empty()
    }

    /// Detect orphaned runs: active runs whose requester (parent) session has
    /// already reached a terminal state. Root sessions (not themselves children
    /// of another subagent) are excluded — they cannot be orphaned.
    pub fn detect_orphans(&self) -> Vec<&SubagentRunRecord> {
        self.runs
            .values()
            .filter(|r| {
                if r.status.is_terminal() {
                    return false;
                }
                // Check if the requester is itself a registered child.
                // If not registered (root session), it can't be orphaned.
                match self.get_by_child_session_key(&r.requester_session_key) {
                    Some(parent) => parent.status.is_terminal(),
                    None => false, // Root session — not orphaned.
                }
            })
            .collect()
    }

    /// Clear all runs (for restart/reset).
    pub fn clear(&mut self) {
        self.runs.clear();
        self.by_child_key.clear();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_run(run_id: &str, child: &str, requester: &str) -> SubagentRunRecord {
        SubagentRunRecord {
            run_id: run_id.to_string(),
            child_session_key: child.to_string(),
            requester_session_key: requester.to_string(),
            controller_session_key: requester.to_string(),
            status: SubagentRunStatus::Running,
            model: None,
            provider: None,
            prompt: None,
            started_at_ms: Some(1000),
            ended_at_ms: None,
            output: None,
            task: None,
            label: None,
            workspace_dir: None,
            run_timeout_seconds: None,
            spawn_mode: None,
            cleanup: None,
            session_started_at: None,
            accumulated_runtime_ms: None,
            ended_reason: None,
            suppress_announce_reason: None,
            expects_completion_message: None,
            announce_retry_count: None,
            last_announce_retry_at: None,
            frozen_result_text: None,
            frozen_result_captured_at: None,
            fallback_frozen_result_text: None,
            fallback_frozen_result_captured_at: None,
            attachments_dir: None,
            attachments_root_dir: None,
            retain_attachments_on_keep: None,
        }
    }

    #[test]
    fn register_and_list() {
        let mut reg = SubagentRegistry::new();
        reg.register(make_run("r1", "child:1", "parent:main"));
        reg.register(make_run("r2", "child:2", "parent:main"));
        assert_eq!(reg.len(), 2);
        assert_eq!(reg.list_for_requester("parent:main").len(), 2);
    }

    #[test]
    fn release_run() {
        let mut reg = SubagentRegistry::new();
        reg.register(make_run("r1", "child:1", "parent:main"));
        assert!(reg.release("r1").is_some());
        assert_eq!(reg.len(), 0);
        assert!(reg.release("r1").is_none());
    }

    #[test]
    fn update_status() {
        let mut reg = SubagentRegistry::new();
        reg.register(make_run("r1", "child:1", "parent:main"));
        assert!(reg.update_status("r1", SubagentRunStatus::Completed, Some(2000)));
        let run = reg.get_by_child_session_key("child:1").unwrap();
        assert_eq!(run.status, SubagentRunStatus::Completed);
        assert_eq!(run.ended_at_ms, Some(2000));
    }

    #[test]
    fn is_session_run_active() {
        let mut reg = SubagentRegistry::new();
        reg.register(make_run("r1", "child:1", "parent:main"));
        assert!(reg.is_session_run_active("child:1"));
        reg.update_status("r1", SubagentRunStatus::Completed, Some(2000));
        assert!(!reg.is_session_run_active("child:1"));
    }

    #[test]
    fn count_active() {
        let mut reg = SubagentRegistry::new();
        reg.register(make_run("r1", "child:1", "parent:main"));
        reg.register(make_run("r2", "child:2", "parent:main"));
        assert_eq!(reg.count_active_for_session("parent:main"), 2);
        reg.update_status("r1", SubagentRunStatus::Failed, Some(2000));
        assert_eq!(reg.count_active_for_session("parent:main"), 1);
    }

    #[test]
    fn should_ignore_post_completion() {
        let mut reg = SubagentRegistry::new();
        reg.register(make_run("r1", "child:1", "parent:main"));
        assert!(!reg.should_ignore_post_completion_announce("child:1"));
        reg.update_status("r1", SubagentRunStatus::Completed, Some(2000));
        assert!(reg.should_ignore_post_completion_announce("child:1"));
        assert!(reg.should_ignore_post_completion_announce("child:unknown"));
    }

    #[test]
    fn status_is_terminal() {
        assert!(!SubagentRunStatus::Pending.is_terminal());
        assert!(!SubagentRunStatus::Running.is_terminal());
        assert!(SubagentRunStatus::Completed.is_terminal());
        assert!(SubagentRunStatus::Failed.is_terminal());
        assert!(SubagentRunStatus::Killed.is_terminal());
        assert!(SubagentRunStatus::Timeout.is_terminal());
    }

    #[test]
    fn detect_orphans_parent_terminal() {
        let mut reg = SubagentRegistry::new();
        // Parent run: child:parent requested by root:main.
        reg.register(make_run("r-parent", "child:parent", "root:main"));
        // Child run: child:grandchild requested by child:parent.
        reg.register(make_run("r-child", "child:grandchild", "child:parent"));
        // Parent completes — child becomes orphan.
        reg.update_status("r-parent", SubagentRunStatus::Completed, Some(2000));
        let orphans = reg.detect_orphans();
        assert_eq!(orphans.len(), 1);
        assert_eq!(orphans[0].run_id, "r-child");
    }

    #[test]
    fn detect_orphans_root_not_orphaned() {
        let mut reg = SubagentRegistry::new();
        // Root-level run: child:1 requested by root:main (not itself a child).
        reg.register(make_run("r1", "child:1", "root:main"));
        // root:main is not registered as a child, so r1 should NOT be orphaned.
        let orphans = reg.detect_orphans();
        assert!(orphans.is_empty());
    }
}
