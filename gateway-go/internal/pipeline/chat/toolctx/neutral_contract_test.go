package toolctx

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

var (
	_ ChatMessage              = chatport.ChatMessage{}
	_ chatport.ChatMessage     = ChatMessage{}
	_ TranscriptStore          = chatport.TranscriptStore(nil)
	_ chatport.TranscriptStore = TranscriptStore(nil)
)

func TestNeutralTranscriptAliasesPreserveWireAndHelpers(t *testing.T) {
	neutral := chatport.NewTextChatMessage("user", "hello", 42)
	compat := NewTextChatMessage("user", "hello", 42)

	neutralJSON, err := json.Marshal(neutral)
	if err != nil {
		t.Fatal(err)
	}
	compatJSON, err := json.Marshal(compat)
	if err != nil {
		t.Fatal(err)
	}
	if string(compatJSON) != string(neutralJSON) {
		t.Fatalf("toolctx JSON = %s, neutral JSON = %s", compatJSON, neutralJSON)
	}
	if compat.TextContent() != neutral.TextContent() || !compat.HasContent() {
		t.Fatalf("compat helper mismatch: %#v / %#v", compat, neutral)
	}

	names := []string{"web", "github:search"}
	if got, want := FormatFetchActivationNotice(names), chatport.FormatFetchActivationNotice(names); got != want {
		t.Fatalf("activation notice = %q, want %q", got, want)
	}
}
