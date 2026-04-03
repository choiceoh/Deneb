//! Secrets schema validators — mirrors `src/gateway/protocol/schema/secrets.ts`.

use crate::protocol::validation::*;

define_schema! { pub fn validate_secrets_reload_params {} }

define_schema! {
    pub fn validate_secrets_resolve_params {
        [req "commandName" => non_empty_string],
        [req "targetIds" => custom(check_non_empty_string_array)],
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_reload_valid() {
        let mut e = Vec::new();
        validate_secrets_reload_params(&json!({}), "", &mut e);
        assert!(e.is_empty());
    }

    #[test]
    fn test_resolve_valid() {
        let mut e = Vec::new();
        validate_secrets_resolve_params(
            &json!({"commandName": "cmd", "targetIds": ["a", "b"]}),
            "",
            &mut e,
        );
        assert!(e.is_empty());
    }

    #[test]
    fn test_resolve_missing() {
        let mut e = Vec::new();
        validate_secrets_resolve_params(&json!({}), "", &mut e);
        assert!(e.len() >= 2);
    }
}
