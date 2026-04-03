use rusqlite::{params, Connection};
use serde_json::{json, Value};

use rustc_hash::FxHashMap;
use std::fs;
use std::path::PathBuf;

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

/// Detect changes since the last snapshot.
/// Compares current DB state against `.snapshot.json` stored next to the DB.
/// Detects: new projects, removed projects, status changes, new comms,
/// modified chunks, new chunks.
pub fn cmd_changelog(args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("changelog", &e),
    };

    let snapshot_path = snapshot_path(config);
    let prev = load_snapshot(&snapshot_path);
    let current = build_snapshot(&conn);

    let current_snapshot = match current {
        Ok(s) => s,
        Err(e) => return CommandResult::err("changelog", &e),
    };

    let changes = diff_snapshots(&prev, &current_snapshot);

    // Optionally save the new snapshot (default: true)
    let save = args
        .get("save")
        .and_then(serde_json::Value::as_bool)
        .unwrap_or(true);

    if save {
        if let Err(e) = save_snapshot(&snapshot_path, &current_snapshot) {
            return CommandResult::err("changelog", &format!("스냅샷 저장 실패: {e}"));
        }
    }

    let is_non_empty = |key: &str| -> bool {
        changes
            .get(key)
            .and_then(|v| v.as_array())
            .is_some_and(|a| !a.is_empty())
    };
    let has_changes = is_non_empty("new_projects")
        || is_non_empty("removed_projects")
        || is_non_empty("status_changes")
        || is_non_empty("new_comms")
        || is_non_empty("modified_chunks")
        || is_non_empty("new_chunks");

    CommandResult::ok(
        "changelog",
        json!({
            "has_changes": has_changes,
            "changes": changes,
            "snapshot_saved": save,
        }),
    )
}

// ---------------------------------------------------------------------------
// Snapshot types and helpers
// ---------------------------------------------------------------------------

/// Determine the path for `.snapshot.json` (sits next to the DB file).
fn snapshot_path(config: &VegaConfig) -> PathBuf {
    let mut p = config.db_path.clone();
    p.set_extension("snapshot.json");
    p
}

/// Load a previous snapshot from disk. Returns empty map if missing/corrupt.
fn load_snapshot(path: &PathBuf) -> FxHashMap<i64, Value> {
    let Ok(data) = fs::read_to_string(path) else {
        return FxHashMap::default();
    };
    let Ok(val) = serde_json::from_str::<Value>(&data) else {
        return FxHashMap::default();
    };
    let Some(obj) = val.as_object() else {
        return FxHashMap::default();
    };

    let mut map = FxHashMap::default();
    for (k, v) in obj {
        if let Ok(id) = k.parse::<i64>() {
            map.insert(id, v.clone());
        }
    }
    map
}

/// Build a current snapshot from the DB.
fn build_snapshot(conn: &Connection) -> Result<FxHashMap<i64, Value>, String> {
    let mut map = FxHashMap::default();

    // Projects
    let mut stmt = conn
        .prepare("SELECT id, name, status FROM projects")
        .map_err(|e| format!("프로젝트 조회 실패: {e}"))?;

    let projects: Vec<(i64, String, Option<String>)> = stmt
        .query_map([], |row| Ok((row.get(0)?, row.get(1)?, row.get(2)?)))
        .map_err(|e| format!("프로젝트 조회 실패: {e}"))?
        .filter_map(std::result::Result::ok)
        .collect();

    for (pid, name, status) in &projects {
        let comm_count = count_comms(conn, *pid)?;
        let chunk_hashes = get_chunk_hashes(conn, *pid)?;

        map.insert(
            *pid,
            json!({
                "name": name,
                "status": status,
                "comm_count": comm_count,
                "chunks": chunk_hashes,
            }),
        );
    }

    Ok(map)
}

fn count_comms(conn: &Connection, project_id: i64) -> Result<i64, String> {
    conn.query_row(
        "SELECT COUNT(*) FROM comm_log WHERE project_id = ?1",
        params![project_id],
        |row| row.get(0),
    )
    .map_err(|e| format!("커뮤니케이션 카운트 실패: {e}"))
}

fn get_chunk_hashes(conn: &Connection, project_id: i64) -> Result<Value, String> {
    let mut stmt = conn
        .prepare("SELECT heading, content_hash FROM chunks WHERE project_id = ?1")
        .map_err(|e| format!("청크 해시 조회 실패: {e}"))?;

    let mut hashes = serde_json::Map::new();
    let rows = stmt
        .query_map(params![project_id], |row| {
            let heading: String = row.get::<_, Option<String>>(0)?.unwrap_or_default();
            let hash: String = row.get::<_, Option<String>>(1)?.unwrap_or_default();
            Ok((heading, hash))
        })
        .map_err(|e| format!("청크 해시 조회 실패: {e}"))?;

    for r in rows.flatten() {
        hashes.insert(r.0, Value::String(r.1));
    }

    Ok(Value::Object(hashes))
}

