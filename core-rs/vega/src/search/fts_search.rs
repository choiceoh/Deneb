//! SQLite FTS5 search engine for Vega.
//!
//! Port of Python vega/search/router.py — sqlite_search section.
//! Multi-stage search: strict FTS → broad FTS → trigram → LIKE fallback.

use std::collections::HashSet;

use regex::Regex;
use rusqlite::Connection;
use serde::{Deserialize, Serialize};
use std::sync::LazyLock;

use super::query_analyzer::{normalize_keyword, ExtractedFields};

/// Search result limits (matching Python constants).
const CHUNK_LIMIT: usize = 50;
const LIKE_LIMIT: usize = 30;
const COMM_LIMIT: usize = 15;

/// A chunk search result row.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChunkRow {
    pub chunk_id: i64,
    pub project_id: i64,
    pub name: String,
    pub client: String,
    pub status: String,
    pub person_internal: String,
    pub capacity: String,
    pub section_heading: String,
    pub content: String,
    pub chunk_type: String,
    pub entry_date: String,
}

/// A communication log search result row.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CommRow {
    pub id: i64,
    pub project_id: i64,
    pub name: String,
    pub log_date: String,
    pub sender: String,
    pub subject: String,
    pub summary: String,
}

/// Result of a SQLite search.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct SqliteSearchResult {
    pub chunks: Vec<ChunkRow>,
    pub comms: Vec<CommRow>,
    pub project_ids: Vec<i64>,
    pub project_names: Vec<String>,
    pub match_methods: Vec<String>,
}

// -- FTS5 safety --

static FTS_RESERVED: LazyLock<HashSet<&'static str>> =
    LazyLock::new(|| ["AND", "OR", "NOT", "NEAR"].into_iter().collect());

#[allow(clippy::expect_used)]
static SPECIAL_CHARS: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[&|!@#$%^*()\-+=\[\]{}<>?/\\~`]").expect("valid regex"));

#[allow(clippy::expect_used)]
static HAS_ALNUM: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[가-힣a-zA-Z0-9]").expect("valid regex"));

#[allow(clippy::expect_used)]
static KO_JOSA: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(은|는|이|가|을|를|의|에|에서|으로|로|와|과|만|까지|부터|에게|한테|께|보다|처럼|같이|에서도|까지도|만도|부터도|라고|이라고|이란)$").expect("valid regex")
});

#[allow(clippy::expect_used)]
static TOKEN_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[가-힣A-Za-z0-9&+/.\-]+").expect("valid regex"));

/// Sanitize a single FTS5 search term.
fn sanitize_fts_single(term: &str) -> Option<String> {
    let t = term.trim();
    if t.is_empty() {
        return None;
    }
    if FTS_RESERVED.contains(t.to_uppercase().as_str()) {
        return Some(format!("\"{}\"", t));
    }
    if t.contains(':') {
        return Some(format!("\"{}\"", t));
    }
    if SPECIAL_CHARS.is_match(t) {
        return Some(format!("\"{}\"", t));
    }
    if !HAS_ALNUM.is_match(t) {
        return None;
    }
    Some(t.to_string())
}

/// Check if a term is "strong" (English/digit or >=3 chars Korean).
fn is_strong_term(term: &str) -> bool {
    if term.is_empty() {
        return false;
    }
    let bare = term.trim_matches('"');
    #[allow(clippy::expect_used)]
    if Regex::new(r"[A-Za-z0-9]")
        .expect("valid regex")
        .is_match(bare)
    {
        return true;
    }
    bare.chars().count() >= 3
}

