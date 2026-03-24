package protocol

import (
	"encoding/json"
	"testing"
)

func TestConnectParamsRoundTrip(t *testing.T) {
	params := ConnectParams{
		MinProtocol: 1,
		MaxProtocol: 3,
		Client: ConnectClientInfo{
			ID:       "control-ui",
			Version:  "1.0.0",
			Platform: "darwin",
			Mode:     "control",
		},
		Caps: []string{"streaming", "canvas"},
		Auth: &ConnectAuth{Token: "test-token"},
	}

	b, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ConnectParams
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.MinProtocol != 1 || decoded.MaxProtocol != 3 {
		t.Errorf("protocol range mismatch: %d-%d", decoded.MinProtocol, decoded.MaxProtocol)
	}
	if decoded.Client.ID != "control-ui" {
		t.Errorf("Client.ID = %q, want %q", decoded.Client.ID, "control-ui")
	}
	if decoded.Auth == nil || decoded.Auth.Token != "test-token" {
		t.Error("Auth.Token round-trip failed")
	}
}

func TestHelloOkRoundTrip(t *testing.T) {
	hello := HelloOk{
		Type:     "hello-ok",
		Protocol: ProtocolVersion,
		Server:   HelloServer{Version: "2025.1.0", ConnID: "conn-abc123"},
		Features: HelloFeatures{
			Methods: []string{"health", "sessions.list"},
			Events:  []string{"tick", "shutdown"},
		},
		Snapshot: Snapshot{},
		Policy: HelloPolicy{
			MaxPayload:       MaxPayloadBytes,
			MaxBufferedBytes: MaxBufferedBytes,
			TickIntervalMs:   TickIntervalMs,
		},
	}

	b, err := json.Marshal(hello)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded HelloOk
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Type != "hello-ok" {
		t.Errorf("Type = %q, want %q", decoded.Type, "hello-ok")
	}
	if decoded.Protocol != ProtocolVersion {
		t.Errorf("Protocol = %d, want %d", decoded.Protocol, ProtocolVersion)
	}
	if decoded.Policy.MaxPayload != MaxPayloadBytes {
		t.Errorf("MaxPayload = %d, want %d", decoded.Policy.MaxPayload, MaxPayloadBytes)
	}
}

func TestValidateProtocolVersion(t *testing.T) {
	tests := []struct {
		name string
		min  int
		max  int
		want bool
	}{
		{"exact match", 3, 3, true},
		{"range includes", 1, 5, true},
		{"min too high", 4, 5, false},
		{"max too low", 1, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := &ConnectParams{MinProtocol: tt.min, MaxProtocol: tt.max}
			got := ValidateProtocolVersion(params)
			if got != tt.want {
				t.Errorf("ValidateProtocolVersion(%d, %d) = %v, want %v", tt.min, tt.max, got, tt.want)
			}
		})
	}
}

func TestValidateConnectParams(t *testing.T) {
	valid := &ConnectParams{
		MinProtocol: 1,
		MaxProtocol: 3,
		Client: ConnectClientInfo{
			ID: "test", Version: "1.0", Platform: "linux", Mode: "control",
		},
	}
	if err := ValidateConnectParams(valid); err != nil {
		t.Errorf("valid params rejected: %v", err)
	}

	tests := []struct {
		name   string
		modify func(*ConnectParams)
	}{
		{"empty id", func(p *ConnectParams) { p.Client.ID = "" }},
		{"empty version", func(p *ConnectParams) { p.Client.Version = "" }},
		{"empty platform", func(p *ConnectParams) { p.Client.Platform = "" }},
		{"empty mode", func(p *ConnectParams) { p.Client.Mode = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := *valid
			p.Client = valid.Client // copy
			tt.modify(&p)
			if err := ValidateConnectParams(&p); err == nil {
				t.Error("expected error for invalid params")
			}
		})
	}
}
