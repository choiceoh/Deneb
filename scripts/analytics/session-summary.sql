-- session-summary.sql — DuckDB SQL for session summary statistics.
--
-- Session transcripts are JSONL files at ~/.deneb/agents/*/sessions/*.jsonl.
-- The first line of each file is a session header: {"type":"session","version":N,"id":"...","timestamp":N}.
-- Subsequent lines are transcript entries with fields: role, type, content, timestamp.
--
-- Usage:
--   duckdb < scripts/analytics/session-summary.sql
--   duckdb -c ".read scripts/analytics/session-summary.sql"

-- Read all JSONL session files.
CREATE OR REPLACE TEMP TABLE raw_entries AS
SELECT
    *,
    filename                                                           AS source_file
FROM read_json(
    glob('~/.deneb/agents/*/sessions/*.jsonl'),
    format = 'newline_delimited',
    ignore_errors = true,
    filename = true,
    columns = {
        type: 'VARCHAR',
        role: 'VARCHAR',
        id: 'VARCHAR',
        version: 'INT',
        timestamp: 'BIGINT',
        content: 'VARCHAR',
        model: 'VARCHAR',
        provider: 'VARCHAR',
        status: 'VARCHAR',
        inputTokens: 'BIGINT',
        outputTokens: 'BIGINT',
        totalTokens: 'BIGINT',
        usage: 'STRUCT(inputTokens BIGINT, outputTokens BIGINT, totalTokens BIGINT)'
    }
);

-- Extract session headers (first line of each file, type="session").
CREATE OR REPLACE TEMP TABLE sessions AS
SELECT
    id                                                                  AS session_id,
    source_file,
    timestamp                                                           AS created_at_ms,
    to_timestamp(timestamp / 1000)                                      AS created_at
FROM raw_entries
WHERE type = 'session';

-- Compute per-session stats from non-header entries.
CREATE OR REPLACE TEMP TABLE session_stats AS
SELECT
    source_file,
    COUNT(*)                                                            AS message_count,
    COUNT(*) FILTER (WHERE role = 'user')                               AS user_messages,
    COUNT(*) FILTER (WHERE role = 'assistant')                          AS assistant_messages,
    MIN(timestamp)                                                      AS first_msg_ms,
    MAX(timestamp)                                                      AS last_msg_ms,
    -- Detect terminal status from lifecycle events if present.
    MAX(CASE WHEN status IN ('DONE', 'FAILED', 'KILLED', 'TIMEOUT')
        THEN status ELSE NULL END)                                      AS final_status,
    -- Token totals per session.
    SUM(COALESCE(totalTokens, (usage).totalTokens, 0))                  AS total_tokens,
    SUM(COALESCE(inputTokens, (usage).inputTokens, 0))                  AS input_tokens,
    SUM(COALESCE(outputTokens, (usage).outputTokens, 0))                AS output_tokens
FROM raw_entries
WHERE type != 'session'
GROUP BY source_file;

-- ============================================================
-- Report 1: Session count by status
-- ============================================================
SELECT '=== Sessions by Status ===' AS report;

SELECT
    COALESCE(ss.final_status, 'UNKNOWN')                                AS status,
    COUNT(*)                                                            AS session_count
FROM sessions s
LEFT JOIN session_stats ss ON s.source_file = ss.source_file
GROUP BY status
ORDER BY session_count DESC;

-- ============================================================
-- Report 2: Average session duration and message counts
-- ============================================================
SELECT '=== Session Duration & Activity ===' AS report;

SELECT
    COUNT(*)                                                            AS total_sessions,
    ROUND(AVG((ss.last_msg_ms - ss.first_msg_ms) / 1000.0), 1)         AS avg_duration_sec,
    ROUND(MEDIAN((ss.last_msg_ms - ss.first_msg_ms) / 1000.0), 1)      AS median_duration_sec,
    ROUND(AVG(ss.message_count), 1)                                     AS avg_messages,
    ROUND(AVG(ss.user_messages), 1)                                     AS avg_user_messages,
    ROUND(AVG(ss.assistant_messages), 1)                                 AS avg_assistant_messages,
    SUM(ss.total_tokens)                                                AS total_tokens_all,
    ROUND(AVG(ss.total_tokens), 0)                                      AS avg_tokens_per_session
FROM sessions s
LEFT JOIN session_stats ss ON s.source_file = ss.source_file
WHERE ss.message_count > 0;

-- ============================================================
-- Report 3: Most-used tools (extracted from tool_use type entries)
-- ============================================================
SELECT '=== Most Used Tools ===' AS report;

-- Tool usage is recorded as entries with type containing "tool" or role-based tool calls.
-- We extract tool names from content or type fields.
CREATE OR REPLACE TEMP TABLE tool_entries AS
SELECT
    CASE
        -- If the entry has a recognizable tool type pattern, use it.
        WHEN type LIKE 'tool_%' THEN REPLACE(type, 'tool_', '')
        WHEN type = 'tool_use' THEN COALESCE(
            json_extract_string(content, '$.name'),
            'unknown_tool'
        )
        ELSE COALESCE(type, 'unknown')
    END AS tool_name
FROM raw_entries
WHERE
    type != 'session'
    AND (
        type LIKE 'tool%'
        OR role = 'tool'
    );

SELECT
    tool_name,
    COUNT(*) AS use_count
FROM tool_entries
GROUP BY tool_name
ORDER BY use_count DESC
LIMIT 20;

-- ============================================================
-- Report 4: Activity by day
-- ============================================================
SELECT '=== Daily Activity ===' AS report;

SELECT
    CAST(s.created_at AS DATE)                                          AS day,
    COUNT(DISTINCT s.session_id)                                        AS sessions,
    SUM(ss.user_messages)                                               AS user_messages,
    SUM(ss.assistant_messages)                                          AS assistant_messages,
    SUM(ss.total_tokens)                                                AS total_tokens
FROM sessions s
LEFT JOIN session_stats ss ON s.source_file = ss.source_file
GROUP BY day
ORDER BY day DESC
LIMIT 30;