/// Build strict AND and broad OR FTS queries from terms.
fn build_fts_queries(terms: &[String]) -> (Option<String>, Option<String>) {
    let safe: Vec<String> = terms
        .iter()
        .filter_map(|t| sanitize_fts_single(t))
        .collect();
    if safe.is_empty() {
        return (None, None);
    }
    let strong: Vec<&String> = safe
        .iter()
        .filter(|t| is_strong_term(t.trim_matches('"')))
        .collect();
    let strict = if strong.len() >= 2 {
        Some(
            strong[..strong.len().min(4)]
                .iter()
                .map(|s| s.as_str())
                .collect::<Vec<_>>()
                .join(" AND "),
        )
    } else if !safe.is_empty() {
        Some(safe[0].clone())
    } else {
        None
    };
    let broad = Some(safe.join(" OR "));
    (strict, broad)
}

/// Korean preprocessing: strip particles, normalize.
fn preprocess_korean(query: &str) -> Vec<String> {
    let mut seen = HashSet::new();
    let mut result = Vec::new();
    for m in TOKEN_RE.find_iter(query) {
        let raw = m.as_str();
        let cleaned = KO_JOSA.replace(raw, "").to_string();
        let normalized = normalize_keyword(&cleaned);
        if !normalized.is_empty() && seen.insert(normalized.clone()) {
            result.push(normalized);
        }
    }
    result
}

/// Execute a full SQLite search (chunks + comms).
pub fn sqlite_search(
    conn: &Connection,
    query: &str,
    extracted: &ExtractedFields,
) -> SqliteSearchResult {
    let mut result = SqliteSearchResult::default();

    // Build filter conditions
    let mut conditions = Vec::new();
    let mut params: Vec<String> = Vec::new();

    // Client filter
    if !extracted.clients.is_empty() {
        let mut cl_conds = Vec::new();
        for cl in &extracted.clients {
            cl_conds.push("(p.client LIKE ? OR p.name LIKE ?)".to_string());
            params.push(format!("%{}%", cl));
            params.push(format!("%{}%", cl));
        }
        conditions.push(format!("({})", cl_conds.join(" OR ")));
    }

    // Person filter
    if !extracted.persons.is_empty() {
        let mut p_conds = Vec::new();
        for p in &extracted.persons {
            p_conds.push("(p.person_internal LIKE ? OR c.content LIKE ?)".to_string());
            params.push(format!("%{}%", p));
            params.push(format!("%{}%", p));
        }
        conditions.push(format!("({})", p_conds.join(" OR ")));
    }

    // Status filter (with synonym expansion)
    if !extracted.statuses.is_empty() {
        let mut s_conds = Vec::new();
        let synonyms: &[(&str, &[&str])] = &[
            ("급한", &["긴급"]),
            ("위급", &["긴급"]),
            ("긴급", &["급한"]),
        ];
        for s in &extracted.statuses {
            let mut all_terms = vec![s.clone()];
            for (key, syns) in synonyms {
                if s == key {
                    all_terms.extend(syns.iter().map(|s| s.to_string()));
                }
            }
            for syn in &all_terms {
                s_conds.push("p.status LIKE ?".to_string());
                params.push(format!("%{}%", syn));
            }
        }
        conditions.push(format!("({})", s_conds.join(" OR ")));
    }

    // FTS terms
    let fts_terms = &extracted.keywords;
    let (strict_fts, broad_fts) = if !fts_terms.is_empty() {
        build_fts_queries(fts_terms)
    } else if extracted.clients.is_empty()
        && extracted.persons.is_empty()
        && extracted.statuses.is_empty()
    {
        let s = sanitize_fts_single(query);
        (s.clone(), s)
    } else {
        (None, None)
    };

    // Run chunk query with FTS
    result.chunks = run_chunk_query(
        conn,
        query,
        &conditions,
        &params,
        extracted,
        strict_fts.as_deref(),
    );
    if !result.chunks.is_empty() {
        result.match_methods.push("fts5_strict".into());
    }

    // Broad FTS fallback if too few active results
    let active_count = result
        .chunks
        .iter()
        .filter(|r| !r.status.contains("완료") && !r.status.contains("취소"))
        .count();
    if active_count < 5 {
        if let Some(ref broad) = broad_fts {
            if strict_fts.as_deref() != Some(broad.as_str()) {
                let existing_ids: HashSet<i64> = result.chunks.iter().map(|r| r.chunk_id).collect();
                let before = result.chunks.len();
                let broad_results =
                    run_chunk_query(conn, query, &conditions, &params, extracted, Some(broad));
                for row in broad_results {
                    if !existing_ids.contains(&row.chunk_id) {
                        result.chunks.push(row);
                    }
                }
                if result.chunks.len() > before {
                    result.match_methods.push("fts5_broad".into());
                }
            }
        }
    }

    // Trigram FTS fallback
    if result.chunks.len() < 3 && !query.trim().is_empty() {
        let existing_ids: HashSet<i64> = result.chunks.iter().map(|r| r.chunk_id).collect();
        let before = result.chunks.len();
        if let Ok(tri_results) = run_trigram_query(conn, query) {
            for row in tri_results {
                if !existing_ids.contains(&row.chunk_id) {
                    result.chunks.push(row);
                }
            }
        }
        if result.chunks.len() > before {
            result.match_methods.push("trigram".into());
        }
    }

    // LIKE fallback
    if result.chunks.len() < 3 && !query.trim().is_empty() {
        let existing_ids: HashSet<i64> = result.chunks.iter().map(|r| r.chunk_id).collect();
        let before = result.chunks.len();
        if let Ok(like_results) = run_like_query(conn, query, &existing_ids) {
            for row in like_results {
                if !existing_ids.contains(&row.chunk_id) {
                    result.chunks.push(row);
                }
            }
        }
        if result.chunks.len() > before {
            result.match_methods.push("like_fallback".into());
        }
    }

    // Collect project IDs
    let mut pid_set = HashSet::new();
    let mut name_set = HashSet::new();
    for row in &result.chunks {
        if pid_set.insert(row.project_id) {
            result.project_ids.push(row.project_id);
        }
        if name_set.insert(row.name.clone()) {
            result.project_names.push(row.name.clone());
        }
    }

    // Comm log search
    result.comms = run_comm_query(conn, query, extracted, &result.project_ids);

    result
}

