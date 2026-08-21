package mcpclient

// MCP 2026-07-28 ("MCP 2.0") on the client side.
//
// The revision removed the `initialize` handshake: a 2.0 server has no
// connection state to establish, and every request instead declares its own
// protocol version, client identity, and capabilities in `params._meta`.
//
// Which era a server speaks cannot be assumed — this gateway spawns whatever
// npx wrapper the operator configured, and those move at their own pace. So
// the client asks: `server/discover` is the one method every 2.0 server MUST
// implement, and a handshake-era server answers it with "method not found".
// That single probe is the era detector; everything else follows from the
// version it settles on.

import (
	"context"
	"encoding/json"
	"strings"
)

// protocolVersion2026 is the stateless revision — "MCP 2.0" in SDK numbering.
const protocolVersion2026 = "2026-07-28"

// Reserved `_meta` keys from the revision (the `io.modelcontextprotocol/`
// prefix is reserved for the spec, so these names are stable).
const (
	metaProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	metaClientInfo         = "io.modelcontextprotocol/clientInfo"
	metaClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	metaServerInfo         = "io.modelcontextprotocol/serverInfo"
)

// resultTypeInputRequired marks an MRTR interim result — the revision's
// replacement for server-initiated sampling/elicitation/roots requests.
const resultTypeInputRequired = "input_required"

// Client identity reported to servers, in both eras.
const (
	clientName    = "deneb-gateway"
	clientVersion = "1.0"
)

// handshakeResult is what one initialization attempt settles on, whichever
// era got us there.
type handshakeResult struct {
	serverName    string
	serverVersion string
	// protocol is the revision to speak for subsequent requests. Empty means
	// the handshake era, whose version was pinned by initialize and needs no
	// per-request declaration.
	protocol string
}

// statelessMeta is the per-request metadata 2026-07-28 requires in place of
// the handshake. Capabilities are declared per-request and servers MUST NOT
// infer them from an earlier one, so an empty object here is a standing
// statement: this client offers no sampling, elicitation, or roots.
func statelessMeta() map[string]any {
	return map[string]any{
		metaProtocolVersion:    protocolVersion2026,
		metaClientInfo:         map[string]any{"name": clientName, "version": clientVersion},
		metaClientCapabilities: map[string]any{},
	}
}

// withStatelessMeta attaches that metadata when the negotiated revision wants
// it, and leaves handshake-era params untouched.
func withStatelessMeta(params map[string]any, protocol string) map[string]any {
	if protocol != protocolVersion2026 {
		return params
	}
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = statelessMeta()
	return params
}

// protocol reports the revision settled on by the last successful handshake.
func (c *Client) protocol() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.negotiatedProtocol
}

// discover probes for the stateless revision. A false return is the ordinary
// answer from a handshake-era server, not a fault — the caller falls back to
// initialize.
func (c *Client) discover(ctx context.Context) (handshakeResult, bool) {
	// Bound the probe to half the init budget. A conforming server answers
	// instantly either way (result or "method not found"), but a server that
	// silently DROPS methods it does not implement would otherwise hold the
	// whole initialization hostage — this way the fallback to initialize still
	// gets the other half of the budget.
	probeCtx, cancel := context.WithTimeout(ctx, initTimeout/2)
	defer cancel()

	raw, err := c.probe(probeCtx, "server/discover", withStatelessMeta(nil, protocolVersion2026))
	if err != nil {
		c.logger.Debug("mcp server/discover unavailable; using initialize handshake",
			"cmd", c.argv[0], "error", err)
		return handshakeResult{}, false
	}
	var out struct {
		SupportedVersions []string `json:"supportedVersions"`
		Meta              struct {
			ServerInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"io.modelcontextprotocol/serverInfo"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		c.logger.Debug("mcp server/discover result unreadable; using initialize handshake",
			"cmd", c.argv[0], "error", err)
		return handshakeResult{}, false
	}
	// A server may implement discover for a revision we do not speak. Only a
	// mutually supported one lets us go stateless.
	if !containsVersion(out.SupportedVersions, protocolVersion2026) {
		c.logger.Debug("mcp server/discover lists no revision we speak; using initialize handshake",
			"cmd", c.argv[0], "supported", strings.Join(out.SupportedVersions, ","))
		return handshakeResult{}, false
	}
	c.logger.Info("mcp server discovered (stateless)",
		"server", out.Meta.ServerInfo.Name,
		"serverVersion", out.Meta.ServerInfo.Version,
		"protocolVersion", protocolVersion2026)
	return handshakeResult{
		serverName:    out.Meta.ServerInfo.Name,
		serverVersion: out.Meta.ServerInfo.Version,
		protocol:      protocolVersion2026,
	}, true
}

func containsVersion(versions []string, want string) bool {
	for _, v := range versions {
		if v == want {
			return true
		}
	}
	return false
}
