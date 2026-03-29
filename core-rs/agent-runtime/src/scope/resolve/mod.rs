//! Agent registry resolution — pure logic for config-driven agent management.
//!
//! Mirrors `src/agents/agent-scope.ts` (pure-logic subset). Keep in sync.
//! Filesystem-dependent functions remain in TypeScript.

mod agent_ids;
mod config;
mod session_keys;

pub use agent_ids::{
    is_valid_agent_id, normalize_account_id, normalize_agent_id, normalize_main_key,
    sanitize_agent_id, DEFAULT_ACCOUNT_ID, DEFAULT_AGENT_ID, DEFAULT_MAIN_KEY,
};

pub use session_keys::{
    build_agent_main_session_key, build_agent_peer_session_key, build_group_history_key,
    classify_session_key_shape, derive_session_chat_type, get_subagent_depth, is_acp_session_key,
    is_cron_run_session_key, is_cron_session_key, is_subagent_session_key, parse_agent_session_key,
    resolve_agent_id_from_session_key, resolve_thread_parent_session_key,
    resolve_thread_session_keys, to_agent_request_session_key, to_agent_store_session_key,
    BuildAgentPeerSessionKeyParams, ParsedAgentSessionKey, SessionKeyChatType, SessionKeyShape,
};

pub use config::{
    has_configured_model_fallbacks, list_agent_entries, list_agent_ids, resolve_agent_config,
    resolve_agent_effective_model_primary, resolve_agent_explicit_model_primary,
    resolve_agent_model_fallback_values, resolve_agent_model_fallbacks_override,
    resolve_agent_model_primary, resolve_agent_skills_filter, resolve_default_agent_id,
    resolve_effective_model_fallbacks, resolve_fallback_agent_id,
    resolve_run_model_fallbacks_override, resolve_session_agent_id, resolve_session_agent_ids,
    AgentEntry, ResolvedAgentConfig, SessionAgentIds,
};
