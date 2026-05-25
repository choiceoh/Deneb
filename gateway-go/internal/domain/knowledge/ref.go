package knowledge

import (
	"fmt"
	"strings"
)

// Layer identifies the storage backend a Ref points to.
type Layer string

const (
	LayerWiki      Layer = "w"
	LayerHindsight Layer = "h"
)

// Ref is a unified handle to a piece of knowledge across layers. Prefix-based
// (vs opaque) so refs are human-debuggable in logs, grep, and chat output.
type Ref struct {
	Layer Layer
	ID    string // wiki: page relative path; hindsight: bank memory id
}

// String renders the ref in its canonical wire form (e.g. "w:인물/박부장").
func (r Ref) String() string {
	if r.Layer == "" {
		return ""
	}
	return string(r.Layer) + ":" + r.ID
}

// ParseRef decodes a canonical "<layer>:<id>" ref. Returns an error when the
// prefix is missing, malformed, or names an unknown layer.
func ParseRef(s string) (Ref, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Ref{}, fmt.Errorf("empty ref")
	}
	idx := strings.IndexByte(s, ':')
	if idx <= 0 || idx == len(s)-1 {
		return Ref{}, fmt.Errorf("invalid ref %q: expected <layer>:<id>", s)
	}
	layer := Layer(s[:idx])
	id := strings.TrimSpace(s[idx+1:])
	if id == "" {
		return Ref{}, fmt.Errorf("invalid ref %q: empty id", s)
	}
	switch layer {
	case LayerWiki, LayerHindsight:
		return Ref{Layer: layer, ID: id}, nil
	default:
		return Ref{}, fmt.Errorf("unknown layer %q in ref %q", layer, s)
	}
}
