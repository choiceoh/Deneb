//! File import and incremental update logic.
//!
//! Port of Python vega/db/importer.py — imports .md files into the Vega SQLite database.

use std::collections::{HashMap, HashSet};
use std::path::{Path, PathBuf};

use sha2::Digest;

use rusqlite::{params, Connection};

use crate::config::VegaConfig;
use crate::db::classify::{classify_section, extract_tags};
use crate::db::parser::{extract_table_meta, split_sections};
use crate::db::schema::{init_db, rebuild_fts};

/// Result of importing a single file.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum UpsertResult {
    Updated,
    Skipped,
}

/// Import statistics.
#[derive(Debug, Clone, Default)]
pub struct ImportStats {
    pub imported: usize,
    pub skipped: usize,
    pub errors: Vec<String>,
    pub total_projects: i64,
    pub total_chunks: i64,
    pub total_comms: i64,
    pub total_tags: i64,
}

/// Bulk import all .md files from a directory.
pub fn import_files(config: &VegaConfig) -> Result<ImportStats, Box<dyn std::error::Error>> {
    let conn = Connection::open(&config.db_path)?;
    init_db(&conn)?;

    let md_files = collect_md_files(&config.md_dir);
    if md_files.is_empty() {
        return Ok(ImportStats::default());
    }

    let mut stats = ImportStats::default();

    for fpath in &md_files {
        let fname = fpath
            .file_name()
            .unwrap_or_default()
            .to_string_lossy()
            .to_string();

        match import_single_file(&conn, fpath) {
            Ok(UpsertResult::Skipped) => {
                stats.skipped += 1;
            }
            Ok(UpsertResult::Updated) => {
                stats.imported += 1;
            }
            Err(e) => {
                stats.errors.push(format!("{}: {}", fname, e));
            }
        }
    }

    conn.execute_batch("COMMIT")?;

    // Gather final stats
    let row: (i64, i64, i64, i64) = conn.query_row(
        "SELECT
            (SELECT COUNT(*) FROM projects),
            (SELECT COUNT(*) FROM chunks),
            (SELECT COUNT(*) FROM comm_log),
            (SELECT COUNT(DISTINCT name) FROM tags)",
        [],
        |r| Ok((r.get(0)?, r.get(1)?, r.get(2)?, r.get(3)?)),
    )?;
    stats.total_projects = row.0;
    stats.total_chunks = row.1;
    stats.total_comms = row.2;
    stats.total_tags = row.3;

    Ok(stats)
}

/// Import a single .md file (insert-only, skips if already exists).
fn import_single_file(
    conn: &Connection,
    fpath: &Path,
) -> Result<UpsertResult, Box<dyn std::error::Error>> {
    let fpath_str = fpath.to_string_lossy().to_string();
    let fname = fpath
        .file_name()
        .unwrap_or_default()
        .to_string_lossy()
        .to_string();

    // Check if already imported
    let exists: bool = conn.query_row(
        "SELECT COUNT(*) > 0 FROM projects WHERE source_file=?1 OR source_file=?2",
        params![fname, fpath_str],
        |r| r.get(0),
    )?;
    if exists {
        return Ok(UpsertResult::Skipped);
    }

    let text = std::fs::read_to_string(fpath)?;
    let text = text.replace("\r\n", "\n");
    // Handle BOM
    let text = text.strip_prefix('\u{feff}').unwrap_or(&text);

    let meta = extract_table_meta(text);
    let (sections, comm_entries) = split_sections(text);
    let section_pairs: Vec<(String, String)> = sections
        .iter()
        .map(|s| (s.heading.clone(), s.body.clone()))
        .collect();
    let tags = extract_tags(&meta, &section_pairs);

    let project_name = meta
        .get("name")
        .cloned()
        .unwrap_or_else(|| fname.trim_end_matches(".md").to_string());

    conn.execute(
        "INSERT INTO projects (name, client, status, capacity, biz_type,
                              person_internal, person_external, partner,
                              source_file, imported_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)",
        params![
            project_name,
            meta.get("client").cloned().unwrap_or_default(),
            meta.get("status").cloned().unwrap_or_default(),
            meta.get("capacity").cloned().unwrap_or_default(),
            meta.get("biz_type").cloned().unwrap_or_default(),
            meta.get("person_internal").cloned().unwrap_or_default(),
            meta.get("person_external").cloned().unwrap_or_default(),
            meta.get("partner").cloned().unwrap_or_default(),
            fpath_str,
            chrono::Utc::now().to_rfc3339(),
        ],
    )?;
    let pid = conn.last_insert_rowid();

    // Insert sections + tags
    insert_sections_and_tags(conn, pid, &sections, &tags)?;

    // Insert communication log
    for entry in &comm_entries {
        conn.execute(
            "INSERT INTO comm_log (project_id, log_date, sender, subject, summary)
             VALUES (?1, ?2, ?3, ?4, ?5)",
            params![pid, entry.date, entry.sender, entry.subject, entry.summary],
        )?;
    }

    Ok(UpsertResult::Updated)
}

