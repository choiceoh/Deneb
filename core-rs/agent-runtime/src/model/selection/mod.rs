//! Model reference parsing, normalization, and key generation.
//!
//! Mirrors `src/agents/models/model-selection.ts`. Keep in sync.

mod allowlist;
mod catalog;
mod keys;
mod normalize;
mod parse;
mod resolve;
mod thinking;
pub mod types;

pub use allowlist::{
    build_allowed_model_set, build_configured_allowlist_keys, build_model_alias_index,
    resolve_allowed_model_ref, resolve_allowlist_model_key,
};
pub use catalog::{
    find_model_in_catalog, get_model_ref_status, model_supports_document, model_supports_vision,
    resolve_reasoning_default,
};
pub use keys::{legacy_model_key, model_key};
pub use normalize::{normalize_google_model_id, normalize_model_ref, normalize_model_selection};
pub use parse::{parse_model_ref, resolve_model_ref_from_string, split_trailing_auth_profile};
pub use resolve::{
    infer_unique_provider_from_configured_models, is_cli_provider,
    resolve_agent_model_primary_value, resolve_configured_model_ref,
    resolve_default_model_for_agent, resolve_hooks_gmail_model,
    resolve_subagent_configured_model_selection, resolve_subagent_spawn_model_selection,
    to_agent_model_list_like,
};
pub use thinking::{resolve_thinking_default, resolve_thinking_default_for_model};
pub use types::{
    AllowedModelSet, ModelAliasIndex, ModelCatalogEntry, ModelInputType, ModelRef, ModelRefStatus,
    ProviderConfigEntry, ProviderModelEntry, ResolveAllowedModelRefParams, ThinkLevel,
    ThinkingCatalogEntry,
};
