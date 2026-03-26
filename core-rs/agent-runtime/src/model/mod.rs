//! Model selection, parsing, normalization, and catalog support.
//!
//! Mirrors `src/agents/models/model-selection.ts`, `src/agents/provider-id.ts`,
//! `src/agents/models/model-id-normalization.ts`,
//! `src/agents/models/model-ref-profile.ts`, and
//! `src/auto-reply/thinking.shared.ts`.

mod provider_id;
mod selection;

pub use provider_id::{
    find_normalized_provider_key, find_normalized_provider_value, normalize_provider_id,
    normalize_provider_id_for_auth,
};
pub use selection::{
    // Core parsing & normalization
    legacy_model_key, model_key, normalize_google_model_id, normalize_model_ref,
    normalize_model_selection, parse_model_ref, split_trailing_auth_profile,
    // Types
    ModelAliasIndex, ModelCatalogEntry, ModelInputType, ModelRef, ModelRefStatus,
    ThinkLevel, ThinkingCatalogEntry,
    // Config-driven resolution
    build_configured_allowlist_keys, build_model_alias_index,
    infer_unique_provider_from_configured_models, is_cli_provider,
    resolve_allowlist_model_key, resolve_model_ref_from_string,
    // Catalog queries
    find_model_in_catalog, get_model_ref_status, model_supports_document,
    model_supports_vision, resolve_reasoning_default, resolve_thinking_default_for_model,
};
