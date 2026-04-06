//! Utility functions for Vega — fuzzy matching, SQL safety, Korean NL extraction.
//!
//! Port of Python vega/core.py utility functions:
//! `escape_like`, `fuzzy_find_project`, `find_project_id_in_text`,
//! `extract_days`, `extract_limit`, `extract_bullets`, `build_search_suggestions`.

use regex::Regex;
use rusqlite::Connection;
use serde::{Deserialize, Serialize};
use std::sync::LazyLock;

// Pre-compiled regex patterns for hot-path functions.
#[allow(clippy::expect_used)]
static KOREAN_TOKEN_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[가-힣A-Za-z0-9]+").expect("valid regex"));
#[allow(clippy::expect_used)]
static DAYS_FLAG_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"--days\s+(\d+)").expect("valid regex"));
#[allow(clippy::expect_used)]
static DAYS_IL_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d+)\s*일").expect("valid regex"));
#[allow(clippy::expect_used)]
static DAYS_JU_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d+)\s*주").expect("valid regex"));
#[allow(clippy::expect_used)]
static DAYS_GAEWOL_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d+)\s*개월").expect("valid regex"));
#[allow(clippy::expect_used)]
static LIMIT_FLAG_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"--limit\s+(\d+)").expect("valid regex"));
#[allow(clippy::expect_used)]
static LIMIT_GAE_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"(\d+)\s*개").expect("valid regex"));
#[allow(clippy::expect_used)]
static BULLET_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[\-•*]+\s*").expect("valid regex"));
#[allow(clippy::expect_used)]
static NUMBERED_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^\d+[.)]\s*").expect("valid regex"));

/// Escape special SQL LIKE characters (%, _, \).
/// Use with `ESCAPE '\'` in the LIKE clause.
pub fn escape_like(input: &str) -> String {
    let mut out = String::with_capacity(input.len());
    for ch in input.chars() {
        match ch {
            '\\' => out.push_str("\\\\"),
            '%' => out.push_str("\\%"),
            '_' => out.push_str("\\_"),
            _ => out.push(ch),
        }
    }
    out
}

/// Character bigram similarity (Dice coefficient), returns 0.0..1.0.
/// Approximates Python `difflib.SequenceMatcher.ratio()`.
pub fn sequence_similarity(a: &str, b: &str) -> f64 {
    let a_chars: Vec<char> = a.chars().collect();
    let b_chars: Vec<char> = b.chars().collect();

    if a_chars.is_empty() && b_chars.is_empty() {
        return 1.0;
    }
    if a_chars.is_empty() || b_chars.is_empty() {
        return 0.0;
    }
    // Single-char strings: equality check
    if a_chars.len() == 1 && b_chars.len() == 1 {
        return if a_chars[0] == b_chars[0] { 1.0 } else { 0.0 };
    }
    if a_chars.len() == 1 || b_chars.len() == 1 {
        // Check containment for single char vs multi
        let (single, multi) = if a_chars.len() == 1 {
            (&a_chars, &b_chars)
        } else {
            (&b_chars, &a_chars)
        };
        return if multi.contains(&single[0]) { 0.3 } else { 0.0 };
    }

    // Build bigram sets
    let a_bigrams: Vec<(char, char)> = a_chars.windows(2).map(|w| (w[0], w[1])).collect();
    let b_bigrams: Vec<(char, char)> = b_chars.windows(2).map(|w| (w[0], w[1])).collect();

    let mut matches = 0;
    let mut b_used = vec![false; b_bigrams.len()];
    for ab in &a_bigrams {
        for (i, bb) in b_bigrams.iter().enumerate() {
            if !b_used[i] && ab == bb {
                matches += 1;
                b_used[i] = true;
                break;
            }
        }
    }

    (2.0 * matches as f64) / (a_bigrams.len() + b_bigrams.len()) as f64
}

