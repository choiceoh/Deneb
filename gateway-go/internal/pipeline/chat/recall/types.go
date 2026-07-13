package recall

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
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
	Wiki         *wiki.Store
	Transcript   toolport.TranscriptStore
	FileRecall   FileRecallFunc
	Org          func() (org.OrgTree, error)
	Briefcase    bool
	StrictErrors interface{ Record(error) }
	Now          func() time.Time
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
