use std::path::PathBuf;

/// Resolve the home directory, respecting `DENEB_HOME` override.
pub fn resolve_home_dir() -> PathBuf {
    if let Ok(v) = std::env::var("DENEB_HOME") {
        let v = v.trim().to_string();
        if !v.is_empty() {
            return expand_tilde(&v);
        }
    }
    dirs::home_dir().expect("cannot determine home directory")
}

/// Expand a leading `~` to the user's home directory.
fn expand_tilde(path: &str) -> PathBuf {
    if let Some(rest) = path.strip_prefix("~/") {
        let home = dirs::home_dir().expect("cannot determine home directory");
        home.join(rest)
    } else if path == "~" {
        dirs::home_dir().expect("cannot determine home directory")
    } else {
        PathBuf::from(path)
    }
}

/// Check if an environment variable is set to a truthy value (1, true, yes).
pub fn is_truthy_env(key: &str) -> bool {
    std::env::var(key)
        .ok()
        .map(|v| matches!(v.trim().to_lowercase().as_str(), "1" | "true" | "yes"))
        .unwrap_or(false)
}

/// Get a trimmed, non-empty env var or `None`.
pub fn get_env_trimmed(key: &str) -> Option<String> {
    std::env::var(key).ok().and_then(|v| {
        let trimmed = v.trim().to_string();
        if trimmed.is_empty() {
            None
        } else {
            Some(trimmed)
        }
    })
}