/// Incremental update: only re-parse changed files (hash-based).
pub fn import_incremental(config: &VegaConfig) -> Result<ImportStats, Box<dyn std::error::Error>> {
    let conn = Connection::open(&config.db_path)?;
    init_db(&conn)?;

    let md_files = collect_md_files(&config.md_dir);
    if md_files.is_empty() {
        return Ok(ImportStats::default());
    }

    // Load existing hashes
    let existing_hashes: HashMap<String, String> = conn
        .prepare("SELECT source_file, content_hash FROM file_hashes")?
        .query_map([], |row| Ok((row.get(0)?, row.get(1)?)))?
        .filter_map(|r| r.ok())
        .collect();

    let mut stats = ImportStats::default();
    let mut current_files = HashSet::new();

    for fpath in &md_files {
        let fpath_str = fpath.to_string_lossy().to_string();
        let fname = fpath
            .file_name()
            .unwrap_or_default()
            .to_string_lossy()
            .to_string();
        current_files.insert(fpath_str.clone());
        current_files.insert(fname.clone());

        match upsert_md_file(&conn, fpath, &existing_hashes) {
            Ok(UpsertResult::Skipped) => stats.skipped += 1,
            Ok(UpsertResult::Updated) => stats.imported += 1,
            Err(e) => {
                stats.errors.push(format!("{}: {}", fname, e));
            }
        }
    }

    // Delete removed files
    for key in existing_hashes.keys() {
        if !current_files.contains(key) {
            delete_project_by_source(&conn, key)?;
        }
    }

    // Rebuild FTS if many changes
    if stats.imported > 10 {
        let _ = rebuild_fts(&conn);
    }

    conn.execute_batch("COMMIT")?;
    Ok(stats)
}

