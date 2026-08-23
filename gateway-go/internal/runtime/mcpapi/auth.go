// auth.go — dedicated MCP token (improvement-ideas.md 5.5, personal MCP
// broker). DENEB_MCP_TOKEN optionally gives /mcp its OWN credential, carried
// in the same X-Deneb-Client-Token header. A tailnet-exposed broker client
// then never holds the full-privileged native client token — the MCP surface
// stays read-only and revocable independently. Unset (default) keeps the
// status quo: the single-user client token only.
package mcpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

// WithDedicatedToken returns an Authenticate that accepts token via the
// standard header before falling back to the normal client-token check.
// An empty token returns fallback unchanged.
func WithDedicatedToken(token string, fallback Authenticate) Authenticate {
	token = strings.TrimSpace(token)
	if token == "" {
		return fallback
	}
	return func(w http.ResponseWriter, r *http.Request) (*clientauth.Identity, bool) {
		presented := strings.TrimSpace(r.Header.Get(clientauth.Header))
		if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) == 1 {
			return &clientauth.Identity{
				User: &clientauth.User{
					ID:        1,
					FirstName: "Deneb MCP Broker",
				},
				AuthDate: time.Now(),
				ChatType: "private",
				Raw:      map[string]string{"auth": "mcp_token"},
			}, true
		}
		return fallback(w, r)
	}
}