/// Token-level fuzzy match: normalize, split into tokens, find best
/// token-pair similarity, return max score with partial inclusion bonus.
pub fn fuzzy_match_score(query: &str, candidate: &str) -> f64 {
    let q_norm = normalize_for_match(query);
    let c_norm = normalize_for_match(candidate);

    if q_norm.is_empty() || c_norm.is_empty() {
        return 0.0;
    }

    // Partial inclusion bonus
    if q_norm.contains(&c_norm) || c_norm.contains(&q_norm) {
        return 0.85_f64.max(sequence_similarity(&q_norm, &c_norm));
    }

    // Whole-string similarity
    let mut best = sequence_similarity(&q_norm, &c_norm);

    // Token-level matching
    let q_tokens = tokenize_korean(&q_norm);
    let c_tokens = tokenize_korean(&c_norm);

    // Query tokens vs candidate name
    for qt in &q_tokens {
        if qt.chars().count() < 2 {
            continue;
        }
        let sim = sequence_similarity(qt, &c_norm);
        if sim > best {
            best = sim;
        }
        // vs candidate tokens
        for ct in &c_tokens {
            let sim = sequence_similarity(qt, ct);
            if sim > best {
                best = sim;
            }
        }
    }

    // Candidate tokens vs query
    for ct in &c_tokens {
        if ct.chars().count() < 2 {
            continue;
        }
        let sim = sequence_similarity(ct, &q_norm);
        if sim > best {
            best = sim;
        }
    }

    best
}

/// Normalize text for fuzzy matching: lowercase, remove whitespace.
fn normalize_for_match(s: &str) -> String {
    s.to_lowercase()
        .chars()
        .filter(|c| !c.is_whitespace())
        .collect()
}

/// Tokenize text into Korean/alphanumeric tokens (2+ chars).
fn tokenize_korean(s: &str) -> Vec<String> {
    KOREAN_TOKEN_RE
        .find_iter(s)
        .map(|m| m.as_str().to_string())
        .filter(|t| t.chars().count() >= 2)
        .collect()
}

/// Extract day count from Korean NL text or --days flag.
/// "3일" → 3, "2주" → 14, "이번 주" → 7, "이번 달" → 30,
/// "--days 7" → 7. Returns None if no pattern found.
pub fn extract_days(text: &str, default: i64) -> i64 {
    // --days flag
    if let Some(caps) = DAYS_FLAG_RE.captures(text) {
        if let Ok(d) = caps[1].parse::<i64>() {
            return d.clamp(1, 90);
        }
    }

    // Korean patterns: N일, N주, N개월
    let patterns: &[(&LazyLock<Regex>, i64)] =
        &[(&DAYS_IL_RE, 1), (&DAYS_JU_RE, 7), (&DAYS_GAEWOL_RE, 30)];
    for (re, mult) in patterns {
        if let Some(caps) = re.captures(text) {
            if let Ok(n) = caps[1].parse::<i64>() {
                return (n * mult).clamp(1, 90);
            }
        }
    }

    // Fixed phrases
    if text.contains("이번 주") || text.contains("이번주") {
        return 7;
    }
    if text.contains("이번 달") || text.contains("이번달") || text.contains("금월") {
        return 30;
    }

    default
}

/// Extract result limit from text. "--limit 5" → 5, "3개" → 3.
pub fn extract_limit(text: &str, default: usize) -> usize {
    if let Some(caps) = LIMIT_FLAG_RE.captures(text) {
        if let Ok(n) = caps[1].parse::<usize>() {
            return n.clamp(1, 100);
        }
    }
    if let Some(caps) = LIMIT_GAE_RE.captures(text) {
        if let Ok(n) = caps[1].parse::<usize>() {
            return n.clamp(1, 100);
        }
    }
    default
}

/// Extract bullet points from markdown content.
/// Matches lines starting with "- ", "* ", numbered "1. ", or nested "  - ".
/// Deduplicates and limits to `limit` items.
pub fn extract_bullets(content: &str, limit: usize) -> Vec<String> {
    let mut items = Vec::new();

    for raw in content.lines() {
        let line = raw.trim();
        if line.is_empty() {
            continue;
        }
        let mut cleaned = BULLET_RE.replace(line, "").to_string();
        cleaned = NUMBERED_RE.replace(&cleaned, "").to_string();
        // Collapse whitespace
        cleaned = cleaned.split_whitespace().collect::<Vec<_>>().join(" ");
        if cleaned.chars().count() < 3 {
            continue;
        }
        // Truncate to 220 chars
        let truncated: String = cleaned.chars().take(220).collect();
        if !items.contains(&truncated) {
            items.push(truncated);
        }
        if items.len() >= limit {
            break;
        }
    }

    // Fallback: if no bullets found, use first line of content
    if items.is_empty() {
        let compact: String = content
            .split_whitespace()
            .collect::<Vec<_>>()
            .join(" ")
            .chars()
            .take(220)
            .collect();
        if !compact.is_empty() {
            items.push(compact);
        }
    }

    items
}