/// Save snapshot to disk.
fn save_snapshot(path: &PathBuf, snapshot: &FxHashMap<i64, Value>) -> Result<(), String> {
    let mut obj = serde_json::Map::new();
    for (id, val) in snapshot {
        obj.insert(id.to_string(), val.clone());
    }
    let data = serde_json::to_string_pretty(&Value::Object(obj)).map_err(|e| e.to_string())?;
    fs::write(path, data).map_err(|e| e.to_string())
}

/// Diff two snapshots and produce a structured changelog.
fn diff_snapshots(prev: &FxHashMap<i64, Value>, current: &FxHashMap<i64, Value>) -> Value {
    let mut new_projects: Vec<Value> = Vec::new();
    let mut removed_projects: Vec<Value> = Vec::new();
    let mut status_changes: Vec<Value> = Vec::new();
    let mut new_comms: Vec<Value> = Vec::new();
    let mut modified_chunks: Vec<Value> = Vec::new();
    let mut new_chunks: Vec<Value> = Vec::new();

    // Detect new projects and changes
    for (id, cur) in current {
        let name = cur.get("name").and_then(|v| v.as_str()).unwrap_or("");
        match prev.get(id) {
            None => {
                new_projects.push(json!({ "id": id, "name": name }));
            }
            Some(old) => {
                // Status change
                let old_status = old.get("status").and_then(|v| v.as_str()).unwrap_or("");
                let new_status = cur.get("status").and_then(|v| v.as_str()).unwrap_or("");
                if old_status != new_status {
                    status_changes.push(json!({
                        "id": id,
                        "name": name,
                        "old_status": old_status,
                        "new_status": new_status,
                    }));
                }

                // New comms
                let old_count = old
                    .get("comm_count")
                    .and_then(serde_json::Value::as_i64)
                    .unwrap_or(0);
                let new_count = cur
                    .get("comm_count")
                    .and_then(serde_json::Value::as_i64)
                    .unwrap_or(0);
                if new_count > old_count {
                    new_comms.push(json!({
                        "id": id,
                        "name": name,
                        "new_entries": new_count - old_count,
                    }));
                }

                // Chunk changes
                let old_chunks = old
                    .get("chunks")
                    .and_then(|v| v.as_object())
                    .cloned()
                    .unwrap_or_default();
                let new_chunks_map = cur
                    .get("chunks")
                    .and_then(|v| v.as_object())
                    .cloned()
                    .unwrap_or_default();

                for (heading, new_hash) in &new_chunks_map {
                    match old_chunks.get(heading) {
                        None => {
                            new_chunks.push(json!({
                                "project_id": id,
                                "project_name": name,
                                "heading": heading,
                            }));
                        }
                        Some(old_hash) => {
                            if old_hash != new_hash {
                                modified_chunks.push(json!({
                                    "project_id": id,
                                    "project_name": name,
                                    "heading": heading,
                                }));
                            }
                        }
                    }
                }
            }
        }
    }

    // Detect removed projects
    for (id, old) in prev {
        if !current.contains_key(id) {
            let name = old.get("name").and_then(|v| v.as_str()).unwrap_or("");
            removed_projects.push(json!({ "id": id, "name": name }));
        }
    }

    json!({
        "new_projects": new_projects,
        "removed_projects": removed_projects,
        "status_changes": status_changes,
        "new_comms": new_comms,
        "modified_chunks": modified_chunks,
        "new_chunks": new_chunks,
    })
}

pub struct ChangelogHandler;

impl super::CommandHandler for ChangelogHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_changelog(args, config)
    }
}

#[cfg(test)]
#[allow(clippy::expect_used)]
mod tests {
    use super::*;

    fn make_project(name: &str, status: &str, comm_count: i64) -> Value {
        json!({
            "name": name,
            "status": status,
            "comm_count": comm_count,
            "chunks": {}
        })
    }

    #[test]
    fn test_diff_snapshots_empty() {
        let prev: FxHashMap<i64, Value> = FxHashMap::default();
        let current: FxHashMap<i64, Value> = FxHashMap::default();
        let diff = diff_snapshots(&prev, &current);

        assert_eq!(
            diff["new_projects"]
                .as_array()
                .expect("new_projects as array")
                .len(),
            0
        );
        assert_eq!(
            diff["removed_projects"]
                .as_array()
                .expect("removed_projects as array")
                .len(),
            0
        );
        assert_eq!(
            diff["status_changes"]
                .as_array()
                .expect("status_changes as array")
                .len(),
            0
        );
    }

