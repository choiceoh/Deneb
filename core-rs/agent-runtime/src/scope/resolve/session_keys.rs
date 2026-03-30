//! Session key parsing, building, classification, and predicate checks.
//!
//! Mirrors `src/routing/session-key.ts` and `src/sessions/session-key-utils.ts`
//! (pure-logic subset). Keep in sync.

use regex::Regex;
use std::sync::LazyLock;

use super::agent_ids::{
    normalize_account_id, normalize_agent_id, normalize_main_key, DEFAULT_AGENT_ID,
};

/// Parsed agent session key components.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ParsedAgentSessionKey {
    pub agent_id: String,
    pub rest: String,
}

/// Classification of a session key's shape.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SessionKeyShape {
    Missing,
    Agent,
    LegacyOrAlias,
    MalformedAgent,
}

/// Chat type derived from session key structure.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SessionKeyChatType {
    Direct,
    Group,
    Channel,
    Unknown,
}

/// Parameters for `build_agent_peer_session_key`.
pub struct BuildAgentPeerSessionKeyParams<'a> {
    pub agent_id: &'a str,
    pub main_key: Option<&'a str>,
    pub channel: &'a str,
    pub account_id: Option<&'a str>,
    pub peer_kind: Option<&'a str>,
    pub peer_id: Option<&'a str>,
    pub identity_links: Option<&'a std::collections::HashMap<String, Vec<String>>>,
    pub dm_scope: Option<&'a str>,
}

// Pre-compiled regexes used by multiple functions.
#[allow(clippy::expect_used)]
static CRON_RUN_RE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^cron:[^:]+:run:[^:]+$").expect("valid regex"));

/// Normalize `s` to lowercase, falling back to `"unknown"` when empty.
fn normalize_or_unknown(s: &str) -> String {
    let t = s.trim().to_lowercase();
    if t.is_empty() {
        "unknown".to_string()
    } else {
        t
    }
}

/// Parse an agent-scoped session key in canonical format "agent:<id>:<rest>".
/// Returns both the agent ID and the remainder (rest), both normalized to lowercase.
/// Mirrors `src/sessions/session-key-utils.ts#parseAgentSessionKey`. Keep in sync.
pub fn parse_agent_session_key(session_key: &str) -> Option<ParsedAgentSessionKey> {
    let raw = session_key.trim().to_lowercase();
    if raw.is_empty() {
        return None;
    }
    let parts: Vec<&str> = raw.split(':').filter(|s| !s.is_empty()).collect();
    if parts.len() < 3 {
        return None;
    }
    if parts[0] != "agent" {
        return None;
    }
    let agent_id = parts[1].trim();
    let rest = parts[2..].join(":");
    if agent_id.is_empty() || rest.is_empty() {
        return None;
    }
    Some(ParsedAgentSessionKey {
        agent_id: agent_id.to_string(),
        rest,
    })
}

/// Resolve the agent ID from a session key. Falls back to DEFAULT_AGENT_ID.
pub fn resolve_agent_id_from_session_key(session_key: &str) -> String {
    let parsed = parse_agent_session_key(session_key);
    normalize_agent_id(
        parsed
            .as_ref()
            .map(|p| p.agent_id.as_str())
            .unwrap_or(DEFAULT_AGENT_ID),
    )
    .into_owned()
}

/// Classify the shape of a session key.
pub fn classify_session_key_shape(session_key: &str) -> SessionKeyShape {
    let raw = session_key.trim();
    if raw.is_empty() {
        return SessionKeyShape::Missing;
    }
    if parse_agent_session_key(raw).is_some() {
        return SessionKeyShape::Agent;
    }
    if raw.to_lowercase().starts_with("agent:") {
        SessionKeyShape::MalformedAgent
    } else {
        SessionKeyShape::LegacyOrAlias
    }
}

