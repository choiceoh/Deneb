package session

import (
	"fmt"
	"strings"
)

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
	// SpawnedChildPrefix namespaces delegated sub-agent runs. It stays under
	// `client:` — the prompt-facing channel is derived from that prefix
	// (sessionFallbackChannel), so leaving it would give these runs a different
	// runtime line and split the APC prefix family (prompt-cache doctrine §1.5
	// rule 4); `client:` also governs auto-resume markers and chat-activity
	// recording. What it does leave is the `client:main` CONVERSATION hierarchy,
	// so restore, the drawer, auto-titling and the GC exempt it structurally
	// instead of by classifying a key shape.
	SpawnedChildPrefix = "client:sub:"
)

// SpawnedChildKey mints the session key for a delegated sub-agent run. Single
// minting point so the shape and IsSpawnedChildKey can never drift. The parent
// link is NOT encoded in the key — every consumer reads it from the SpawnedBy
// field — so the namespace stays flat and a nested spawn does not grow an
// unbounded key. The label is slugged (a raw label could carry ':') and the
// epoch keeps sibling spawns of one label distinct.
func SpawnedChildKey(label string, atMs int64) string {
	slug := slugForKeySegment(label)
	if slug == "" {
		return fmt.Sprintf("%s%d", SpawnedChildPrefix, atMs)
	}
	return fmt.Sprintf("%s%s-%d", SpawnedChildPrefix, slug, atMs)
}

// slugForKeySegment reduces a free-form label to one safe key segment.
func slugForKeySegment(label string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(strings.TrimSpace(label)) {
		switch {
		case (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			b.WriteRune(c)
		default:
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	return slug
}

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
// rather than a conversation the user opened. Current keys answer structurally
// (they are not in the client:main hierarchy at all); the rest of this function
// is compatibility for the two shapes minted before the namespace existed,
// whose transcripts are still on disk — dropping them would sail those runs
// back into the conversation list on the next restart.
//
// Conversation keys never match: the desktop mints `client:main:<base36>`, the
// phone `client:main:<uuid>`, work-feed side chats `client:main:wf-<id>`.
func IsSpawnedChildKey(sessionKey string) bool {
	if strings.HasPrefix(sessionKey, SpawnedChildPrefix) {
		return true
	}
	rest, found := strings.CutPrefix(sessionKey, NativeWorkSessionKey+":")
	if !found {
		return false
	}
	for _, segment := range strings.Split(rest, ":") {
		// Legacy shape 1 — the `sub-` segment marker (#4357).
		if strings.HasPrefix(segment, "sub-") {
			return true
		}
	}
	// Legacy shape 2 — pre-marker `<label>:<unix-ms>`, discriminated by the
	// trailing epoch segment (#4355).
	return isLegacyEpochTailedKey(rest)
}

func isLegacyEpochTailedKey(rest string) bool {
	label, stamp, split := strings.Cut(rest, ":")
	if !split || label == "" {
		return false
	}
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
	// A sub-agent run is a client session too, but the heartbeat must not reason
	// inside the agent's own delegated scratch conversation.
	if IsSpawnedChildKey(lastSessionKey) {
		return NativeWorkSessionKey
	}
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
	// Delegated runs live in their own client:sub: namespace, but classify before
	// the client: chat bucket (which would otherwise swallow them) — and before
	// legacy spawn keys still under client:main: reach it. A bare client:main:
	// prefix test used to file the user's own per-conversation chats under
	// "subagent" while the chat bucket saw only the client:main home session.
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
