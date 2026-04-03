//! Tags command — list all tags with project counts.

use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

pub struct TagsHandler;

impl super::CommandHandler for TagsHandler {
    fn execute(&self, config: &VegaConfig, args: &Value) -> CommandResult {
        cmd_tags(args, config)
    }
}

/// tags: List all tags with project counts.
pub(super) fn cmd_tags(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("tags", &e),
    };

    let mut stmt = match conn.prepare(
        "SELECT t.name, COUNT(DISTINCT c.project_id) as cnt
             FROM tags t
             JOIN chunk_tags ct ON ct.tag_id = t.id
             JOIN chunks c ON c.id = ct.chunk_id
             GROUP BY t.name ORDER BY t.name",
    ) {
        Ok(s) => s,
        Err(e) => return CommandResult::err("tags", &format!("태그 쿼리 실패: {e}")),
    };

    let tags: Vec<Value> = match stmt.query_map([], |r| {
        Ok(json!({
            "name": r.get::<_, String>(0)?,
            "project_count": r.get::<_, i64>(1)?,
        }))
    }) {
        Ok(rows) => rows.filter_map(std::result::Result::ok).collect(),
        Err(e) => return CommandResult::err("tags", &format!("태그 쿼리 실패: {e}")),
    };

    CommandResult::ok("tags", json!({ "tags": tags }))
}

#[cfg(test)]
#[allow(clippy::expect_used)]
mod tests {
    use std::path::PathBuf;

    use rusqlite::params;
    use serde_json::json;
    use tempfile::TempDir;

    use super::*;
    use crate::db::schema::init_db;

    fn setup_db() -> Result<(TempDir, VegaConfig), Box<dyn std::error::Error>> {
        let tmp = tempfile::tempdir()?;
        let db_path = tmp.path().join("projects.db");
        let conn = rusqlite::Connection::open(&db_path)?;
        init_db(&conn)?;

        conn.execute(
            "INSERT INTO projects (id, name) VALUES (?1, ?2)",
            params![1_i64, "Alpha"],
        )?;
        conn.execute(
            "INSERT INTO chunks (id, project_id, section_heading, content, chunk_type)
             VALUES (?1, ?2, ?3, ?4, ?5)",
            params![10_i64, 1_i64, "메모", "내용", "memo"],
        )?;
        conn.execute(
            "INSERT INTO tags (id, name) VALUES (?1, ?2)",
            params![100_i64, "urgent"],
        )?;
        conn.execute(
            "INSERT INTO chunk_tags (chunk_id, tag_id) VALUES (?1, ?2)",
            params![10_i64, 100_i64],
        )?;

        let cfg = VegaConfig {
            db_path,
            md_dir: PathBuf::from("projects"),
            ..VegaConfig::default()
        };
        Ok((tmp, cfg))
    }

    #[test]
    fn cmd_tags_returns_tag_with_project_count() -> Result<(), Box<dyn std::error::Error>> {
        let (_tmp, cfg) = setup_db()?;

        let result = cmd_tags(&json!({}), &cfg);
        assert!(result.success);
        let tags = result
            .data
            .get("tags")
            .and_then(|v| v.as_array())
            .expect("tags should be an array");
        assert_eq!(tags.len(), 1);
        assert_eq!(tags[0].get("name").and_then(|v| v.as_str()), Some("urgent"));
        assert_eq!(
            tags[0]
                .get("project_count")
                .and_then(serde_json::Value::as_i64),
            Some(1)
        );
        Ok(())
    }
}
