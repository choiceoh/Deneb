//! SQLite schema v6 for Vega project database.
//!
//! Port of Python vega/db/schema.py.

use rusqlite::Connection;

use crate::config::SCHEMA_VERSION;

/// Initialize the database schema (create tables if not exists).
pub fn ensure_schema(conn: &Connection) -> rusqlite::Result<()> {
    conn.execute_batch(
        "
        PRAGMA journal_mode = WAL;
        PRAGMA busy_timeout = 5000;

        CREATE TABLE IF NOT EXISTS projects (
            id          INTEGER PRIMARY KEY,
            name        TEXT NOT NULL UNIQUE,
            status      TEXT DEFAULT '',
            priority    TEXT DEFAULT 'normal',
            category    TEXT DEFAULT '',
            summary     TEXT DEFAULT '',
            raw_md      TEXT DEFAULT '',
            file_hash   TEXT DEFAULT '',
            updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
            created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
        );

        CREATE TABLE IF NOT EXISTS chunks (
            id          INTEGER PRIMARY KEY,
            project_id  INTEGER NOT NULL REFERENCES projects(id),
            section     TEXT DEFAULT '',
            content     TEXT NOT NULL,
            embedding   BLOB,
            chunk_idx   INTEGER DEFAULT 0
        );

        CREATE TABLE IF NOT EXISTS comm_log (
            id          INTEGER PRIMARY KEY,
            project_id  INTEGER NOT NULL REFERENCES projects(id),
            direction   TEXT DEFAULT 'inbound',
            channel     TEXT DEFAULT '',
            content     TEXT NOT NULL,
            created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
        );

        CREATE TABLE IF NOT EXISTS file_hashes (
            path        TEXT PRIMARY KEY,
            hash        TEXT NOT NULL,
            updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
        );

        CREATE TABLE IF NOT EXISTS audit_log (
            id          INTEGER PRIMARY KEY,
            project_id  INTEGER,
            action      TEXT NOT NULL,
            actor       TEXT DEFAULT 'user',
            field       TEXT,
            old_value   TEXT,
            new_value   TEXT,
            created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
        );
        ",
    )?;

    // FTS5 virtual table for full-text search.
    conn.execute_batch(
        "
        CREATE VIRTUAL TABLE IF NOT EXISTS projects_fts USING fts5(
            name, status, category, summary, raw_md,
            content=projects,
            content_rowid=id,
            tokenize='unicode61'
        );

        CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
            section, content,
            content=chunks,
            content_rowid=id,
            tokenize='unicode61'
        );
        ",
    )?;

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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_schema_creation() {
        let conn = Connection::open_in_memory().unwrap();
        ensure_schema(&conn).unwrap();
        assert!(check_schema_version(&conn).unwrap());
    }
}
