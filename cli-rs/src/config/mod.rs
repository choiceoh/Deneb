pub mod io;
pub mod legacy_compat;
pub mod paths;
pub mod types;

pub use io::{load_config, load_config_best_effort, set_config_value, write_config};
pub use paths::DEFAULT_GATEWAY_PORT;
pub use types::DenebConfig;

/// Resolve the state directory by composing env + path-policy + filesystem probes.
pub fn resolve_state_dir() -> std::path::PathBuf {
    let env = crate::env::read_config_env();
    let default_dir = paths::default_state_dir(&env.home_dir);
    let legacy_dirs = paths::legacy_state_dirs(&env.home_dir);

    paths::resolve_state_dir_policy(
        &env.home_dir,
        env.state_dir_override.as_deref(),
        env.fast_mode,
        io::path_exists(&default_dir),
        io::first_existing_path(&legacy_dirs).as_deref(),
    )
}

/// Resolve the config path by composing env + path-policy + filesystem probes.
pub fn resolve_config_path() -> std::path::PathBuf {
    let env = crate::env::read_config_env();
    let state_dir = resolve_state_dir();
    let candidates = paths::config_candidates(&state_dir);

    paths::resolve_config_path_policy(
        &state_dir,
        env.config_path_override.as_deref(),
        io::first_existing_path(&candidates).as_deref(),
    )
}

/// Resolve gateway port by composing env + path-policy inputs.
pub fn resolve_gateway_port(config_port: Option<u16>) -> u16 {
    let env = crate::env::read_config_env();
    paths::resolve_gateway_port_policy(config_port, env.gateway_port_override)
}
