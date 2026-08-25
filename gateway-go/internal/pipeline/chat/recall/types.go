package recall

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// OrgLoader loads the operator org chart for the recall org source.
type OrgLoader func() (org.OrgTree, error)

// Params contains only the turn metadata required by recall preflight.
type Params struct {
	SessionKey    string
	Message       string
	EphemeralUser bool
	// AllowRecall lets an ephemeral turn through this gate — see
	// runstate.RunParams.AllowRecall for why the two are separate.
	AllowRecall bool
	SkipRecall  bool
}

// recallSuppressed reports whether this turn must not run the preflight.
//
// The two inputs are independent and only one of them is the user's own choice.
// SkipRecall is the native client's "focused chat / memory off" toggle, so it
// wins unconditionally. EphemeralUser only says the inbound message is not
// persisted; a caller that has a real subject anyway says so with AllowRecall.
func (p Params) recallSuppressed() bool {
	if p.SkipRecall {
		return true
	}
	return p.EphemeralUser && !p.AllowRecall
}

// Deps contains the optional recall evidence sources. Nil fields disable their source.
type Deps struct {
	Wiki       *wiki.Store
	Transcript toolport.TranscriptStore
	FileRecall FileRecallFunc
	Org        OrgLoader
	Briefcase  bool
	// SelfFactsInSystemPrompt reports that this turn's system prompt already
	// carries the generated self-fact projection (workspace MEMORY.md). When it
	// does, repeating self claims in the per-turn <current-facts> block is pure
	// duplication — measured 2026-08-25: the block's 18 self claims were a
	// strict subset of the projection's 46. Subject facts (a person or client
	// named in THIS message) are never in the projection and always render.
	//
	// False keeps every self claim in the block. That is the correct default
	// and the only safe value whenever the projection is absent: the briefcase
	// preset loads no context files at all, and a degraded projection window
	// suppresses the generated files (chat.WithoutFactDerivedFiles).
	SelfFactsInSystemPrompt bool
	StrictErrors            interface{ Record(error) }
	Now                     func() time.Time
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d Deps) recordStrictError(err error) {
	if d.StrictErrors != nil && err != nil {
		d.StrictErrors.Record(err)
	}
}

func abbreviateSession(key string) string {
	const (
		clientPrefix = "client:"
		shortPrefix  = "cl:"
	)
	if len(key) > len(clientPrefix) && key[:len(clientPrefix)] == clientPrefix {
		return shortPrefix + key[len(clientPrefix):]
	}
	return key
}