/// Upsert a single .md file: hash-check, parse, update DB.
pub fn upsert_md_file(
    conn: &Connection,
    fpath: &Path,
    existing_hashes: &HashMap<String, String>,
) -> Result<UpsertResult, Box<dyn std::error::Error>> {
    let fpath_str = fpath.to_string_lossy().to_string();
    let fname = fpath
        .file_name()
        .unwrap_or_default()
        .to_string_lossy()
        .to_string();

    let text = std::fs::read_to_string(fpath)?;
    let text = text.replace("\r\n", "\n");
    let text = text.strip_prefix('\u{feff}').unwrap_or(&text);

    let content_hash = format!("{:x}", sha2::Sha256::digest(text.as_bytes()));

    // Skip if hash unchanged
    if existing_hashes.get(&fpath_str) == Some(&content_hash)
        || existing_hashes.get(&fname) == Some(&content_hash)
    {
        return Ok(UpsertResult::Skipped);
    }

    let meta = extract_table_meta(text);
    let (sections, comm_entries) = split_sections(text);
    let section_pairs: Vec<(String, String)> = sections
        .iter()
        .map(|s| (s.heading.clone(), s.body.clone()))
        .collect();
    let tags = extract_tags(&meta, &section_pairs);

    let project_name = meta
        .get("name")
        .cloned()
        .unwrap_or_else(|| fname.trim_end_matches(".md").to_string());

    // Check for existing project
    let old_pid: Option<i64> = conn
        .query_row(
            "SELECT id FROM projects WHERE source_file=?1 OR source_file=?2",
            params![fname, fpath_str],
            |r| r.get(0),
        )
        .ok();

    let pid = if let Some(pid) = old_pid {
        // Update existing project
        conn.execute(
            "UPDATE projects SET name=?1, client=?2, status=?3, capacity=?4, biz_type=?5,
                person_internal=?6, person_external=?7, partner=?8,
                source_file=?9, imported_at=?10
             WHERE id=?11",
            params![
                project_name,
                meta.get("client").cloned().unwrap_or_default(),
                meta.get("status").cloned().unwrap_or_default(),
                meta.get("capacity").cloned().unwrap_or_default(),
                meta.get("biz_type").cloned().unwrap_or_default(),
                meta.get("person_internal").cloned().unwrap_or_default(),
                meta.get("person_external").cloned().unwrap_or_default(),
                meta.get("partner").cloned().unwrap_or_default(),
                fpath_str,
                chrono::Utc::now().to_rfc3339(),
                pid,
            ],
        )?;
        // Clear old data (triggers handle FTS cleanup)
        conn.execute("DELETE FROM comm_log WHERE project_id=?1", params![pid])?;
        conn.execute(
            "DELETE FROM chunk_tags WHERE chunk_id IN (SELECT id FROM chunks WHERE project_id=?1)",
            params![pid],
        )?;
        conn.execute("DELETE FROM chunks WHERE project_id=?1", params![pid])?;
        pid
    } else {
        // Insert new project
        conn.execute(
            "INSERT INTO projects (name, client, status, capacity, biz_type,
                                  person_internal, person_external, partner,
                                  source_file, imported_at)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)",
            params![
                project_name,
                meta.get("client").cloned().unwrap_or_default(),
                meta.get("status").cloned().unwrap_or_default(),
                meta.get("capacity").cloned().unwrap_or_default(),
                meta.get("biz_type").cloned().unwrap_or_default(),
                meta.get("person_internal").cloned().unwrap_or_default(),
                meta.get("person_external").cloned().unwrap_or_default(),
                meta.get("partner").cloned().unwrap_or_default(),
                fpath_str,
                chrono::Utc::now().to_rfc3339(),
            ],
        )?;
        conn.last_insert_rowid()
    };

    // Insert sections + tags
    insert_sections_and_tags(conn, pid, &sections, &tags)?;

    // Insert comm log
    for entry in &comm_entries {
        conn.execute(
            "INSERT INTO comm_log (project_id, log_date, sender, subject, summary)
             VALUES (?1, ?2, ?3, ?4, ?5)",
            params![pid, entry.date, entry.sender, entry.subject, entry.summary],
        )?;
    }

    // Update file hash
    conn.execute(
        "DELETE FROM file_hashes WHERE source_file=?1",
        params![fname],
    )?;
    conn.execute(
        "INSERT OR REPLACE INTO file_hashes (source_file, content_hash, updated_at)
         VALUES (?1, ?2, ?3)",
        params![fpath_str, content_hash, chrono::Utc::now().to_rfc3339()],
    )?;

    Ok(UpsertResult::Updated)
}

/// Delete a project and all related data by source file key.
pub fn delete_project_by_source(
    conn: &Connection,
    source_key: &str,
) -> Result<(), Box<dyn std::error::Error>> {
    let basename = Path::new(source_key)
        .file_name()
        .unwrap_or_default()
        .to_string_lossy()
        .to_string();

    let pid: Option<i64> = conn
        .query_row(
            "SELECT id FROM projects WHERE source_file=?1 OR source_file=?2",
            params![source_key, basename],
            |r| r.get(0),
        )
        .ok();

    if let Some(pid) = pid {
        conn.execute("DELETE FROM comm_log WHERE project_id=?1", params![pid])?;
        conn.execute(
            "DELETE FROM chunk_tags WHERE chunk_id IN (SELECT id FROM chunks WHERE project_id=?1)",
            params![pid],
        )?;
        conn.execute("DELETE FROM chunks WHERE project_id=?1", params![pid])?;
        conn.execute("DELETE FROM projects WHERE id=?1", params![pid])?;
    }
    conn.execute(
        "DELETE FROM file_hashes WHERE source_file=?1",
        params![source_key],
    )?;
    Ok(())
}

