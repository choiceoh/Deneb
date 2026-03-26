//! Agent scope — registry, config resolution, and ID normalization.
//!
//! Mirrors `src/agents/agent-scope.ts`. Keep in sync.
//!
//! Note: Functions that require filesystem access (resolveAgentWorkspaceDir,
//! resolveAgentDir, resolveAgentIdsByWorkspacePath) remain in TypeScript because
//! they use realpath, process.env, and path.resolve. This module handles the
//! pure-logic subset.

mod resolve;

pub use resolve::{
    list_agent_entries, list_agent_ids, resolve_agent_config, resolve_agent_effective_model_primary,
    resolve_agent_explicit_model_primary, resolve_agent_model_fallbacks_override,
    resolve_agent_skills_filter, resolve_default_agent_id, resolve_session_agent_ids,
    AgentEntry, ResolvedAgentConfig, SessionAgentIds,
};