/// Run a chunk query with optional FTS.
fn run_chunk_query(
    conn: &Connection,
    query: &str,
    conditions: &[String],
    filter_params: &[String],
    extracted: &ExtractedFields,
    fts_query: Option<&str>,
) -> Vec<ChunkRow> {
    let mut sql = String::from(
        "SELECT DISTINCT
            c.id as chunk_id, p.id as project_id,
            p.name, p.client, p.status,
            p.person_internal, p.capacity,
            c.section_heading, c.content, c.chunk_type, c.entry_date
         FROM chunks c
         JOIN projects p ON c.project_id = p.id",
    );

    let mut all_conditions = conditions.to_vec();
    let mut all_params: Vec<String> = filter_params.to_vec();
    let mut fts_joined = false;

    if let Some(fts_q) = fts_query {
        sql.push_str(" JOIN chunks_fts fts ON fts.rowid = c.id");
        all_conditions.push("chunks_fts MATCH ?".into());
        all_params.push(fts_q.to_string());
        fts_joined = true;
    } else if !extracted.keywords.is_empty() {
        for term in &extracted.keywords {
            if !term.trim().is_empty() {
                all_conditions.push("c.content LIKE ?".into());
                all_params.push(format!("%{}%", term));
            }
        }
    } else if !query.trim().is_empty()
        && extracted.clients.is_empty()
        && extracted.persons.is_empty()
        && extracted.statuses.is_empty()
    {
        all_conditions.push("c.content LIKE ?".into());
        all_params.push(format!("%{}%", query));
    }

    // Tag filter
    if !extracted.tags.is_empty() {
        let mut tag_conds = Vec::new();
        for tag in &extracted.tags {
            tag_conds.push("(c.content LIKE ? OR p.name LIKE ?)".to_string());
            all_params.push(format!("%{}%", tag));
            all_params.push(format!("%{}%", tag));
        }
        all_conditions.push(format!("({})", tag_conds.join(" OR ")));
    }

    if !all_conditions.is_empty() {
        sql.push_str(" WHERE ");
        sql.push_str(&all_conditions.join(" AND "));
    }

    if fts_joined {
        sql.push_str(&format!(
            " ORDER BY
                CASE WHEN p.status LIKE '%완료%' OR p.status LIKE '%취소%' THEN 1 ELSE 0 END,
                bm25(chunks_fts, 5.0, 3.0, 2.0, 1.0),
                c.entry_date DESC NULLS LAST
             LIMIT {}",
            CHUNK_LIMIT
        ));
    } else {
        sql.push_str(&format!(
            " ORDER BY
                CASE WHEN p.status LIKE '%완료%' OR p.status LIKE '%취소%' THEN 1 ELSE 0 END,
                c.entry_date DESC NULLS LAST, p.id DESC
             LIMIT {}",
            CHUNK_LIMIT
        ));
    }

    execute_chunk_query(conn, &sql, &all_params).unwrap_or_default()
}

