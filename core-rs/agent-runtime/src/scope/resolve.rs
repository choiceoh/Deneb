//! Agent registry resolution — pure logic for config-driven agent management.
//!
//! Mirrors `src/agents/agent-scope.ts` (pure-logic subset). Keep in sync.
//! Filesystem-dependent functions remain in TypeScript.

use once_cell::sync::Lazy;
use regex::Regex;
use serde::{Deserialize, Serialize};

/// Default agent identifier when none is configured.
pub const DEFAULT_AGENT_ID: &str = "main";

// Pre-compiled regexes for agent ID normalization.
static VALID_ID_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$").unwrap());
static INVALID_CHARS_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"[^a-z0-9_-]+").unwrap());
static LEADING_DASH_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"^-+").unwrap());
static TRAILING_DASH_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"-+$").unwrap());

/// Normalize an agent ID to a path-safe, shell-friendly form.
pub fn normalize_agent_id(value: &str) -> String {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return DEFAULT_AGENT_ID.to_string();
    }
    if VALID_ID_RE.is_match(trimmed) {
        return trimmed.to_lowercase();
    }
    // Best-effort fallback: collapse invalid characters to "-".
    let lowered = trimmed.to_lowercase();
    let collapsed = INVALID_CHARS_RE.replace_all(&lowered, "-");
    let no_leading = LEADING_DASH_RE.replace(&collapsed, "");
    let no_trailing = TRAILING_DASH_RE.replace(&no_leading, "");
    let result = if no_trailing.len() > 64 {
        &no_trailing[..64]
    } else {
        &no_trailing
    };
    if result.is_empty() {
        DEFAULT_AGENT_ID.to_string()
    } else {
        result.to_string()
    }
}

/// Default main session key.
pub const DEFAULT_MAIN_KEY: &str = "main";

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

/// Agent entry from config (mirrors DenebConfig.agents.list[]).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AgentEntry {
    /// Agent identifier.
    #[serde(default)]
    pub id: String,
    /// Human-readable name.
    pub name: Option<String>,
    /// Whether this is the default agent.
    #[serde(default)]
    pub default: bool,
    /// Workspace directory path.
    pub workspace: Option<String>,
    /// Agent directory path override.
    pub agent_dir: Option<String>,
    /// Model configuration (string or object with primary + fallbacks).
    pub model: Option<serde_json::Value>,
    /// Skills filter (array of skill names).
    pub skills: Option<Vec<String>>,
    /// Memory search configuration.
    pub memory_search: Option<serde_json::Value>,
    /// Human delay configuration.
    pub human_delay: Option<serde_json::Value>,
    /// Heartbeat configuration.
    pub heartbeat: Option<serde_json::Value>,
    /// Identity/persona configuration.
    pub identity: Option<serde_json::Value>,
    /// Group chat configuration.
    pub group_chat: Option<serde_json::Value>,
    /// Subagents configuration.
    pub subagents: Option<serde_json::Value>,
    /// Sandbox configuration.
    pub sandbox: Option<serde_json::Value>,
    /// Tools configuration.
    pub tools: Option<serde_json::Value>,
}

/// Resolved agent configuration (subset of AgentEntry with normalized values).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ResolvedAgentConfig {
    pub name: Option<String>,
    pub workspace: Option<String>,
    pub agent_dir: Option<String>,
    pub model: Option<serde_json::Value>,
    pub skills: Option<Vec<String>>,
    pub memory_search: Option<serde_json::Value>,
    pub human_delay: Option<serde_json::Value>,
    pub heartbeat: Option<serde_json::Value>,
    pub identity: Option<serde_json::Value>,
    pub group_chat: Option<serde_json::Value>,
    pub subagents: Option<serde_json::Value>,
    pub sandbox: Option<serde_json::Value>,
    pub tools: Option<serde_json::Value>,
}

/// Result of resolving both default and session agent IDs.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SessionAgentIds {
    pub default_agent_id: String,
    pub session_agent_id: String,
}

