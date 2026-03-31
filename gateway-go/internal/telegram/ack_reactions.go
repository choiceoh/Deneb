// Ack reaction gating — determines whether to send a message acknowledgement
// reaction based on scope, chat type, and mention state.
//
// Mirrors src/channels/ack-reactions.ts.
package telegram

// AckReactionScope controls when ack reactions are sent.
type AckReactionScope string

const (
	AckScopeAll           AckReactionScope = "all"
	AckScopeDirect        AckReactionScope = "direct"
	AckScopeGroupAll      AckReactionScope = "group-all"
	AckScopeGroupMentions AckReactionScope = "group-mentions"
	AckScopeOff           AckReactionScope = "off"
	AckScopeNone          AckReactionScope = "none"
)

// AckReactionGateParams holds the inputs for ack reaction gating.
type AckReactionGateParams struct {
	Scope                 AckReactionScope
	IsDirect              bool
	IsGroup               bool
	IsMentionableGroup    bool
	RequireMention        bool
	CanDetectMention      bool
	EffectiveWasMentioned bool
	ShouldBypassMention   bool
}

// ShouldAckReaction determines whether an ack reaction should be sent.
func ShouldAckReaction(p AckReactionGateParams) bool {
	scope := p.Scope
	if scope == "" {
		scope = AckScopeGroupMentions
	}
	switch scope {
	case AckScopeOff, AckScopeNone:
		return false
	case AckScopeAll:
		return true
	case AckScopeDirect:
		return p.IsDirect
	case AckScopeGroupAll:
		return p.IsGroup
	case AckScopeGroupMentions:
		if !p.IsMentionableGroup {
			return false
		}
		if !p.RequireMention {
			return false
		}
		if !p.CanDetectMention {
			return false
		}
		return p.EffectiveWasMentioned || p.ShouldBypassMention
	default:
		return false
	}
}

// RemoveAckReactionAfterReplyParams holds the inputs for post-reply ack removal.
type RemoveAckReactionAfterReplyParams struct {
	RemoveAfterReply bool
	DidAck           bool
	Remove           func() error
	OnError          func(err error)
}

// RemoveAckReactionAfterReply removes the ack reaction after a reply is sent,
// if configured to do so.
func RemoveAckReactionAfterReply(p RemoveAckReactionAfterReplyParams) {
	if !p.RemoveAfterReply || !p.DidAck || p.Remove == nil {
		return
	}
	if err := p.Remove(); err != nil && p.OnError != nil {
		p.OnError(err)
	}
}
