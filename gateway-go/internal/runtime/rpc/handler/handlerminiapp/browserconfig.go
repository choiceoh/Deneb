package handlerminiapp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// BrowserConfigDeps wires the remote browser-rules RPC to the gateway state dir.
type BrowserConfigDeps struct {
	DenebDir string
}

// BrowserQuirkOut is one remotely-delivered site compatibility quirk: a CSS
// stylesheet injected into pages on the listed hosts.
//
//deneb:wire
type BrowserQuirkOut struct {
	Hosts []string `json:"hosts"`
	Css   string   `json:"css"`
}

// BrowserConfigOut is the miniapp.browser.config.get payload. Entries are
// ADDITIVE: the native browser merges them onto its compiled-in defaults, so an
// empty or missing file simply means "built-ins only" and a bad entry can never
// disable the built-in blocklist.
//
//deneb:wire
type BrowserConfigOut struct {
	Version        int               `json:"version"`
	AdHostSuffixes []string          `json:"adHostSuffixes"`
	AdPathSegments []string          `json:"adPathSegments"`
	AdPathTokens   []string          `json:"adPathTokens"`
	AdQueryMarkers []string          `json:"adQueryMarkers"`
	Quirks         []BrowserQuirkOut `json:"quirks"`
}

// Validation budgets: generous for real fixes, tight enough that a fat-fingered
// file cannot bloat every client fetch or inject megabytes of CSS.
const (
	browserRulesMaxHosts       = 300
	browserRulesMaxTokens      = 100
	browserRulesMaxHostLen     = 253
	browserRulesMaxTokenLen    = 128
	browserRulesMaxQuirks      = 20
	browserRulesMaxQuirkHosts  = 20
	browserRulesMaxQuirkCssLen = 8 << 10
)

// BrowserConfigMethods registers the remote browser-rules RPC. The rules file
// (<denebDir>/browser-rules.json) is edited by the operator or the agent on the
// gateway host, so new ad/tracker hosts and site quirks ship WITHOUT an app
// release; phones pick the file up on their next fetch.
//
// File format (all fields optional, unknown fields ignored):
//
//	{
//	  "version": 3,
//	  "adHostSuffixes": ["ads.example.com"],
//	  "adPathSegments": ["/banners/"],
//	  "adPathTokens": ["ad.example/banners"],
//	  "adQueryMarkers": ["campaign_id="],
//	  "quirks": [{"hosts": ["example.com"], "css": "body{overflow:auto !important}"}]
//	}
//
// Invalid entries (bad host charset, "</style" in CSS, over-budget lists) are
// dropped individually; a malformed or absent file degrades to empty rules.
func BrowserConfigMethods(deps BrowserConfigDeps) map[string]rpcutil.HandlerFunc {
	if deps.DenebDir == "" {
		return nil
	}
	store := newBrowserRulesStore(filepath.Join(deps.DenebDir, "browser-rules.json"))
	return map[string]rpcutil.HandlerFunc{
		"miniapp.browser.config.get": authenticated(func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
			return rpcutil.RespondOK(req.ID, store.rules())
		}),
	}
}

// browserRulesStore serves the parsed rules file, re-reading only when the
// file's stat changes. Serve-path failures never error: a broken file must not
// take the rule fetch (or anything behind it) down.
type browserRulesStore struct {
	path string

	mu      sync.Mutex
	modTime time.Time
	size    int64
	loaded  bool
	cached  BrowserConfigOut
}

func newBrowserRulesStore(path string) *browserRulesStore {
	return &browserRulesStore{path: path}
}

func (s *browserRulesStore) rules() BrowserConfigOut {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, err := os.Stat(s.path)
	if err != nil || info.IsDir() {
		// File gone: stop serving the stale cache (deleted rules must win).
		s.loaded = false
		s.cached = emptyBrowserRules()
		return emptyBrowserRules()
	}
	if s.loaded && info.ModTime().Equal(s.modTime) && info.Size() == s.size {
		return s.cached
	}
	s.modTime = info.ModTime()
	s.size = info.Size()
	s.cached = parseBrowserRules(s.path)
	s.loaded = true
	return s.cached
}

// Non-nil empty lists on every path so the wire JSON carries [] rather than
// null — the Kotlin side deserializes into non-null lists.
func emptyBrowserRules() BrowserConfigOut {
	return BrowserConfigOut{
		AdHostSuffixes: []string{},
		AdPathSegments: []string{},
		AdPathTokens:   []string{},
		AdQueryMarkers: []string{},
		Quirks:         []BrowserQuirkOut{},
	}
}

func parseBrowserRules(path string) BrowserConfigOut {
	raw, err := os.ReadFile(path)
	if err != nil {
		return emptyBrowserRules()
	}
	var doc BrowserConfigOut
	if err := json.Unmarshal(raw, &doc); err != nil {
		return emptyBrowserRules()
	}
	return BrowserConfigOut{
		Version:        doc.Version,
		AdHostSuffixes: sanitizeHostList(doc.AdHostSuffixes, browserRulesMaxHosts),
		AdPathSegments: sanitizeTokenList(doc.AdPathSegments, browserRulesMaxTokens),
		AdPathTokens:   sanitizeTokenList(doc.AdPathTokens, browserRulesMaxTokens),
		AdQueryMarkers: sanitizeTokenList(doc.AdQueryMarkers, browserRulesMaxTokens),
		Quirks:         sanitizeQuirks(doc.Quirks),
	}
}

func sanitizeHostList(values []string, max int) []string {
	out := make([]string, 0, min(len(values), max))
	for _, v := range values {
		if len(out) >= max {
			break
		}
		host := strings.ToLower(strings.TrimSpace(v))
		if !validBrowserRuleHost(host) {
			continue
		}
		out = append(out, host)
	}
	return out
}

func sanitizeTokenList(values []string, max int) []string {
	out := make([]string, 0, min(len(values), max))
	for _, v := range values {
		if len(out) >= max {
			break
		}
		token := strings.TrimSpace(v)
		if token == "" || len(token) > browserRulesMaxTokenLen || !isPrintableASCII(token) {
			continue
		}
		out = append(out, token)
	}
	return out
}

func sanitizeQuirks(quirks []BrowserQuirkOut) []BrowserQuirkOut {
	out := make([]BrowserQuirkOut, 0, min(len(quirks), browserRulesMaxQuirks))
	for _, q := range quirks {
		if len(out) >= browserRulesMaxQuirks {
			break
		}
		css := strings.TrimSpace(q.Css)
		if css == "" || len(css) > browserRulesMaxQuirkCssLen ||
			strings.Contains(strings.ToLower(css), "</style") {
			continue
		}
		hosts := sanitizeHostList(q.Hosts, browserRulesMaxQuirkHosts)
		if len(hosts) == 0 {
			continue
		}
		out = append(out, BrowserQuirkOut{Hosts: hosts, Css: css})
	}
	return out
}

func validBrowserRuleHost(host string) bool {
	if host == "" || len(host) > browserRulesMaxHostLen {
		return false
	}
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}
