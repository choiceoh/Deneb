//! Legacy name compatibility for state directories and config files.
//!
//! 1:1 port of `gateway-go/internal/config/legacy_compat.go`.
//!
//! Deneb was previously shipped under different brand names. These constants
//! and helpers allow the current release to transparently migrate users who
//! still have the old directory / file names on disk.

use std::path::{Path, PathBuf};

/// Legacy state directory names, in newest-to-oldest order.
pub const LEGACY_STATE_DIRNAMES: &[&str] = &[".clawdbot", ".moldbot", ".moltbot"];

/// Legacy config file names, in newest-to-oldest order.
pub const LEGACY_CONFIG_FILENAMES: &[&str] = &["clawdbot.json", "moldbot.json", "moltbot.json"];

/// Return the first existing legacy state directory under `home`, or `None`.
pub fn find_legacy_state_dir(home: &Path) -> Option<PathBuf> {
    LEGACY_STATE_DIRNAMES
        .iter()
        .map(|name| home.join(name))
        .find(|p| p.is_dir())
}

/// Return the first existing legacy config file inside `state_dir`, or `None`.
pub fn find_legacy_config_file(state_dir: &Path) -> Option<PathBuf> {
    LEGACY_CONFIG_FILENAMES
        .iter()
        .map(|name| state_dir.join(name))
        .find(|p| p.is_file())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn find_state_dir_returns_none_when_all_absent() {
        let tmp = tempfile::tempdir().expect("tempdir");
        assert_eq!(find_legacy_state_dir(tmp.path()), None);
    }

    #[test]
    fn find_state_dir_returns_first_match() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let moldbot = tmp.path().join(".moldbot");
        fs::create_dir_all(&moldbot).expect("mkdir");
        assert_eq!(find_legacy_state_dir(tmp.path()), Some(moldbot));
    }

    #[test]
    fn find_state_dir_prefers_earlier_entry() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let clawdbot = tmp.path().join(".clawdbot");
        let moldbot = tmp.path().join(".moldbot");
        fs::create_dir_all(&clawdbot).expect("mkdir");
        fs::create_dir_all(&moldbot).expect("mkdir");
        assert_eq!(find_legacy_state_dir(tmp.path()), Some(clawdbot));
    }

    #[test]
    fn find_config_file_returns_none_when_all_absent() {
        let tmp = tempfile::tempdir().expect("tempdir");
        assert_eq!(find_legacy_config_file(tmp.path()), None);
    }

    #[test]
    fn find_config_file_returns_first_match() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let moldbot_json = tmp.path().join("moldbot.json");
        fs::write(&moldbot_json, b"{}").expect("write");
        assert_eq!(find_legacy_config_file(tmp.path()), Some(moldbot_json));
    }

    #[test]
    fn find_config_file_prefers_earlier_entry() {
        let tmp = tempfile::tempdir().expect("tempdir");
        let clawdbot_json = tmp.path().join("clawdbot.json");
        let moldbot_json = tmp.path().join("moldbot.json");
        fs::write(&clawdbot_json, b"{}").expect("write");
        fs::write(&moldbot_json, b"{}").expect("write");
        assert_eq!(find_legacy_config_file(tmp.path()), Some(clawdbot_json));
    }
}
