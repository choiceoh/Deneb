//! Legacy name compatibility for state directories and config files.
//!
//! Deneb was previously shipped under different brand names. These constants
//! and helpers allow the current release to transparently migrate users who
//! still have the old directory / file names on disk.
//!
//! When a legacy name can be removed, delete the corresponding entry from the
//! slice and its associated tests — the compiler will point out every consumer.

/// Legacy state directory names, in newest-to-oldest order.
pub const LEGACY_STATE_DIRNAMES: &[&str] = &[".clawdbot", ".moldbot", ".moltbot"];

/// Legacy config file names, in newest-to-oldest order.
pub const LEGACY_CONFIG_FILENAMES: &[&str] = &["clawdbot.json", "moldbot.json", "moltbot.json"];
