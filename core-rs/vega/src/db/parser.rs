//! Markdown parsing: table metadata extraction, section splitting, comm log parsing.
//!
//! Port of Python vega/db/parser.py.

use std::collections::HashMap;

use regex::Regex;

use once_cell::sync::Lazy;

/// Parsed communication log entry.
#[derive(Debug, Clone)]
pub struct CommEntry {
    pub date: String,
    pub sender: String,
    pub subject: String,
    pub summary: String,
}

/// A parsed section from a markdown file.
#[derive(Debug, Clone)]
pub struct Section {
    pub heading: String,
    pub body: String,
    /// Date extracted from date-heading sections (YYYY-MM-DD).
    pub entry_date: Option<String>,
}

// -- Regex patterns (compiled once) --

#[allow(clippy::expect_used)]
static HEADING_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"(?m)^(#{1,3})\s+(.+)").expect("valid regex"));

#[allow(clippy::expect_used)]
static TABLE_ROW_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(?m)^\|\s*\*?\*?(.+?)\*?\*?\s*\|\s*(.+?)\s*\|").expect("valid regex"));

#[allow(clippy::expect_used)]
static STATUS_EMOJI_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"[🟢🟡🟠🔴⚪]").expect("valid regex"));

#[allow(clippy::expect_used)]
static DATE_HEADING_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"^(20\d{2}[-/]\d{2}[-/]\d{2})").expect("valid regex"));

#[allow(clippy::expect_used)]
static TABLE_LINE_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"(?m)^\|.+\|$").expect("valid regex"));

// Comm entry patterns (bolded/unbolded, with/without sender)
#[allow(clippy::expect_used)]
static COMM_PAT_BOLD_SENDER: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"^[-*]\s*\*{1,2}(.+?)\*{1,2}\s*\(([^)]+)\)\s*$").expect("valid regex"));
#[allow(clippy::expect_used)]
static COMM_PAT_PLAIN_SENDER: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"^[-*]\s+(.+?)\s*\(([^)]+)\)\s*$").expect("valid regex"));
#[allow(clippy::expect_used)]
static COMM_PAT_BOLD_ONLY: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"^[-*]\s*\*{1,2}(.+?)\*{1,2}\s*$").expect("valid regex"));

/// Field mapping from Korean table header to metadata key.
fn field_map() -> HashMap<&'static str, &'static str> {
    let pairs = [
        ("상태", "status"),
        ("발주처", "client"),
        ("고객사", "client"),
        ("사내 담당", "person_internal"),
        ("사내담당", "person_internal"),
        ("거래처 담당", "person_external"),
        ("거래처담당", "person_external"),
        ("규모", "capacity"),
        ("용량", "capacity"),
        ("품목", "biz_type"),
        ("사업구조", "biz_type"),
        ("파트너", "partner"),
        ("주요 인물", "person_external"),
        ("현대엔지니어링", "person_external"),
        ("해저케이블", "_해저케이블"),
        ("CU 헷징", "_CU헷징"),
        ("모듈", "_모듈"),
        ("금융", "_금융"),
        ("신규 소유자", "client"),
    ];
    pairs.into_iter().collect()
}

/// Extract metadata from markdown table (| key | value | format).
pub fn extract_table_meta(text: &str) -> HashMap<String, String> {
    let mut meta = HashMap::new();

    // Project name from first # heading
    #[allow(clippy::expect_used)]
    if let Some(caps) = Regex::new(r"(?m)^#\s+(.+)").expect("valid regex").captures(text) {
        meta.insert("name".into(), caps[1].trim().to_string());
    }

    let fmap = field_map();
    let skip_keys = ["항목", "---", "-", "내용", "구분"];

    for caps in TABLE_ROW_RE.captures_iter(text) {
        let key = caps[1].trim().replace('*', "");
        let key = key.trim();
        let val = caps[2].trim().replace('*', "");
        let val = val.trim().to_string();

        if skip_keys.contains(&key) {
            continue;
        }

        if let Some(&mapped) = fmap.get(key) {
            if mapped.starts_with('_') {
                // Technical fields stored with _ prefix
                meta.insert(mapped.to_string(), val);
            } else if !meta.contains_key(mapped) || meta[mapped].is_empty() {
                meta.insert(mapped.to_string(), val);
            }
        }
    }

    // Remove emoji from status
    if let Some(status) = meta.get("status").cloned() {
        let cleaned = STATUS_EMOJI_RE.replace_all(&status, "").trim().to_string();
        meta.insert("status".into(), cleaned);
    }

    meta
}

