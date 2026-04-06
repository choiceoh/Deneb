use rusqlite::{params, Connection};
use serde_json::{json, Value};
use std::fs;

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

/// FTS5 full-text search across chunks.
/// Args: query (string), limit (int, default 20), project (optional string)
pub fn cmd_memory_search(args: &Value, config: &VegaConfig) -> CommandResult {
    let Some(query) = args.get("query").and_then(|v| v.as_str()) else {
        return CommandResult::err("memory-search", "query 파라미터가 필요합니다");
    };
    let limit = args
        .get("limit")
        .and_then(serde_json::Value::as_i64)
        .unwrap_or(20);
    let project_filter = args.get("project").and_then(|v| v.as_str());

    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("memory-search", &e),
    };

    // FTS5 search
    let mut fts_results = match fts_search(&conn, query, limit, project_filter) {
        Ok(r) => r,
        Err(e) => return CommandResult::err("memory-search", &e),
    };

    // Vector search is now handled by the Go gateway via SGLang HTTP API.
    // The Rust side only runs FTS search for the memory-search command.

    // Truncate to limit
    fts_results.truncate(limit as usize);

    CommandResult::ok(
        "memory-search",
        json!({
            "query": query,
            "count": fts_results.len(),
            "results": fts_results,
        }),
    )
}

/// FTS5 search helper.
fn fts_search(
    conn: &Connection,
    query: &str,
    limit: i64,
    project_filter: Option<&str>,
) -> Result<Vec<Value>, String> {
    let fts_query = sanitize_fts_query(query);

    let sql = if project_filter.is_some() {
        "SELECT c.id, c.project_id, c.section_heading, c.content, c.source_file,
                p.name as project_name,
                rank
         FROM chunks_fts
         JOIN chunks c ON chunks_fts.rowid = c.id
         LEFT JOIN projects p ON c.project_id = p.id
         WHERE chunks_fts MATCH ?1 AND p.name = ?3
         ORDER BY rank
         LIMIT ?2"
    } else {
        "SELECT c.id, c.project_id, c.section_heading, c.content, c.source_file,
                p.name as project_name,
                rank
         FROM chunks_fts
         JOIN chunks c ON chunks_fts.rowid = c.id
         LEFT JOIN projects p ON c.project_id = p.id
         WHERE chunks_fts MATCH ?1
         ORDER BY rank
         LIMIT ?2"
    };

    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("FTS 쿼리 준비 실패: {e}"))?;

    let map_row = |row: &rusqlite::Row| -> rusqlite::Result<Value> {
        Ok(json!({
            "id": row.get::<_, i64>(0)?,
            "project_id": row.get::<_, Option<i64>>(1)?,
            "section": row.get::<_, Option<String>>(2)?,
            "content": row.get::<_, String>(3)?,
            "source_file": row.get::<_, Option<String>>(4)?,
            "project_name": row.get::<_, Option<String>>(5)?,
            "score": row.get::<_, f64>(6)?,
            "source": "fts",
        }))
    };

    let rows: Vec<Value> = if let Some(pf) = project_filter {
        stmt.query_map(params![fts_query, limit, pf], map_row)
            .map_err(|e| format!("FTS 쿼리 실행 실패: {e}"))?
            .filter_map(|r: rusqlite::Result<Value>| r.ok())
            .collect()
    } else {
        stmt.query_map(params![fts_query, limit], map_row)
            .map_err(|e| format!("FTS 쿼리 실행 실패: {e}"))?
            .filter_map(|r: rusqlite::Result<Value>| r.ok())
            .collect()
    };

    Ok(rows)
}

/// Sanitize FTS5 query: escape special characters.
fn sanitize_fts_query(query: &str) -> String {
    // FTS5 uses double-quotes for phrase queries; escape bare quotes
    let cleaned = query.replace('"', "\"\"");
    // Wrap each token in quotes for exact matching
    cleaned
        .split_whitespace()
        .map(|token| format!("\"{token}\""))
        .collect::<Vec<_>>()
        .join(" ")
}

// Vector search, cosine similarity, and RRF reranking removed.
// These are now handled by the Go gateway via SGLang HTTP API.