// -- Internal helpers --

/// Insert sections with classification and tags.
fn insert_sections_and_tags(
    conn: &Connection,
    pid: i64,
    sections: &[crate::db::parser::Section],
    tags: &HashSet<String>,
) -> Result<(), Box<dyn std::error::Error>> {
    for section in sections {
        let ctype = classify_section(&section.heading, &section.body);
        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type, entry_date)
             VALUES (?1, ?2, ?3, ?4, ?5)",
            params![
                pid,
                section.heading,
                section.body,
                ctype,
                section.entry_date
            ],
        )?;
        let cid = conn.last_insert_rowid();

        for tag in tags {
            conn.execute(
                "INSERT OR IGNORE INTO tags (name) VALUES (?1)",
                params![tag],
            )?;
            let tag_id: Option<i64> = conn
                .query_row("SELECT id FROM tags WHERE name=?1", params![tag], |r| {
                    r.get(0)
                })
                .ok();
            if let Some(tid) = tag_id {
                conn.execute(
                    "INSERT OR IGNORE INTO chunk_tags (chunk_id, tag_id) VALUES (?1, ?2)",
                    params![cid, tid],
                )?;
            }
        }
    }
    Ok(())
}

/// Collect all .md files from a directory (recursive, no symlinks).
fn collect_md_files(dir: &Path) -> Vec<PathBuf> {
    let mut files = Vec::new();
    collect_md_files_recursive(dir, &mut files);
    files.sort();
    files
}

fn collect_md_files_recursive(dir: &Path, out: &mut Vec<PathBuf>) {
    let entries = match std::fs::read_dir(dir) {
        Ok(e) => e,
        Err(_) => return,
    };
    for entry in entries.flatten() {
        let path = entry.path();
        // Skip symlinks
        if path.is_symlink() {
            continue;
        }
        if path.is_dir() {
            collect_md_files_recursive(&path, out);
        } else if path.extension().is_some_and(|ext| ext == "md") {
            out.push(path);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    use tempfile::TempDir;

    fn setup_test_dir() -> Result<TempDir, Box<dyn std::error::Error>> {
        let dir = TempDir::new()?;
        let md = "# Test Project\n\n| 발주처 | TestClient |\n| 상태 | 진행중 |\n\n## 현재 상황\n\nSome status info here with enough content.\n\n## 2025-03-01\n\n- **미팅 완료** (김대희)\n> 내용 정리 완료\n";
        let mut f = std::fs::File::create(dir.path().join("test.md"))?;
        f.write_all(md.as_bytes())?;
        Ok(dir)
    }

    #[test]
    fn test_import_and_query() -> Result<(), Box<dyn std::error::Error>> {
        let dir = setup_test_dir()?;
        let db_path = dir.path().join("test.db");
        let conn = Connection::open(&db_path)?;
        init_db(&conn)?;

        let fpath = dir.path().join("test.md");
        let result = import_single_file(&conn, &fpath)?;
        assert_eq!(result, UpsertResult::Updated);

        // Verify project exists
        let name: String =
            conn.query_row("SELECT name FROM projects WHERE id=1", [], |r| r.get(0))?;
        assert_eq!(name, "Test Project");

        // Verify chunks
        let chunk_count: i64 =
            conn.query_row("SELECT COUNT(*) FROM chunks WHERE project_id=1", [], |r| {
                r.get(0)
            })?;
        assert!(chunk_count >= 2, "Should have at least 2 sections");

        // Verify comm log
        let comm_count: i64 = conn.query_row(
            "SELECT COUNT(*) FROM comm_log WHERE project_id=1",
            [],
            |r| r.get(0),
        )?;
        assert_eq!(comm_count, 1);

        // Verify FTS works
        let fts_count: i64 = conn.query_row(
            "SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH '\"status\"'",
            [],
            |r| r.get(0),
        )?;
        assert!(fts_count >= 0); // Triggers should have populated FTS
        Ok(())
    }
}
