use std::path::PathBuf;

/// Resolve the home directory, respecting `DENEB_HOME` override.
#[allow(clippy::expect_used)]
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
#[allow(clippy::expect_used)]
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
#[allow(dead_code)]
pub fn is_truthy_env(key: &str) -> bool {
    std::env::var(key).ok().is_some_and(|v| {
        let normalized = v.trim();
        normalized == "1"
            || normalized.eq_ignore_ascii_case("true")
            || normalized.eq_ignore_ascii_case("yes")
    })
}

/// Get a trimmed, non-empty env var or `None`.
pub fn get_env_trimmed(key: &str) -> Option<String> {
    std::env::var(key).ok().and_then(|v| {
        let trimmed = v.trim();
        (!trimmed.is_empty()).then(|| trimmed.to_owned())
    })
}

#[cfg(test)]
mod tests {
    use super::{get_env_trimmed, is_truthy_env, resolve_home_dir};
    use std::path::PathBuf;
    use std::sync::{Mutex, OnceLock};

    fn env_lock() -> &'static Mutex<()> {
        static LOCK: OnceLock<Mutex<()>> = OnceLock::new();
        LOCK.get_or_init(|| Mutex::new(()))
    }

    fn home_dir() -> PathBuf {
        dirs::home_dir().expect("home directory should exist in test environment")
    }

    #[test]
    fn resolve_home_dir_uses_deneb_home_when_set() {
        let _guard = env_lock().lock().expect("mutex poisoned");
        std::env::set_var("DENEB_HOME", "/tmp/deneb-home");
        let resolved = resolve_home_dir();
        std::env::remove_var("DENEB_HOME");

        assert_eq!(resolved, PathBuf::from("/tmp/deneb-home"));
    }

    #[test]
    fn resolve_home_dir_trims_and_expands_tilde() {
        let _guard = env_lock().lock().expect("mutex poisoned");
        std::env::set_var("DENEB_HOME", "  ~/deneb-custom  ");
        let resolved = resolve_home_dir();
        std::env::remove_var("DENEB_HOME");

        assert_eq!(resolved, home_dir().join("deneb-custom"));
    }

    #[test]
    fn resolve_home_dir_falls_back_when_deneb_home_blank() {
        let _guard = env_lock().lock().expect("mutex poisoned");
        std::env::set_var("DENEB_HOME", "   ");
        let resolved = resolve_home_dir();
        std::env::remove_var("DENEB_HOME");

        assert_eq!(resolved, home_dir());
    }

    #[test]
    fn is_truthy_env_accepts_common_truthy_values() {
        let _guard = env_lock().lock().expect("mutex poisoned");
        for value in ["1", "true", "TRUE", " yes "] {
            std::env::set_var("DENEB_TRUTHY_TEST", value);
            assert!(
                is_truthy_env("DENEB_TRUTHY_TEST"),
                "expected {value} to be truthy"
            );
        }
        std::env::remove_var("DENEB_TRUTHY_TEST");
    }

    #[test]
    fn is_truthy_env_rejects_non_truthy_values_and_missing_var() {
        let _guard = env_lock().lock().expect("mutex poisoned");
        for value in ["0", "false", "no", "random"] {
            std::env::set_var("DENEB_TRUTHY_TEST", value);
            assert!(
                !is_truthy_env("DENEB_TRUTHY_TEST"),
                "expected {value} to be non-truthy"
            );
        }
        std::env::remove_var("DENEB_TRUTHY_TEST");
        assert!(!is_truthy_env("DENEB_TRUTHY_TEST"));
    }

    #[test]
    fn get_env_trimmed_returns_trimmed_string() {
        let _guard = env_lock().lock().expect("mutex poisoned");
        std::env::set_var("DENEB_TRIMMED_TEST", "  value  ");
        let value = get_env_trimmed("DENEB_TRIMMED_TEST");
        std::env::remove_var("DENEB_TRIMMED_TEST");

        assert_eq!(value, Some(String::from("value")));
    }

    #[test]
    fn get_env_trimmed_returns_none_for_blank_or_missing() {
        let _guard = env_lock().lock().expect("mutex poisoned");
        std::env::set_var("DENEB_TRIMMED_TEST", "   ");
        assert_eq!(get_env_trimmed("DENEB_TRIMMED_TEST"), None);
        std::env::remove_var("DENEB_TRIMMED_TEST");

        assert_eq!(get_env_trimmed("DENEB_TRIMMED_TEST"), None);
    }
}