/// Index .md files into DB with content hash tracking.
/// Scans `md_dir`, computes SHA-256 hash per file, upserts chunks.
pub fn cmd_memory_update(args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("memory-update", &e),
    };

    let force = args
        .get("force")
        .and_then(serde_json::Value::as_bool)
        .unwrap_or(false);
    let md_dir = &config.md_dir;

    if !md_dir.exists() {
        return CommandResult::err(
            "memory-update",
            &format!(
                "마크다운 디렉토리가 존재하지 않습니다: {}",
                md_dir.display()
            ),
        );
    }

    let mut files_scanned: i64 = 0;
    let mut files_updated: i64 = 0;
    let mut files_skipped: i64 = 0;
    let mut chunks_created: i64 = 0;
    let mut chunks_deleted: i64 = 0;
    let mut errors: Vec<String> = Vec::new();

    let entries: Vec<_> = match fs::read_dir(md_dir) {
        Ok(rd) => rd.filter_map(std::result::Result::ok).collect(),
        Err(e) => return CommandResult::err("memory-update", &format!("디렉토리 읽기 실패: {e}")),
    };

    for entry in entries {
        let path = entry.path();
        if path.extension().and_then(|e| e.to_str()) != Some("md") {
            continue;
        }

        files_scanned += 1;

        let content = match fs::read_to_string(&path) {
            Ok(c) => c,
            Err(e) => {
                errors.push(format!("{}: {e}", path.display()));
                continue;
            }
        };

        let hash = compute_content_hash(&content);
        let filename = path.file_name().and_then(|n| n.to_str()).unwrap_or("");

        // Check existing hash
        if !force {
            let existing_hash: Option<String> = conn
                .query_row(
                    "SELECT content_hash FROM file_hashes WHERE filename = ?1",
                    params![filename],
                    |row| row.get(0),
                )
                .ok();

            if existing_hash.as_deref() == Some(&hash) {
                files_skipped += 1;
                continue;
            }
        }

        // Find or create project
        let project_name = path.file_stem().and_then(|n| n.to_str()).unwrap_or("");

        let project_id: Option<i64> = conn
            .query_row(
                "SELECT id FROM projects WHERE name = ?1",
                params![project_name],
                |row| row.get(0),
            )
            .ok();

        // Delete old chunks for this file
        let deleted: i64 = conn
            .execute(
                "DELETE FROM chunks WHERE source_file = ?1",
                params![filename],
            )
            .unwrap_or(0) as i64;

        chunks_deleted += deleted;

        // Parse sections and insert chunks
        let chunks = parse_md_sections(&content);
        let mut created = 0i64;
        for (section, chunk_content) in &chunks {
            if chunk_content.trim().is_empty() {
                continue;
            }
            let _ = conn.execute(
                "INSERT INTO chunks (project_id, section_heading, content, source_file)
                 VALUES (?1, ?2, ?3, ?4)",
                params![project_id, section, chunk_content, filename],
            );
            created += 1;
        }

        chunks_created += created;

        // Update file hash
        conn.execute(
            "INSERT OR REPLACE INTO file_hashes (filename, content_hash) VALUES (?1, ?2)",
            params![filename, hash],
        )
        .ok();

        files_updated += 1;
    }

    CommandResult::ok(
        "memory-update",
        json!({
            "files_scanned": files_scanned,
            "files_updated": files_updated,
            "files_skipped": files_skipped,
            "chunks_created": chunks_created,
            "chunks_deleted": chunks_deleted,
            "errors": errors,
        }),
    )
}

/// Parse markdown into (`section_name`, content) pairs.
fn parse_md_sections(content: &str) -> Vec<(String, String)> {
    let mut sections = Vec::new();
    let mut current_section = String::from("_header");
    let mut current_content = String::new();

    for line in content.lines() {
        if line.starts_with("## ") || line.starts_with("# ") {
            // Save previous section
            if !current_content.is_empty() {
                sections.push((current_section.clone(), current_content.clone()));
                current_content.clear();
            }
            current_section = line.trim_start_matches('#').trim().to_string();
        } else {
            current_content.push_str(line);
            current_content.push('\n');
        }
    }

    // Save last section
    if !current_content.is_empty() {
        sections.push((current_section, current_content));
    }

    sections
}

/// Compute SHA-256 hex digest of content.
fn compute_content_hash(content: &str) -> String {
    use std::collections::hash_map::DefaultHasher;
    use std::hash::{Hash, Hasher};
    // Simple hash for change detection (not cryptographic)
    let mut hasher = DefaultHasher::new();
    content.hash(&mut hasher);
    format!("{:016x}", hasher.finish())
}

/// Generate embeddings for chunks.
/// In sglang mode, embedding is handled by the Go gateway via `SGLang` HTTP API.
pub fn cmd_memory_embed(_args: &Value, config: &VegaConfig) -> CommandResult {
    if config.has_sglang() {
        return CommandResult::ok(
            "memory-embed",
            json!({
                "message": "SGLang 모드: 임베딩은 Go 게이트웨이에서 SGLang HTTP API로 처리됩니다.",
                "backend": "sglang",
            }),
        );
    }

    CommandResult::err(
        "memory-embed",
        "임베딩 백엔드가 설정되지 않았습니다 (VEGA_INFERENCE=sglang 권장)",
    )
}

