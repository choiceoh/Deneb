package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestProviderMetaJSON(t *testing.T) {
	meta := protocol.ProviderMeta{
		ID:      "anthropic",
		Label:   "Anthropic",
		Aliases: []string{"claude"},
		EnvVars: []string{"ANTHROPIC_API_KEY"},
	}
	data := testutil.Must(json.Marshal(meta))
	var decoded protocol.ProviderMeta
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ID != "anthropic" {
		t.Errorf("ID = %q, want %q", decoded.ID, "anthropic")
	}
	if len(decoded.Aliases) != 1 || decoded.Aliases[0] != "claude" {
		t.Errorf("Aliases = %v, want [claude]", decoded.Aliases)
	}
}