/// Extract valid agent entries from a config's agent list.
pub fn list_agent_entries(agents_list: &[serde_json::Value]) -> Vec<AgentEntry> {
    agents_list
        .iter()
        .filter_map(|v| serde_json::from_value::<AgentEntry>(v.clone()).ok())
        .collect()
}

/// List all agent IDs from config, defaulting to [DEFAULT_AGENT_ID].
pub fn list_agent_ids(agents_list: &[serde_json::Value]) -> Vec<String> {
    let agents = list_agent_entries(agents_list);
    if agents.is_empty() {
        return vec![DEFAULT_AGENT_ID.to_string()];
    }

    let mut seen = std::collections::HashSet::new();
    let mut ids = Vec::new();
    for entry in &agents {
        let id = normalize_agent_id(&entry.id);
        if seen.insert(id.clone()) {
            ids.push(id);
        }
    }

    if ids.is_empty() {
        vec![DEFAULT_AGENT_ID.to_string()]
    } else {
        ids
    }
}

/// Resolve the default agent ID from config.
/// Uses the first agent marked `default=true`, or the first entry.
pub fn resolve_default_agent_id(agents_list: &[serde_json::Value]) -> String {
    let agents = list_agent_entries(agents_list);
    if agents.is_empty() {
        return DEFAULT_AGENT_ID.to_string();
    }

    let defaults: Vec<_> = agents.iter().filter(|a| a.default).collect();
    let chosen = defaults.first().copied().or(agents.first());

    match chosen {
        Some(agent) => {
            let id = agent.id.trim();
            normalize_agent_id(if id.is_empty() { DEFAULT_AGENT_ID } else { id })
        }
        None => DEFAULT_AGENT_ID.to_string(),
    }
}

/// Resolve both default and session-specific agent IDs.
pub fn resolve_session_agent_ids(
    agents_list: &[serde_json::Value],
    session_key: Option<&str>,
    explicit_agent_id: Option<&str>,
) -> SessionAgentIds {
    let default_agent_id = resolve_default_agent_id(agents_list);

    let explicit_id = explicit_agent_id
        .map(|s| s.trim().to_lowercase())
        .filter(|s| !s.is_empty())
        .map(|s| normalize_agent_id(&s));

    let session_agent_id = if let Some(id) = explicit_id {
        id
    } else if let Some(key) = session_key {
        // Extract agent ID from session key format "agent:<id>:<rest>".
        parse_agent_session_key(key)
            .map(|p| normalize_agent_id(&p.agent_id))
            .unwrap_or_else(|| default_agent_id.clone())
    } else {
        default_agent_id.clone()
    };

    SessionAgentIds {
        default_agent_id,
        session_agent_id,
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
    normalize_agent_id(parsed.as_ref().map(|p| p.agent_id.as_str()).unwrap_or(DEFAULT_AGENT_ID))
}

/// Normalize a main key to lowercase with default fallback.
pub fn normalize_main_key(value: &str) -> String {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        DEFAULT_MAIN_KEY.to_string()
    } else {
        trimmed.to_lowercase()
    }
}

/// Check if an agent ID is syntactically valid.
pub fn is_valid_agent_id(value: &str) -> bool {
    let trimmed = value.trim();
    !trimmed.is_empty() && VALID_ID_RE.is_match(trimmed)
}

