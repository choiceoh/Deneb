package mcpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

// The dedicated MCP token is accepted, a wrong token still falls through to
// the client-token authenticator (which rejects), and the client token keeps
// working — the two credentials coexist on /mcp.
func TestWithDedicatedTokenAcceptsBrokerTokenAndKeepsFallback(t *testing.T) {
	const brokerToken = "broker-secret-token"
	fallback := func(w http.ResponseWriter, r *http.Request) (*clientauth.Identity, bool) {
		if r.Header.Get(clientauth.Header) == "client-secret-token" {
			return &clientauth.Identity{User: &clientauth.User{ID: 1}}, true
		}
		http.Error(w, "invalid client token", http.StatusUnauthorized)
		return nil, false
	}
	auth := WithDedicatedToken(brokerToken, fallback)

	// Broker token authenticates with the broker identity.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set(clientauth.Header, brokerToken)
	if id, ok := auth(rec, req); !ok || id == nil || id.Raw["auth"] != "mcp_token" {
		t.Fatalf("broker token must authenticate, id=%+v ok=%v", id, ok)
	}

	// The client token still authenticates via the fallback.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set(clientauth.Header, "client-secret-token")
	if _, ok := auth(rec, req); !ok || rec.Code != http.StatusOK {
		t.Fatalf("client token must keep working, code=%d ok=%v", rec.Code, ok)
	}

	// A wrong token is rejected by the fallback (401 written there).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set(clientauth.Header, "wrong-token")
	if _, ok := auth(rec, req); ok || rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token must be rejected, code=%d ok=%v", rec.Code, ok)
	}

	// Empty dedicated token leaves the fallback untouched.
	if got := WithDedicatedToken("  ", fallback); got == nil {
		t.Fatal("empty dedicated token must return the fallback unchanged")
	}
}