    #[test]
    fn test_diff_snapshots_new_project() {
        let prev: FxHashMap<i64, Value> = FxHashMap::default();
        let mut current: FxHashMap<i64, Value> = FxHashMap::default();
        current.insert(1, make_project("Project A", "active", 0));

        let diff = diff_snapshots(&prev, &current);
        let new_projects = diff["new_projects"]
            .as_array()
            .expect("new_projects as array");
        assert_eq!(new_projects.len(), 1);
        assert_eq!(new_projects[0]["name"], "Project A");
    }

    #[test]
    fn test_diff_snapshots_removed_project() {
        let mut prev: FxHashMap<i64, Value> = FxHashMap::default();
        prev.insert(1, make_project("Project A", "active", 0));
        let current: FxHashMap<i64, Value> = FxHashMap::default();

        let diff = diff_snapshots(&prev, &current);
        let removed = diff["removed_projects"]
            .as_array()
            .expect("removed_projects as array");
        assert_eq!(removed.len(), 1);
        assert_eq!(removed[0]["name"], "Project A");
    }

    #[test]
    fn test_diff_snapshots_status_change() {
        let mut prev: FxHashMap<i64, Value> = FxHashMap::default();
        prev.insert(1, make_project("Project A", "active", 5));

        let mut current: FxHashMap<i64, Value> = FxHashMap::default();
        current.insert(1, make_project("Project A", "completed", 5));

        let diff = diff_snapshots(&prev, &current);
        let changes = diff["status_changes"]
            .as_array()
            .expect("status_changes as array");
        assert_eq!(changes.len(), 1);
        assert_eq!(changes[0]["old_status"], "active");
        assert_eq!(changes[0]["new_status"], "completed");
    }

    #[test]
    fn test_diff_snapshots_new_comms() {
        let mut prev: FxHashMap<i64, Value> = FxHashMap::default();
        prev.insert(1, make_project("Project A", "active", 3));

        let mut current: FxHashMap<i64, Value> = FxHashMap::default();
        current.insert(1, make_project("Project A", "active", 7));

        let diff = diff_snapshots(&prev, &current);
        let comms = diff["new_comms"].as_array().expect("new_comms as array");
        assert_eq!(comms.len(), 1);
        assert_eq!(comms[0]["new_entries"], 4);
    }

    #[test]
    fn test_diff_snapshots_chunk_changes() {
        let mut prev: FxHashMap<i64, Value> = FxHashMap::default();
        prev.insert(
            1,
            json!({
                "name": "Project A",
                "status": "active",
                "comm_count": 0,
                "chunks": {
                    "overview": "hash_old",
                    "notes": "hash_same"
                }
            }),
        );

        let mut current: FxHashMap<i64, Value> = FxHashMap::default();
        current.insert(
            1,
            json!({
                "name": "Project A",
                "status": "active",
                "comm_count": 0,
                "chunks": {
                    "overview": "hash_new",
                    "notes": "hash_same",
                    "contacts": "hash_brand_new"
                }
            }),
        );

        let diff = diff_snapshots(&prev, &current);
        let modified = diff["modified_chunks"]
            .as_array()
            .expect("modified_chunks as array");
        assert_eq!(modified.len(), 1);
        assert_eq!(modified[0]["heading"], "overview");

        let new_chunks = diff["new_chunks"].as_array().expect("new_chunks as array");
        assert_eq!(new_chunks.len(), 1);
        assert_eq!(new_chunks[0]["heading"], "contacts");
    }

    #[test]
    fn test_diff_snapshots_no_changes() {
        let mut prev: FxHashMap<i64, Value> = FxHashMap::default();
        prev.insert(1, make_project("Project A", "active", 5));

        let mut current: FxHashMap<i64, Value> = FxHashMap::default();
        current.insert(1, make_project("Project A", "active", 5));

        let diff = diff_snapshots(&prev, &current);
        assert_eq!(
            diff["new_projects"]
                .as_array()
                .expect("new_projects as array")
                .len(),
            0
        );
        assert_eq!(
            diff["removed_projects"]
                .as_array()
                .expect("removed_projects as array")
                .len(),
            0
        );
        assert_eq!(
            diff["status_changes"]
                .as_array()
                .expect("status_changes as array")
                .len(),
            0
        );
        assert_eq!(
            diff["new_comms"]
                .as_array()
                .expect("new_comms as array")
                .len(),
            0
        );
    }
}