/// Sanitize an agent ID (alias for normalize_agent_id).
pub fn sanitize_agent_id(value: &str) -> String {
    normalize_agent_id(value)
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
    if raw.is_empty() || raw.to_lowercase() == DEFAULT_MAIN_KEY {
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
    // Legacy Discord keys: discord:<accountId>:guild-<guildId>:channel-<channelId>
    static DISCORD_LEGACY_RE: Lazy<Regex> = Lazy::new(|| {
        Regex::new(r"^discord:(?:[^:]+:)?guild-[^:]+:channel-[^:]+$").unwrap()
    });
    if DISCORD_LEGACY_RE.is_match(&scoped) {
        return SessionKeyChatType::Channel;
    }
    SessionKeyChatType::Unknown
}

/// Check if a session key represents a cron run.
pub fn is_cron_run_session_key(session_key: &str) -> bool {
    static CRON_RUN_RE: Lazy<Regex> = Lazy::new(|| {
        Regex::new(r"^cron:[^:]+:run:[^:]+$").unwrap()
    });
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
pub fn get_subagent_depth(session_key: &str) -> usize {
    let raw = session_key.trim().to_lowercase();
    if raw.is_empty() {
        return 0;
    }
    raw.matches(":subagent:").count()
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
    if parent.is_empty() { None } else { Some(parent.to_string()) }
}

/// Resolve the fallback agent ID from explicit agent ID or session key.
pub fn resolve_fallback_agent_id(
    agent_id: Option<&str>,
    session_key: Option<&str>,
) -> String {
    let explicit = agent_id
        .map(|s| s.trim().to_string())
        .filter(|s| !s.is_empty());
    if let Some(id) = explicit {
        return normalize_agent_id(&id);
    }
    resolve_agent_id_from_session_key(session_key.unwrap_or(""))
}

/// Resolve a specific agent's config from the list.
pub fn resolve_agent_config(
    agents_list: &[serde_json::Value],
    agent_id: &str,
) -> Option<ResolvedAgentConfig> {
    let id = normalize_agent_id(agent_id);
    let entries = list_agent_entries(agents_list);
    let entry = entries
        .into_iter()
        .find(|e| normalize_agent_id(&e.id) == id)?;

    Some(ResolvedAgentConfig {
        name: entry.name,
        workspace: entry.workspace,
        agent_dir: entry.agent_dir,
        model: entry.model,
        skills: entry.skills,
        memory_search: entry.memory_search,
        human_delay: entry.human_delay,
        heartbeat: entry.heartbeat,
        identity: entry.identity,
        group_chat: entry.group_chat,
        subagents: entry.subagents,
        sandbox: entry.sandbox,
        tools: entry.tools,
    })
}

/// Resolve the primary model string for an agent from its config entry.
fn resolve_model_primary(raw: Option<&serde_json::Value>) -> Option<String> {
    match raw? {
        serde_json::Value::String(s) => {
            let trimmed = s.trim();
            if trimmed.is_empty() { None } else { Some(trimmed.to_string()) }
        }
        serde_json::Value::Object(obj) => {
            let primary = obj.get("primary")?.as_str()?;
            let trimmed = primary.trim();
            if trimmed.is_empty() { None } else { Some(trimmed.to_string()) }
        }
        _ => None,
    }
}

/// Resolve the agent's explicitly configured primary model.
pub fn resolve_agent_explicit_model_primary(
    agents_list: &[serde_json::Value],
    agent_id: &str,
) -> Option<String> {
    let config = resolve_agent_config(agents_list, agent_id)?;
    resolve_model_primary(config.model.as_ref())
}

/// Resolve the agent's effective primary model (agent config -> global defaults).
pub fn resolve_agent_effective_model_primary(
    agents_list: &[serde_json::Value],
    agent_id: &str,
    global_model: Option<&serde_json::Value>,
) -> Option<String> {
    resolve_agent_explicit_model_primary(agents_list, agent_id)
        .or_else(|| resolve_model_primary(global_model))
}

/// Resolve the agent's model fallbacks override.
pub fn resolve_agent_model_fallbacks_override(
    agents_list: &[serde_json::Value],
    agent_id: &str,
) -> Option<Vec<String>> {
    let config = resolve_agent_config(agents_list, agent_id)?;
    let model_value = config.model.as_ref()?;

    match model_value {
        serde_json::Value::String(_) => None,
        serde_json::Value::Object(obj) => {
            if !obj.contains_key("fallbacks") {
                return None;
            }
            let fallbacks = obj.get("fallbacks")?;
            if let serde_json::Value::Array(arr) = fallbacks {
                Some(
                    arr.iter()
                        .filter_map(|v| v.as_str().map(|s| s.to_string()))
                        .collect(),
                )
            } else {
                None
            }
        }
        _ => None,
    }
}

/// Resolve the agent's skills filter.
pub fn resolve_agent_skills_filter(
    agents_list: &[serde_json::Value],
    agent_id: &str,
) -> Option<Vec<String>> {
    let config = resolve_agent_config(agents_list, agent_id)?;
    config.skills
}

/// Resolve only the session agent ID (convenience wrapper).
pub fn resolve_session_agent_id(
    agents_list: &[serde_json::Value],
    session_key: Option<&str>,
) -> String {
    resolve_session_agent_ids(agents_list, session_key, None).session_agent_id
}

/// Backward-compatible alias for `resolve_agent_explicit_model_primary`.
pub fn resolve_agent_model_primary(
    agents_list: &[serde_json::Value],
    agent_id: &str,
) -> Option<String> {
    resolve_agent_explicit_model_primary(agents_list, agent_id)
}

/// Extract fallback model values from a model config value.
/// Mirrors `src/config/model-input.ts#resolveAgentModelFallbackValues`. Keep in sync.
pub fn resolve_agent_model_fallback_values(model: Option<&serde_json::Value>) -> Vec<String> {
    let val = match model {
        Some(v) => v,
        None => return Vec::new(),
    };
    match val {
        serde_json::Value::Object(obj) => {
            match obj.get("fallbacks") {
                Some(serde_json::Value::Array(arr)) => arr
                    .iter()
                    .filter_map(|v| v.as_str().map(|s| s.trim().to_string()))
                    .filter(|s| !s.is_empty())
                    .collect(),
                _ => Vec::new(),
            }
        }
        _ => Vec::new(),
    }
}

/// Resolve model fallbacks override for a run context.
/// Uses explicit agent ID or session key to find the agent, then checks its fallbacks.
pub fn resolve_run_model_fallbacks_override(
    agents_list: &[serde_json::Value],
    agent_id: Option<&str>,
    session_key: Option<&str>,
) -> Option<Vec<String>> {
    let id = resolve_fallback_agent_id(agent_id, session_key);
    resolve_agent_model_fallbacks_override(agents_list, &id)
}

/// Check if any model fallbacks are configured (agent-level or global defaults).
pub fn has_configured_model_fallbacks(
    agents_list: &[serde_json::Value],
    global_model: Option<&serde_json::Value>,
    agent_id: Option<&str>,
    session_key: Option<&str>,
) -> bool {
    let fallbacks_override = resolve_run_model_fallbacks_override(agents_list, agent_id, session_key);
    let default_fallbacks = resolve_agent_model_fallback_values(global_model);
    let effective = fallbacks_override.unwrap_or(default_fallbacks);
    !effective.is_empty()
}

/// Resolve the effective model fallbacks, considering agent overrides and session state.
pub fn resolve_effective_model_fallbacks(
    agents_list: &[serde_json::Value],
    agent_id: &str,
    global_model: Option<&serde_json::Value>,
    has_session_model_override: bool,
) -> Option<Vec<String>> {
    let agent_fallbacks_override = resolve_agent_model_fallbacks_override(agents_list, agent_id);
    if !has_session_model_override {
        return agent_fallbacks_override;
    }
    let default_fallbacks = resolve_agent_model_fallback_values(global_model);
    Some(agent_fallbacks_override.unwrap_or(default_fallbacks))
}

/// Default account identifier.
pub const DEFAULT_ACCOUNT_ID: &str = "default";

static VALID_ACCOUNT_ID_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$").unwrap());

/// Normalize an account ID to a path-safe form. Defaults to DEFAULT_ACCOUNT_ID.
/// Mirrors `src/routing/account-id.ts#normalizeAccountId`. Keep in sync.
pub fn normalize_account_id(value: &str) -> String {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return DEFAULT_ACCOUNT_ID.to_string();
    }
    if VALID_ACCOUNT_ID_RE.is_match(trimmed) {
        return trimmed.to_lowercase();
    }
    let lowered = trimmed.to_lowercase();
    let collapsed = INVALID_CHARS_RE.replace_all(&lowered, "-");
    let no_leading = LEADING_DASH_RE.replace(&collapsed, "");
    let no_trailing = TRAILING_DASH_RE.replace(&no_leading, "");
    let result = if no_trailing.len() > 64 {
        &no_trailing[..64]
    } else {
        &no_trailing
    };
    if result.is_empty() {
        DEFAULT_ACCOUNT_ID.to_string()
    } else {
        result.to_string()
    }
}

/// Build a group history key from channel, account, peer kind, and peer ID.
pub fn build_group_history_key(
    channel: &str,
    account_id: Option<&str>,
    peer_kind: &str,
    peer_id: &str,
) -> String {
    let ch = {
        let t = channel.trim().to_lowercase();
        if t.is_empty() { "unknown".to_string() } else { t }
    };
    let acct = normalize_account_id(account_id.unwrap_or(""));
    let pid = {
        let t = peer_id.trim().to_lowercase();
        if t.is_empty() { "unknown".to_string() } else { t }
    };
    format!("{}:{}:{}:{}", ch, acct, peer_kind, pid)
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
    let mut candidates = std::collections::HashSet::new();
    let raw_candidate = pid.trim().to_lowercase();
    if !raw_candidate.is_empty() {
        candidates.insert(raw_candidate);
    }
    let ch = channel.trim().to_lowercase();
    if !ch.is_empty() {
        let scoped = format!("{}:{}", ch, pid.trim().to_lowercase());
        if !scoped.is_empty() {
            candidates.insert(scoped);
        }
    }
    if candidates.is_empty() {
        return None;
    }
    for (canonical, ids) in identity_links {
        let canonical_name = canonical.trim();
        if canonical_name.is_empty() {
            continue;
        }
        for id in ids {
            let normalized = id.trim().to_lowercase();
            if !normalized.is_empty() && candidates.contains(&normalized) {
                return Some(canonical_name.to_string());
            }
        }
    }
    None
}

/// Build a peer-scoped session key for an agent.
/// Mirrors `src/routing/session-key.ts#buildAgentPeerSessionKey`. Keep in sync.
pub fn build_agent_peer_session_key(
    agent_id: &str,
    main_key: Option<&str>,
    channel: &str,
    account_id: Option<&str>,
    peer_kind: Option<&str>,
    peer_id: Option<&str>,
    identity_links: Option<&std::collections::HashMap<String, Vec<String>>>,
    dm_scope: Option<&str>,
) -> String {
    let kind = peer_kind.unwrap_or("direct");
    let aid = normalize_agent_id(agent_id);

    if kind == "direct" {
        let scope = dm_scope.unwrap_or("main");
        let mut pid = peer_id.unwrap_or("").trim().to_string();

        // Resolve identity links for non-main DM scopes.
        if scope != "main" {
            if let Some(links) = identity_links {
                if let Some(linked) = resolve_linked_peer_id(links, channel, &pid) {
                    pid = linked;
                }
            }
        }
        let pid = pid.to_lowercase();

        if scope == "per-account-channel-peer" && !pid.is_empty() {
            let ch = {
                let t = channel.trim().to_lowercase();
                if t.is_empty() { "unknown".to_string() } else { t }
            };
            let acct = normalize_account_id(account_id.unwrap_or(""));
            return format!("agent:{}:{}:{}:direct:{}", aid, ch, acct, pid);
        }
        if scope == "per-channel-peer" && !pid.is_empty() {
            let ch = {
                let t = channel.trim().to_lowercase();
                if t.is_empty() { "unknown".to_string() } else { t }
            };
            return format!("agent:{}:{}:direct:{}", aid, ch, pid);
        }
        if scope == "per-peer" && !pid.is_empty() {
            return format!("agent:{}:direct:{}", aid, pid);
        }
        return build_agent_main_session_key(agent_id, main_key);
    }

    let ch = {
        let t = channel.trim().to_lowercase();
        if t.is_empty() { "unknown".to_string() } else { t }
    };
    let pid = {
        let t = peer_id.unwrap_or("").trim().to_lowercase();
        if t.is_empty() { "unknown".to_string() } else { t }
    };
    format!("agent:{}:{}:{}:{}", aid, ch, kind, pid)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn normalize_agent_id_basic() {
        assert_eq!(normalize_agent_id("main"), "main");
        assert_eq!(normalize_agent_id("  Main  "), "main");
        assert_eq!(normalize_agent_id(""), DEFAULT_AGENT_ID);
        assert_eq!(normalize_agent_id("my-agent_1"), "my-agent_1");
    }

    #[test]
    fn normalize_agent_id_invalid_chars() {
        assert_eq!(normalize_agent_id("my agent!"), "my-agent");
        assert_eq!(normalize_agent_id("---"), DEFAULT_AGENT_ID);
    }

    #[test]
    fn list_agent_ids_empty() {
        assert_eq!(list_agent_ids(&[]), vec!["main"]);
    }

    #[test]
    fn list_agent_ids_deduplicates() {
        let list = vec![
            json!({"id": "alpha"}),
            json!({"id": "beta"}),
            json!({"id": "Alpha"}),
        ];
        let ids = list_agent_ids(&list);
        assert_eq!(ids, vec!["alpha", "beta"]);
    }

    #[test]
    fn resolve_default_agent_id_empty() {
        assert_eq!(resolve_default_agent_id(&[]), "main");
    }

    #[test]
    fn resolve_default_agent_id_uses_default_flag() {
        let list = vec![
            json!({"id": "alpha"}),
            json!({"id": "beta", "default": true}),
        ];
        assert_eq!(resolve_default_agent_id(&list), "beta");
    }

    #[test]
    fn resolve_default_agent_id_first_entry_fallback() {
        let list = vec![json!({"id": "alpha"}), json!({"id": "beta"})];
        assert_eq!(resolve_default_agent_id(&list), "alpha");
    }

    #[test]
    fn resolve_session_agent_ids_explicit() {
        let list = vec![json!({"id": "alpha"})];
        let result = resolve_session_agent_ids(&list, None, Some("beta"));
        assert_eq!(result.session_agent_id, "beta");
    }

    #[test]
    fn resolve_session_agent_ids_from_key() {
        let list = vec![json!({"id": "alpha"})];
        let result = resolve_session_agent_ids(&list, Some("agent:beta:main"), None);
        assert_eq!(result.session_agent_id, "beta");
    }

    #[test]
    fn resolve_session_agent_ids_default_fallback() {
        let list = vec![json!({"id": "alpha"})];
        let result = resolve_session_agent_ids(&list, None, None);
        assert_eq!(result.session_agent_id, "alpha");
    }

    #[test]
    fn resolve_agent_config_found() {
        let list = vec![
            json!({"id": "alpha", "name": "Alpha Agent", "workspace": "/tmp/alpha"}),
        ];
        let config = resolve_agent_config(&list, "alpha").unwrap();
        assert_eq!(config.name.as_deref(), Some("Alpha Agent"));
        assert_eq!(config.workspace.as_deref(), Some("/tmp/alpha"));
    }

    #[test]
    fn resolve_agent_config_not_found() {
        let list = vec![json!({"id": "alpha"})];
        assert!(resolve_agent_config(&list, "beta").is_none());
    }

    #[test]
    fn resolve_explicit_model_primary_string() {
        let list = vec![json!({"id": "alpha", "model": "claude-opus-4-6"})];
        assert_eq!(
            resolve_agent_explicit_model_primary(&list, "alpha"),
            Some("claude-opus-4-6".to_string())
        );
    }

    #[test]
    fn resolve_explicit_model_primary_object() {
        let list = vec![json!({"id": "alpha", "model": {"primary": "claude-sonnet-4-6"}})];
        assert_eq!(
            resolve_agent_explicit_model_primary(&list, "alpha"),
            Some("claude-sonnet-4-6".to_string())
        );
    }

    #[test]
    fn resolve_model_fallbacks_override() {
        let list = vec![
            json!({"id": "alpha", "model": {"primary": "m1", "fallbacks": ["m2", "m3"]}}),
        ];
        assert_eq!(
            resolve_agent_model_fallbacks_override(&list, "alpha"),
            Some(vec!["m2".to_string(), "m3".to_string()])
        );
    }

    #[test]
    fn resolve_model_fallbacks_override_string_model() {
        let list = vec![json!({"id": "alpha", "model": "claude-opus-4-6"})];
        assert_eq!(resolve_agent_model_fallbacks_override(&list, "alpha"), None);
    }

    #[test]
    fn parse_agent_session_key_valid() {
        let parsed = parse_agent_session_key("agent:mybot:main").unwrap();
        assert_eq!(parsed.agent_id, "mybot");
        assert_eq!(parsed.rest, "main");
    }

    #[test]
    fn parse_agent_session_key_with_rest() {
        let parsed = parse_agent_session_key("agent:mybot:cron:daily:run:123").unwrap();
        assert_eq!(parsed.agent_id, "mybot");
        assert_eq!(parsed.rest, "cron:daily:run:123");
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
        assert_eq!(resolve_agent_id_from_session_key("agent:mybot:main"), "mybot");
        assert_eq!(resolve_agent_id_from_session_key("not-agent"), DEFAULT_AGENT_ID);
    }

    #[test]
    fn normalize_main_key_basic() {
        assert_eq!(normalize_main_key(""), DEFAULT_MAIN_KEY);
        assert_eq!(normalize_main_key("  Custom  "), "custom");
    }

    #[test]
    fn is_valid_agent_id_basic() {
        assert!(is_valid_agent_id("main"));
        assert!(is_valid_agent_id("my-agent_1"));
        assert!(!is_valid_agent_id(""));
        assert!(!is_valid_agent_id("invalid chars!"));
    }

    #[test]
    fn classify_session_key_shape_basic() {
        assert_eq!(classify_session_key_shape(""), SessionKeyShape::Missing);
        assert_eq!(classify_session_key_shape("agent:bot:main"), SessionKeyShape::Agent);
        assert_eq!(classify_session_key_shape("agent:"), SessionKeyShape::MalformedAgent);
        assert_eq!(classify_session_key_shape("legacy-key"), SessionKeyShape::LegacyOrAlias);
    }

    #[test]
    fn build_agent_main_session_key_basic() {
        assert_eq!(build_agent_main_session_key("mybot", None), "agent:mybot:main");
        assert_eq!(build_agent_main_session_key("mybot", Some("custom")), "agent:mybot:custom");
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
        assert_eq!(get_subagent_depth("agent:bot:subagent:child:subagent:grandchild"), 2);
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
    fn resolve_fallback_agent_id_basic() {
        assert_eq!(resolve_fallback_agent_id(Some("mybot"), None), "mybot");
        assert_eq!(
            resolve_fallback_agent_id(None, Some("agent:mybot:main")),
            "mybot"
        );
        assert_eq!(resolve_fallback_agent_id(None, None), DEFAULT_AGENT_ID);
    }

    #[test]
    fn resolve_session_agent_id_basic() {
        let list = vec![json!({"id": "alpha"})];
        assert_eq!(resolve_session_agent_id(&list, Some("agent:beta:main")), "beta");
        assert_eq!(resolve_session_agent_id(&list, None), "alpha");
    }

    #[test]
    fn resolve_agent_model_primary_alias() {
        let list = vec![json!({"id": "alpha", "model": "claude-opus-4-6"})];
        assert_eq!(
            resolve_agent_model_primary(&list, "alpha"),
            Some("claude-opus-4-6".to_string())
        );
    }

    #[test]
    fn resolve_agent_model_fallback_values_basic() {
        let model = json!({"primary": "m1", "fallbacks": ["m2", "m3"]});
        assert_eq!(
            resolve_agent_model_fallback_values(Some(&model)),
            vec!["m2".to_string(), "m3".to_string()]
        );
    }

    #[test]
    fn resolve_agent_model_fallback_values_string() {
        let model = json!("claude-opus-4-6");
        assert!(resolve_agent_model_fallback_values(Some(&model)).is_empty());
    }

    #[test]
    fn resolve_agent_model_fallback_values_none() {
        assert!(resolve_agent_model_fallback_values(None).is_empty());
    }

    #[test]
    fn resolve_run_model_fallbacks_override_basic() {
        let list = vec![
            json!({"id": "alpha", "model": {"primary": "m1", "fallbacks": ["m2"]}}),
        ];
        assert_eq!(
            resolve_run_model_fallbacks_override(&list, Some("alpha"), None),
            Some(vec!["m2".to_string()])
        );
    }

    #[test]
    fn has_configured_model_fallbacks_agent_level() {
        let list = vec![
            json!({"id": "alpha", "model": {"primary": "m1", "fallbacks": ["m2"]}}),
        ];
        assert!(has_configured_model_fallbacks(&list, None, Some("alpha"), None));
    }

    #[test]
    fn has_configured_model_fallbacks_global() {
        let list: Vec<serde_json::Value> = vec![];
        let global = json!({"primary": "m1", "fallbacks": ["m2"]});
        assert!(has_configured_model_fallbacks(&list, Some(&global), None, None));
    }

    #[test]
    fn has_configured_model_fallbacks_none() {
        let list: Vec<serde_json::Value> = vec![];
        assert!(!has_configured_model_fallbacks(&list, None, None, None));
    }

    #[test]
    fn resolve_effective_model_fallbacks_no_override() {
        let list = vec![
            json!({"id": "alpha", "model": {"primary": "m1", "fallbacks": ["m2"]}}),
        ];
        assert_eq!(
            resolve_effective_model_fallbacks(&list, "alpha", None, false),
            Some(vec!["m2".to_string()])
        );
    }

    #[test]
    fn resolve_effective_model_fallbacks_with_session_override() {
        let list = vec![json!({"id": "alpha", "model": "m1"})];
        let global = json!({"primary": "m1", "fallbacks": ["g1", "g2"]});
        assert_eq!(
            resolve_effective_model_fallbacks(&list, "alpha", Some(&global), true),
            Some(vec!["g1".to_string(), "g2".to_string()])
        );
    }

    #[test]
    fn normalize_account_id_basic() {
        assert_eq!(normalize_account_id(""), DEFAULT_ACCOUNT_ID);
        assert_eq!(normalize_account_id("MyAccount"), "myaccount");
        assert_eq!(normalize_account_id("account-123"), "account-123");
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
    fn build_agent_peer_session_key_group() {
        assert_eq!(
            build_agent_peer_session_key("bot", None, "telegram", None, Some("group"), Some("123"), None, None),
            "agent:bot:telegram:group:123"
        );
    }

    #[test]
    fn build_agent_peer_session_key_direct_main() {
        // Default dm_scope is "main", which falls back to main session key.
        assert_eq!(
            build_agent_peer_session_key("bot", None, "telegram", None, Some("direct"), Some("user1"), None, None),
            "agent:bot:main"
        );
    }

    #[test]
    fn build_agent_peer_session_key_per_peer() {
        assert_eq!(
            build_agent_peer_session_key("bot", None, "telegram", None, Some("direct"), Some("User1"), None, Some("per-peer")),
            "agent:bot:direct:user1"
        );
    }

    #[test]
    fn build_agent_peer_session_key_per_channel_peer() {
        assert_eq!(
            build_agent_peer_session_key("bot", None, "telegram", None, Some("direct"), Some("User1"), None, Some("per-channel-peer")),
            "agent:bot:telegram:direct:user1"
        );
    }

    #[test]
    fn build_agent_peer_session_key_per_account_channel_peer() {
        assert_eq!(
            build_agent_peer_session_key("bot", None, "telegram", Some("acct1"), Some("direct"), Some("User1"), None, Some("per-account-channel-peer")),
            "agent:bot:telegram:acct1:direct:user1"
        );
    }

    #[test]
    fn build_agent_peer_session_key_with_identity_links() {
        let mut links = std::collections::HashMap::new();
        links.insert("canonical-user".to_string(), vec!["telegram:user1".to_string()]);
        assert_eq!(
            build_agent_peer_session_key("bot", None, "telegram", None, Some("direct"), Some("User1"), Some(&links), Some("per-peer")),
            "agent:bot:direct:canonical-user"
        );
    }

    #[test]
    fn build_agent_peer_session_key_identity_links_no_match() {
        let mut links = std::collections::HashMap::new();
        links.insert("other-user".to_string(), vec!["discord:other".to_string()]);
        assert_eq!(
            build_agent_peer_session_key("bot", None, "telegram", None, Some("direct"), Some("User1"), Some(&links), Some("per-peer")),
            "agent:bot:direct:user1"
        );
    }
}