/// Return file/chunk/embedding counts and status.
pub fn cmd_memory_status(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("memory-status", &e),
    };

    let chunk_count: i64 = conn
        .query_row("SELECT COUNT(*) FROM chunks", [], |row| row.get(0))
        .unwrap_or(0);

    let file_count: i64 = conn
        .query_row(
            "SELECT COUNT(DISTINCT source_file) FROM chunks",
            [],
            |row| row.get(0),
        )
        .unwrap_or(0);

    let project_count: i64 = conn
        .query_row("SELECT COUNT(*) FROM projects", [], |row| row.get(0))
        .unwrap_or(0);

    let embedding_count: i64 = conn
        .query_row("SELECT COUNT(*) FROM embeddings", [], |row| row.get(0))
        .unwrap_or(0);

    let fts_count: i64 = conn
        .query_row("SELECT COUNT(*) FROM chunks_fts", [], |row| row.get(0))
        .unwrap_or(0);

    let sglang_enabled = config.has_sglang();

    CommandResult::ok(
        "memory-status",
        json!({
            "projects": project_count,
            "files": file_count,
            "chunks": chunk_count,
            "embeddings": embedding_count,
            "fts_indexed": fts_count,
            "sglang_enabled": sglang_enabled,
            "md_dir": config.md_dir.display().to_string(),
            "db_path": config.db_path.display().to_string(),
        }),
    )
}

/// Return version info for the memory backend.
pub fn cmd_memory_version(_args: &Value, _config: &VegaConfig) -> CommandResult {
    CommandResult::ok(
        "memory-version",
        json!({
            "version": "2.0.0",
            "backend": "rust",
            "features": {
                "fts5": true,
                "vector_search": false,
                "reranking": false,
            },
        }),
    )
}

pub struct MemorySearchHandler;

impl super::CommandHandler for MemorySearchHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_memory_search(args, config)
    }
}

pub struct MemoryUpdateHandler;

impl super::CommandHandler for MemoryUpdateHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_memory_update(args, config)
    }
}

pub struct MemoryEmbedHandler;

impl super::CommandHandler for MemoryEmbedHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_memory_embed(args, config)
    }
}

pub struct MemoryStatusHandler;

impl super::CommandHandler for MemoryStatusHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_memory_status(args, config)
    }
}

pub struct MemoryVersionHandler;

impl super::CommandHandler for MemoryVersionHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_memory_version(args, config)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_sanitize_fts_query_simple() {
        let result = sanitize_fts_query("hello world");
        assert_eq!(result, r#""hello" "world""#);
    }

    #[test]
    fn test_sanitize_fts_query_single_token() {
        let result = sanitize_fts_query("rust");
        assert_eq!(result, r#""rust""#);
    }

    #[test]
    fn test_sanitize_fts_query_with_quotes() {
        let result = sanitize_fts_query(r#"hello "world""#);
        // Quotes are escaped by doubling, then tokens are quoted
        // Input: hello "world" → replace " with "": hello ""world"" → split: [hello, ""world""] → wrap: "hello" """world"""
        assert_eq!(result, r#""hello" """world""""#);
    }

    #[test]
    fn test_sanitize_fts_query_extra_whitespace() {
        let result = sanitize_fts_query("  hello   world  ");
        assert_eq!(result, r#""hello" "world""#);
    }

    #[test]
    fn test_sanitize_fts_query_empty() {
        let result = sanitize_fts_query("");
        assert_eq!(result, "");
    }

    #[test]
    fn test_parse_md_sections_basic() {
        let content = "# Title\nSome intro\n## Section A\nContent A\n## Section B\nContent B\n";
        let sections = parse_md_sections(content);
        assert_eq!(sections.len(), 3);
        assert_eq!(sections[0].0, "Title");
        assert!(sections[0].1.contains("Some intro"));
        assert_eq!(sections[1].0, "Section A");
        assert!(sections[1].1.contains("Content A"));
        assert_eq!(sections[2].0, "Section B");
        assert!(sections[2].1.contains("Content B"));
    }

    #[test]
    fn test_parse_md_sections_no_headings() {
        let content = "Just some text\nwith multiple lines\n";
        let sections = parse_md_sections(content);
        assert_eq!(sections.len(), 1);
        assert_eq!(sections[0].0, "_header");
        assert!(sections[0].1.contains("Just some text"));
    }

    #[test]
    fn test_parse_md_sections_empty() {
        let sections = parse_md_sections("");
        assert!(sections.is_empty());
    }

    #[test]
    fn test_parse_md_sections_consecutive_headings() {
        let content = "# First\n## Second\nContent\n";
        let sections = parse_md_sections(content);
        // First heading has no content, so only Second appears
        assert_eq!(sections.len(), 1);
        assert_eq!(sections[0].0, "Second");
    }

    #[test]
    fn test_compute_content_hash_deterministic() {
        let hash1 = compute_content_hash("hello world");
        let hash2 = compute_content_hash("hello world");
        assert_eq!(hash1, hash2);
    }

    #[test]
    fn test_compute_content_hash_different_inputs() {
        let hash1 = compute_content_hash("hello");
        let hash2 = compute_content_hash("world");
        assert_ne!(hash1, hash2);
    }

    #[test]
    fn test_compute_content_hash_format() {
        let hash = compute_content_hash("test");
        assert_eq!(hash.len(), 16); // 16 hex chars = 64-bit hash
        assert!(hash.chars().all(|c| c.is_ascii_hexdigit()));
    }
}
