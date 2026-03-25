//! SQLite schema v6 for Vega project database.
//!
//! Port of Python vega/db/schema.py — matches the real production schema
//! with projects, chunks, tags, chunk_tags, comm_log, FTS5 indexes, and triggers.

use rusqlite::Connection;

use crate::config::SCHEMA_VERSION;

/// Full schema DDL matching Python vega/db/schema.py.
const SCHEMA: &str = "
CREATE TABLE IF NOT EXISTS projects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT,
    client TEXT,
    status TEXT,
    capacity TEXT,
    biz_type TEXT,
    person_internal TEXT,
    person_external TEXT,
    partner TEXT,
    source_file TEXT UNIQUE,
    imported_at TEXT,
    source_type TEXT DEFAULT 'project'
);

CREATE TABLE IF NOT EXISTS chunks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER REFERENCES projects(id),
    section_heading TEXT,
    content TEXT,
    chunk_type TEXT,
    entry_date TEXT,
    start_line INTEGER,
    end_line INTEGER
);

CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE
);

CREATE TABLE IF NOT EXISTS chunk_tags (
    chunk_id INTEGER REFERENCES chunks(id),
    tag_id INTEGER REFERENCES tags(id),
    PRIMARY KEY (chunk_id, tag_id)
);

CREATE TABLE IF NOT EXISTS comm_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER REFERENCES projects(id),
    log_date TEXT,
    sender TEXT,
    subject TEXT,
    summary TEXT
);

CREATE TABLE IF NOT EXISTS file_hashes (
    source_file TEXT PRIMARY KEY,
    content_hash TEXT,
    updated_at TEXT
);

CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER REFERENCES projects(id),
    action TEXT,
    actor TEXT DEFAULT 'user',
    field TEXT,
    old_value TEXT,
    new_value TEXT,
    timestamp TEXT DEFAULT (datetime('now'))
);

-- FTS5 indexes: chunks (unicode61 + trigram), comm_log
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
    project_name,
    client,
    section_heading,
    content,
    content='chunks',
    content_rowid='id',
    tokenize='unicode61'
);

CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, project_name, client, section_heading, content)
    SELECT NEW.id, p.name, p.client, NEW.section_heading, NEW.content
    FROM projects p WHERE p.id = NEW.project_id;
END;

CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, project_name, client, section_heading, content)
    SELECT 'delete', OLD.id, p.name, p.client, OLD.section_heading, OLD.content
    FROM projects p WHERE p.id = OLD.project_id;
END;

CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts_trigram USING fts5(
    project_name,
    content,
    content='chunks',
    content_rowid='id',
    tokenize='trigram'
);

CREATE TRIGGER IF NOT EXISTS chunks_tri_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts_trigram(rowid, project_name, content)
    SELECT NEW.id, p.name, NEW.content
    FROM projects p WHERE p.id = NEW.project_id;
END;

CREATE TRIGGER IF NOT EXISTS chunks_tri_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts_trigram(chunks_fts_trigram, rowid, project_name, content)
    SELECT 'delete', OLD.id, p.name, OLD.content
    FROM projects p WHERE p.id = OLD.project_id;
END;

CREATE VIRTUAL TABLE IF NOT EXISTS comm_fts USING fts5(
    project_name,
    sender,
    subject,
    summary,
    content='comm_log',
    content_rowid='id',
    tokenize='unicode61'
);

CREATE TRIGGER IF NOT EXISTS comm_ai AFTER INSERT ON comm_log BEGIN
    INSERT INTO comm_fts(rowid, project_name, sender, subject, summary)
    SELECT NEW.id, p.name, NEW.sender, NEW.subject, NEW.summary
    FROM projects p WHERE p.id = NEW.project_id;
END;

CREATE TRIGGER IF NOT EXISTS comm_ad AFTER DELETE ON comm_log BEGIN
    INSERT INTO comm_fts(comm_fts, rowid, project_name, sender, subject, summary)
    SELECT 'delete', OLD.id, p.name, OLD.sender, OLD.subject, OLD.summary
    FROM projects p WHERE p.id = OLD.project_id;
END;

