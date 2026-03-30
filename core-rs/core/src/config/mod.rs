//! Deneb configuration loading, validation, and bootstrap.
//!
//! 1:1 port of `gateway-go/internal/config/`.
//!
//! This module provides the full config pipeline: type definitions, path
//! resolution, .env loading, JSON parsing, validation, default application,
//! auth bootstrap, and runtime config resolution.

pub mod bootstrap;
pub mod dotenv;
pub mod legacy_compat;
pub mod loader;
pub mod paths;
pub mod runtime;
pub mod schema;
pub mod types;

// Re-exports for convenience.
pub use bootstrap::{
    bootstrap_gateway_config, generate_random_token, merge_auth_config, merge_tailscale_config,
    resolve_media_cleanup_ttl_ms, BootstrapOptions, BootstrapResult, ResolvedGatewayAuth,
};
pub use dotenv::load_dotenv_files;
pub use loader::{
    apply_defaults, hash_raw, load_config, load_config_from_default_path, validate_config,
    ConfigIssue, ConfigSnapshot,
};
pub use paths::{
    resolve_agent_workspace_dir, resolve_config_path, resolve_gateway_port, resolve_state_dir,
    ConfigPathPolicy, GatewayPortPolicy, StateDirPolicy, DEFAULT_CONFIG_FILENAME,
    DEFAULT_GATEWAY_PORT, DEFAULT_STATE_DIRNAME,
};
pub use runtime::{
    get_control_ui_allowed_origins, is_loopback_host, is_trusted_proxy_address, is_valid_ipv4,
    normalize_control_ui_base_path, resolve_bind_host, resolve_gateway_runtime_config,
    GatewayRuntimeConfig, RuntimeConfigParams,
};
pub use schema::{get_schema, hash_string, lookup_schema, SchemaNode};
pub use types::DenebConfig;
