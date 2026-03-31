package unified

// schemaSQL defines the unified database schema combining Aurora context
// management with structured long-term memory in a single SQLite database.
//
// Tables are organized by tier:
//   - Short-term: messages (raw conversation)
//   - Medium-term: summaries (compacted context with structured sections)
//   - Long-term: facts (extracted knowledge with importance/expiry)
//   - Cross-tier: memory_index + FTS + embeddings (unified search)
const schemaSQL = `
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA synchronous = NORMAL;
PRAGMA cache_size = -16000;
PRAGMA mmap_size = 268435456;
PRAGMA temp_store = MEMORY;

-- ════════════════════════════════════════════════════════════════════════════
-- Sequences & metadata
-- ════════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS sequences (
	name  TEXT PRIMARY KEY,
	value INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS metadata (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

-- ════════════════════════════════════════════════════════════════════════════
-- Short-term: Messages (raw conversation)
-- ════════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS messages (
	message_id      INTEGER PRIMARY KEY,
	conversation_id INTEGER NOT NULL,
	seq             INTEGER NOT NULL,
	role            TEXT NOT NULL,
	content         TEXT NOT NULL,
	token_count     INTEGER NOT NULL,
	created_at      INTEGER NOT NULL  -- epoch ms
);

CREATE INDEX IF NOT EXISTS idx_msg_conv ON messages(conversation_id);

-- Keep the unified cross-tier search index in sync with message mutations.
CREATE TRIGGER IF NOT EXISTS messages_ai_unified_index AFTER INSERT ON messages BEGIN
	INSERT INTO memory_index (item_type, item_id, tier, importance, created_at, updated_at)
	VALUES ('message', CAST(new.message_id AS TEXT), 'short', 0.0, new.created_at, NULL)
	ON CONFLICT(item_type, item_id) DO UPDATE SET
		tier = excluded.tier,
		importance = excluded.importance,
		created_at = excluded.created_at,
		updated_at = excluded.updated_at;
	INSERT OR REPLACE INTO memory_fts(rowid, content)
	VALUES (
		(SELECT id FROM memory_index WHERE item_type = 'message' AND item_id = CAST(new.message_id AS TEXT)),
		new.content
	);
	INSERT OR REPLACE INTO memory_fts_trigram(rowid, content)
	VALUES (
		(SELECT id FROM memory_index WHERE item_type = 'message' AND item_id = CAST(new.message_id AS TEXT)),
		new.content
	);
END;

CREATE TRIGGER IF NOT EXISTS messages_au_unified_index AFTER UPDATE OF content, created_at ON messages BEGIN
	INSERT INTO memory_index (item_type, item_id, tier, importance, created_at, updated_at)
	VALUES ('message', CAST(new.message_id AS TEXT), 'short', 0.0, new.created_at, NULL)
	ON CONFLICT(item_type, item_id) DO UPDATE SET
		tier = excluded.tier,
		importance = excluded.importance,
		created_at = excluded.created_at,
		updated_at = excluded.updated_at;
	INSERT OR REPLACE INTO memory_fts(rowid, content)
	VALUES (
		(SELECT id FROM memory_index WHERE item_type = 'message' AND item_id = CAST(new.message_id AS TEXT)),
		new.content
	);
	INSERT OR REPLACE INTO memory_fts_trigram(rowid, content)
	VALUES (
		(SELECT id FROM memory_index WHERE item_type = 'message' AND item_id = CAST(new.message_id AS TEXT)),
		new.content
	);
END;

CREATE TRIGGER IF NOT EXISTS messages_ad_unified_index AFTER DELETE ON messages BEGIN
	INSERT INTO memory_fts(memory_fts, rowid, content)
	VALUES (
		'delete',
		(SELECT id FROM memory_index WHERE item_type = 'message' AND item_id = CAST(old.message_id AS TEXT)),
		old.content
	);
	INSERT INTO memory_fts_trigram(memory_fts_trigram, rowid, content)
	VALUES (
		'delete',
		(SELECT id FROM memory_index WHERE item_type = 'message' AND item_id = CAST(old.message_id AS TEXT)),
		old.content
	);
	DELETE FROM memory_index
	WHERE item_type = 'message' AND item_id = CAST(old.message_id AS TEXT);
END;

-- ════════════════════════════════════════════════════════════════════════════
-- Medium-term: Summaries (compacted context)
-- ════════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS summaries (
	summary_id                 TEXT PRIMARY KEY,
	conversation_id            INTEGER NOT NULL,
	kind                       TEXT NOT NULL,       -- 'leaf' or 'condensed'
	depth                      INTEGER NOT NULL DEFAULT 0,
	content                    TEXT NOT NULL,        -- full summary text (backward compat)
	-- Structured sections (populated when compaction uses structured prompts).
	-- NULL for legacy plain-text summaries.
	narrative                  TEXT,                 -- concise narrative
	decisions                  TEXT,                 -- JSON array: [{"decision":"...","reason":"..."}]
	pending                    TEXT,                 -- JSON array: [{"type":"todo|blocked","detail":"..."}]
	refs                       TEXT,                 -- JSON array: ["file:line", "tool:name -> result"]
	importance                 REAL NOT NULL DEFAULT 0.3,  -- 0.0-1.0 compaction-assessed
	token_count                INTEGER NOT NULL,
	file_ids                   TEXT NOT NULL DEFAULT '[]',
	earliest_at                INTEGER,
	latest_at                  INTEGER,
	descendant_count           INTEGER NOT NULL DEFAULT 0,
	descendant_token_count     INTEGER NOT NULL DEFAULT 0,
	source_message_token_count INTEGER NOT NULL DEFAULT 0,
	created_at                 INTEGER NOT NULL      -- epoch ms
);

CREATE INDEX IF NOT EXISTS idx_sum_conv ON summaries(conversation_id);
CREATE INDEX IF NOT EXISTS idx_sum_importance ON summaries(importance DESC);

-- Keep the unified cross-tier search index in sync with summary mutations.
CREATE TRIGGER IF NOT EXISTS summaries_ai_unified_index AFTER INSERT ON summaries BEGIN
	INSERT INTO memory_index (item_type, item_id, tier, importance, created_at, updated_at)
	VALUES ('summary', new.summary_id, 'medium', COALESCE(new.importance, 0.3), new.created_at, NULL)
	ON CONFLICT(item_type, item_id) DO UPDATE SET
		tier = excluded.tier,
		importance = excluded.importance,
		created_at = excluded.created_at,
		updated_at = excluded.updated_at;
	INSERT OR REPLACE INTO memory_fts(rowid, content)
	VALUES (
		(SELECT id FROM memory_index WHERE item_type = 'summary' AND item_id = new.summary_id),
		COALESCE(new.narrative, new.content)
	);
	INSERT OR REPLACE INTO memory_fts_trigram(rowid, content)
	VALUES (
		(SELECT id FROM memory_index WHERE item_type = 'summary' AND item_id = new.summary_id),
		COALESCE(new.narrative, new.content)
	);
END;

CREATE TRIGGER IF NOT EXISTS summaries_au_unified_index AFTER UPDATE OF content, narrative, importance, created_at ON summaries BEGIN
	INSERT INTO memory_index (item_type, item_id, tier, importance, created_at, updated_at)
	VALUES ('summary', new.summary_id, 'medium', COALESCE(new.importance, 0.3), new.created_at, NULL)
	ON CONFLICT(item_type, item_id) DO UPDATE SET
		tier = excluded.tier,
		importance = excluded.importance,
		created_at = excluded.created_at,
		updated_at = excluded.updated_at;
	INSERT OR REPLACE INTO memory_fts(rowid, content)
	VALUES (
		(SELECT id FROM memory_index WHERE item_type = 'summary' AND item_id = new.summary_id),
		COALESCE(new.narrative, new.content)
	);
	INSERT OR REPLACE INTO memory_fts_trigram(rowid, content)
	VALUES (
		(SELECT id FROM memory_index WHERE item_type = 'summary' AND item_id = new.summary_id),
		COALESCE(new.narrative, new.content)
	);
END;

CREATE TRIGGER IF NOT EXISTS summaries_ad_unified_index AFTER DELETE ON summaries BEGIN
	INSERT INTO memory_fts(memory_fts, rowid, content)
	VALUES (
		'delete',
		(SELECT id FROM memory_index WHERE item_type = 'summary' AND item_id = old.summary_id),
		COALESCE(old.narrative, old.content)
	);
	INSERT INTO memory_fts_trigram(memory_fts_trigram, rowid, content)
	VALUES (
		'delete',
		(SELECT id FROM memory_index WHERE item_type = 'summary' AND item_id = old.summary_id),
		COALESCE(old.narrative, old.content)
	);
	DELETE FROM memory_index
	WHERE item_type = 'summary' AND item_id = old.summary_id;
END;

-- Summary DAG relationships.
CREATE TABLE IF NOT EXISTS summary_parents (
	summary_id TEXT NOT NULL REFERENCES summaries(summary_id) ON DELETE CASCADE,
	parent_id  TEXT NOT NULL REFERENCES summaries(summary_id) ON DELETE CASCADE,
	PRIMARY KEY (summary_id, parent_id)
);

-- Summary-to-message links.
CREATE TABLE IF NOT EXISTS summary_messages (
	summary_id TEXT NOT NULL REFERENCES summaries(summary_id) ON DELETE CASCADE,
	message_id INTEGER NOT NULL REFERENCES messages(message_id) ON DELETE CASCADE,
	PRIMARY KEY (summary_id, message_id)
);

-- Context items: ordered sequence of messages and summaries.
CREATE TABLE IF NOT EXISTS context_items (
	conversation_id INTEGER NOT NULL,
	ordinal         INTEGER NOT NULL,
	item_type       TEXT NOT NULL,  -- 'message' or 'summary'
	message_id      INTEGER REFERENCES messages(message_id) ON DELETE CASCADE,
	summary_id      TEXT REFERENCES summaries(summary_id) ON DELETE CASCADE,
	created_at      INTEGER NOT NULL,
	PRIMARY KEY (conversation_id, ordinal)
);

CREATE INDEX IF NOT EXISTS idx_ci_conv ON context_items(conversation_id);

-- ════════════════════════════════════════════════════════════════════════════
-- Long-term: Facts (structured knowledge)
-- ════════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS facts (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	content         TEXT NOT NULL,
	category        TEXT NOT NULL DEFAULT 'context',
	importance      REAL NOT NULL DEFAULT 0.5,
	source          TEXT DEFAULT 'auto_extract',
	created_at      TEXT NOT NULL,
	updated_at      TEXT NOT NULL,
	last_accessed_at TEXT,
	access_count    INTEGER NOT NULL DEFAULT 0,
	verified_at     TEXT,
	expires_at      TEXT,
	superseded_by   INTEGER REFERENCES facts(id),
	active          INTEGER NOT NULL DEFAULT 1,
	merge_depth     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_facts_active ON facts(active);
CREATE INDEX IF NOT EXISTS idx_facts_category ON facts(category);
CREATE INDEX IF NOT EXISTS idx_facts_importance ON facts(importance DESC);
CREATE INDEX IF NOT EXISTS idx_facts_created ON facts(created_at DESC);

-- Composite indexes for common filtered queries (active=1 prefix).
CREATE INDEX IF NOT EXISTS idx_facts_active_importance ON facts(active, importance DESC);
CREATE INDEX IF NOT EXISTS idx_facts_active_category ON facts(active, category, importance DESC);
CREATE INDEX IF NOT EXISTS idx_facts_active_created ON facts(active, created_at DESC);

-- Fact-level FTS indices (used by memory.Store search path).
CREATE VIRTUAL TABLE IF NOT EXISTS facts_fts USING fts5(
	content,
	category,
	content=facts,
	content_rowid=id,
	tokenize='unicode61'
);

CREATE VIRTUAL TABLE IF NOT EXISTS facts_fts_trigram USING fts5(
	content,
	content=facts,
	content_rowid=id,
	tokenize='trigram'
);

-- Keep facts FTS in sync.
CREATE TRIGGER IF NOT EXISTS facts_ai AFTER INSERT ON facts BEGIN
	INSERT INTO facts_fts(rowid, content, category) VALUES (new.id, new.content, new.category);
	INSERT INTO facts_fts_trigram(rowid, content) VALUES (new.id, new.content);
END;

CREATE TRIGGER IF NOT EXISTS facts_ad AFTER DELETE ON facts BEGIN
	INSERT INTO facts_fts(facts_fts, rowid, content, category) VALUES ('delete', old.id, old.content, old.category);
	INSERT INTO facts_fts_trigram(facts_fts_trigram, rowid, content) VALUES ('delete', old.id, old.content);
END;

CREATE TRIGGER IF NOT EXISTS facts_au AFTER UPDATE OF content, category ON facts BEGIN
	INSERT INTO facts_fts(facts_fts, rowid, content, category) VALUES ('delete', old.id, old.content, old.category);
	INSERT INTO facts_fts(rowid, content, category) VALUES (new.id, new.content, new.category);
	INSERT INTO facts_fts_trigram(facts_fts_trigram, rowid, content) VALUES ('delete', old.id, old.content);
	INSERT INTO facts_fts_trigram(rowid, content) VALUES (new.id, new.content);
END;

-- Keep the unified cross-tier search index in sync with fact mutations.
CREATE TRIGGER IF NOT EXISTS facts_ai_unified_index AFTER INSERT ON facts
WHEN new.active = 1 BEGIN
	INSERT INTO memory_index (item_type, item_id, tier, importance, created_at, updated_at)
	VALUES (
		'fact',
		CAST(new.id AS TEXT),
		'long',
		new.importance,
		COALESCE(
			CASE
				WHEN new.created_at GLOB '[0-9]*' THEN CAST(new.created_at AS INTEGER)
				WHEN strftime('%s', new.created_at) IS NOT NULL THEN CAST(strftime('%s', new.created_at) AS INTEGER) * 1000
			END,
			CAST(strftime('%s', 'now') AS INTEGER) * 1000
		),
		CASE
			WHEN new.updated_at GLOB '[0-9]*' THEN CAST(new.updated_at AS INTEGER)
			WHEN strftime('%s', new.updated_at) IS NOT NULL THEN CAST(strftime('%s', new.updated_at) AS INTEGER) * 1000
		END
	)
	ON CONFLICT(item_type, item_id) DO UPDATE SET
		tier = excluded.tier,
		importance = excluded.importance,
		created_at = excluded.created_at,
		updated_at = excluded.updated_at;
	INSERT OR REPLACE INTO memory_fts(rowid, content)
	VALUES (
		(SELECT id FROM memory_index WHERE item_type = 'fact' AND item_id = CAST(new.id AS TEXT)),
		new.content
	);
	INSERT OR REPLACE INTO memory_fts_trigram(rowid, content)
	VALUES (
		(SELECT id FROM memory_index WHERE item_type = 'fact' AND item_id = CAST(new.id AS TEXT)),
		new.content
	);
END;

CREATE TRIGGER IF NOT EXISTS facts_au_unified_index AFTER UPDATE OF content, importance, created_at, updated_at, active ON facts
WHEN new.active = 1 BEGIN
	INSERT INTO memory_index (item_type, item_id, tier, importance, created_at, updated_at)
	VALUES (
		'fact',
		CAST(new.id AS TEXT),
		'long',
		new.importance,
		COALESCE(
			CASE
				WHEN new.created_at GLOB '[0-9]*' THEN CAST(new.created_at AS INTEGER)
				WHEN strftime('%s', new.created_at) IS NOT NULL THEN CAST(strftime('%s', new.created_at) AS INTEGER) * 1000
			END,
			CAST(strftime('%s', 'now') AS INTEGER) * 1000
		),
		CASE
			WHEN new.updated_at GLOB '[0-9]*' THEN CAST(new.updated_at AS INTEGER)
			WHEN strftime('%s', new.updated_at) IS NOT NULL THEN CAST(strftime('%s', new.updated_at) AS INTEGER) * 1000
		END
	)
	ON CONFLICT(item_type, item_id) DO UPDATE SET
		tier = excluded.tier,
		importance = excluded.importance,
		created_at = excluded.created_at,
		updated_at = excluded.updated_at;
	INSERT OR REPLACE INTO memory_fts(rowid, content)
	VALUES (
		(SELECT id FROM memory_index WHERE item_type = 'fact' AND item_id = CAST(new.id AS TEXT)),
		new.content
	);
	INSERT OR REPLACE INTO memory_fts_trigram(rowid, content)
	VALUES (
		(SELECT id FROM memory_index WHERE item_type = 'fact' AND item_id = CAST(new.id AS TEXT)),
		new.content
	);
END;

CREATE TRIGGER IF NOT EXISTS facts_au_unified_delete AFTER UPDATE OF active ON facts
WHEN old.active = 1 AND new.active <> 1 BEGIN
	INSERT INTO memory_fts(memory_fts, rowid, content)
	VALUES (
		'delete',
		(SELECT id FROM memory_index WHERE item_type = 'fact' AND item_id = CAST(old.id AS TEXT)),
		old.content
	);
	INSERT INTO memory_fts_trigram(memory_fts_trigram, rowid, content)
	VALUES (
		'delete',
		(SELECT id FROM memory_index WHERE item_type = 'fact' AND item_id = CAST(old.id AS TEXT)),
		old.content
	);
	DELETE FROM memory_index
	WHERE item_type = 'fact' AND item_id = CAST(old.id AS TEXT);
END;

CREATE TRIGGER IF NOT EXISTS facts_ad_unified_index AFTER DELETE ON facts
WHEN old.active = 1 BEGIN
	INSERT INTO memory_fts(memory_fts, rowid, content)
	VALUES (
		'delete',
		(SELECT id FROM memory_index WHERE item_type = 'fact' AND item_id = CAST(old.id AS TEXT)),
		old.content
	);
	INSERT INTO memory_fts_trigram(memory_fts_trigram, rowid, content)
	VALUES (
		'delete',
		(SELECT id FROM memory_index WHERE item_type = 'fact' AND item_id = CAST(old.id AS TEXT)),
		old.content
	);
	DELETE FROM memory_index
	WHERE item_type = 'fact' AND item_id = CAST(old.id AS TEXT);
END;

-- Fact embeddings (vector storage for semantic search).
CREATE TABLE IF NOT EXISTS fact_embeddings (
	fact_id    INTEGER PRIMARY KEY REFERENCES facts(id) ON DELETE CASCADE,
	embedding  BLOB NOT NULL,
	model_name TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_fact_embeddings_fact_id ON fact_embeddings(fact_id);

-- Fact relations (knowledge graph edges between facts).
CREATE TABLE IF NOT EXISTS fact_relations (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	from_fact_id  INTEGER NOT NULL REFERENCES facts(id) ON DELETE CASCADE,
	to_fact_id    INTEGER NOT NULL REFERENCES facts(id) ON DELETE CASCADE,
	relation_type TEXT NOT NULL CHECK(relation_type IN ('evolves','contradicts','supports','causes','related')),
	confidence    REAL NOT NULL DEFAULT 1.0,
	created_at    TEXT NOT NULL,
	UNIQUE(from_fact_id, to_fact_id, relation_type)
);

CREATE INDEX IF NOT EXISTS idx_relations_from ON fact_relations(from_fact_id);
CREATE INDEX IF NOT EXISTS idx_relations_to ON fact_relations(to_fact_id);

-- Named entities for object-centric fact grouping.
CREATE TABLE IF NOT EXISTS entities (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	name          TEXT NOT NULL UNIQUE,
	entity_type   TEXT NOT NULL DEFAULT 'unknown' CHECK(entity_type IN ('person','project','tool','system','concept','organization')),
	first_seen    TEXT NOT NULL,
	last_seen     TEXT NOT NULL,
	mention_count INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(name);

-- Fact-entity associations with role context.
CREATE TABLE IF NOT EXISTS fact_entities (
	fact_id   INTEGER NOT NULL REFERENCES facts(id) ON DELETE CASCADE,
	entity_id INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
	role      TEXT NOT NULL DEFAULT 'mentioned',
	PRIMARY KEY (fact_id, entity_id)
);

CREATE INDEX IF NOT EXISTS idx_fact_entities_entity ON fact_entities(entity_id);

-- User model (key-value profile & relationship dynamics).
CREATE TABLE IF NOT EXISTS user_model (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL,
	confidence REAL NOT NULL DEFAULT 0.5,
	updated_at TEXT NOT NULL
);

-- Dreaming consolidation audit log.
CREATE TABLE IF NOT EXISTS dreaming_log (
	id                 INTEGER PRIMARY KEY AUTOINCREMENT,
	ran_at             TEXT NOT NULL,
	facts_verified     INTEGER NOT NULL DEFAULT 0,
	facts_merged       INTEGER NOT NULL DEFAULT 0,
	facts_expired      INTEGER NOT NULL DEFAULT 0,
	facts_pruned       INTEGER NOT NULL DEFAULT 0,
	patterns_extracted INTEGER NOT NULL DEFAULT 0,
	duration_ms        INTEGER NOT NULL DEFAULT 0
);

-- ════════════════════════════════════════════════════════════════════════════
-- Cross-tier: Unified memory index
-- ════════════════════════════════════════════════════════════════════════════

-- memory_index bridges all memory tiers with a common search layer.
-- Each row points to a source record (message, summary, or fact) and
-- carries tier/importance metadata for unified scoring.
CREATE TABLE IF NOT EXISTS memory_index (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	item_type  TEXT NOT NULL,           -- 'message', 'summary', 'fact'
	item_id    TEXT NOT NULL,           -- source table PK (as string)
	tier       TEXT NOT NULL,           -- 'short', 'medium', 'long'
	importance REAL NOT NULL DEFAULT 0.0,
	created_at INTEGER NOT NULL,        -- epoch ms
	updated_at INTEGER,
	UNIQUE(item_type, item_id)
);

CREATE INDEX IF NOT EXISTS idx_mi_tier ON memory_index(tier);
CREATE INDEX IF NOT EXISTS idx_mi_importance ON memory_index(importance DESC);
CREATE INDEX IF NOT EXISTS idx_mi_type ON memory_index(item_type);

-- Unified FTS5 index across all tiers (unicode61 for general tokenization).
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
	content,
	content=memory_index,
	content_rowid=id,
	tokenize='unicode61 remove_diacritics 2'
);

-- Trigram index for Korean/CJK substring matching.
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts_trigram USING fts5(
	content,
	content=memory_index,
	content_rowid=id,
	tokenize='trigram'
);

-- Vector embeddings keyed by memory_index ID (for semantic search).
CREATE TABLE IF NOT EXISTS embeddings (
	memory_index_id INTEGER PRIMARY KEY REFERENCES memory_index(id) ON DELETE CASCADE,
	embedding       BLOB NOT NULL,       -- float32 little-endian
	created_at      INTEGER NOT NULL     -- epoch ms
);

-- ════════════════════════════════════════════════════════════════════════════
-- Audit: Compaction events
-- ════════════════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS compaction_events (
	id                 INTEGER PRIMARY KEY AUTOINCREMENT,
	conversation_id    INTEGER NOT NULL,
	pass               TEXT NOT NULL,
	level              TEXT NOT NULL,
	tokens_before      INTEGER NOT NULL,
	tokens_after       INTEGER NOT NULL,
	created_summary_id TEXT NOT NULL,
	created_at         INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ce_conv ON compaction_events(conversation_id);

-- Memory transfer tracking (which summaries were graduated to facts).
CREATE TABLE IF NOT EXISTS transferred_summaries (
	summary_id     TEXT PRIMARY KEY,
	transferred_at INTEGER NOT NULL
);
`
