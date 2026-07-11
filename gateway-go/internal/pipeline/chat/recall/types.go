package recall

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// Params contains only the turn metadata required by recall preflight.
type Params struct {
	SessionKey    string
	Message       string
	EphemeralUser bool
	SkipRecall    bool
}

// Deps contains the optional recall evidence sources. Nil fields disable their source.
type Deps struct {
	Wiki       *wiki.Store
	Transcript toolctx.TranscriptStore
	FileRecall FileRecallFunc
	Org        func() (org.OrgTree, error)
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