/// Extract the request session key from a store key.
pub fn to_agent_request_session_key(store_key: &str) -> Option<String> {
    let raw = store_key.trim();
    if raw.is_empty() {
        return None;
    }
    parse_agent_session_key(raw)
        .map(|p| p.rest)
        .or_else(|| Some(raw.to_string()))
}

/// Build the store session key from agent ID and request key.
pub fn to_agent_store_session_key(
    agent_id: &str,
    request_key: &str,
    main_key: Option<&str>,
) -> String {
    let raw = request_key.trim();
    if raw.is_empty() || raw.to_lowercase() == super::agent_ids::DEFAULT_MAIN_KEY {
        return build_agent_main_session_key(agent_id, main_key);
    }
    if let Some(parsed) = parse_agent_session_key(raw) {
        return format!("agent:{}:{}", parsed.agent_id, parsed.rest);
    }
    let lowered = raw.to_lowercase();
    if lowered.starts_with("agent:") {
        return lowered;
    }
    format!("agent:{}:{}", normalize_agent_id(agent_id), lowered)
}

/// Build the main session key for an agent.
pub fn build_agent_main_session_key(agent_id: &str, main_key: Option<&str>) -> String {
    let id = normalize_agent_id(agent_id);
    let key = normalize_main_key(main_key.unwrap_or(""));
    format!("agent:{}:{}", id, key)
}

/// Derive the chat type from a session key.
pub fn derive_session_chat_type(session_key: &str) -> SessionKeyChatType {
    let raw = session_key.trim().to_lowercase();
    if raw.is_empty() {
        return SessionKeyChatType::Unknown;
    }
    let scoped = parse_agent_session_key(&raw)
        .map(|p| p.rest)
        .unwrap_or_else(|| raw.clone());
    let mut has_group = false;
    let mut has_channel = false;
    let mut has_direct = false;
    for token in scoped.split(':').filter(|s| !s.is_empty()) {
        match token {
            "group" => has_group = true,
            "channel" => has_channel = true,
            "direct" | "dm" => has_direct = true,
            _ => {}
        }
    }
    if has_group {
        return SessionKeyChatType::Group;
    }
    if has_channel {
        return SessionKeyChatType::Channel;
    }
    if has_direct {
        return SessionKeyChatType::Direct;
    }
    SessionKeyChatType::Unknown
}

/// Check if a session key represents a cron run.
pub fn is_cron_run_session_key(session_key: &str) -> bool {
    parse_agent_session_key(session_key)
        .map(|p| CRON_RUN_RE.is_match(&p.rest))
        .unwrap_or(false)
}

/// Check if a session key is cron-scoped.
pub fn is_cron_session_key(session_key: &str) -> bool {
    parse_agent_session_key(session_key)
        .map(|p| p.rest.starts_with("cron:"))
        .unwrap_or(false)
}

/// Check if a session key is subagent-scoped.
pub fn is_subagent_session_key(session_key: &str) -> bool {
    let raw = session_key.trim();
    if raw.is_empty() {
        return false;
    }
    if raw.to_lowercase().starts_with("subagent:") {
        return true;
    }
    parse_agent_session_key(raw)
        .map(|p| p.rest.starts_with("subagent:"))
        .unwrap_or(false)
}

/// Get the subagent nesting depth from a session key.
/// Uses split to match TS behavior: `raw.split(":subagent:").length - 1`.
pub fn get_subagent_depth(session_key: &str) -> usize {
    let raw = session_key.trim().to_lowercase();
    if raw.is_empty() {
        return 0;
    }
    // split(":subagent:") counts delimiters correctly even when the string
    // starts with "subagent:" (no leading colon). This matches the TS algorithm.
    raw.split(":subagent:").count().saturating_sub(1)
}

/// Check if a session key is ACP-scoped.
pub fn is_acp_session_key(session_key: &str) -> bool {
    let raw = session_key.trim();
    if raw.is_empty() {
        return false;
    }
    let normalized = raw.to_lowercase();
    if normalized.starts_with("acp:") {
        return true;
    }
    parse_agent_session_key(raw)
        .map(|p| p.rest.starts_with("acp:"))
        .unwrap_or(false)
}

