//! Agents/models/skills validators — mirrors `src/gateway/protocol/schema/agents-models-skills.ts`.

use crate::protocol::validation::*;

define_schema! { pub fn validate_agents_list_params {} }

define_schema! {
    pub fn validate_agents_create_params {
        [req "name" => non_empty_string],
        [req "workspace" => non_empty_string],
        [opt "emoji" => string],
        [opt "avatar" => string],
    }
}

define_schema! {
    pub fn validate_agents_update_params {
        [req "agentId" => non_empty_string],
        [opt "name" => non_empty_string],
        [opt "workspace" => non_empty_string],
        [opt "model" => non_empty_string],
        [opt "avatar" => string],
    }
}

define_schema! {
    pub fn validate_agents_delete_params {
        [req "agentId" => non_empty_string],
        [opt "deleteFiles" => boolean],
    }
}

define_schema! {
    pub fn validate_agents_files_list_params {
        [req "agentId" => non_empty_string],
    }
}

define_schema! {
    pub fn validate_agents_files_get_params {
        [req "agentId" => non_empty_string],
        [req "name" => non_empty_string],
    }
}

define_schema! {
    pub fn validate_agents_files_set_params {
        [req "agentId" => non_empty_string],
        [req "name" => non_empty_string],
        [req "content" => string],
    }
}

define_schema! { pub fn validate_models_list_params {} }

define_schema! {
    pub fn validate_skills_status_params {
        [opt "agentId" => non_empty_string],
    }
}

define_schema! { pub fn validate_skills_bins_params {} }

define_schema! {
    pub fn validate_skills_install_params {
        [req "name" => non_empty_string],
        [req "installId" => non_empty_string],
        [opt "timeoutMs" => integer(Some(1000), None)],
    }
}

define_schema! {
    pub fn validate_skills_update_params {
        [req "skillKey" => non_empty_string],
        [opt "enabled" => boolean],
        [opt "apiKey" => string],
        [opt "env" => custom(check_string_record)],
    }
}

/// Validate `Record<NonEmptyString, String>`: keys must be non-empty, values must be strings.
fn check_string_record(v: &serde_json::Value, p: &str, e: &mut Vec<ValidationError>) {
    if require_object(v, p, e) {
        if let Some(map) = v.as_object() {
            for (key, val) in map {
                if key.is_empty() {
                    e.push(ValidationError {
                        path: p.to_string(),
                        message: "key must be non-empty, got \"\"".to_string(),
                        keyword: "minLength",
                    });
                }
                check_string(val, &format!("{p}/{key}"), e);
            }
        }
    }
}

define_schema! {
    pub fn validate_tools_catalog_params {
        [opt "agentId" => non_empty_string],
        [opt "includePlugins" => boolean],
    }
}
