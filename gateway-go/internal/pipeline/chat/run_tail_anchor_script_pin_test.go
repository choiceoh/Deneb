package chat

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The replay script (scripts/dev/telemachus_ab_replay.py) splices the SAME
// anchor the gateway sends, or its A/B proves nothing about production. The
// script keeps the text as a Python string constant; this test re-reads it so
// the two copies cannot drift silently.
func TestTelemachusReplayAnchorMatchesGateway(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "scripts", "dev", "telemachus_ab_replay.py")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("replay script not present at %s: %v", path, err)
	}
	// ANCHOR = ( "…"\n "…"\n … )  — concatenate the adjacent string literals.
	m := regexp.MustCompile(`(?s)ANCHOR = \((.*?)\n\)`).FindSubmatch(raw)
	if m == nil {
		t.Fatalf("ANCHOR constant not found in %s", path)
	}
	var sb strings.Builder
	for _, lit := range regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`).FindAllSubmatch(m[1], -1) {
		sb.WriteString(strings.ReplaceAll(string(lit[1]), `\n`, "\n"))
	}
	if got := sb.String(); got != responseLanguageAnchor {
		t.Fatalf("replay script anchor drifted from the gateway constant:\nscript:  %q\ngateway: %q", got, responseLanguageAnchor)
	}
}
