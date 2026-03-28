use once_cell::sync::Lazy;
use regex::Regex;
use rusqlite::Connection;
use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

static NAME_RE: Lazy<Regex> = Lazy::new(|| {
    Regex::new(r"([가-힣]{2,4})\s*(과장|대리|부장|사원|팀장|차장|이사|사장|실장|수석|선임|책임|매니저|담당|주임|부사장|전무|상무)?").expect("valid regex")
});
static PHONE_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(0\d{1,2}[-.\s]?\d{3,4}[-.\s]?\d{4})").expect("valid regex"));
static EMAIL_RE: Lazy<Regex> = Lazy::new(|| {
    Regex::new(r"([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})").expect("valid regex")
});

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

    let name_re = &*NAME_RE;
    let phone_re = &*PHONE_RE;
    let email_re = &*EMAIL_RE;

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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_extract_from_text_korean_name_with_phone() {
        let body = "김철수 과장 010-1234-5678\n";
        let contacts = extract_from_text(body, "note", "테스트 프로젝트", 1);
        assert!(!contacts.is_empty(), "expected at least one contact");
        // Find the contact with the phone number
        let c = contacts.iter().find(|c| c["phone"] == "010-1234-5678").expect("contact with phone 010-1234-5678");
        assert_eq!(c["name"], "김철수");
        assert_eq!(c["title"], "과장");
    }

    #[test]
    fn test_extract_from_text_email() {
        let body = "홍길동 대리 (hong@example.com)";
        let contacts = extract_from_text(body, "note", "프로젝트", 1);
        assert!(!contacts.is_empty());
        let c = &contacts[0];
        assert_eq!(c["name"], "홍길동");
        assert_eq!(c["email"], "hong@example.com");
    }

    #[test]
    fn test_extract_from_text_person_internal() {
        let body = "박지영 팀장\n010-9876-5432\npark@company.co.kr\n기타 정보";
        let contacts = extract_from_text(body, "person_internal", "프로젝트", 1);
        assert_eq!(contacts.len(), 1);
        let c = &contacts[0];
        assert_eq!(c["name"], "박지영 팀장");
        assert_eq!(c["phone"], "010-9876-5432");
        assert_eq!(c["email"], "park@company.co.kr");
        assert_eq!(c["type"], "internal");
    }

    #[test]
    fn test_extract_from_text_person_external() {
        let body = "이민호\n02-123-4567";
        let contacts = extract_from_text(body, "person_external", "외부 프로젝트", 2);
        assert_eq!(contacts.len(), 1);
        let c = &contacts[0];
        assert_eq!(c["type"], "external");
        assert_eq!(c["project_id"], 2);
    }

    #[test]
    fn test_extract_from_text_no_contacts() {
        let body = "This is just a regular note with no contact info.";
        let contacts = extract_from_text(body, "note", "프로젝트", 1);
        assert!(contacts.is_empty());
    }

    #[test]
    fn test_extract_from_text_multiple_contacts() {
        let body = "김영희 과장 010-1111-2222\n\n이철수 대리 010-3333-4444\n";
        let contacts = extract_from_text(body, "note", "프로젝트", 1);
        assert!(contacts.len() >= 2, "expected at least 2 contacts, got {}", contacts.len());
    }

    #[test]
    fn test_extract_from_text_landline_format() {
        let body = "사무실 연락처: 최수진 부장 02-555-1234";
        let contacts = extract_from_text(body, "note", "프로젝트", 1);
        assert!(!contacts.is_empty());
        assert_eq!(contacts[0]["phone"], "02-555-1234");
    }

    #[test]
    fn test_extract_from_text_phone_no_dash() {
        let body = "정민수 사원 01012345678";
        let contacts = extract_from_text(body, "note", "프로젝트", 1);
        assert!(!contacts.is_empty());
        assert_eq!(contacts[0]["phone"], "01012345678");
    }
}

pub struct ContactsHandler;

impl super::CommandHandler for ContactsHandler {
    fn execute(&self, config: &crate::config::VegaConfig, args: &serde_json::Value) -> super::CommandResult {
        cmd_contacts(args, config)
    }
}