/// Resolve the parent session key for a thread (strips ":thread:<id>" or ":topic:<id>" suffix).
/// Session keys are ASCII by convention; lowercase search is done on the same
/// lowercased string to avoid byte-index misalignment across UTF-8 boundaries.
pub fn resolve_thread_parent_session_key(session_key: &str) -> Option<String> {
    let raw = session_key.trim();
    if raw.is_empty() {
        return None;
    }
    // Work entirely on lowercased string, then return the slice length from it.
    // Session keys are ASCII, so byte indices are stable across case transforms.
    let lower = raw.to_lowercase();
    let markers = [":thread:", ":topic:"];
    let mut best: Option<usize> = None;
    for marker in &markers {
        if let Some(candidate) = lower.rfind(marker) {
            best = Some(best.map_or(candidate, |prev| prev.max(candidate)));
        }
    }
    let pos = best?;
    if pos == 0 {
        return None;
    }
    // Use the position on the original raw string. Safe because session keys
    // are ASCII (agent IDs, channel names, peer IDs are all ASCII-constrained).
    // Guard against non-ASCII edge cases by checking char boundary.
    if !raw.is_char_boundary(pos) {
        return None;
    }
    let parent = raw[..pos].trim();
    if parent.is_empty() {
        None
    } else {
        Some(parent.to_string())
    }
}

/// Resolve thread session keys by appending :thread:<id> suffix.
pub fn resolve_thread_session_keys(
    base_session_key: &str,
    thread_id: Option<&str>,
    parent_session_key: Option<&str>,
    use_suffix: bool,
) -> (String, Option<String>) {
    let tid = thread_id.map(|s| s.trim()).unwrap_or("").to_string();
    if tid.is_empty() {
        return (base_session_key.to_string(), None);
    }
    let normalized_thread_id = tid.to_lowercase();
    let session_key = if use_suffix {
        format!("{}:thread:{}", base_session_key, normalized_thread_id)
    } else {
        base_session_key.to_string()
    };
    (session_key, parent_session_key.map(|s| s.to_string()))
}

/// Build a group history key from channel, account, peer kind, and peer ID.
pub fn build_group_history_key(
    channel: &str,
    account_id: Option<&str>,
    peer_kind: &str,
    peer_id: &str,
) -> String {
    let ch = normalize_or_unknown(channel);
    let acct = normalize_account_id(account_id.unwrap_or(""));
    let pid = normalize_or_unknown(peer_id);
    format!("{}:{}:{}:{}", ch, acct, peer_kind, pid)
}

/// Resolve a linked peer ID from identity links mapping.
/// Mirrors `src/routing/session-key.ts#resolveLinkedPeerId`. Keep in sync.
fn resolve_linked_peer_id(
    identity_links: &std::collections::HashMap<String, Vec<String>>,
    channel: &str,
    peer_id: &str,
) -> Option<String> {
    let pid = peer_id.trim();
    if pid.is_empty() {
        return None;
    }
    // At most two candidates: bare peer ID and channel-scoped peer ID.
    let raw_candidate = pid.to_lowercase();
    let ch = channel.trim().to_lowercase();
    let scoped_candidate = if ch.is_empty() {
        String::new()
    } else {
        format!("{}:{}", ch, raw_candidate)
    };
    let candidates = [raw_candidate, scoped_candidate];

    for (canonical, ids) in identity_links {
        let canonical_name = canonical.trim();
        if canonical_name.is_empty() {
            continue;
        }
        for id in ids {
            let normalized = id.trim().to_lowercase();
            if !normalized.is_empty()
                && candidates.iter().any(|c| !c.is_empty() && *c == normalized)
            {
                return Some(canonical_name.to_string());
            }
        }
    }
    None
}