fn run_trigram_query(conn: &Connection, query: &str) -> Result<Vec<ChunkRow>, rusqlite::Error> {
    let sql = format!(
        "SELECT DISTINCT c.id as chunk_id, p.id as project_id,
            p.name, p.client, p.status, p.person_internal, p.capacity,
            c.section_heading, c.content, c.chunk_type, c.entry_date
         FROM chunks c JOIN projects p ON c.project_id = p.id
         JOIN chunks_fts_trigram tri ON tri.rowid = c.id
         WHERE chunks_fts_trigram MATCH ?
         LIMIT {}",
        CHUNK_LIMIT
    );
    let quoted = format!("\"{}\"", query);
    execute_chunk_query(conn, &sql, &[quoted])
}

fn run_like_query(
    conn: &Connection,
    query: &str,
    existing_ids: &HashSet<i64>,
) -> Result<Vec<ChunkRow>, rusqlite::Error> {
    let ko_terms = preprocess_korean(query);
    let mut all_terms: Vec<String> = vec![query.to_string()];
    for t in ko_terms {
        if !all_terms.contains(&t) {
            all_terms.push(t);
        }
    }

    let like_conds: Vec<String> = all_terms
        .iter()
        .map(|_| "c.content LIKE ?".to_string())
        .collect();
    let mut like_params: Vec<String> = all_terms.iter().map(|t| format!("%{}%", t)).collect();

    let mut sql = format!(
        "SELECT DISTINCT c.id as chunk_id, p.id as project_id,
            p.name, p.client, p.status, p.person_internal, p.capacity,
            c.section_heading, c.content, c.chunk_type, c.entry_date
         FROM chunks c JOIN projects p ON c.project_id = p.id
         WHERE ({})",
        like_conds.join(" OR ")
    );

    if !existing_ids.is_empty() {
        let ph: Vec<&str> = existing_ids.iter().map(|_| "?").collect();
        sql.push_str(&format!(" AND c.id NOT IN ({})", ph.join(",")));
        for id in existing_ids {
            like_params.push(id.to_string());
        }
    }
    sql.push_str(&format!(" LIMIT {}", LIKE_LIMIT));

    execute_chunk_query(conn, &sql, &like_params)
}

