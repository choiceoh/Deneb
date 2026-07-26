package session

import "strings"

const (
	// NativeWorkSessionKey is the primary native work conversation.
	NativeWorkSessionKey = "client:main"
	// DreamWorkSessionKey is the dedicated background-dream conversation.
	DreamWorkSessionKey = NativeWorkSessionKey + ":dream"
	// NativeWorkSessionTarget is the channel target portion of client:main.
	NativeWorkSessionTarget = "main"
	// HeartbeatWorkSessionKey is the ISOLATED session the 30-min heartbeat turn
	// reasons in. Kept separate from client:main so autonomous ticks never
	// assemble or compact the user's live conversation (the old client:main
	// piggyback forced a Polaris compaction of client:main every tick). The
	// heartbeat's user-facing report is delivered separately via the proactive
	// relay (RelayNative → client:main + push), so isolation costs no visibility.
	HeartbeatWorkSessionKey = "submain:heartbeat"
	// GlassesWorkSessionKey is the Even Realities G2 Custom AI bridge session.
	// Kept separate from client:main so HUD turns do not clutter the phone
	// drawer; long results still land in this transcript for later review.
	GlassesWorkSessionKey = "glasses:main"
)

// RestorableTranscriptChannel classifies transcript keys that should be
// restored into the native session drawer after a gateway restart. It accepts
// the current client:main hierarchy and readable legacy chat conversations,
// while rejecting retired client:topic and bare client identifiers. Delegated
// sub-agent runs share the client:main hierarchy but are not conversations —
// see IsSpawnedChildKey.
func RestorableTranscriptChannel(sessionKey string) (channel string, ok bool) {
	if IsSpawnedChildKey(sessionKey) {
		return "", false
	}
	isNative := sessionKey == NativeWorkSessionKey ||
		strings.HasPrefix(sessionKey, NativeWorkSessionKey+":")
	isLegacyChat := strings.HasPrefix(sessionKey, "chat:") &&
		strings.TrimPrefix(sessionKey, "chat:") != ""
	if isNative || isLegacyChat {
		return "client", true
	}
	return "", false
}

// IsSpawnedChildKey reports whether a key belongs to a delegated sub-agent run
// rather than a conversation the user opened. The sub-agent spawner mints
// `<parent>:<label>:<unix-ms>` (chat/tools/runtimeops/sessions_tool.go) under the
// SAME client:main hierarchy the user's conversations use, and creates it as
// KindDirect — so neither the kind nor the prefix separates the two, and the
// trailing epoch segment is the only discriminator that survives a restart
// (SpawnedBy is in-memory only). Conversation keys never carry it: the desktop
// mints `client:main:<base36>`, the phone `client:main:<uuid>`, work-feed
// side-chats `client:main:wf-<id>` — one segment, no epoch tail.
func IsSpawnedChildKey(sessionKey string) bool {
	rest, found := strings.CutPrefix(sessionKey, NativeWorkSessionKey+":")
	if !found {
		return false
	}
	label, stamp, split := strings.Cut(rest, ":")
	if !split || label == "" {
		return false
	}
	// The stamp is the last segment; a deeper chain (A→B→C) keeps nesting, so
	// classify on the tail rather than the first split.
	if idx := strings.LastIndex(stamp, ":"); idx >= 0 {
		stamp = stamp[idx+1:]
	}
	if len(stamp) < 10 { // unix millis are 13 digits; stay tolerant, not narrow
		return false
	}
	for _, c := range stamp {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// HeartbeatTargetSession keeps an active native conversation as the target and
// otherwise falls back to the primary work session.
func HeartbeatTargetSession(lastSessionKey string) string {
	if strings.HasPrefix(lastSessionKey, "client:") && strings.TrimPrefix(lastSessionKey, "client:") != "" {
		return lastSessionKey
	}
	return NativeWorkSessionKey
}

// WorkTypeForKey classifies a session key into a coarse work-type slug for usage
// reporting (heartbeat, phone-event, chat, mail-analysis, …). The session key
// encodes the work type by prefix; this is the single source of truth for that
// mapping so callers don't re-implement scattered prefix switches. Returns a
// stable English slug the UI localizes; unrecognized keys fold into "other".
func WorkTypeForKey(sessionKey string) string {
	switch {
	case sessionKey == HeartbeatWorkSessionKey || strings.HasPrefix(sessionKey, "submain:heartbeat"):
		return "heartbeat"
	case strings.HasPrefix(sessionKey, "phone-event"):
		return "phone-event"
	case sessionKey == "system:mailpoll":
		return "mail-analysis"
	case strings.HasPrefix(sessionKey, "mail-qa"):
		return "mail-qa"
	case strings.HasPrefix(sessionKey, "cron:"):
		return "cron"
	case strings.HasPrefix(sessionKey, "noti-digest"):
		return "noti-digest"
	case strings.HasPrefix(sessionKey, "supernote-digest"):
		return "supernote-digest"
	case strings.HasPrefix(sessionKey, "wiki-"):
		return "wiki-background"
	case strings.HasPrefix(sessionKey, "system:skill-review"), strings.HasPrefix(sessionKey, "system:skill-workout"):
		return "skill-review"
	case strings.HasPrefix(sessionKey, "system:groupware"):
		return "groupware-radar"
	// Local-model helper calls (session titles, triage, summarizers, mail-analysis
	// stages) are logged under system:helper via agentlog helper.llm events. Classify
	// before the bare "system" bucket so local-model usage gets its own slug.
	case strings.HasPrefix(sessionKey, "system:helper"):
		return "helper"
	case sessionKey == "boot":
		return "boot"
	// A native subagent runs under client:main:<label>:<ms>; classify it before
	// the bare client:main chat bucket so delegated work is not counted as chat.
	// Match the epoch-tailed spawn shape only — every per-conversation key the
	// clients mint (client:main:<uuid> on the phone, <base36> on the desktop,
	// wf-<id> for work-feed side chats) also lives under client:main:, and a
	// bare prefix test filed the user's own chats under "subagent" in the usage
	// report while the chat bucket saw only the client:main home session.
	case IsSpawnedChildKey(sessionKey):
		return "subagent"
	case sessionKey == DreamWorkSessionKey:
		return "dream"
	case sessionKey == NativeWorkSessionKey ||
		sessionKey == GlassesWorkSessionKey ||
		strings.HasPrefix(sessionKey, "glasses:") ||
		strings.HasPrefix(sessionKey, "client:") ||
		strings.HasPrefix(sessionKey, "telegram:") ||
		strings.HasPrefix(sessionKey, "discord:") ||
		strings.HasPrefix(sessionKey, "chat:"):
		return "chat"
	case strings.HasPrefix(sessionKey, "system"):
		return "system"
	default:
		return "other"
	}
}
