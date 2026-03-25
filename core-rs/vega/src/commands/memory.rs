#[cfg(feature = "ml")]
use std::collections::HashMap;
use rusqlite::{params, Connection};
use serde_json::{json, Value};
use std::fs;

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

/// FTS5 full-text search across chunks.
/// Args: query (string), limit (int, default 20), project (optional string)
pub fn cmd_memory_search(args: &Value, config: &VegaConfig) -> CommandResult {
    let query = match args.get("query").and_then(|v| v.as_str()) {
        Some(q) => q,
        None => return CommandResult::err("memory-search", "query 파라미터가 필요합니다"),
    };
    let limit = args.get("limit").and_then(|v| v.as_i64()).unwrap_or(20);
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

    // Optional vector search with reranking
    #[cfg(feature = "ml")]
    {
        if config.has_ml() {
            if let Ok(vec_results) = vector_search(&conn, query, limit, config) {
                fts_results = rerank_results(fts_results, vec_results);
            }
        }
    }

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
        "SELECT c.id, c.project_id, c.section, c.content, c.source_file,
                p.name as project_name,
                rank
         FROM chunks_fts
         JOIN chunks c ON chunks_fts.rowid = c.id
         LEFT JOIN projects p ON c.project_id = p.id
         WHERE chunks_fts MATCH ?1 AND p.name = ?3
         ORDER BY rank
         LIMIT ?2"
    } else {
        "SELECT c.id, c.project_id, c.section, c.content, c.source_file,
                p.name as project_name,
                rank
         FROM chunks_fts
         JOIN chunks c ON chunks_fts.rowid = c.id
         LEFT JOIN projects p ON c.project_id = p.id
         WHERE chunks_fts MATCH ?1
         ORDER BY rank
         LIMIT ?2"
    };

    let mut stmt = conn.prepare(sql).map_err(|e| format!("FTS 쿼리 준비 실패: {e}"))?;

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

/// Vector search using embeddings (ML feature only).
#[cfg(feature = "ml")]
fn vector_search(
    conn: &Connection,
    query: &str,
    limit: i64,
    config: &VegaConfig,
) -> Result<Vec<Value>, String> {
    use std::process::Command;

    let embedder = match &config.model_embedder {
        Some(p) => p,
        None => return Err("임베더 모델 경로가 설정되지 않았습니다".into()),
    };

    // Generate query embedding via external embedder process
    let output = Command::new(embedder)
        .arg("--query")
        .arg(query)
        .output()
        .map_err(|e| format!("임베더 실행 실패: {e}"))?;

    if !output.status.success() {
        return Err(format!(
            "임베더 오류: {}",
            String::from_utf8_lossy(&output.stderr)
        ));
    }

    let query_embedding: Vec<f32> = serde_json::from_slice(&output.stdout)
        .map_err(|e| format!("임베딩 파싱 실패: {e}"))?;

    // Load chunk embeddings from DB and compute cosine similarity
    let mut stmt = conn
        .prepare(
            "SELECT c.id, c.project_id, c.section, c.content, c.source_file,
                    p.name as project_name, e.embedding
             FROM embeddings e
             JOIN chunks c ON e.chunk_id = c.id
             LEFT JOIN projects p ON c.project_id = p.id",
        )
        .map_err(|e| format!("벡터 검색 쿼리 실패: {e}"))?;

    let mut scored: Vec<(f64, Value)> = stmt
        .query_map([], |row| {
            let embedding_blob: Vec<u8> = row.get(6)?;
            Ok((
                json!({
                    "id": row.get::<_, i64>(0)?,
                    "project_id": row.get::<_, Option<i64>>(1)?,
                    "section": row.get::<_, Option<String>>(2)?,
                    "content": row.get::<_, String>(3)?,
                    "source_file": row.get::<_, Option<String>>(4)?,
                    "project_name": row.get::<_, Option<String>>(5)?,
                    "source": "vector",
                }),
                embedding_blob,
            ))
        })
        .map_err(|e| format!("벡터 검색 실행 실패: {e}"))?
        .filter_map(|r| r.ok())
        .map(|(mut val, blob)| {
            let embedding = bytes_to_f32_vec(&blob);
            let score = cosine_similarity(&query_embedding, &embedding);
            val.as_object_mut().unwrap().insert("score".into(), json!(score));
            (score, val)
        })
        .collect();

    // Sort by score descending
    scored.sort_by(|a, b| b.0.partial_cmp(&a.0).unwrap_or(std::cmp::Ordering::Equal));
    scored.truncate(limit as usize);

    Ok(scored.into_iter().map(|(_, v)| v).collect())
}

#[cfg(feature = "ml")]
fn bytes_to_f32_vec(bytes: &[u8]) -> Vec<f32> {
    bytes
        .chunks_exact(4)
        .map(|chunk| f32::from_le_bytes([chunk[0], chunk[1], chunk[2], chunk[3]]))
        .collect()
}