/// Build the session key for a direct DM scope.
fn build_direct_dm_session_key(
    aid: &str,
    agent_id: &str,
    main_key: Option<&str>,
    channel: &str,
    account_id: Option<&str>,
    peer_id: Option<&str>,
    identity_links: Option<&std::collections::HashMap<String, Vec<String>>>,
    dm_scope: &str,
) -> String {
    let mut pid = peer_id.unwrap_or("").trim().to_string();

    // Resolve identity links for non-main DM scopes.
    if dm_scope != "main" {
        if let Some(links) = identity_links {
            if let Some(linked) = resolve_linked_peer_id(links, channel, &pid) {
                pid = linked;
            }
        }
    }
    let pid = pid.to_lowercase();

    if dm_scope == "per-account-channel-peer" && !pid.is_empty() {
        let ch = normalize_or_unknown(channel);
        let acct = normalize_account_id(account_id.unwrap_or(""));
        return format!("agent:{}:{}:{}:direct:{}", aid, ch, acct, pid);
    }
    if dm_scope == "per-channel-peer" && !pid.is_empty() {
        let ch = normalize_or_unknown(channel);
        return format!("agent:{}:{}:direct:{}", aid, ch, pid);
    }
    if dm_scope == "per-peer" && !pid.is_empty() {
        return format!("agent:{}:direct:{}", aid, pid);
    }
    build_agent_main_session_key(agent_id, main_key)
}

