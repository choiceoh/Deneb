//! Config schema tree.
//!
//! 1:1 port of `gateway-go/internal/config/schema.go`.

use sha2::{Digest, Sha256};
use std::collections::HashMap;

use crate::config::paths::DEFAULT_GATEWAY_PORT;

/// A single node in the config schema tree.
#[derive(Debug, Clone)]
pub struct SchemaNode {
    pub type_name: String,
    pub description: String,
    pub default: Option<serde_json::Value>,
    pub enum_values: Vec<String>,
    pub properties: HashMap<String, SchemaNode>,
    pub required: Vec<String>,
}

/// Returns the full config schema tree.
pub fn get_schema() -> SchemaNode {
    SchemaNode {
        type_name: "object".to_string(),
        description: "Deneb configuration schema".to_string(),
        default: None,
        enum_values: vec![],
        required: vec![],
        properties: HashMap::from([
            (
                "gateway".to_string(),
                SchemaNode {
                    type_name: "object".to_string(),
                    description: "Gateway server settings".to_string(),
                    default: None,
                    enum_values: vec![],
                    required: vec![],
                    properties: HashMap::from([
                        (
                            "port".to_string(),
                            SchemaNode {
                                type_name: "number".to_string(),
                                description: "Gateway port".to_string(),
                                default: Some(serde_json::json!(DEFAULT_GATEWAY_PORT)),
                                enum_values: vec![],
                                properties: HashMap::new(),
                                required: vec![],
                            },
                        ),
                        (
                            "mode".to_string(),
                            SchemaNode {
                                type_name: "string".to_string(),
                                description: "Gateway mode".to_string(),
                                default: None,
                                enum_values: vec![
                                    "local".to_string(),
                                    "remote".to_string(),
                                ],
                                properties: HashMap::new(),
                                required: vec![],
                            },
                        ),
                        (
                            "bind".to_string(),
                            SchemaNode {
                                type_name: "string".to_string(),
                                description: "Bind mode".to_string(),
                                default: None,
                                enum_values: vec![
                                    "auto".to_string(),
                                    "lan".to_string(),
                                    "loopback".to_string(),
                                    "custom".to_string(),
                                    "tailnet".to_string(),
                                ],
                                properties: HashMap::new(),
                                required: vec![],
                            },
                        ),
                    ]),
                },
            ),
            (
                "logging".to_string(),
                SchemaNode {
                    type_name: "object".to_string(),
                    description: "Logging configuration".to_string(),
                    default: None,
                    enum_values: vec![],
                    required: vec![],
                    properties: HashMap::from([
                        (
                            "level".to_string(),
                            SchemaNode {
                                type_name: "string".to_string(),
                                description: "Log level".to_string(),
                                default: None,
                                enum_values: vec![
                                    "debug".to_string(),
                                    "info".to_string(),
                                    "warn".to_string(),
                                    "error".to_string(),
                                ],
                                properties: HashMap::new(),
                                required: vec![],
                            },
                        ),
                        (
                            "file".to_string(),
                            SchemaNode {
                                type_name: "string".to_string(),
                                description: "Log file path".to_string(),
                                default: None,
                                enum_values: vec![],
                                properties: HashMap::new(),
                                required: vec![],
                            },
                        ),
                    ]),
                },
            ),
            (
                "session".to_string(),
                SchemaNode {
                    type_name: "object".to_string(),
                    description: "Session configuration".to_string(),
                    default: None,
                    enum_values: vec![],
                    required: vec![],
                    properties: HashMap::from([(
                        "mainKey".to_string(),
                        SchemaNode {
                            type_name: "string".to_string(),
                            description: "Main session key".to_string(),
                            default: Some(serde_json::json!("main")),
                            enum_values: vec![],
                            properties: HashMap::new(),
                            required: vec![],
                        },
                    )]),
                },
            ),
            (
                "agents".to_string(),
                SchemaNode {
                    type_name: "object".to_string(),
                    description: "Agent runtime configuration".to_string(),
                    default: None,
                    enum_values: vec![],
                    required: vec![],
                    properties: HashMap::from([(
                        "maxConcurrent".to_string(),
                        SchemaNode {
                            type_name: "number".to_string(),
                            description: "Maximum concurrent agents".to_string(),
                            default: Some(serde_json::json!(8)),
                            enum_values: vec![],
                            properties: HashMap::new(),
                            required: vec![],
                        },
                    )]),
                },
            ),
        ]),
    }
}

/// Find a schema node by dotted path (e.g. "gateway.port").
pub fn lookup_schema(path: &str) -> Option<SchemaNode> {
    let schema = get_schema();
    if path.is_empty() {
        return Some(schema);
    }

    let mut current = schema;
    for part in path.split('.') {
        let node = current.properties.remove(part)?;
        current = node;
    }
    Some(current)
}

/// Compute a SHA-256 hex hash of a string.
pub fn hash_string(s: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(s.as_bytes());
    hex::encode(hasher.finalize())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn lookup_root() {
        let node = lookup_schema("").expect("root");
        assert_eq!(node.type_name, "object");
        assert!(!node.properties.is_empty());
    }

    #[test]
    fn lookup_gateway() {
        let node = lookup_schema("gateway").expect("gateway");
        assert_eq!(node.type_name, "object");
        assert!(node.properties.contains_key("port"));
    }

    #[test]
    fn lookup_gateway_port() {
        let node = lookup_schema("gateway.port").expect("gateway.port");
        assert_eq!(node.type_name, "number");
        assert_eq!(node.default, Some(serde_json::json!(DEFAULT_GATEWAY_PORT)));
    }

    #[test]
    fn lookup_gateway_mode_enums() {
        let node = lookup_schema("gateway.mode").expect("gateway.mode");
        assert!(node.enum_values.contains(&"local".to_string()));
        assert!(node.enum_values.contains(&"remote".to_string()));
    }

    #[test]
    fn lookup_logging_level_enums() {
        let node = lookup_schema("logging.level").expect("logging.level");
        assert!(node.enum_values.contains(&"debug".to_string()));
        assert!(node.enum_values.contains(&"info".to_string()));
    }

    #[test]
    fn lookup_nonexistent() {
        assert!(lookup_schema("nonexistent.path").is_none());
    }

    #[test]
    fn hash_string_deterministic() {
        let h1 = hash_string("hello");
        let h2 = hash_string("hello");
        assert_eq!(h1, h2);
        assert_ne!(h1, hash_string("world"));
    }

    #[test]
    fn hash_string_known_vector() {
        // SHA-256 of "hello"
        assert_eq!(
            hash_string("hello"),
            "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
        );
    }
}
