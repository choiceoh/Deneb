//! Sync DB changes back to .md files.
//!
//! Port of Python vega/addons/sync_back.py.

use std::fs;

use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

/// Sync database changes back to markdown files.
pub fn cmd_sync_back(args: &Value, config: &VegaConfig) -> CommandResult {
    let dry_run = args
        .get("dry_run")
        .and_then(|v| v.as_bool())
        .unwrap_or(false);

    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("sync-back", &e),
    };

    let mut stmt = conn
        .prepare("SELECT id, name, source_file, status, client, person_internal FROM projects WHERE source_file IS NOT NULL")
        .unwrap();

    let projects: Vec<(
        i64,
        String,
        String,
        Option<String>,
        Option<String>,
        Option<String>,
    )> = stmt
        .query_map([], |r| {
            Ok((
                r.get::<_, i64>(0)?,
                r.get::<_, Option<String>>(1)?.unwrap_or_default(),
                r.get::<_, Option<String>>(2)?.unwrap_or_default(),
                r.get::<_, Option<String>>(3)?,
                r.get::<_, Option<String>>(4)?,
                r.get::<_, Option<String>>(5)?,
            ))
        })
        .unwrap()
        .filter_map(|r| r.ok())
        .collect();

    let mut synced = 0;
    let mut skipped = 0;
    let mut errors = Vec::new();

    for (_pid, name, source_file, status, _client, _person) in &projects {
        let clean = source_file
            .trim_start_matches("memory:")
            .trim_start_matches("file:");

        let md_path = if std::path::Path::new(clean).is_absolute() {
            std::path::PathBuf::from(clean)
        } else {
            config.md_dir.join(clean)
        };

        if !md_path.is_file() {
            skipped += 1;
            continue;
        }

        if dry_run {
            synced += 1;
            continue;
        }

        let content = match fs::read_to_string(&md_path) {
            Ok(c) => c,
            Err(e) => {
                errors.push(json!({"project": name, "error": e.to_string()}));
                continue;
            }
        };

        // Update status in table if present
        let mut new_content = content.clone();
        if let Some(st) = status {
            let re = regex::Regex::new(r"(?m)^\|\s*\*?\*?상태\*?\*?\s*\|\s*(.*?)\s*\|").unwrap();
            if let Some(cap) = re.captures(&content) {
                let old = cap.get(1).unwrap().as_str();
                if old.trim() != st.trim() {
                    new_content = re
                        .replace(&new_content, |caps: &regex::Captures| {
                            caps[0].replace(old, st)
                        })
                        .to_string();
                }
            }
        }

        if new_content != content {
            if let Err(e) = fs::write(&md_path, &new_content) {
                errors.push(json!({"project": name, "error": e.to_string()}));
            } else {
                synced += 1;
            }
        } else {
            skipped += 1;
        }
    }

    CommandResult::ok(
        "sync-back",
        json!({
            "total": projects.len(),
            "synced": synced,
            "skipped": skipped,
            "errors": errors,
            "dry_run": dry_run,
        }),
    )
}
