package mailarchive

// export.go — thin exported wrappers over the archive's internal helpers so the
// sibling mailstore package can reuse the exact ranking, clustering, dedupe, and
// message-shaping logic instead of duplicating (and drifting from) it. Keeping
// the originals unexported avoids churning every in-package caller; these
// wrappers are the single seam mailstore depends on.

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// ContextMessageFromDetail builds the agent-facing ContextMessage from a parsed
// message detail (LMTP intake and backfill share this shaping). when is filled.
func ContextMessageFromDetail(mailbox, uid string, detail *gmail.MessageDetail, bodyRunes int) ContextMessage {
	return contextMessageFromDetail(mailbox, uid, detail, bodyRunes)
}

// ContextIndexFields returns the full-text index fields for a message (subject,
// participants, snippet, body, message-id, refs, attachment names).
func ContextIndexFields(msg ContextMessage) []string { return contextIndexFields(msg) }

// RankProjectMessages scores project-history candidates (deadline/money/subject
// signals) for a query, most-relevant first.
func RankProjectMessages(query string, msgs []ContextMessage) []ContextMessage {
	return rankProjectMessages(query, msgs)
}

// ClusterProjectThreads groups messages by normalized subject into thread handles.
func ClusterProjectThreads(msgs []ContextMessage) []ProjectThread { return clusterProjectThreads(msgs) }

// SortContextMessages orders messages by sent time (chronological or newest-first),
// tie-broken by mailbox+UID. Requires the unexported when field to be populated —
// use RehydrateWhen after loading messages that did not come through
// ContextMessageFromDetail.
func SortContextMessages(msgs []ContextMessage, chronological bool) {
	sortContextMessages(msgs, chronological)
}

// ContextMessageDedupeKey is the stable identity key (Message-ID preferred, then
// id, then locator) used for dedup across re-delivery and backfill re-runs.
func ContextMessageDedupeKey(msg ContextMessage) string { return contextMessageDedupeKey(msg) }

// SentOnOrAfter reports whether a Date header falls on/after since's KST day.
func SentOnOrAfter(dateHeader string, since time.Time) bool { return sentOnOrAfter(dateHeader, since) }

// ParseMailDate parses an RFC mail Date header to a time (zero on failure).
func ParseMailDate(raw string) time.Time { return parseMailDate(raw) }

// ArchiveLocator composes the "mailbox:uid" locator the read/thread paths resolve.
func ArchiveLocator(mailbox, uid string) string { return archiveLocator(mailbox, uid) }

// ArchiveLocatorParts splits a locator into (mailbox, uid, ok).
func ArchiveLocatorParts(id string) (string, string, bool) { return archiveLocatorParts(id) }

// NormalizeMsgID normalizes a Message-ID for thread/dedup lookups.
func NormalizeMsgID(id string) string { return normalizeMsgID(id) }

// RehydrateWhen repopulates the unexported sort key from the Date header after a
// message is reloaded from disk (JSONL round-trip drops the unexported field).
func RehydrateWhen(msg *ContextMessage) { msg.when = parseMailDate(msg.Date) }
