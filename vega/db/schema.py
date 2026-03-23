"""DB schema, init_db(), and migration logic."""

import sqlite3
import config
from config import set_schema_version


# ──────────────────────────────────────────────
# 1. 스키마
# ──────────────────────────────────────────────

SCHEMA = """
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

-- FTS5 trigram 테이블 (부분 문자열 매칭, SQLite 3.34+)
-- unicode61은 토큰 단위, trigram은 부분 문자열 매칭 지원
-- "해저케이블" → "해저 케이블"도 매칭
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

-- 벡터 임베딩 저장 (v1.4: 로컬 모델 내장)
CREATE TABLE IF NOT EXISTS chunk_embeddings (
    chunk_id INTEGER PRIMARY KEY REFERENCES chunks(id),
    embedding BLOB NOT NULL,
    model_name TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- chunk 삭제 시 임베딩도 삭제
CREATE TRIGGER IF NOT EXISTS chunks_emb_ad AFTER DELETE ON chunks BEGIN
    DELETE FROM chunk_embeddings WHERE chunk_id = OLD.id;
END;

-- DELETE 트리거: 증분 업데이트 시 FTS 인덱스 정합성 유지
CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, project_name, client, section_heading, content)
    SELECT 'delete', OLD.id, p.name, p.client, OLD.section_heading, OLD.content
    FROM projects p WHERE p.id = OLD.project_id;
END;

CREATE TRIGGER IF NOT EXISTS chunks_tri_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts_trigram(chunks_fts_trigram, rowid, project_name, content)
    SELECT 'delete', OLD.id, p.name, OLD.content
    FROM projects p WHERE p.id = OLD.project_id;
END;

CREATE TRIGGER IF NOT EXISTS comm_ad AFTER DELETE ON comm_log BEGIN
    INSERT INTO comm_fts(comm_fts, rowid, project_name, sender, subject, summary)
    SELECT 'delete', OLD.id, p.name, OLD.sender, OLD.subject, OLD.summary
    FROM projects p WHERE p.id = OLD.project_id;
END;
"""


def init_db(db_path=None):
    db_path = db_path or config.DB_PATH
    conn = sqlite3.connect(db_path, timeout=10)
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA busy_timeout=5000")
    conn.executescript(SCHEMA)

    # ── Migration logic ──
    user_ver = conn.execute("PRAGMA user_version").fetchone()[0]
    if user_ver < 4:
        try:
            conn.execute("ALTER TABLE projects ADD COLUMN amount REAL")
        except Exception:
            pass  # column already exists
        conn.execute("""
            CREATE TABLE IF NOT EXISTS audit_log (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                project_id INTEGER REFERENCES projects(id),
                action TEXT,
                actor TEXT DEFAULT 'user',
                field TEXT,
                old_value TEXT,
                new_value TEXT,
                timestamp TEXT DEFAULT (datetime('now'))
            )
        """)

    if user_ver < 5:
        # v1.4: 벡터 임베딩 테이블 + 삭제 트리거
        conn.execute("""
            CREATE TABLE IF NOT EXISTS chunk_embeddings (
                chunk_id INTEGER PRIMARY KEY REFERENCES chunks(id),
                embedding BLOB NOT NULL,
                model_name TEXT NOT NULL,
                updated_at TEXT NOT NULL
            )
        """)
        conn.execute("""
            CREATE TRIGGER IF NOT EXISTS chunks_emb_ad AFTER DELETE ON chunks BEGIN
                DELETE FROM chunk_embeddings WHERE chunk_id = OLD.id;
            END
        """)

    if user_ver < 6:
        # v1.43: memory backend — 라인 번호 + source_type
        for stmt in [
            "ALTER TABLE chunks ADD COLUMN start_line INTEGER",
            "ALTER TABLE chunks ADD COLUMN end_line INTEGER",
            "ALTER TABLE projects ADD COLUMN source_type TEXT DEFAULT 'project'",
        ]:
            try:
                conn.execute(stmt)
            except Exception:
                pass  # column already exists

    set_schema_version(conn)
    conn.commit()
    return conn
