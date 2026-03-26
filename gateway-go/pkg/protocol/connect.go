package protocol

import "fmt"

// ConnectParams represents the handshake payload sent by the client.
// Mirrors ConnectParamsSchema from src/gateway/protocol/schema/frames.ts.
type ConnectParams struct {
	MinProtocol int               `json:"minProtocol"`
	MaxProtocol int               `json:"maxProtocol"`
	Client      ConnectClientInfo `json:"client"`
	Caps        []string          `json:"caps,omitempty"`
	Commands    []string          `json:"commands,omitempty"`
	Permissions map[string]bool   `json:"permissions,omitempty"`
	PathEnv     string            `json:"pathEnv,omitempty"`
	Role        string            `json:"role,omitempty"`
	Scopes      []string          `json:"scopes,omitempty"`
	Device      *ConnectDevice    `json:"device,omitempty"`
	Auth        *ConnectAuth      `json:"auth,omitempty"`
	Locale      string            `json:"locale,omitempty"`
	UserAgent   string            `json:"userAgent,omitempty"`
}

// ConnectClientInfo identifies the connecting client.
type ConnectClientInfo struct {
	ID              string `json:"id"`
	DisplayName     string `json:"displayName,omitempty"`
	Version         string `json:"version"`
	Platform        string `json:"platform"`
	DeviceFamily    string `json:"deviceFamily,omitempty"`
	ModelIdentifier string `json:"modelIdentifier,omitempty"`
	Mode            string `json:"mode"`
	InstanceID      string `json:"instanceId,omitempty"`
}

// ConnectDevice contains device identity and proof for pairing.
type ConnectDevice struct {
	ID        string `json:"id"`
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
	SignedAt  int64  `json:"signedAt"`
	Nonce     string `json:"nonce"`
}

// ConnectAuth contains authentication credentials.
type ConnectAuth struct {
	Token          string `json:"token,omitempty"`
	BootstrapToken string `json:"bootstrapToken,omitempty"`
	DeviceToken    string `json:"deviceToken,omitempty"`
	Password       string `json:"password,omitempty"`
}

// HelloOk is the server's handshake response.
// Mirrors HelloOk message in proto/gateway.proto.
type HelloOk struct {
	Type          string        `json:"type"` // always "hello-ok"
	Protocol      int           `json:"protocol"`
	Server        HelloServer   `json:"server"`
	Features      HelloFeatures `json:"features"`
	Snapshot      Snapshot      `json:"snapshot"`
	CanvasHostURL string        `json:"canvasHostUrl,omitempty"`
	Auth          *HelloAuth    `json:"auth,omitempty"`
	Policy        HelloPolicy   `json:"policy"`
}

// HelloServer identifies the server.
type HelloServer struct {
	Version string `json:"version"`
	ConnID  string `json:"connId"`
}

// HelloFeatures lists available methods and events.
type HelloFeatures struct {
	Methods []string `json:"methods"`
	Events  []string `json:"events"`
}

// HelloAuth contains authentication result from the handshake.
type HelloAuth struct {
	DeviceToken string   `json:"deviceToken"`
	Role        string   `json:"role"`
	Scopes      []string `json:"scopes"`
	IssuedAtMs  *uint64  `json:"issuedAtMs,omitempty"`
}

// HelloPolicy communicates server limits to the client.
type HelloPolicy struct {
	MaxPayload       uint64 `json:"maxPayload"`
	MaxBufferedBytes uint64 `json:"maxBufferedBytes"`
	TickIntervalMs   uint64 `json:"tickIntervalMs"`
}

// Snapshot represents the initial state snapshot included in HelloOk.
type Snapshot struct {
	Health   any             `json:"health,omitempty"`
	Presence []PresenceEntry `json:"presence,omitempty"`
	Sessions any             `json:"sessions,omitempty"`
}

// PresenceEntry represents a connected client's presence.
// Mirrors PresenceEntry message in proto/gateway.proto.
type PresenceEntry struct {
	Host            string   `json:"host,omitempty"`
	IP              string   `json:"ip,omitempty"`
	Version         string   `json:"version,omitempty"`
	Platform        string   `json:"platform,omitempty"`
	DeviceFamily    string   `json:"deviceFamily,omitempty"`
	ModelIdentifier string   `json:"modelIdentifier,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	LastInputSecs   *uint64  `json:"lastInputSeconds,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	Text            string   `json:"text,omitempty"`
	Ts              uint64   `json:"ts"`
	DeviceID        string   `json:"deviceId,omitempty"`
	Roles           []string `json:"roles,omitempty"`
	Scopes          []string `json:"scopes,omitempty"`
	InstanceID      string   `json:"instanceId,omitempty"`
}

// ValidateConnectParams checks that required client fields are present.
func ValidateConnectParams(params *ConnectParams) error {
	if params.Client.ID == "" {
		return fmt.Errorf("client.id is required")
	}
	if params.Client.Version == "" {
		return fmt.Errorf("client.version is required")
	}
	if params.Client.Platform == "" {
		return fmt.Errorf("client.platform is required")
	}
	if params.Client.Mode == "" {
		return fmt.Errorf("client.mode is required")
	}
	return nil
}

// ValidateProtocolVersion checks whether the server's protocol version
// falls within the client's supported range.
func ValidateProtocolVersion(params *ConnectParams) bool {
	return params.MinProtocol <= ProtocolVersion && ProtocolVersion <= params.MaxProtocol
}