-- Vector embedding storage (for semantic search)
CREATE TABLE IF NOT EXISTS chunk_embeddings (
    chunk_id INTEGER PRIMARY KEY REFERENCES chunks(id),
    embedding BLOB NOT NULL,
    model_name TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TRIGGER IF NOT EXISTS chunks_emb_ad AFTER DELETE ON chunks BEGIN
    DELETE FROM chunk_embeddings WHERE chunk_id = OLD.id;
END;
";

/// Initialize the database with pragmas and full schema.
pub fn init_db(conn: &Connection) -> rusqlite::Result<()> {
    conn.execute_batch(
        "PRAGMA journal_mode = WAL;
         PRAGMA busy_timeout = 5000;",
    )?;
    conn.execute_batch(SCHEMA)?;

    // Run migrations for older databases
    let user_ver: u32 = conn.pragma_query_value(None, "user_version", |row| row.get(0))?;
    if user_ver < 4 {
        let _ = conn.execute_batch("ALTER TABLE projects ADD COLUMN amount REAL");
    }
    if user_ver < 5 {
        let _ = conn.execute_batch(
            "CREATE TABLE IF NOT EXISTS chunk_embeddings (
                chunk_id INTEGER PRIMARY KEY REFERENCES chunks(id),
                embedding BLOB NOT NULL,
                model_name TEXT NOT NULL,
                updated_at TEXT NOT NULL
            );
            CREATE TRIGGER IF NOT EXISTS chunks_emb_ad AFTER DELETE ON chunks BEGIN
                DELETE FROM chunk_embeddings WHERE chunk_id = OLD.id;
            END;",
        );
    }
    if user_ver < 6 {
        // v1.43: memory backend — line numbers + source_type
        for stmt in &[
            "ALTER TABLE chunks ADD COLUMN start_line INTEGER",
            "ALTER TABLE chunks ADD COLUMN end_line INTEGER",
            "ALTER TABLE projects ADD COLUMN source_type TEXT DEFAULT 'project'",
        ] {
            let _ = conn.execute_batch(stmt);
        }
    }

    set_schema_version(conn)?;
    Ok(())
}

/// Check if the current schema version matches the expected version.
pub fn check_schema_version(conn: &Connection) -> rusqlite::Result<bool> {
    let ver: u32 = conn.pragma_query_value(None, "user_version", |row| row.get(0))?;
    Ok(ver >= SCHEMA_VERSION)
}

/// Set the schema version pragma.
fn set_schema_version(conn: &Connection) -> rusqlite::Result<()> {
    conn.pragma_update(None, "user_version", SCHEMA_VERSION)?;
    Ok(())
}

/// Rebuild all FTS indexes by delete-all + re-INSERT with JOINs.
pub fn rebuild_fts(conn: &Connection) -> rusqlite::Result<()> {
    // chunks_fts
    conn.execute_batch("INSERT INTO chunks_fts(chunks_fts) VALUES('delete-all')")?;
    conn.execute_batch(
        "INSERT INTO chunks_fts(rowid, project_name, client, section_heading, content)
         SELECT c.id, p.name, p.client, c.section_heading, c.content
         FROM chunks c JOIN projects p ON p.id = c.project_id",
    )?;

    // chunks_fts_trigram
    conn.execute_batch("INSERT INTO chunks_fts_trigram(chunks_fts_trigram) VALUES('delete-all')")?;
    conn.execute_batch(
        "INSERT INTO chunks_fts_trigram(rowid, project_name, content)
         SELECT c.id, p.name, c.content
         FROM chunks c JOIN projects p ON p.id = c.project_id",
    )?;

    // comm_fts
    conn.execute_batch("INSERT INTO comm_fts(comm_fts) VALUES('delete-all')")?;
    conn.execute_batch(
        "INSERT INTO comm_fts(rowid, project_name, sender, subject, summary)
         SELECT cl.id, p.name, cl.sender, cl.subject, cl.summary
         FROM comm_log cl JOIN projects p ON p.id = cl.project_id",
    )?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_schema_creation() {
        let conn = Connection::open_in_memory().unwrap();
        init_db(&conn).unwrap();
        assert!(check_schema_version(&conn).unwrap());
    }

    #[test]
    fn test_fts_triggers_work() {
        let conn = Connection::open_in_memory().unwrap();
        init_db(&conn).unwrap();

        // Insert a project and chunk — FTS trigger should fire
        conn.execute(
            "INSERT INTO projects (name, client, status) VALUES (?1, ?2, ?3)",
            rusqlite::params!["Test Project", "Test Client", "진행중"],
        )
        .unwrap();
        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
             VALUES (1, '개요', 'Test content', 'status')",
            [],
        )
        .unwrap();

        // FTS should find the chunk
        let count: i64 = conn
            .query_row(
                "SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH '\"Test\"'",
                [],
                |r| r.get(0),
            )
            .unwrap();
        assert!(count > 0, "FTS trigger should have indexed the chunk");
    }

    #[test]
    fn test_rebuild_fts() {
        let conn = Connection::open_in_memory().unwrap();
        init_db(&conn).unwrap();

        conn.execute(
            "INSERT INTO projects (name, client) VALUES ('TestProj', 'Client')",
            [],
        )
        .unwrap();
        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
             VALUES (1, '섹션', '내용 테스트', 'other')",
            [],
        )
        .unwrap();

        rebuild_fts(&conn).unwrap();

        // Verify FTS works by searching (COUNT(*) on external-content FTS reads
        // from the content table, but MATCH queries use the actual index)
        let count: i64 = conn
            .query_row(
                "SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH '\"TestProj\"'",
                [],
                |r| r.get(0),
            )
            .unwrap();
        assert!(count > 0, "FTS should find rebuilt index data");
    }
}