fn run_comm_query(
    conn: &Connection,
    query: &str,
    extracted: &ExtractedFields,
    project_ids: &[i64],
) -> Vec<CommRow> {
    let comm_terms: Vec<&String> = if !extracted.keywords.is_empty() {
        extracted.keywords.iter().collect()
    } else {
        vec![]
    };

    let comm_fts_query = if comm_terms.len() > 1 {
        let safe: Vec<String> = comm_terms
            .iter()
            .filter_map(|t| sanitize_fts_single(t))
            .collect();
        if safe.is_empty() {
            None
        } else {
            Some(safe.join(" OR "))
        }
    } else if !comm_terms.is_empty() {
        sanitize_fts_single(comm_terms[0])
    } else {
        sanitize_fts_single(query)
    };

    let fts_q = match comm_fts_query {
        Some(q) => q,
        None => return Vec::new(),
    };

    let mut sql = String::from(
        "SELECT cl.id, p.id as project_id, p.name,
                cl.log_date, cl.sender, cl.subject, cl.summary
         FROM comm_log cl
         JOIN projects p ON cl.project_id = p.id
         JOIN comm_fts cf ON cf.rowid = cl.id
         WHERE comm_fts MATCH ?",
    );
    let mut params: Vec<String> = vec![fts_q];

    if !project_ids.is_empty() {
        let ph: Vec<&str> = project_ids.iter().map(|_| "?").collect();
        sql.push_str(&format!(" AND cl.project_id IN ({})", ph.join(",")));
        for id in project_ids {
            params.push(id.to_string());
        }
    } else if !extracted.clients.is_empty() {
        let mut cl_conds = Vec::new();
        for cl in &extracted.clients {
            cl_conds.push("p.name LIKE ?".to_string());
            params.push(format!("%{}%", cl));
        }
        sql.push_str(&format!(" AND ({})", cl_conds.join(" OR ")));
    }

    sql.push_str(&format!(
        " ORDER BY cl.log_date DESC, bm25(comm_fts, 3.0, 2.0, 2.0, 1.0) LIMIT {}",
        COMM_LIMIT
    ));

    execute_comm_query(conn, &sql, &params).unwrap_or_default()
}

// -- Query execution helpers --

fn execute_chunk_query(
    conn: &Connection,
    sql: &str,
    params: &[String],
) -> Result<Vec<ChunkRow>, rusqlite::Error> {
    let mut stmt = conn.prepare(sql)?;
    let param_refs: Vec<&dyn rusqlite::types::ToSql> = params
        .iter()
        .map(|p| p as &dyn rusqlite::types::ToSql)
        .collect();
    let rows = stmt.query_map(param_refs.as_slice(), |row| {
        Ok(ChunkRow {
            chunk_id: row.get(0)?,
            project_id: row.get(1)?,
            name: row.get::<_, Option<String>>(2)?.unwrap_or_default(),
            client: row.get::<_, Option<String>>(3)?.unwrap_or_default(),
            status: row.get::<_, Option<String>>(4)?.unwrap_or_default(),
            person_internal: row.get::<_, Option<String>>(5)?.unwrap_or_default(),
            capacity: row.get::<_, Option<String>>(6)?.unwrap_or_default(),
            section_heading: row.get::<_, Option<String>>(7)?.unwrap_or_default(),
            content: row.get::<_, Option<String>>(8)?.unwrap_or_default(),
            chunk_type: row.get::<_, Option<String>>(9)?.unwrap_or_default(),
            entry_date: row.get::<_, Option<String>>(10)?.unwrap_or_default(),
        })
    })?;
    rows.collect()
}

