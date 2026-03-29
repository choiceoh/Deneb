//! Legacy name compatibility for state directories and config files.
//!
//! Deneb was previously shipped under different brand names. These constants
//! and helpers allow the current release to transparently migrate users who
//! still have the old directory / file names on disk.
//!
//! When a legacy name can be removed, delete the corresponding entry from the
//! slice and its associated tests — the compiler will point out every consumer.

use std::path::{Path, PathBuf};

/// Legacy state directory names, in newest-to-oldest order.
pub const LEGACY_STATE_DIRNAMES: &[&str] = &[".clawdbot", ".moldbot", ".moltbot"];

/// Legacy config file names, in newest-to-oldest order.
pub const LEGACY_CONFIG_FILENAMES: &[&str] = &["clawdbot.json", "moldbot.json", "moltbot.json"];

/// Return the first existing legacy state directory under `home`, or `None`.
///
/// Searches `LEGACY_STATE_DIRNAMES` in order; returns on the first hit.
pub fn find_legacy_state_dir(home: &Path) -> Option<PathBuf> {
    LEGACY_STATE_DIRNAMES
        .iter()
        .map(|name| home.join(name))
        .find(|p| p.exists())
}

/// Return the first existing legacy config file inside `state_dir`, or `None`.
///
/// Searches `LEGACY_CONFIG_FILENAMES` in order; returns on the first hit.
pub fn find_legacy_config_file(state_dir: &Path) -> Option<PathBuf> {
    LEGACY_CONFIG_FILENAMES
        .iter()
        .map(|name| state_dir.join(name))
        .find(|p| p.exists())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn find_state_dir_returns_none_when_all_absent() {
        let tmp = tempfile::tempdir().unwrap();
        assert_eq!(find_legacy_state_dir(tmp.path()), None);
    }

    #[test]
    fn find_state_dir_returns_first_match() {
        let tmp = tempfile::tempdir().unwrap();
        // Only .moldbot exists — .clawdbot is absent.
        let moldbot = tmp.path().join(".moldbot");
        fs::create_dir_all(&moldbot).unwrap();
        assert_eq!(find_legacy_state_dir(tmp.path()), Some(moldbot));
    }

    #[test]
    fn find_state_dir_prefers_earlier_entry() {
        let tmp = tempfile::tempdir().unwrap();
        let clawdbot = tmp.path().join(".clawdbot");
        let moldbot = tmp.path().join(".moldbot");
        fs::create_dir_all(&clawdbot).unwrap();
        fs::create_dir_all(&moldbot).unwrap();
        // .clawdbot appears first in the slice → it is returned.
        assert_eq!(find_legacy_state_dir(tmp.path()), Some(clawdbot));
    }

    #[test]
    fn find_config_file_returns_none_when_all_absent() {
        let tmp = tempfile::tempdir().unwrap();
        assert_eq!(find_legacy_config_file(tmp.path()), None);
    }

    #[test]
    fn find_config_file_returns_first_match() {
        let tmp = tempfile::tempdir().unwrap();
        let moldbot_json = tmp.path().join("moldbot.json");
        fs::write(&moldbot_json, b"{}").unwrap();
        assert_eq!(find_legacy_config_file(tmp.path()), Some(moldbot_json));
    }

    #[test]
    fn find_config_file_prefers_earlier_entry() {
        let tmp = tempfile::tempdir().unwrap();
        let clawdbot_json = tmp.path().join("clawdbot.json");
        let moldbot_json = tmp.path().join("moldbot.json");
        fs::write(&clawdbot_json, b"{}").unwrap();
        fs::write(&moldbot_json, b"{}").unwrap();
        assert_eq!(find_legacy_config_file(tmp.path()), Some(clawdbot_json));
    }
}
