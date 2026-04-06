//! Config schema validators — mirrors `src/gateway/protocol/schema/config.ts`.

use crate::protocol::validation::*;

define_schema! { pub fn validate_config_get_params {} }

define_schema! {
    pub fn validate_config_set_params {
        [req "raw" => non_empty_string],
        [opt "baseHash" => non_empty_string],
    }
}

define_schema! {
    fn validate_config_apply_like {
        [req "raw" => non_empty_string],
        [opt "baseHash" => non_empty_string],
        [opt "sessionKey" => string],
        [opt "note" => string],
        [opt "restartDelayMs" => integer(Some(0), None)],
    }
}

pub fn validate_config_apply_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_config_apply_like(value, path, errors);
}

pub fn validate_config_patch_params(
    value: &serde_json::Value,
    path: &str,
    errors: &mut Vec<ValidationError>,
) {
    validate_config_apply_like(value, path, errors);
}

define_schema! { pub fn validate_config_schema_params {} }

define_schema! {
    pub fn validate_config_schema_lookup_params {
        [req "path" => custom(check_config_path)],
    }
}

fn check_config_path(v: &serde_json::Value, p: &str, e: &mut Vec<ValidationError>) {
    check_non_empty_string(v, p, e);
    check_max_length(v, p, 1024, e);
    #[allow(clippy::expect_used)]
    static PATH_RE: std::sync::LazyLock<regex::Regex> = std::sync::LazyLock::new(|| {
        regex::Regex::new(r"^[A-Za-z0-9_./\[\]\-*]+$").expect("valid regex")
    });
    check_pattern(v, p, &PATH_RE, e);
}

define_schema! {
    pub fn validate_update_run_params {
        [opt "sessionKey" => string],
        [opt "note" => string],
        [opt "restartDelayMs" => integer(Some(0), None)],
        [opt "timeoutMs" => integer(Some(1), None)],
    }
}
