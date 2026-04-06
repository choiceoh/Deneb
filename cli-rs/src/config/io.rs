use std::fs;
use std::path::{Path, PathBuf};

use crate::config::types::DenebConfig;
use crate::errors::CliError;

/// Load config from the resolved config path.
/// Returns default config if the file does not exist.
pub fn load_config(config_path: &Path) -> Result<DenebConfig, CliError> {
    if !config_path.exists() {
        return Ok(DenebConfig::default());
    }

    let raw = fs::read_to_string(config_path).map_err(|e| {
        CliError::Config(format!(
            "failed to read config at {}: {e}",
            config_path.display()
        ))
    })?;

    parse_config_json5(&raw, config_path)
}

/// Parse a config string into a `DenebConfig`.
///
/// Fast path: try standard JSON first (`write_config` always writes standard JSON).
/// Fallback: parse as JSON5 to support hand-edited configs with comments or trailing commas.
fn parse_config_json5(raw: &str, path: &Path) -> Result<DenebConfig, CliError> {
    if let Ok(config) = serde_json::from_str::<DenebConfig>(raw) {
        return Ok(config);
    }
    json5::from_str(raw)
        .map_err(|e| CliError::Config(format!("failed to parse config at {}: {e}", path.display())))
}

/// Best-effort config load: returns default config on any error.
pub fn load_config_best_effort(config_path: &Path) -> DenebConfig {
    load_config(config_path).unwrap_or_default()
}

/// Write config back to disk as formatted JSON.
/// Note: JSON5 comments are not preserved on roundtrip (limitation of json5 crate).
pub fn write_config(config_path: &Path, config: &DenebConfig) -> Result<(), CliError> {
    // Ensure parent directory exists
    if let Some(parent) = config_path.parent() {
        fs::create_dir_all(parent)?;
    }

    let json = serde_json::to_string_pretty(config)
        .map_err(|e| CliError::Config(format!("failed to serialize config: {e}")))?;

    fs::write(config_path, json)?;
    Ok(())
}

/// Check whether a path exists.
pub fn path_exists(path: &Path) -> bool {
    path.exists()
}

/// Return the first existing path from the provided candidates.
pub fn first_existing_path(candidates: &[PathBuf]) -> Option<PathBuf> {
    candidates
        .iter()
        .find(|candidate| candidate.exists())
        .cloned()
}

/// Set a value at a dot-separated path in the config.
pub fn set_config_value(
    config: &mut DenebConfig,
    path: &str,
    value: &serde_json::Value,
) -> Result<(), CliError> {
    validate_config_path(path)?;

    let mut full = serde_json::to_value(&*config)
        .map_err(|e| CliError::Config(format!("failed to serialize config: {e}")))?;

    let segments: Vec<&str> = path.split('.').collect();
    let mut current = &mut full;
    for (i, segment) in segments.iter().enumerate() {
        if i == segments.len() - 1 {
            current[segment] = value.clone();
        } else {
            if current.get(segment).is_none() || !current[segment].is_object() {
                current[segment] = serde_json::json!({});
            }
            current = &mut current[segment];
        }
    }

    *config = serde_json::from_value(full)
        .map_err(|e| CliError::Config(format!("failed to deserialize config: {e}")))?;
    Ok(())
}

/// Unset (remove) a value at a dot-separated path in the config.
pub fn unset_config_value(config: &mut DenebConfig, path: &str) -> Result<bool, CliError> {
    validate_config_path(path)?;

    let mut full = serde_json::to_value(&*config)
        .map_err(|e| CliError::Config(format!("failed to serialize config: {e}")))?;

    let segments: Vec<&str> = path.split('.').collect();
    let mut current = &mut full;

    for (i, segment) in segments.iter().enumerate() {
        if i == segments.len() - 1 {
            let removed = current
                .as_object_mut()
                .map(|obj| obj.remove(*segment))
                .is_some();
            *config = serde_json::from_value(full)
                .map_err(|e| CliError::Config(format!("failed to deserialize config: {e}")))?;
            return Ok(removed);
        }
        if current.get(segment).is_none() {
            return Ok(false);
        }
        current = &mut current[segment];
    }

    Ok(false)
}

fn validate_config_path(path: &str) -> Result<(), CliError> {
    if path.is_empty() {
        return Err(CliError::Config(
            "config path must not be empty".to_string(),
        ));
    }

    if path.split('.').any(str::is_empty) {
        return Err(CliError::Config(format!(
            "invalid config path '{path}': empty path segment",
        )));
    }

    Ok(())
}