#[cfg(feature = "ml")]
fn cosine_similarity(a: &[f32], b: &[f32]) -> f64 {
    if a.len() != b.len() || a.is_empty() {
        return 0.0;
    }
    let mut dot = 0.0_f64;
    let mut norm_a = 0.0_f64;
    let mut norm_b = 0.0_f64;
    for (x, y) in a.iter().zip(b.iter()) {
        let x = *x as f64;
        let y = *y as f64;
        dot += x * y;
        norm_a += x * x;
        norm_b += y * y;
    }
    let denom = norm_a.sqrt() * norm_b.sqrt();
    if denom == 0.0 {
        0.0
    } else {
        dot / denom
    }
}

/// Rerank FTS and vector results using reciprocal rank fusion.
#[cfg(feature = "ml")]
fn rerank_results(fts: Vec<Value>, vector: Vec<Value>) -> Vec<Value> {
    let k = 60.0_f64; // RRF constant

    let mut scores: HashMap<i64, (f64, Value)> = HashMap::new();

    for (rank, item) in fts.iter().enumerate() {
        let id = item.get("id").and_then(|v| v.as_i64()).unwrap_or(-1);
        let rrf = 1.0 / (k + rank as f64 + 1.0);
        scores
            .entry(id)
            .and_modify(|(s, _)| *s += rrf)
            .or_insert((rrf, item.clone()));
    }

    for (rank, item) in vector.iter().enumerate() {
        let id = item.get("id").and_then(|v| v.as_i64()).unwrap_or(-1);
        let rrf = 1.0 / (k + rank as f64 + 1.0);
        scores
            .entry(id)
            .and_modify(|(s, _)| *s += rrf)
            .or_insert((rrf, item.clone()));
    }

    let mut merged: Vec<(f64, Value)> = scores.into_values().collect();
    merged.sort_by(|a, b| b.0.partial_cmp(&a.0).unwrap_or(std::cmp::Ordering::Equal));

    merged
        .into_iter()
        .map(|(score, mut v): (f64, Value)| {
            v.as_object_mut()
                .unwrap()
                .insert("rrf_score".into(), json!(score));
            v
        })
        .collect()
}

/// Index .md files into DB with content hash tracking.
/// Scans md_dir, computes SHA-256 hash per file, upserts chunks.
pub fn cmd_memory_update(args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("memory-update", &e),
    };

    let force = args.get("force").and_then(|v| v.as_bool()).unwrap_or(false);
    let md_dir = &config.md_dir;

    if !md_dir.exists() {
        return CommandResult::err(
            "memory-update",
            &format!("마크다운 디렉토리가 존재하지 않습니다: {}", md_dir.display()),
        );
    }

    let mut stats = json!({
        "files_scanned": 0,
        "files_updated": 0,
        "files_skipped": 0,
        "chunks_created": 0,
        "chunks_deleted": 0,
        "errors": [],
    });

    let entries: Vec<_> = match fs::read_dir(md_dir) {
        Ok(rd) => rd.filter_map(|e| e.ok()).collect(),
        Err(e) => {
            return CommandResult::err(
                "memory-update",
                &format!("디렉토리 읽기 실패: {e}"),
            )
        }
    };

    for entry in entries {
        let path = entry.path();
        if path.extension().and_then(|e| e.to_str()) != Some("md") {
            continue;
        }

        *stats.get_mut("files_scanned").unwrap() =
            json!(stats["files_scanned"].as_i64().unwrap() + 1);

        let content = match fs::read_to_string(&path) {
            Ok(c) => c,
            Err(e) => {
                let errors = stats["errors"].as_array_mut().unwrap();
                errors.push(json!(format!("{}: {e}", path.display())));
                continue;
            }
        };

        let hash = compute_content_hash(&content);
        let filename = path
            .file_name()
            .and_then(|n| n.to_str())
            .unwrap_or("");

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
                *stats.get_mut("files_skipped").unwrap() =
                    json!(stats["files_skipped"].as_i64().unwrap() + 1);
                continue;
            }
        }

        // Find or create project
        let project_name = path
            .file_stem()
            .and_then(|n| n.to_str())
            .unwrap_or("");

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

        *stats.get_mut("chunks_deleted").unwrap() =
            json!(stats["chunks_deleted"].as_i64().unwrap() + deleted);

        // Parse sections and insert chunks
        let chunks = parse_md_sections(&content);
        let mut created = 0i64;
        for (section, chunk_content) in &chunks {
            if chunk_content.trim().is_empty() {
                continue;
            }
            let _ = conn.execute(
                "INSERT INTO chunks (project_id, section, content, source_file)
                 VALUES (?1, ?2, ?3, ?4)",
                params![project_id, section, chunk_content, filename],
            );
            created += 1;
        }

        *stats.get_mut("chunks_created").unwrap() =
            json!(stats["chunks_created"].as_i64().unwrap() + created);

        // Update file hash
        conn.execute(
            "INSERT OR REPLACE INTO file_hashes (filename, content_hash) VALUES (?1, ?2)",
            params![filename, hash],
        )
        .ok();

        *stats.get_mut("files_updated").unwrap() =
            json!(stats["files_updated"].as_i64().unwrap() + 1);
    }

    CommandResult::ok("memory-update", stats)
}

