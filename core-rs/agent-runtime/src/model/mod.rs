//! Model selection, parsing, and normalization.
//!
//! Mirrors `src/agents/models/model-selection.ts` and `src/agents/provider-id.ts`.

mod provider_id;
mod selection;

pub use provider_id::{
    find_normalized_provider_key, find_normalized_provider_value, normalize_provider_id,
    normalize_provider_id_for_auth,
};
pub use selection::{
    model_key, legacy_model_key, normalize_model_ref, parse_model_ref, ModelRef, ThinkLevel,
};
