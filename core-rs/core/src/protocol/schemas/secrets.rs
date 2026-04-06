//! Secrets schema validators — mirrors `src/gateway/protocol/schema/secrets.ts`.

use crate::protocol::validation::*;

define_schema! { pub fn validate_secrets_reload_params {} }

define_schema! {
    pub fn validate_secrets_resolve_params {
        [req "commandName" => non_empty_string],
        [req "targetIds" => custom(check_non_empty_string_array)],
    }
}