/// Split markdown text into sections and communication log entries.
pub fn split_sections(text: &str) -> (Vec<Section>, Vec<CommEntry>) {
    let mut sections = Vec::new();
    let mut comm_entries = Vec::new();

    // Split by heading regex — we use find_iter to get positions
    let heading_matches: Vec<_> = HEADING_RE.find_iter(text).collect();

    // Text before first heading
    let first_start = heading_matches
        .first()
        .map(|m| m.start())
        .unwrap_or(text.len());
    let intro = &text[..first_start];
    if !intro.trim().is_empty() {
        // Skip table rows, keep remaining text
        let mut table_end = 0;
        for m in TABLE_LINE_RE.find_iter(intro) {
            table_end = m.end();
        }
        let remaining = intro[table_end..].trim();
        if remaining.len() > 20 {
            sections.push(Section {
                heading: "개요".into(),
                body: remaining.into(),
                entry_date: None,
            });
        }
    }

    // Parse each heading + body
    let caps_vec: Vec<_> = HEADING_RE.captures_iter(text).collect();
    for (i, caps) in caps_vec.iter().enumerate() {
        let heading = caps[2].trim().to_string();
        let match_end = caps.get(0).unwrap_or_else(|| unreachable!("capture group 0 always exists")).end();
        let body_end = if i + 1 < caps_vec.len() {
            caps_vec[i + 1].get(0).unwrap_or_else(|| unreachable!("capture group 0 always exists")).start()
        } else {
            text.len()
        };
        let body = text[match_end..body_end].trim().to_string();

        // Detect date heading
        if let Some(date_caps) = DATE_HEADING_RE.captures(&heading) {
            let entry_date = date_caps[1].replace('/', "-");
            parse_comm_block(&entry_date, &body, &mut comm_entries);
            if !body.is_empty() {
                sections.push(Section {
                    heading: format!("로그 {}", entry_date),
                    body,
                    entry_date: Some(entry_date),
                });
            }
        } else if !body.is_empty() {
            sections.push(Section {
                heading,
                body,
                entry_date: None,
            });
        }
    }

    (sections, comm_entries)
}

/// Parse communication entries from a date block body.
///
/// Matching patterns (tolerant mode):
/// 1. - **subject** (sender)
/// 2. - subject (sender)
/// 3. - **subject**
/// 4. - plain text → merge into previous entry's summary
fn parse_comm_block(date_str: &str, body: &str, comm_entries: &mut Vec<CommEntry>) {
    let patterns: [&Lazy<Regex>; 3] = [
        &COMM_PAT_BOLD_SENDER,
        &COMM_PAT_PLAIN_SENDER,
        &COMM_PAT_BOLD_ONLY,
    ];

    let mut current_subject: Option<String> = None;
    let mut current_sender: Option<String> = None;
    let mut current_summary_lines: Vec<String> = Vec::new();

    for line in body.lines() {
        let stripped = line.trim();
        if stripped.is_empty() {
            continue;
        }

        let mut matched = false;
        for pat in &patterns {
            if let Some(caps) = pat.captures(stripped) {
                // Save previous entry
                if let Some(ref subj) = current_subject {
                    comm_entries.push(CommEntry {
                        date: date_str.into(),
                        sender: current_sender.take().unwrap_or_default(),
                        subject: subj.trim().trim_matches('*').trim().into(),
                        summary: current_summary_lines.join("\n").trim().into(),
                    });
                }
                current_subject = Some(caps[1].trim().trim_matches('*').trim().to_string());
                current_sender = caps.get(2).map(|m| m.as_str().trim().to_string());
                current_summary_lines.clear();
                matched = true;
                break;
            }
        }

        if !matched {
            if stripped.starts_with('>') {
                current_summary_lines.push(stripped.trim_start_matches('>').trim().into());
            } else if stripped.starts_with('-') || stripped.starts_with('*') {
                let plain = stripped.trim_start_matches(['-', '*']).trim().to_string();
                if current_subject.is_some() {
                    current_summary_lines.push(plain);
                } else if plain.len() >= 5 {
                    // Independent comm entry from plain bullet
                    current_subject = Some(plain);
                    current_sender = Some(String::new());
                    current_summary_lines.clear();
                }
            } else if current_subject.is_some() {
                current_summary_lines.push(stripped.into());
            }
        }
    }

    // Flush last entry
    if let Some(subj) = current_subject {
        comm_entries.push(CommEntry {
            date: date_str.into(),
            sender: current_sender.unwrap_or_default(),
            subject: subj.trim().to_string(),
            summary: current_summary_lines.join("\n").trim().into(),
        });
    }
}

#[cfg(test)]
#[allow(clippy::expect_used)]
mod tests {
    use super::*;

    #[test]
    fn test_extract_table_meta() {
        let md = "# 비금도 해상태양광\n\n| 항목 | 내용 |\n|---|---|\n| 발주처 | 한국전력 |\n| 상태 | 🟢 진행중 |\n| 사내 담당 | 김대희 |\n";
        let meta = extract_table_meta(md);
        assert_eq!(meta.get("name").expect("key 'name' should exist"), "비금도 해상태양광");
        assert_eq!(meta.get("client").expect("key 'client' should exist"), "한국전력");
        assert_eq!(meta.get("status").expect("key 'status' should exist"), "진행중");
        assert_eq!(meta.get("person_internal").expect("key 'person_internal' should exist"), "김대희");
    }

    #[test]
    fn test_split_sections() {
        let md = "# Project\n\n| 발주처 | Client |\n\nSome intro text that is longer than twenty chars.\n\n## 현재 상황\n\nStatus info.\n\n## 2025-01-15\n\n- **미팅 진행** (김대희)\n> 내용 정리\n";
        let (sections, comms) = split_sections(md);
        assert!(sections.len() >= 2);
        assert_eq!(comms.len(), 1);
        assert_eq!(comms[0].sender, "김대희");
        assert_eq!(comms[0].subject, "미팅 진행");
    }

    #[test]
    fn test_comm_parsing_tolerant() {
        let body = "- **제목A** (발신자A)\n> 요약A\n- 제목B (발신자B)\n- **제목C**\n- plain bullet text merged\n";
        let mut entries = Vec::new();
        parse_comm_block("2025-01-01", body, &mut entries);
        assert_eq!(entries.len(), 3);
        assert_eq!(entries[0].sender, "발신자A");
        assert_eq!(entries[1].sender, "발신자B");
        assert_eq!(entries[2].sender, "");
    }
}
