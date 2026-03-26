package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"regexp"
	"strings"
	"time"
)

// InboundMeta holds parsed metadata from an inbound message.
type InboundMeta struct {
	Channel       string
	From          string
	SenderID      string
	SenderName    string
	IsGroup       bool
	ChatType      string // "direct", "group", "supergroup", "channel"
	AccountID     string
	ThreadID      string
	WasMentioned  bool
	ForwardedFrom string
	Timestamp     time.Time
	HasMedia      bool
	MediaCount    int
	HasReplyTo    bool
	ReplyToID     string
}

// FinalizeInboundContext normalizes inbound message fields for consistent processing.
func FinalizeInboundContext(ctx *types.MsgContext) {
	if ctx == nil {
		return
	}

	// Ensure RawBody is preserved.
	if ctx.RawBody == "" {
		ctx.RawBody = ctx.Body
	}

	// Default CommandBody to Body.
	if ctx.CommandBody == "" {
		ctx.CommandBody = ctx.Body
	}

	// Default BodyForAgent to Body.
	if ctx.BodyForAgent == "" {
		ctx.BodyForAgent = ctx.Body
	}

	// Default BodyForCommands to Body.
	if ctx.BodyForCommands == "" {
		ctx.BodyForCommands = ctx.Body
	}

	// Normalize ChatType.
	if ctx.ChatType == "" {
		if ctx.IsGroup {
			ctx.ChatType = "group"
		} else {
			ctx.ChatType = "direct"
		}
	}

	// Default CommandAuthorized to true for direct messages.
	if !ctx.IsGroup {
		ctx.CommandAuthorized = true
	}
}

// StripMentions removes @mentions from message text.
func StripMentions(text, botUsername string) string {
	if botUsername == "" || text == "" {
		return text
	}
	// Strip @botname (case-insensitive).
	re := regexp.MustCompile(`(?i)@` + regexp.QuoteMeta(botUsername))
	return strings.TrimSpace(re.ReplaceAllString(text, ""))
}

// StripStructuralPrefixes removes structural prefixes like forwarded-from
// markers and reply quotations.
func StripStructuralPrefixes(text string) string {
	// Strip leading reply quotation ("> ...").
	lines := strings.Split(text, "\n")
	var clean []string
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "> ") {
			continue
		}
		clean = append(clean, line)
	}
	return strings.TrimSpace(strings.Join(clean, "\n"))
}

// InboundDedupe tracks recently processed message IDs to prevent duplicates.
type InboundDedupe struct {
	seen    map[string]int64
	maxSize int
}

// NewInboundDedupe creates a new deduplication tracker.
func NewInboundDedupe(maxSize int) *InboundDedupe {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &InboundDedupe{
		seen:    make(map[string]int64),
		maxSize: maxSize,
	}
}

// IsDuplicate returns true if the message ID was recently processed.
func (d *InboundDedupe) IsDuplicate(messageID string) bool {
	if messageID == "" {
		return false
	}
	_, exists := d.seen[messageID]
	return exists
}

// Record marks a message ID as processed.
func (d *InboundDedupe) Record(messageID string) {
	if messageID == "" {
		return
	}
	// Evict oldest if at capacity.
	if len(d.seen) >= d.maxSize {
		var oldestKey string
		var oldestTs int64
		for k, v := range d.seen {
			if oldestTs == 0 || v < oldestTs {
				oldestKey = k
				oldestTs = v
			}
		}
		if oldestKey != "" {
			delete(d.seen, oldestKey)
		}
	}
	d.seen[messageID] = timeNowMs()
}

func timeNowMs() int64 {
	return time.Now().UnixMilli()
}
