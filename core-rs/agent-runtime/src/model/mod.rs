//! Model selection, parsing, normalization, catalog, and allowlist support.
//!
//! Mirrors `src/agents/models/model-selection.ts`, `src/agents/provider-id.ts`,
//! `src/agents/models/model-id-normalization.ts`,
//! `src/agents/models/model-ref-profile.ts`,
//! `src/auto-reply/thinking.shared.ts`, and `src/config/model-input.ts`.

mod provider_id;
mod selection;

pub use provider_id::{
    find_normalized_provider_key, find_normalized_provider_value, normalize_provider_id,
    normalize_provider_id_for_auth,
};
pub use selection::{
    // Config-driven resolution
    build_allowed_model_set,
    build_configured_allowlist_keys,
    build_model_alias_index,
    // Catalog queries
    find_model_in_catalog,
    get_model_ref_status,
    infer_unique_provider_from_configured_models,
    is_cli_provider,
    // Core parsing & normalization
    legacy_model_key,
    model_key,
    model_supports_document,
    model_supports_vision,
    normalize_google_model_id,
    normalize_model_ref,
    normalize_model_selection,
    parse_model_ref,
    // Config helpers
    resolve_agent_model_primary_value,
    resolve_allowed_model_ref,
    resolve_allowlist_model_key,
    resolve_configured_model_ref,
    resolve_default_model_for_agent,
    resolve_hooks_gmail_model,
    resolve_model_ref_from_string,
    resolve_reasoning_default,
    resolve_subagent_configured_model_selection,
    resolve_subagent_spawn_model_selection,
    resolve_thinking_default,
    resolve_thinking_default_for_model,
    split_trailing_auth_profile,
    to_agent_model_list_like,
    // Types
    AllowedModelSet,
    ModelAliasIndex,
    ModelCatalogEntry,
    ModelInputType,
    ModelRef,
    ModelRefStatus,
    ProviderConfigEntry,
    ProviderModelEntry,
    ResolveAllowedModelRefParams,
    ThinkLevel,
    ThinkingCatalogEntry,
};
