//! Model selection, parsing, and normalization.
//!
//! Mirrors `src/agents/models/model-selection.ts`, `src/agents/provider-id.ts`,
//! `src/agents/models/model-id-normalization.ts`, and
//! `src/agents/models/model-ref-profile.ts`.

mod provider_id;
mod selection;

pub use provider_id::{
    find_normalized_provider_key, find_normalized_provider_value, normalize_provider_id,
    normalize_provider_id_for_auth,
};
pub use selection::{
    build_configured_allowlist_keys, build_model_alias_index, infer_unique_provider_from_configured_models,
    is_cli_provider, legacy_model_key, model_key, normalize_google_model_id,
    normalize_model_ref, normalize_model_selection, parse_model_ref,
    resolve_allowlist_model_key, resolve_model_ref_from_string, split_trailing_auth_profile,
    ModelAliasIndex, ModelRef, ThinkLevel,
};
