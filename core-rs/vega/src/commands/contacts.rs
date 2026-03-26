use regex::Regex;
use rusqlite::Connection;
use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

/// Extract contacts (names, phones, emails) from project text using regex patterns.
pub fn cmd_contacts(args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("contacts", &e),
    };

    let project_filter = args.get("project").and_then(|v| v.as_str());

    let contacts = if let Some(query) = project_filter {
        match extract_contacts_for_project(&conn, query) {
            Ok(c) => c,
            Err(e) => return CommandResult::err("contacts", &e),
        }
    } else {
        match extract_all_contacts(&conn) {
            Ok(c) => c,
            Err(e) => return CommandResult::err("contacts", &e),
        }
    };

    let summary = json!({
        "total_contacts": contacts.len(),
        "with_phone": contacts.iter().filter(|c| c["phone"].as_str().is_some_and(|s| !s.is_empty())).count(),
        "with_email": contacts.iter().filter(|c| c["email"].as_str().is_some_and(|s| !s.is_empty())).count(),
    });

    CommandResult::ok(
        "contacts",
        json!({
            "summary": summary,
            "contacts": contacts
        }),
    )
}

/// Extract contacts for a specific project.
fn extract_contacts_for_project(conn: &Connection, query: &str) -> Result<Vec<Value>, String> {
    let sql = "
        SELECT c.body, c.chunk_type, p.title, p.id
        FROM chunks c
        JOIN projects p ON c.project_id = p.id
        WHERE (p.title LIKE ?1 OR p.slug LIKE ?1 OR CAST(p.id AS TEXT) = ?2)
          AND c.chunk_type IN ('person_internal', 'person_external', 'body', 'note')
        ORDER BY c.chunk_type, c.seq
    ";
    let pattern = format!("%{}%", query);
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map(rusqlite::params![pattern, query], |row| {
            let body: String = row.get(0)?;
            let chunk_type: String = row.get(1)?;
            let title: String = row.get(2)?;
            let project_id: i64 = row.get(3)?;
            Ok((body, chunk_type, title, project_id))
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    let mut contacts = Vec::new();
    let mut seen: std::collections::HashSet<String> = std::collections::HashSet::new();

    for row in rows {
        let (body, chunk_type, title, project_id) =
            row.map_err(|e| format!("행 읽기 실패: {}", e))?;
        let extracted = extract_from_text(&body, &chunk_type, &title, project_id);
        for contact in extracted {
            let key = contact["name"].as_str().unwrap_or("").to_string();
            if !key.is_empty() && seen.insert(key) {
                contacts.push(contact);
            }
        }
    }

    Ok(contacts)
}

/// Extract contacts from all projects.
fn extract_all_contacts(conn: &Connection) -> Result<Vec<Value>, String> {
    let sql = "
        SELECT c.body, c.chunk_type, p.title, p.id
        FROM chunks c
        JOIN projects p ON c.project_id = p.id
        WHERE c.chunk_type IN ('person_internal', 'person_external', 'body', 'note')
        ORDER BY p.title, c.chunk_type, c.seq
    ";
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map([], |row| {
            let body: String = row.get(0)?;
            let chunk_type: String = row.get(1)?;
            let title: String = row.get(2)?;
            let project_id: i64 = row.get(3)?;
            Ok((body, chunk_type, title, project_id))
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    let mut contacts = Vec::new();
    let mut seen: std::collections::HashSet<String> = std::collections::HashSet::new();

    for row in rows {
        let (body, chunk_type, title, project_id) =
            row.map_err(|e| format!("행 읽기 실패: {}", e))?;
        let extracted = extract_from_text(&body, &chunk_type, &title, project_id);
        for contact in extracted {
            let key = contact["name"].as_str().unwrap_or("").to_string();
            if !key.is_empty() && seen.insert(key) {
                contacts.push(contact);
            }
        }
    }

    Ok(contacts)
}

/// Extract contact information from text using regex patterns for Korean names,
/// phone numbers, and email addresses.
fn extract_from_text(
    body: &str,
    chunk_type: &str,
    project_title: &str,
    project_id: i64,
) -> Vec<Value> {
    let mut contacts = Vec::new();

    // Korean name pattern: 2-4 Korean characters (가-힣)
    let name_re = Regex::new(r"([가-힣]{2,4})\s*(과장|대리|부장|사원|팀장|차장|이사|사장|실장|수석|선임|책임|매니저|담당|주임|부사장|전무|상무)?"
    ).unwrap();

    // Phone patterns: Korean mobile/landline formats
    let phone_re = Regex::new(r"(0\d{1,2}[-.\s]?\d{3,4}[-.\s]?\d{4})").unwrap();

    // Email pattern
    let email_re = Regex::new(r"([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})").unwrap();

    // For person_internal/person_external chunks, the first line is typically the name
    if chunk_type == "person_internal" || chunk_type == "person_external" {
        let first_line = body.lines().next().unwrap_or("").trim();
        if !first_line.is_empty() {
            let phone = phone_re
                .find(body)
                .map(|m| m.as_str().to_string())
                .unwrap_or_default();
            let email = email_re
                .find(body)
                .map(|m| m.as_str().to_string())
                .unwrap_or_default();
            let title_match = name_re
                .captures(first_line)
                .and_then(|caps| caps.get(2).map(|m| m.as_str().to_string()));

            contacts.push(json!({
                "name": first_line,
                "title": title_match.unwrap_or_default(),
                "phone": phone,
                "email": email,
                "type": chunk_type.replace("person_", ""),
                "project": project_title,
                "project_id": project_id
            }));
            return contacts;
        }
    }

    // For body/note chunks, scan for embedded contact info
    let phones: Vec<String> = phone_re
        .find_iter(body)
        .map(|m| m.as_str().to_string())
        .collect();
    let emails: Vec<String> = email_re
        .find_iter(body)
        .map(|m| m.as_str().to_string())
        .collect();

    for caps in name_re.captures_iter(body) {
        let name = caps.get(1).map(|m| m.as_str()).unwrap_or("");
        let title = caps
            .get(2)
            .map(|m| m.as_str().to_string())
            .unwrap_or_default();

        // Try to associate nearby phone/email with this name
        let name_pos = caps.get(0).map(|m| m.start()).unwrap_or(0);

        let nearby_phone = phones
            .iter()
            .find(|p| {
                if let Some(pos) = body.find(p.as_str()) {
                    (pos as i64 - name_pos as i64).unsigned_abs() < 200
                } else {
                    false
                }
            })
            .cloned()
            .unwrap_or_default();

        let nearby_email = emails
            .iter()
            .find(|e| {
                if let Some(pos) = body.find(e.as_str()) {
                    (pos as i64 - name_pos as i64).unsigned_abs() < 200
                } else {
                    false
                }
            })
            .cloned()
            .unwrap_or_default();

        if !nearby_phone.is_empty() || !nearby_email.is_empty() {
            contacts.push(json!({
                "name": name,
                "title": title,
                "phone": nearby_phone,
                "email": nearby_email,
                "type": "extracted",
                "project": project_title,
                "project_id": project_id
            }));
        }
    }

    contacts
}