fn execute_comm_query(
    conn: &Connection,
    sql: &str,
    params: &[String],
) -> Result<Vec<CommRow>, rusqlite::Error> {
    let mut stmt = conn.prepare(sql)?;
    let param_refs: Vec<&dyn rusqlite::types::ToSql> = params
        .iter()
        .map(|p| p as &dyn rusqlite::types::ToSql)
        .collect();
    let rows = stmt.query_map(param_refs.as_slice(), |row| {
        Ok(CommRow {
            id: row.get(0)?,
            project_id: row.get(1)?,
            name: row.get::<_, Option<String>>(2)?.unwrap_or_default(),
            log_date: row.get::<_, Option<String>>(3)?.unwrap_or_default(),
            sender: row.get::<_, Option<String>>(4)?.unwrap_or_default(),
            subject: row.get::<_, Option<String>>(5)?.unwrap_or_default(),
            summary: row.get::<_, Option<String>>(6)?.unwrap_or_default(),
        })
    })?;
    rows.collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db::schema::init_db;

    fn setup_test_db() -> Result<Connection, Box<dyn std::error::Error>> {
        let conn = Connection::open_in_memory()?;
        init_db(&conn)?;

        conn.execute(
            "INSERT INTO projects (name, client, status, person_internal)
             VALUES ('비금도 태양광', '한국전력', '진행중', '김대희')",
            [],
        )?;
        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
             VALUES (1, '현재 상황', '해저케이블 154kV 설치 진행중', 'status')",
            [],
        )?;
        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
             VALUES (1, '기술 사양', 'EPC 시공 방식으로 진행', 'technical')",
            [],
        )?;
        conn.execute(
            "INSERT INTO comm_log (project_id, log_date, sender, subject, summary)
             VALUES (1, '2025-03-01', '김대희', '미팅 결과', '설계 검토 완료')",
            [],
        )?;

        Ok(conn)
    }

    #[test]
    fn test_fts_search() -> Result<(), Box<dyn std::error::Error>> {
        let conn = setup_test_db()?;
        let extracted = ExtractedFields {
            keywords: vec!["해저케이블".into()],
            ..Default::default()
        };
        let result = sqlite_search(&conn, "해저케이블", &extracted);
        assert!(
            !result.chunks.is_empty(),
            "Should find chunks via FTS or LIKE"
        );
        Ok(())
    }

    #[test]
    fn test_client_filter() -> Result<(), Box<dyn std::error::Error>> {
        let conn = setup_test_db()?;
        let extracted = ExtractedFields {
            clients: vec!["한국전력".into()],
            ..Default::default()
        };
        let result = sqlite_search(&conn, "한국전력", &extracted);
        assert!(!result.chunks.is_empty());
        assert!(result.chunks.iter().all(|r| r.client.contains("한국전력")));
        Ok(())
    }

    #[test]
    fn test_sanitize_fts() {
        assert_eq!(sanitize_fts_single("AND"), Some("\"AND\"".into()));
        assert_eq!(sanitize_fts_single("O&M"), Some("\"O&M\"".into()));
        assert_eq!(
            sanitize_fts_single("test:value"),
            Some("\"test:value\"".into())
        );
        assert_eq!(sanitize_fts_single(""), None);
        assert_eq!(sanitize_fts_single("비금도"), Some("비금도".into()));
    }

    #[test]
    fn test_sanitize_fts_reserved_or() {
        // "OR" is in FTS_RESERVED and must be quoted to avoid being treated as operator.
        assert_eq!(sanitize_fts_single("OR"), Some("\"OR\"".into()));
        // Case-insensitive: lowercase "or" matches uppercase reserved set.
        assert_eq!(sanitize_fts_single("or"), Some("\"or\"".into()));
    }

    #[test]
    fn test_sanitize_fts_reserved_not() {
        assert_eq!(sanitize_fts_single("NOT"), Some("\"NOT\"".into()));
        assert_eq!(sanitize_fts_single("NEAR"), Some("\"NEAR\"".into()));
    }

    #[test]
    fn test_sanitize_fts_no_alnum_returns_none() {
        // Whitespace-only trims to empty → None.
        assert_eq!(sanitize_fts_single("   "), None);
        // Special chars (e.g. dashes) are first matched by SPECIAL_CHARS → quoted, not None.
        assert_eq!(sanitize_fts_single("---"), Some("\"---\"".into()));
    }

    #[test]
    fn test_person_filter() -> Result<(), Box<dyn std::error::Error>> {
        let conn = setup_test_db()?;
        let extracted = ExtractedFields {
            persons: vec!["김대희".into()],
            ..Default::default()
        };
        let result = sqlite_search(&conn, "김대희", &extracted);
        // The test DB inserts a chunk with person_internal="김대희" and a comm from "김대희".
        assert!(
            !result.chunks.is_empty() || !result.comms.is_empty(),
            "expected at least one result for person filter"
        );
        Ok(())
    }
}
