-- token-usage.sql — DuckDB SQL for token usage analysis from JSONL session logs.
--
-- Session transcripts are JSONL files at ~/.deneb/agents/*/sessions/*.jsonl.
-- Each line is a JSON object. The first line per file is a session header (type="session").
-- Subsequent lines are transcript entries with fields like:
--   role, content, type, timestamp, and nested usage fields (inputTokens, outputTokens, etc.).
--
-- Usage:
--   duckdb < scripts/analytics/token-usage.sql
--   duckdb -c ".read scripts/analytics/token-usage.sql"

-- Read all JSONL session files into a single table.
CREATE OR REPLACE TEMP TABLE raw_entries AS
SELECT *
FROM read_json(
    glob('~/.deneb/agents/*/sessions/*.jsonl'),
    format = 'newline_delimited',
    ignore_errors = true,
    columns = {
        type: 'VARCHAR',
        role: 'VARCHAR',
        model: 'VARCHAR',
        provider: 'VARCHAR',
        timestamp: 'BIGINT',
        inputTokens: 'BIGINT',
        outputTokens: 'BIGINT',
        totalTokens: 'BIGINT',
        cacheReadTokens: 'BIGINT',
        cacheWriteTokens: 'BIGINT',
        -- Nested usage object (some entries store usage in a sub-object).
        usage: 'STRUCT(inputTokens BIGINT, outputTokens BIGINT, totalTokens BIGINT, cacheReadTokens BIGINT, cacheWriteTokens BIGINT)'
    }
);

-- Normalize: merge top-level and nested usage fields, filter to LLM response entries.
CREATE OR REPLACE TEMP TABLE llm_responses AS
SELECT
    COALESCE(model, 'unknown')                                         AS model,
    COALESCE(provider, 'unknown')                                      AS provider,
    -- Prefer top-level token fields; fall back to nested usage object.
    COALESCE(inputTokens, usage.inputTokens, 0)                        AS input_tokens,
    COALESCE(outputTokens, usage.outputTokens, 0)                      AS output_tokens,
    COALESCE(totalTokens, usage.totalTokens,
             COALESCE(inputTokens, usage.inputTokens, 0)
           + COALESCE(outputTokens, usage.outputTokens, 0))            AS total_tokens,
    COALESCE(cacheReadTokens, usage.cacheReadTokens, 0)                AS cache_read_tokens,
    COALESCE(cacheWriteTokens, usage.cacheWriteTokens, 0)              AS cache_write_tokens,
    -- Convert unix ms timestamp to date.
    CAST(to_timestamp(timestamp / 1000) AS DATE)                       AS day
FROM raw_entries
WHERE
    -- Filter for assistant/LLM response entries (exclude session headers and user messages).
    type != 'session'
    AND (role = 'assistant' OR type IN ('llm_response', 'agent_turn', 'response'))
    -- Must have some token usage data.
    AND (
        COALESCE(inputTokens, usage.inputTokens, 0) > 0
        OR COALESCE(outputTokens, usage.outputTokens, 0) > 0
        OR COALESCE(totalTokens, usage.totalTokens, 0) > 0
    );

-- Aggregate token usage by model and day.
SELECT
    day,
    model,
    provider,
    COUNT(*)                    AS response_count,
    SUM(input_tokens)           AS input_tokens,
    SUM(output_tokens)          AS output_tokens,
    SUM(total_tokens)           AS total_tokens,
    SUM(cache_read_tokens)      AS cache_read_tokens,
    SUM(cache_write_tokens)     AS cache_write_tokens
FROM llm_responses
GROUP BY day, model, provider
ORDER BY day DESC, total_tokens DESC;
