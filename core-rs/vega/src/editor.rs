//! Markdown section editor for Vega.
//!
//! Port of Python vega/editor/md.py and `vega/md_editor.py`.
//! Provides section-level editing of project .md files.

use std::fs;
use std::path::{Path, PathBuf};

use regex::Regex;
use rusqlite::{params, Connection};

use crate::config::VegaConfig;

/// Find the .md file path for a project by ID or name.
/// Returns (`project_id`, `project_name`, `md_path`) or None.
pub fn find_md_path(
    config: &VegaConfig,
    project_ref: &str,
) -> Option<(i64, String, Option<PathBuf>)> {
    let conn = Connection::open(&config.db_path).ok()?;

    // Try as numeric ID
    if let Ok(id) = project_ref.parse::<i64>() {
        if let Ok(row) = conn.query_row(
            "SELECT id, name, source_file FROM projects WHERE id=?1",
            params![id],
            |r| {
                Ok((
                    r.get::<_, i64>(0)?,
                    r.get::<_, Option<String>>(1)?,
                    r.get::<_, Option<String>>(2)?,
                ))
            },
        ) {
            let md_path = row.2.and_then(|sf| resolve_source_path(config, &sf));
            return Some((row.0, row.1.unwrap_or_default(), md_path));
        }
    }

    // Try as name (LIKE search)
    if let Ok(row) = conn.query_row(
        "SELECT id, name, source_file FROM projects WHERE name LIKE ?1 LIMIT 1",
        params![format!("%{}%", project_ref)],
        |r| {
            Ok((
                r.get::<_, i64>(0)?,
                r.get::<_, Option<String>>(1)?,
                r.get::<_, Option<String>>(2)?,
            ))
        },
    ) {
        let md_path = row.2.and_then(|sf| resolve_source_path(config, &sf));
        return Some((row.0, row.1.unwrap_or_default(), md_path));
    }

    None
}

/// Resolve `source_file` to an absolute path, checking existence.
fn resolve_source_path(config: &VegaConfig, source_file: &str) -> Option<PathBuf> {
    let clean = source_file
        .trim_start_matches("memory:")
        .trim_start_matches("file:");

    let path = if Path::new(clean).is_absolute() {
        PathBuf::from(clean)
    } else {
        config.md_dir.join(clean)
    };

    if path.is_file() {
        Some(path)
    } else {
        // Try with .md extension
        let with_ext = path.with_extension("md");
        if with_ext.is_file() {
            Some(with_ext)
        } else {
            None
        }
    }
}

/// Update a metadata field in a .md file's table.
/// Returns (success, `old_value`, message).
pub fn update_meta_field(
    md_path: &Path,
    field_name: &str,
    new_value: &str,
) -> (bool, String, String) {
    let content = match fs::read_to_string(md_path) {
        Ok(c) => c,
        Err(e) => return (false, String::new(), format!("파일 읽기 실패: {e}")),
    };

    // Find the field in markdown table: | **필드명** | 값 |
    let pattern = format!(
        r"(?m)^\|\s*\*?\*?{}\*?\*?\s*\|\s*(.*?)\s*\|",
        regex::escape(field_name)
    );

    let Ok(re) = Regex::new(&pattern) else {
        return (false, String::new(), "패턴 생성 실패".to_string());
    };

    if let Some(cap) = re.captures(&content) {
        let old_value = cap
            .get(1)
            .map(|m| m.as_str().trim().to_string())
            .unwrap_or_default();
        let new_content = re
            .replace(&content, |caps: &regex::Captures| {
                caps[0].replace(
                    caps.get(1)
                        .unwrap_or_else(|| unreachable!("capture group 1 exists"))
                        .as_str(),
                    new_value,
                )
            })
            .to_string();

        match fs::write(md_path, &new_content) {
            Ok(()) => (
                true,
                old_value.clone(),
                format!("{field_name}: {old_value} → {new_value}"),
            ),
            Err(e) => (false, old_value, format!("파일 쓰기 실패: {e}")),
        }
    } else {
        (
            false,
            String::new(),
            format!("필드 '{field_name}' 를 찾을 수 없습니다"),
        )
    }
}

/// Update a field in the database.
pub fn update_db_field(
    config: &VegaConfig,
    project_id: i64,
    field_name: &str,
    new_value: &str,
) -> bool {
    let column = match field_name {
        "상태" | "status" => "status",
        "고객사" | "client" => "client",
        "사내 담당" | "person_internal" => "person_internal",
        "거래처 담당" | "person_external" => "person_external",
        "규모" | "capacity" => "capacity",
        _ => return false,
    };

    if let Ok(conn) = Connection::open(&config.db_path) {
        let sql = format!("UPDATE projects SET {column} = ?1 WHERE id = ?2");
        conn.execute(&sql, params![new_value, project_id]).is_ok()
    } else {
        false
    }
}

/// Add an action item to a project's .md file under "다음 예상 액션" section.
pub fn add_action_item(md_path: &Path, text: &str) -> (bool, String) {
    add_to_section(md_path, "다음 예상 액션", text)
}

/// Add a history entry to a project's .md file under "이력" section.
pub fn add_history_entry(md_path: &Path, text: &str) -> (bool, String) {
    let dated_text = format!("[{}] {}", chrono::Local::now().format("%Y-%m-%d"), text);
    add_to_section(md_path, "이력", &dated_text)
}

/// Add text to a specific section in the .md file.
fn add_to_section(md_path: &Path, section_name: &str, text: &str) -> (bool, String) {
    let content = match fs::read_to_string(md_path) {
        Ok(c) => c,
        Err(e) => return (false, format!("파일 읽기 실패: {e}")),
    };

    let heading_pattern = format!(r"(?m)^##\s+{}", regex::escape(section_name));
    let Ok(heading_re) = Regex::new(&heading_pattern) else {
        return (false, "패턴 생성 실패".to_string());
    };

    let entry = format!("- {text}\n");

    let new_content = if let Some(m) = heading_re.find(&content) {
        // Find end of heading line
        let rest = &content[m.end()..];
        let insert_pos = if let Some(nl) = rest.find('\n') {
            m.end() + nl + 1
        } else {
            content.len()
        };

        // Find next section heading to insert before it
        #[allow(clippy::expect_used)]
        let section_end_re = Regex::new(r"(?m)^##\s+").expect("valid regex");
        let actual_insert = if let Some(next_heading) = section_end_re.find(&content[insert_pos..])
        {
            // Insert before next heading, after last content
            let section_content = &content[insert_pos..insert_pos + next_heading.start()];
            let trimmed_end = section_content.trim_end().len();
            insert_pos + trimmed_end
        } else {
            // Last section - insert at the end
            content.trim_end().len()
        };

        format!(
            "{}\n{}\n{}",
            content[..actual_insert].trim_end(),
            entry.trim_end(),
            &content[actual_insert..]
        )
    } else {
        // Section doesn't exist — create it
        format!("{}\n\n## {}\n{}\n", content.trim_end(), section_name, entry)
    };

    match fs::write(md_path, new_content) {
        Ok(()) => (true, format!("{section_name} 섹션에 추가 완료")),
        Err(e) => (false, format!("파일 쓰기 실패: {e}")),
    }
}
