//! Agent scope — registry, config resolution, ID normalization, and session key parsing.
//!
//! Mirrors `src/agents/agent-scope.ts`, `src/routing/session-key.ts`,
//! `src/sessions/session-key-utils.ts`, and `src/routing/account-id.ts`. Keep in sync.
//!
//! Note: Functions that require filesystem access (resolveAgentWorkspaceDir,
//! resolveAgentDir, resolveAgentIdsByWorkspacePath) remain in TypeScript because
//! they use realpath, process.env, and path.resolve. This module handles the
//! pure-logic subset.

mod resolve;

pub use resolve::{
    // Constants
    DEFAULT_ACCOUNT_ID, DEFAULT_AGENT_ID, DEFAULT_MAIN_KEY,
    // Types
    AgentEntry, ParsedAgentSessionKey, ResolvedAgentConfig, SessionAgentIds,
    SessionKeyChatType, SessionKeyShape,
    // Agent registry
    list_agent_entries, list_agent_ids, resolve_agent_config, resolve_agent_effective_model_primary,
    resolve_agent_explicit_model_primary, resolve_agent_model_fallbacks_override,
    resolve_agent_model_primary, resolve_agent_skills_filter, resolve_default_agent_id,
    resolve_session_agent_id, resolve_session_agent_ids,
    // Model fallback resolution
    resolve_agent_model_fallback_values, resolve_run_model_fallbacks_override,
    has_configured_model_fallbacks, resolve_effective_model_fallbacks,
    // Session key parsing
    parse_agent_session_key, resolve_agent_id_from_session_key, normalize_main_key,
    classify_session_key_shape, to_agent_request_session_key, to_agent_store_session_key,
    build_agent_main_session_key, build_agent_peer_session_key, build_group_history_key,
    resolve_thread_session_keys, derive_session_chat_type,
    // Session key predicates
    is_cron_session_key, is_cron_run_session_key, is_subagent_session_key,
    get_subagent_depth, is_acp_session_key, resolve_thread_parent_session_key,
    // Agent/account ID utilities
    normalize_agent_id, is_valid_agent_id, sanitize_agent_id, resolve_fallback_agent_id,
    normalize_account_id,
};