/// Parse markdown into (section_name, content) pairs.
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
            current_section = line
                .trim_start_matches('#')
                .trim()
                .to_string();
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

/// Generate embeddings for chunks that don't have them yet.
/// Delegates to ML embedder model.
#[cfg(feature = "ml")]
pub fn cmd_memory_embed(args: &Value, config: &VegaConfig) -> CommandResult {
    use std::process::Command;

    if !config.has_ml() {
        return CommandResult::err("memory-embed", "ML 기능이 비활성화되어 있습니다");
    }

    let embedder = match &config.model_embedder {
        Some(p) => p,
        None => {
            return CommandResult::err("memory-embed", "임베더 모델 경로가 설정되지 않았습니다")
        }
    };

    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("memory-embed", &e),
    };

    let batch_size = args
        .get("batch_size")
        .and_then(|v| v.as_i64())
        .unwrap_or(32) as usize;

    // Find chunks without embeddings
    let mut stmt = conn
        .prepare(
            "SELECT c.id, c.content FROM chunks c
             LEFT JOIN embeddings e ON c.id = e.chunk_id
             WHERE e.chunk_id IS NULL",
        )
        .map_err(|e| format!("쿼리 실패: {e}"))
        .unwrap();

    let pending: Vec<(i64, String)> = stmt
        .query_map([], |row| Ok((row.get(0)?, row.get(1)?)))
        .map_err(|e| format!("쿼리 실행 실패: {e}"))
        .unwrap()
        .filter_map(|r| r.ok())
        .collect();

    if pending.is_empty() {
        return CommandResult::ok(
            "memory-embed",
            json!({
                "embedded": 0,
                "message": "임베딩할 청크가 없습니다",
            }),
        );
    }

    let total = pending.len();
    let mut embedded = 0usize;
    let mut errors: Vec<String> = Vec::new();

    for batch in pending.chunks(batch_size) {
        let texts: Vec<&str> = batch.iter().map(|(_, c)| c.as_str()).collect();
        let input = serde_json::to_string(&texts).unwrap();

        let output = Command::new(embedder)
            .arg("--batch")
            .stdin(std::process::Stdio::piped())
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::piped())
            .spawn()
            .and_then(|mut child| {
                use std::io::Write;
                if let Some(ref mut stdin) = child.stdin {
                    stdin.write_all(input.as_bytes())?;
                }
                child.wait_with_output()
            });

        match output {
            Ok(out) if out.status.success() => {
                let embeddings: Vec<Vec<f32>> = match serde_json::from_slice(&out.stdout) {
                    Ok(e) => e,
                    Err(e) => {
                        errors.push(format!("임베딩 파싱 실패: {e}"));
                        continue;
                    }
                };

                for ((chunk_id, _), embedding) in batch.iter().zip(embeddings.iter()) {
                    let blob = f32_vec_to_bytes(embedding);
                    if conn
                        .execute(
                            "INSERT OR REPLACE INTO embeddings (chunk_id, embedding) VALUES (?1, ?2)",
                            params![chunk_id, blob],
                        )
                        .is_ok()
                    {
                        embedded += 1;
                    }
                }
            }
            Ok(out) => {
                errors.push(format!(
                    "임베더 오류: {}",
                    String::from_utf8_lossy(&out.stderr)
                ));
            }
            Err(e) => {
                errors.push(format!("임베더 실행 실패: {e}"));
            }
        }
    }

    CommandResult::ok(
        "memory-embed",
        json!({
            "total_pending": total,
            "embedded": embedded,
            "errors": errors,
        }),
    )
}

#[cfg(not(feature = "ml"))]
pub fn cmd_memory_embed(_args: &Value, _config: &VegaConfig) -> CommandResult {
    CommandResult::err("memory-embed", "ML 기능이 비활성화되어 있습니다. 'ml' 피처를 활성화하세요.")
}

#[cfg(feature = "ml")]
fn f32_vec_to_bytes(vec: &[f32]) -> Vec<u8> {
    vec.iter().flat_map(|f| f.to_le_bytes()).collect()
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

    let has_ml = config.has_ml();

    CommandResult::ok(
        "memory-status",
        json!({
            "projects": project_count,
            "files": file_count,
            "chunks": chunk_count,
            "embeddings": embedding_count,
            "fts_indexed": fts_count,
            "ml_enabled": has_ml,
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
                "vector_search": cfg!(feature = "ml"),
                "reranking": cfg!(feature = "ml"),
            },
        }),
    )
}
