//! Session context management for Vega.
//!
//! Port of Python vega/core.py session functions:
//! load_session, save_session, update_session, resolve_session_context.
//! Stores recent project IDs and command history for pronoun resolution.

use std::collections::VecDeque;
use std::fs;
use std::path::PathBuf;

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// Maximum number of recent project IDs to track.
const MAX_RECENT_IDS: usize = 10;

/// Session state persisted between commands.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct VegaSession {
    /// Recently accessed project IDs (most recent first).
    pub recent_ids: VecDeque<i64>,
    /// Last command executed.
    pub last_command: Option<String>,
    /// Last query string.
    pub last_query: Option<String>,
}

impl Default for VegaSession {
    fn default() -> Self {
        Self {
            recent_ids: VecDeque::new(),
            last_command: None,
            last_query: None,
        }
    }
}

impl VegaSession {
    /// Load session from disk. Returns default if file doesn't exist.
    pub fn load(session_path: &PathBuf) -> Self {
        match fs::read_to_string(session_path) {
            Ok(data) => serde_json::from_str(&data).unwrap_or_default(),
            Err(_) => Self::default(),
        }
    }

    /// Save session to disk.
    pub fn save(&self, session_path: &PathBuf) -> Result<(), String> {
        let data =
            serde_json::to_string_pretty(self).map_err(|e| format!("세션 직렬화 실패: {}", e))?;
        if let Some(parent) = session_path.parent() {
            let _ = fs::create_dir_all(parent);
        }
        fs::write(session_path, data).map_err(|e| format!("세션 저장 실패: {}", e))
    }

    /// Update session after a command execution.
    pub fn update(&mut self, command: &str, data: &Value) {
        self.last_command = Some(command.to_string());

        // Extract project IDs from command result
        let mut new_ids = Vec::new();

        // Single project result
        if let Some(id) = data
            .get("project")
            .and_then(|p| p.get("id"))
            .and_then(|v| v.as_i64())
        {
            new_ids.push(id);
        }
        if let Some(id) = data.get("project_id").and_then(|v| v.as_i64()) {
            new_ids.push(id);
        }

        // Multiple project results
        if let Some(projects) = data.get("projects").and_then(|v| v.as_array()) {
            for p in projects.iter().take(3) {
                if let Some(id) = p.get("id").and_then(|v| v.as_i64()) {
                    new_ids.push(id);
                }
            }
        }

        // Push new IDs to front, deduplicate
        for id in new_ids {
            self.recent_ids.retain(|&x| x != id);
            self.recent_ids.push_front(id);
        }

        // Trim to max
        while self.recent_ids.len() > MAX_RECENT_IDS {
            self.recent_ids.pop_back();
        }
    }

    /// Resolve Korean pronouns to project names using session context.
    /// Returns the resolved query if pronouns were found and resolved.
    pub fn resolve_pronouns(
        &self,
        query: &str,
        conn: &rusqlite::Connection,
    ) -> Option<String> {
        if self.recent_ids.is_empty() {
            return None;
        }

        let pronouns = ["그 프로젝트", "거기", "그거", "아까 그", "방금 그"];
        let has_pronoun = pronouns.iter().any(|p| query.contains(p));
        if !has_pronoun {
            return None;
        }

        let recent_id = self.recent_ids.front()?;
        let name: Option<String> = conn
            .query_row(
                "SELECT name FROM projects WHERE id=?1",
                rusqlite::params![recent_id],
                |r| r.get(0),
            )
            .ok();

        let name = name?;
        if name.is_empty() {
            return None;
        }

        let mut resolved = query.to_string();
        for pronoun in &pronouns {
            resolved = resolved.replace(pronoun, &name);
        }
        Some(resolved)
    }

    /// Get the session file path (next to the DB file).
    pub fn session_path(db_path: &std::path::Path) -> PathBuf {
        let parent = db_path.parent().unwrap_or(std::path::Path::new("."));
        parent.join(".vega_session.json")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_session_update() {
        let mut session = VegaSession::default();
        let data = serde_json::json!({"project_id": 5, "project_name": "테스트"});
        session.update("show", &data);
        assert_eq!(session.recent_ids.front(), Some(&5));
        assert_eq!(session.last_command, Some("show".to_string()));
    }

    #[test]
    fn test_session_dedup() {
        let mut session = VegaSession::default();
        let data1 = serde_json::json!({"project_id": 5});
        let data2 = serde_json::json!({"project_id": 7});
        let data3 = serde_json::json!({"project_id": 5});
        session.update("show", &data1);
        session.update("show", &data2);
        session.update("show", &data3);
        assert_eq!(session.recent_ids.len(), 2);
        assert_eq!(session.recent_ids[0], 5);
        assert_eq!(session.recent_ids[1], 7);
    }
}