/// Find a project ID from natural language text by scoring tokens
/// against DB project names using fuzzy matching.
/// Returns (`project_id`, `matched_name`, score).
pub fn find_project_id_in_text(
    conn: &Connection,
    text: &str,
    threshold: f64,
) -> Option<(i64, String, f64)> {
    if text.is_empty() {
        return None;
    }

    let mut stmt = conn.prepare("SELECT id, name FROM projects").ok()?;
    let rows: Vec<(i64, String)> = stmt
        .query_map([], |r| {
            Ok((
                r.get::<_, i64>(0)?,
                r.get::<_, String>(1).unwrap_or_default(),
            ))
        })
        .ok()?
        .filter_map(std::result::Result::ok)
        .filter(|(_, name)| !name.is_empty())
        .collect();

    let mut best: Option<(i64, String, f64)> = None;

    for (id, name) in &rows {
        let score = fuzzy_match_score(text, name);
        if score >= threshold && best.as_ref().is_none_or(|b| score > b.2) {
            best = Some((*id, name.clone(), score));
        }
    }

    best
}

/// Search suggestion for zero-result queries.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Suggestion {
    pub text: String,
    pub kind: String, // "project", "client", "person"
    pub score: f64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub project_id: Option<i64>,
}

/// Build search suggestions (project/client/person candidates)
/// when search returns 0 results. Uses fuzzy matching against DB.
pub fn build_search_suggestions(conn: &Connection, query: &str, limit: usize) -> Vec<Suggestion> {
    if query.is_empty() {
        return Vec::new();
    }

    let mut suggestions = Vec::new();
    let mut seen_projects = std::collections::HashSet::new();
    let mut seen_clients = std::collections::HashSet::new();
    let mut seen_persons = std::collections::HashSet::new();

    let Ok(mut stmt) =
        conn.prepare("SELECT id, name, client, person_internal, person_external FROM projects")
    else {
        return suggestions;
    };

    let rows: Vec<(i64, String, String, String, String)> = match stmt.query_map([], |r| {
        Ok((
            r.get::<_, i64>(0)?,
            r.get::<_, Option<String>>(1)?.unwrap_or_default(),
            r.get::<_, Option<String>>(2)?.unwrap_or_default(),
            r.get::<_, Option<String>>(3)?.unwrap_or_default(),
            r.get::<_, Option<String>>(4)?.unwrap_or_default(),
        ))
    }) {
        Ok(iter) => iter.filter_map(std::result::Result::ok).collect(),
        Err(_) => return suggestions,
    };

    for row in &rows {
        let (id, name, client, person_int, person_ext) = row;
        let id = *id;

        // Project name match
        if !name.is_empty() && !seen_projects.contains(&id) {
            let score = fuzzy_match_score(query, name);
            if score > 0.3 {
                seen_projects.insert(id);
                suggestions.push(Suggestion {
                    text: name.clone(),
                    kind: "project".into(),
                    score,
                    project_id: Some(id),
                });
            }
        }

        // Client match
        let client_trimmed = client.trim().to_string();
        if !client_trimmed.is_empty() && !seen_clients.contains(&client_trimmed) {
            let score = fuzzy_match_score(query, &client_trimmed);
            if score > 0.3 {
                seen_clients.insert(client_trimmed.clone());
                suggestions.push(Suggestion {
                    text: client_trimmed,
                    kind: "client".into(),
                    score,
                    project_id: None,
                });
            }
        }

        // Person matches
        for person_field in [person_int.as_str(), person_ext.as_str()] {
            for person in person_field.split(&['/', ',', '·'][..]) {
                let person = person.trim().to_string();
                if person.is_empty() || seen_persons.contains(&person) {
                    continue;
                }
                let score = fuzzy_match_score(query, &person);
                if score > 0.3 {
                    seen_persons.insert(person.clone());
                    suggestions.push(Suggestion {
                        text: person,
                        kind: "person".into(),
                        score,
                        project_id: None,
                    });
                }
            }
        }
    }

    suggestions.sort_by(|a, b| {
        b.score
            .partial_cmp(&a.score)
            .unwrap_or(std::cmp::Ordering::Equal)
    });
    suggestions.truncate(limit);
    suggestions
}