/// Build a peer-scoped session key for an agent.
/// Mirrors `src/routing/session-key.ts#buildAgentPeerSessionKey`. Keep in sync.
pub fn build_agent_peer_session_key(params: &BuildAgentPeerSessionKeyParams<'_>) -> String {
    let aid = normalize_agent_id(params.agent_id);
    let kind = params.peer_kind.unwrap_or("direct");

    if kind == "direct" {
        return build_direct_dm_session_key(
            &aid,
            params.agent_id,
            params.main_key,
            params.channel,
            params.account_id,
            params.peer_id,
            params.identity_links,
            params.dm_scope.unwrap_or("main"),
        );
    }

    let ch = normalize_or_unknown(params.channel);
    let pid = normalize_or_unknown(params.peer_id.unwrap_or(""));
    format!("agent:{}:{}:{}:{}", aid, ch, kind, pid)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_agent_session_key_valid() -> Result<(), Box<dyn std::error::Error>> {
        let parsed = parse_agent_session_key("agent:mybot:main")
            .ok_or("parse_agent_session_key returned None")?;
        assert_eq!(parsed.agent_id, "mybot");
        assert_eq!(parsed.rest, "main");
        Ok(())
    }

    #[test]
    fn parse_agent_session_key_with_rest() -> Result<(), Box<dyn std::error::Error>> {
        let parsed = parse_agent_session_key("agent:mybot:cron:daily:run:123")
            .ok_or("parse_agent_session_key returned None")?;
        assert_eq!(parsed.agent_id, "mybot");
        assert_eq!(parsed.rest, "cron:daily:run:123");
        Ok(())
    }

    #[test]
    fn parse_agent_session_key_invalid() {
        assert!(parse_agent_session_key("not-agent-key").is_none());
        assert!(parse_agent_session_key("agent::main").is_none());
        assert!(parse_agent_session_key("agent:bot").is_none());
        assert!(parse_agent_session_key("").is_none());
    }

    #[test]
    fn resolve_agent_id_from_session_key_basic() {
        assert_eq!(
            resolve_agent_id_from_session_key("agent:mybot:main"),
            "mybot"
        );
        assert_eq!(
            resolve_agent_id_from_session_key("not-agent"),
            DEFAULT_AGENT_ID
        );
    }

    #[test]
    fn classify_session_key_shape_basic() {
        assert_eq!(classify_session_key_shape(""), SessionKeyShape::Missing);
        assert_eq!(
            classify_session_key_shape("agent:bot:main"),
            SessionKeyShape::Agent
        );
        assert_eq!(
            classify_session_key_shape("agent:"),
            SessionKeyShape::MalformedAgent
        );
        assert_eq!(
            classify_session_key_shape("legacy-key"),
            SessionKeyShape::LegacyOrAlias
        );
    }

    #[test]
    fn build_agent_main_session_key_basic() {
        assert_eq!(
            build_agent_main_session_key("mybot", None),
            "agent:mybot:main"
        );
        assert_eq!(
            build_agent_main_session_key("mybot", Some("custom")),
            "agent:mybot:custom"
        );
    }

    #[test]
    fn to_agent_store_session_key_basic() {
        assert_eq!(
            to_agent_store_session_key("mybot", "", None),
            "agent:mybot:main"
        );
        assert_eq!(
            to_agent_store_session_key("mybot", "main", None),
            "agent:mybot:main"
        );
        assert_eq!(
            to_agent_store_session_key("mybot", "custom-key", None),
            "agent:mybot:custom-key"
        );
    }

    #[test]
    fn derive_session_chat_type_basic() {
        assert_eq!(derive_session_chat_type(""), SessionKeyChatType::Unknown);
        assert_eq!(
            derive_session_chat_type("agent:bot:telegram:group:123"),
            SessionKeyChatType::Group
        );
        assert_eq!(
            derive_session_chat_type("agent:bot:telegram:direct:456"),
            SessionKeyChatType::Direct
        );
        assert_eq!(
            derive_session_chat_type("agent:bot:telegram:channel:789"),
            SessionKeyChatType::Channel
        );
    }

    #[test]
    fn is_cron_session_key_basic() {
        assert!(is_cron_session_key("agent:bot:cron:daily"));
        assert!(!is_cron_session_key("agent:bot:main"));
    }

    #[test]
    fn is_subagent_session_key_basic() {
        assert!(is_subagent_session_key("agent:bot:subagent:child"));
        assert!(is_subagent_session_key("subagent:child"));
        assert!(!is_subagent_session_key("agent:bot:main"));
    }

    #[test]
    fn get_subagent_depth_basic() {
        assert_eq!(get_subagent_depth("agent:bot:main"), 0);
        assert_eq!(get_subagent_depth("agent:bot:subagent:child"), 1);
        assert_eq!(
            get_subagent_depth("agent:bot:subagent:child:subagent:grandchild"),
            2
        );
    }

    #[test]
    fn is_acp_session_key_basic() {
        assert!(is_acp_session_key("acp:session123"));
        assert!(is_acp_session_key("agent:bot:acp:session123"));
        assert!(!is_acp_session_key("agent:bot:main"));
    }

    #[test]
    fn resolve_thread_parent_session_key_basic() {
        assert_eq!(
            resolve_thread_parent_session_key("agent:bot:telegram:group:123:thread:456"),
            Some("agent:bot:telegram:group:123".to_string())
        );
        assert_eq!(resolve_thread_parent_session_key("agent:bot:main"), None);
        assert_eq!(resolve_thread_parent_session_key(""), None);
    }

    #[test]
    fn resolve_thread_session_keys_no_thread() {
        let (key, parent) = resolve_thread_session_keys("agent:bot:main", None, None, true);
        assert_eq!(key, "agent:bot:main");
        assert!(parent.is_none());
    }

    #[test]
    fn resolve_thread_session_keys_with_thread() {
        let (key, parent) = resolve_thread_session_keys(
            "agent:bot:main",
            Some("THREAD-123"),
            Some("agent:bot:parent"),
            true,
        );
        assert_eq!(key, "agent:bot:main:thread:thread-123");
        assert_eq!(parent, Some("agent:bot:parent".to_string()));
    }

    #[test]
    fn resolve_thread_session_keys_no_suffix() {
        let (key, _) = resolve_thread_session_keys("agent:bot:main", Some("t1"), None, false);
        assert_eq!(key, "agent:bot:main");
    }

    #[test]
    fn build_group_history_key_basic() {
        assert_eq!(
            build_group_history_key("telegram", Some("acct1"), "group", "12345"),
            "telegram:acct1:group:12345"
        );
        assert_eq!(
            build_group_history_key("", None, "channel", ""),
            "unknown:default:channel:unknown"
        );
    }

    #[test]
    fn build_agent_peer_session_key_group() {
        assert_eq!(
            build_agent_peer_session_key(&BuildAgentPeerSessionKeyParams {
                agent_id: "bot",
                main_key: None,
                channel: "telegram",
                account_id: None,
                peer_kind: Some("group"),
                peer_id: Some("123"),
                identity_links: None,
                dm_scope: None,
            }),
            "agent:bot:telegram:group:123"
        );
    }

    #[test]
    fn build_agent_peer_session_key_direct_main() {
        assert_eq!(
            build_agent_peer_session_key(&BuildAgentPeerSessionKeyParams {
                agent_id: "bot",
                main_key: None,
                channel: "telegram",
                account_id: None,
                peer_kind: Some("direct"),
                peer_id: Some("user1"),
                identity_links: None,
                dm_scope: None,
            }),
            "agent:bot:main"
        );
    }

    #[test]
    fn build_agent_peer_session_key_per_peer() {
        assert_eq!(
            build_agent_peer_session_key(&BuildAgentPeerSessionKeyParams {
                agent_id: "bot",
                main_key: None,
                channel: "telegram",
                account_id: None,
                peer_kind: Some("direct"),
                peer_id: Some("User1"),
                identity_links: None,
                dm_scope: Some("per-peer"),
            }),
            "agent:bot:direct:user1"
        );
    }

    #[test]
    fn build_agent_peer_session_key_per_channel_peer() {
        assert_eq!(
            build_agent_peer_session_key(&BuildAgentPeerSessionKeyParams {
                agent_id: "bot",
                main_key: None,
                channel: "telegram",
                account_id: None,
                peer_kind: Some("direct"),
                peer_id: Some("User1"),
                identity_links: None,
                dm_scope: Some("per-channel-peer"),
            }),
            "agent:bot:telegram:direct:user1"
        );
    }

    #[test]
    fn build_agent_peer_session_key_per_account_channel_peer() {
        assert_eq!(
            build_agent_peer_session_key(&BuildAgentPeerSessionKeyParams {
                agent_id: "bot",
                main_key: None,
                channel: "telegram",
                account_id: Some("acct1"),
                peer_kind: Some("direct"),
                peer_id: Some("User1"),
                identity_links: None,
                dm_scope: Some("per-account-channel-peer"),
            }),
            "agent:bot:telegram:acct1:direct:user1"
        );
    }

    #[test]
    fn build_agent_peer_session_key_with_identity_links() {
        let mut links = std::collections::HashMap::new();
        links.insert(
            "canonical-user".to_string(),
            vec!["telegram:user1".to_string()],
        );
        assert_eq!(
            build_agent_peer_session_key(&BuildAgentPeerSessionKeyParams {
                agent_id: "bot",
                main_key: None,
                channel: "telegram",
                account_id: None,
                peer_kind: Some("direct"),
                peer_id: Some("User1"),
                identity_links: Some(&links),
                dm_scope: Some("per-peer"),
            }),
            "agent:bot:direct:canonical-user"
        );
    }

    #[test]
    fn build_agent_peer_session_key_identity_links_no_match() {
        let mut links = std::collections::HashMap::new();
        links.insert("other-user".to_string(), vec!["telegram:other".to_string()]);
        assert_eq!(
            build_agent_peer_session_key(&BuildAgentPeerSessionKeyParams {
                agent_id: "bot",
                main_key: None,
                channel: "telegram",
                account_id: None,
                peer_kind: Some("direct"),
                peer_id: Some("User1"),
                identity_links: Some(&links),
                dm_scope: Some("per-peer"),
            }),
            "agent:bot:direct:user1"
        );
    }
}
