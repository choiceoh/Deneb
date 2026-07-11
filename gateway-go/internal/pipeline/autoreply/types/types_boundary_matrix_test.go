package types

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBoundaryNormalizeVerboseAliasMatrix(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want VerboseLevel
		ok   bool
	}{
		{name: "empty", raw: "", want: "", ok: false},
		{name: "spaces", raw: "   ", want: "", ok: false},
		{name: "tab newline", raw: "\t\n", want: "", ok: false},
		{name: "off", raw: "off", want: VerboseOff, ok: true},
		{name: "false", raw: "false", want: VerboseOff, ok: true},
		{name: "no", raw: "no", want: VerboseOff, ok: true},
		{name: "zero", raw: "0", want: VerboseOff, ok: true},
		{name: "off uppercase", raw: "OFF", want: VerboseOff, ok: true},
		{name: "off title case", raw: "Off", want: VerboseOff, ok: true},
		{name: "off padded", raw: " \tOFF\n", want: VerboseOff, ok: true},
		{name: "full", raw: "full", want: VerboseFull, ok: true},
		{name: "all", raw: "all", want: VerboseFull, ok: true},
		{name: "everything", raw: "everything", want: VerboseFull, ok: true},
		{name: "full uppercase", raw: "FULL", want: VerboseFull, ok: true},
		{name: "everything padded", raw: " everything ", want: VerboseFull, ok: true},
		{name: "on", raw: "on", want: VerboseOn, ok: true},
		{name: "minimal", raw: "minimal", want: VerboseOn, ok: true},
		{name: "true", raw: "true", want: VerboseOn, ok: true},
		{name: "yes", raw: "yes", want: VerboseOn, ok: true},
		{name: "one", raw: "1", want: VerboseOn, ok: true},
		{name: "minimal mixed case", raw: "MiNiMaL", want: VerboseOn, ok: true},
		{name: "unknown two", raw: "2", want: "", ok: false},
		{name: "unknown verbose", raw: "verbose", want: "", ok: false},
		{name: "unknown enabled", raw: "enabled", want: "", ok: false},
		{name: "prefix", raw: "onward", want: "", ok: false},
		{name: "suffix", raw: "turn-off", want: "", ok: false},
		{name: "non ascii", raw: "켜기", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeVerboseLevel(tt.raw)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("NormalizeVerboseLevel(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestBoundaryNormalizeElevatedAliasMatrix(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ElevatedLevel
		ok   bool
	}{
		{name: "empty", raw: "", want: "", ok: false},
		{name: "spaces", raw: "  ", want: "", ok: false},
		{name: "off", raw: "off", want: ElevatedOff, ok: true},
		{name: "false", raw: "false", want: ElevatedOff, ok: true},
		{name: "no", raw: "no", want: ElevatedOff, ok: true},
		{name: "zero", raw: "0", want: ElevatedOff, ok: true},
		{name: "off mixed case", raw: "oFf", want: ElevatedOff, ok: true},
		{name: "off padded", raw: "\n off\t", want: ElevatedOff, ok: true},
		{name: "full", raw: "full", want: ElevatedFull, ok: true},
		{name: "auto", raw: "auto", want: ElevatedFull, ok: true},
		{name: "auto approve hyphen", raw: "auto-approve", want: ElevatedFull, ok: true},
		{name: "autoapprove compact", raw: "autoapprove", want: ElevatedFull, ok: true},
		{name: "auto uppercase", raw: "AUTO", want: ElevatedFull, ok: true},
		{name: "ask", raw: "ask", want: ElevatedAsk, ok: true},
		{name: "prompt", raw: "prompt", want: ElevatedAsk, ok: true},
		{name: "approval", raw: "approval", want: ElevatedAsk, ok: true},
		{name: "approve", raw: "approve", want: ElevatedAsk, ok: true},
		{name: "approval padded", raw: " APPROVAL ", want: ElevatedAsk, ok: true},
		{name: "on", raw: "on", want: ElevatedOn, ok: true},
		{name: "true", raw: "true", want: ElevatedOn, ok: true},
		{name: "yes", raw: "yes", want: ElevatedOn, ok: true},
		{name: "one", raw: "1", want: ElevatedOn, ok: true},
		{name: "unknown auto underscore", raw: "auto_approve", want: "", ok: false},
		{name: "unknown always", raw: "always", want: "", ok: false},
		{name: "unknown two", raw: "2", want: "", ok: false},
		{name: "prefix", raw: "asking", want: "", ok: false},
		{name: "non ascii", raw: "승인", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeElevatedLevel(tt.raw)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("NormalizeElevatedLevel(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestBoundaryNormalizeReasoningAliasMatrix(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ReasoningLevel
		ok   bool
	}{
		{name: "empty", raw: "", want: "", ok: false},
		{name: "whitespace", raw: " \t\n", want: "", ok: false},
		{name: "off", raw: "off", want: ReasoningOff, ok: true},
		{name: "false", raw: "false", want: ReasoningOff, ok: true},
		{name: "no", raw: "no", want: ReasoningOff, ok: true},
		{name: "zero", raw: "0", want: ReasoningOff, ok: true},
		{name: "hide", raw: "hide", want: ReasoningOff, ok: true},
		{name: "hidden", raw: "hidden", want: ReasoningOff, ok: true},
		{name: "disable", raw: "disable", want: ReasoningOff, ok: true},
		{name: "disabled", raw: "disabled", want: ReasoningOff, ok: true},
		{name: "hidden mixed case", raw: "HiDdEn", want: ReasoningOff, ok: true},
		{name: "on", raw: "on", want: ReasoningOn, ok: true},
		{name: "true", raw: "true", want: ReasoningOn, ok: true},
		{name: "yes", raw: "yes", want: ReasoningOn, ok: true},
		{name: "one", raw: "1", want: ReasoningOn, ok: true},
		{name: "show", raw: "show", want: ReasoningOn, ok: true},
		{name: "visible", raw: "visible", want: ReasoningOn, ok: true},
		{name: "enable", raw: "enable", want: ReasoningOn, ok: true},
		{name: "enabled", raw: "enabled", want: ReasoningOn, ok: true},
		{name: "visible padded", raw: "  VISIBLE ", want: ReasoningOn, ok: true},
		{name: "stream", raw: "stream", want: ReasoningStream, ok: true},
		{name: "streaming", raw: "streaming", want: ReasoningStream, ok: true},
		{name: "draft", raw: "draft", want: ReasoningStream, ok: true},
		{name: "live", raw: "live", want: ReasoningStream, ok: true},
		{name: "stream uppercase", raw: "STREAM", want: ReasoningStream, ok: true},
		{name: "unknown auto", raw: "auto", want: "", ok: false},
		{name: "unknown full", raw: "full", want: "", ok: false},
		{name: "unknown two", raw: "2", want: "", ok: false},
		{name: "prefix", raw: "lively", want: "", ok: false},
		{name: "non ascii", raw: "표시", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeReasoningLevel(tt.raw)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("NormalizeReasoningLevel(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestBoundaryNormalizeFastModeAliasMatrix(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
		ok   bool
	}{
		{name: "empty", raw: "", want: false, ok: false},
		{name: "whitespace", raw: "  ", want: false, ok: false},
		{name: "off", raw: "off", want: false, ok: true},
		{name: "false", raw: "false", want: false, ok: true},
		{name: "no", raw: "no", want: false, ok: true},
		{name: "zero", raw: "0", want: false, ok: true},
		{name: "disable", raw: "disable", want: false, ok: true},
		{name: "disabled", raw: "disabled", want: false, ok: true},
		{name: "normal", raw: "normal", want: false, ok: true},
		{name: "normal mixed case", raw: "NoRmAl", want: false, ok: true},
		{name: "normal padded", raw: "\t normal \n", want: false, ok: true},
		{name: "on", raw: "on", want: true, ok: true},
		{name: "true", raw: "true", want: true, ok: true},
		{name: "yes", raw: "yes", want: true, ok: true},
		{name: "one", raw: "1", want: true, ok: true},
		{name: "enable", raw: "enable", want: true, ok: true},
		{name: "enabled", raw: "enabled", want: true, ok: true},
		{name: "fast", raw: "fast", want: true, ok: true},
		{name: "fast uppercase", raw: "FAST", want: true, ok: true},
		{name: "unknown auto", raw: "auto", want: false, ok: false},
		{name: "unknown slow", raw: "slow", want: false, ok: false},
		{name: "unknown two", raw: "2", want: false, ok: false},
		{name: "prefix", raw: "faster", want: false, ok: false},
		{name: "non ascii", raw: "빠름", want: false, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeFastMode(tt.raw)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("NormalizeFastMode(%q) = (%v, %v), want (%v, %v)", tt.raw, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestBoundaryNormalizeUsageDisplayAliasMatrix(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want UsageDisplayLevel
		ok   bool
	}{
		{name: "empty", raw: "", want: "", ok: false},
		{name: "whitespace", raw: "\t\n", want: "", ok: false},
		{name: "off", raw: "off", want: UsageOff, ok: true},
		{name: "false", raw: "false", want: UsageOff, ok: true},
		{name: "no", raw: "no", want: UsageOff, ok: true},
		{name: "zero", raw: "0", want: UsageOff, ok: true},
		{name: "disable", raw: "disable", want: UsageOff, ok: true},
		{name: "disabled", raw: "disabled", want: UsageOff, ok: true},
		{name: "off mixed case", raw: "OfF", want: UsageOff, ok: true},
		{name: "on", raw: "on", want: UsageTokens, ok: true},
		{name: "true", raw: "true", want: UsageTokens, ok: true},
		{name: "yes", raw: "yes", want: UsageTokens, ok: true},
		{name: "one", raw: "1", want: UsageTokens, ok: true},
		{name: "enable", raw: "enable", want: UsageTokens, ok: true},
		{name: "enabled", raw: "enabled", want: UsageTokens, ok: true},
		{name: "tokens", raw: "tokens", want: UsageTokens, ok: true},
		{name: "token", raw: "token", want: UsageTokens, ok: true},
		{name: "tok", raw: "tok", want: UsageTokens, ok: true},
		{name: "minimal", raw: "minimal", want: UsageTokens, ok: true},
		{name: "min", raw: "min", want: UsageTokens, ok: true},
		{name: "tokens uppercase padded", raw: " TOKENS ", want: UsageTokens, ok: true},
		{name: "full", raw: "full", want: UsageFull, ok: true},
		{name: "session", raw: "session", want: UsageFull, ok: true},
		{name: "session mixed case", raw: "SeSsIoN", want: UsageFull, ok: true},
		{name: "unknown everything", raw: "everything", want: "", ok: false},
		{name: "unknown usage", raw: "usage", want: "", ok: false},
		{name: "unknown two", raw: "2", want: "", ok: false},
		{name: "prefix", raw: "tokenized", want: "", ok: false},
		{name: "non ascii", raw: "토큰", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeUsageDisplay(tt.raw)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("NormalizeUsageDisplay(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestBoundaryNormalizersRejectNonExactTokens(t *testing.T) {
	invalid := []string{
		" on off ",
		"true false",
		"yes,no",
		"0x1",
		"01",
		"+1",
		"-0",
		"enabled!",
		"off.",
		"full/full",
		"stream\x00",
		"auto approve",
		"auto--approve",
		"min tokens",
		"\u200bon\u200b",
	}
	for _, raw := range invalid {
		t.Run(fmt.Sprintf("raw_%q", raw), func(t *testing.T) {
			if got, ok := NormalizeVerboseLevel(raw); ok || got != "" {
				t.Errorf("verbose accepted %q as %q", raw, got)
			}
			if got, ok := NormalizeElevatedLevel(raw); ok || got != "" {
				t.Errorf("elevated accepted %q as %q", raw, got)
			}
			if got, ok := NormalizeReasoningLevel(raw); ok || got != "" {
				t.Errorf("reasoning accepted %q as %q", raw, got)
			}
			if got, ok := NormalizeFastMode(raw); ok || got {
				t.Errorf("fast accepted %q as %v", raw, got)
			}
			if got, ok := NormalizeUsageDisplay(raw); ok || got != "" {
				t.Errorf("usage accepted %q as %q", raw, got)
			}
		})
	}
}

func TestBoundaryReplyPayloadJSONOmitEmptyMatrix(t *testing.T) {
	tests := []struct {
		name    string
		payload ReplyPayload
		want    string
	}{
		{
			name:    "zero value",
			payload: ReplyPayload{},
			want:    `{}`,
		},
		{
			name:    "text",
			payload: ReplyPayload{Text: "hello"},
			want:    `{"text":"hello"}`,
		},
		{
			name:    "media URL",
			payload: ReplyPayload{MediaURL: "https://example.test/a.png"},
			want:    `{"mediaUrl":"https://example.test/a.png"}`,
		},
		{
			name:    "media URLs",
			payload: ReplyPayload{MediaURLs: []string{"a", "b"}},
			want:    `{"mediaUrls":["a","b"]}`,
		},
		{
			name:    "empty media URLs omitted",
			payload: ReplyPayload{MediaURLs: []string{}},
			want:    `{}`,
		},
		{
			name:    "reply ID",
			payload: ReplyPayload{ReplyToID: "parent-1"},
			want:    `{"replyToId":"parent-1"}`,
		},
		{
			name:    "audio voice true",
			payload: ReplyPayload{AudioAsVoice: true},
			want:    `{"audioAsVoice":true}`,
		},
		{
			name:    "audio voice false omitted",
			payload: ReplyPayload{AudioAsVoice: false},
			want:    `{}`,
		},
		{
			name:    "error true",
			payload: ReplyPayload{IsError: true},
			want:    `{"isError":true}`,
		},
		{
			name:    "error false omitted",
			payload: ReplyPayload{IsError: false},
			want:    `{}`,
		},
		{
			name:    "channel data",
			payload: ReplyPayload{ChannelData: map[string]any{"thread": "t1"}},
			want:    `{"channelData":{"thread":"t1"}}`,
		},
		{
			name:    "empty channel data omitted",
			payload: ReplyPayload{ChannelData: map[string]any{}},
			want:    `{}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.payload)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(raw) != tt.want {
				t.Fatalf("JSON = %s, want %s", raw, tt.want)
			}
		})
	}
}

func TestBoundaryReplyPayloadJSONRoundTripPreservesWireFields(t *testing.T) {
	want := ReplyPayload{
		Text:         "결과",
		MediaURL:     "https://example.test/primary.png",
		MediaURLs:    []string{"https://example.test/one.png", "https://example.test/two.png"},
		ReplyToID:    "reply-parent",
		AudioAsVoice: true,
		IsError:      true,
		ChannelData: map[string]any{
			"thread":  "thread-7",
			"attempt": float64(3),
			"nested":  map[string]any{"ok": true},
		},
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got ReplyPayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestBoundaryReplyPayloadRejectsMalformedJSON(t *testing.T) {
	inputs := []string{
		`{`,
		`[]`,
		`"text"`,
		`{"text":`,
		`{"text":1}`,
		`{"mediaUrls":"one"}`,
		`{"audioAsVoice":"yes"}`,
		`{"isError":1}`,
		`{"channelData":[]}`,
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			var got ReplyPayload
			if err := json.Unmarshal([]byte(input), &got); err == nil {
				t.Fatalf("malformed or type-invalid payload accepted: %#v", got)
			}
		})
	}
}

func TestBoundaryMessagingToolTargetJSONMatrix(t *testing.T) {
	tests := []struct {
		name   string
		target MessagingToolTarget
		want   string
	}{
		{
			name:   "zero value retains required to",
			target: MessagingToolTarget{},
			want:   `{"to":""}`,
		},
		{
			name:   "to only",
			target: MessagingToolTarget{To: "+821012345678"},
			want:   `{"to":"+821012345678"}`,
		},
		{
			name:   "provider and to",
			target: MessagingToolTarget{Provider: "telegram", To: "chat-1"},
			want:   `{"provider":"telegram","to":"chat-1"}`,
		},
		{
			name:   "all fields",
			target: MessagingToolTarget{Provider: "native", To: "device", AccountID: "account-9"},
			want:   `{"provider":"native","to":"device","accountId":"account-9"}`,
		},
		{
			name:   "empty optional fields omitted",
			target: MessagingToolTarget{Provider: "", To: "device", AccountID: ""},
			want:   `{"to":"device"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.target)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tt.want {
				t.Fatalf("JSON = %s, want %s", raw, tt.want)
			}
		})
	}
}

func TestBoundarySessionOriginPromotionAcrossContexts(t *testing.T) {
	origin := SessionOrigin{
		SessionKey: "client:main:project",
		Channel:    "native",
		AccountID:  "account-1",
		ThreadID:   "thread-2",
		IsGroup:    true,
	}
	msg := MsgContext{SessionOrigin: origin}
	template := TemplateContext{AgentID: "main", SessionOrigin: origin}
	block := BlockReplyContext{To: "device", SessionOrigin: origin}
	session := SessionState{SessionOrigin: origin}

	checks := []struct {
		name       string
		sessionKey string
		channel    string
		accountID  string
		threadID   string
		isGroup    bool
	}{
		{name: "message", sessionKey: msg.SessionKey, channel: msg.Channel, accountID: msg.AccountID, threadID: msg.ThreadID, isGroup: msg.IsGroup},
		{name: "template", sessionKey: template.SessionKey, channel: template.Channel, accountID: template.AccountID, threadID: template.ThreadID, isGroup: template.IsGroup},
		{name: "block", sessionKey: block.SessionKey, channel: block.Channel, accountID: block.AccountID, threadID: block.ThreadID, isGroup: block.IsGroup},
		{name: "session", sessionKey: session.SessionKey, channel: session.Channel, accountID: session.AccountID, threadID: session.ThreadID, isGroup: session.IsGroup},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if check.sessionKey != origin.SessionKey ||
				check.channel != origin.Channel ||
				check.accountID != origin.AccountID ||
				check.threadID != origin.ThreadID ||
				check.isGroup != origin.IsGroup {
				t.Fatalf("promoted routing fields changed: %#v", check)
			}
		})
	}

	msg.SessionKey = "message-only"
	if template.SessionKey != origin.SessionKey || block.SessionKey != origin.SessionKey || session.SessionKey != origin.SessionKey {
		t.Fatal("mutating one embedded origin changed another value copy")
	}
}

func TestBoundaryMediaContextPromotionAndSliceOwnership(t *testing.T) {
	paths := []string{"/tmp/a.png", "/tmp/b.pdf"}
	urls := []string{"https://example.test/a.png", "https://example.test/b.pdf"}
	types := []string{"image/png", "application/pdf"}
	msg := MsgContext{MediaContext: MediaContext{
		MediaPath:       paths[0],
		MediaPaths:      append([]string(nil), paths...),
		MediaURL:        urls[0],
		MediaURLs:       append([]string(nil), urls...),
		MediaType:       types[0],
		MediaTypes:      append([]string(nil), types...),
		MediaRemoteHost: "dgx-spark",
	}}

	if msg.MediaPath != paths[0] || msg.MediaURL != urls[0] || msg.MediaType != types[0] {
		t.Fatalf("single media fields = %#v", msg.MediaContext)
	}
	if !reflect.DeepEqual(msg.MediaPaths, paths) || !reflect.DeepEqual(msg.MediaURLs, urls) || !reflect.DeepEqual(msg.MediaTypes, types) {
		t.Fatalf("multi media fields = %#v", msg.MediaContext)
	}
	if msg.MediaRemoteHost != "dgx-spark" {
		t.Fatalf("remote host = %q", msg.MediaRemoteHost)
	}

	paths[0] = "caller-mutated"
	urls[0] = "caller-mutated"
	types[0] = "caller-mutated"
	if msg.MediaPaths[0] == "caller-mutated" || msg.MediaURLs[0] == "caller-mutated" || msg.MediaTypes[0] == "caller-mutated" {
		t.Fatal("fixture did not establish independent media slice ownership")
	}
}

func TestBoundaryMsgContextConcernFieldsDoNotAlias(t *testing.T) {
	msg := MsgContext{
		Body:            "normalized",
		BodyForAgent:    "agent body",
		BodyForCommands: "/command",
		RawBody:         " raw ",
		From:            "sender",
		To:              "receiver",
		MessageSid:      "message-1",
		ReplyToID:       "parent-1",
		SessionOrigin: SessionOrigin{
			SessionKey: "client:main",
			Channel:    "native",
		},
		SenderInfo: SenderInfo{
			SenderID:      "operator-1",
			SenderName:    "선택님",
			ForwardedFrom: "assistant",
			WasMentioned:  true,
			ChatType:      "direct",
		},
		CommandControl: CommandControl{
			CommandBody:       "status",
			CommandAuthorized: true,
			CommandSource:     "native",
		},
	}

	before := msg
	msg.Body = "changed"
	if msg.BodyForAgent != before.BodyForAgent ||
		msg.BodyForCommands != before.BodyForCommands ||
		msg.RawBody != before.RawBody {
		t.Fatal("mutating Body changed another body stage")
	}
	msg.SenderName = "changed sender"
	if msg.SenderID != before.SenderID || msg.From != before.From {
		t.Fatal("mutating SenderName changed another sender field")
	}
	msg.CommandBody = "changed command"
	if msg.BodyForCommands != before.BodyForCommands || msg.CommandAuthorized != before.CommandAuthorized {
		t.Fatal("mutating CommandBody changed command authorization or input body")
	}
}

func TestBoundarySessionExpiryAndIdleSafetyMatrix(t *testing.T) {
	now := time.Now().UnixMilli()
	tests := []struct {
		name    string
		stamp   int64
		limit   int64
		expired bool
		idle    bool
	}{
		{name: "zero stamp disabled", stamp: 0, limit: 1, expired: false, idle: false},
		{name: "negative stamp disabled", stamp: -1, limit: 1, expired: false, idle: false},
		{name: "zero limit disabled", stamp: now - 100000, limit: 0, expired: false, idle: false},
		{name: "negative limit disabled", stamp: now - 100000, limit: -1, expired: false, idle: false},
		{name: "fresh stamp", stamp: now, limit: 60000, expired: false, idle: false},
		{name: "future stamp", stamp: now + 60000, limit: 1, expired: false, idle: false},
		{name: "clearly old stamp", stamp: now - 60000, limit: 1000, expired: true, idle: true},
		{name: "one hour old", stamp: now - int64(time.Hour/time.Millisecond), limit: int64(time.Minute / time.Millisecond), expired: true, idle: true},
		{name: "one day old", stamp: now - int64(24*time.Hour/time.Millisecond), limit: int64(time.Hour / time.Millisecond), expired: true, idle: true},
		{name: "maximum limit", stamp: now - 1000, limit: int64(^uint64(0) >> 1), expired: false, idle: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := SessionResetPolicy{MaxAgeMs: tt.limit, MaxIdleMs: tt.limit}
			if got := IsSessionExpired(tt.stamp, policy); got != tt.expired {
				t.Fatalf("IsSessionExpired(%d, %d) = %v, want %v", tt.stamp, tt.limit, got, tt.expired)
			}
			if got := IsSessionIdle(tt.stamp, policy); got != tt.idle {
				t.Fatalf("IsSessionIdle(%d, %d) = %v, want %v", tt.stamp, tt.limit, got, tt.idle)
			}
		})
	}
}

func TestBoundaryDefaultResetPolicyReturnsIndependentValues(t *testing.T) {
	one := DefaultSessionResetPolicy()
	two := DefaultSessionResetPolicy()
	want := SessionResetPolicy{}
	if one != want || two != want {
		t.Fatalf("defaults = %#v and %#v, want %#v", one, two, want)
	}
	one.MaxAgeMs = 100
	one.MaxIdleMs = 200
	one.OnNewAgent = true
	if two != want {
		t.Fatalf("mutating first policy changed second: %#v", two)
	}
}

func TestBoundaryDeliverFuncDispatchAndErrorPropagation(t *testing.T) {
	wantErr := errors.New("delivery failed")
	tests := []struct {
		name    string
		kind    ReplyDispatchKind
		payload ReplyPayload
		err     error
	}{
		{name: "tool", kind: DispatchKindTool, payload: ReplyPayload{Text: "tool result"}},
		{name: "block", kind: DispatchKindBlock, payload: ReplyPayload{Text: "block result"}},
		{name: "final", kind: DispatchKindFinal, payload: ReplyPayload{Text: "final result"}},
		{name: "custom kind remains representable", kind: ReplyDispatchKind("future"), payload: ReplyPayload{Text: "future result"}},
		{name: "error propagates", kind: DispatchKindFinal, payload: ReplyPayload{Text: "failed"}, err: wantErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPayload ReplyPayload
			var gotKind ReplyDispatchKind
			calls := 0
			fn := DeliverFunc(func(_ context.Context, payload ReplyPayload, kind ReplyDispatchKind) error {
				calls++
				gotPayload = payload
				gotKind = kind
				return tt.err
			})
			err := fn(context.Background(), tt.payload, tt.kind)
			if !errors.Is(err, tt.err) {
				t.Fatalf("error = %v, want %v", err, tt.err)
			}
			if calls != 1 || !reflect.DeepEqual(gotPayload, tt.payload) || gotKind != tt.kind {
				t.Fatalf("delivery observation calls=%d payload=%#v kind=%q", calls, gotPayload, gotKind)
			}
		})
	}
}

func TestBoundaryDeliverFuncSeesContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fn := DeliverFunc(func(ctx context.Context, _ ReplyPayload, _ ReplyDispatchKind) error {
		return ctx.Err()
	})
	if err := fn(ctx, ReplyPayload{}, DispatchKindFinal); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestBoundaryGetReplyCallbacksCarryExactValues(t *testing.T) {
	var starts []AgentRunStartParams
	var blocks []ReplyPayload
	var tools []ReplyPayload
	var replyStarts int
	var cleanupCalls int
	fast := true
	systemPrompt := "system"
	label := "label"
	opts := GetReplyOptions{
		RunID:                  "run-1",
		IsHeartbeat:            true,
		HeartbeatModelOverride: "model-heartbeat",
		TypingPolicy:           TypingPolicyHeartbeat,
		SuppressTyping:         true,
		SuppressToolErrors:     true,
		SkillFilter:            []string{"wiki", "calendar"},
		TimeoutOverrideMs:      1234,
		ContextTokens:          32000,
		MaxTokens:              4096,
		OnAgentRunStart: func(params AgentRunStartParams) {
			starts = append(starts, params)
		},
		OnReplyStart: func() {
			replyStarts++
		},
		OnTypingCleanup: func() {
			cleanupCalls++
		},
		OnBlockReply: func(payload ReplyPayload) {
			blocks = append(blocks, payload)
		},
		OnToolResult: func(payload ReplyPayload) {
			tools = append(tools, payload)
		},
	}
	_ = SessionModification{FastMode: &fast, SystemPrompt: &systemPrompt, Label: &label}

	start := AgentRunStartParams{SessionKey: "client:main", RunID: opts.RunID, Model: "model", Provider: "provider"}
	block := ReplyPayload{Text: "block"}
	tool := ReplyPayload{Text: "tool"}
	opts.OnAgentRunStart(start)
	opts.OnReplyStart()
	opts.OnTypingCleanup()
	opts.OnBlockReply(block)
	opts.OnToolResult(tool)

	if !reflect.DeepEqual(starts, []AgentRunStartParams{start}) {
		t.Fatalf("starts = %#v", starts)
	}
	if replyStarts != 1 || cleanupCalls != 1 {
		t.Fatalf("reply starts=%d cleanup=%d", replyStarts, cleanupCalls)
	}
	if !reflect.DeepEqual(blocks, []ReplyPayload{block}) || !reflect.DeepEqual(tools, []ReplyPayload{tool}) {
		t.Fatalf("blocks=%#v tools=%#v", blocks, tools)
	}
	if opts.TimeoutOverrideMs != 1234 || opts.ContextTokens != 32000 || opts.MaxTokens != 4096 {
		t.Fatalf("numeric options changed: %#v", opts)
	}
}

func TestBoundaryCallbacksCanBeInvokedConcurrentlyByCaller(t *testing.T) {
	var starts atomic.Int64
	var replies atomic.Int64
	var cleanups atomic.Int64
	var blocks atomic.Int64
	var tools atomic.Int64
	opts := GetReplyOptions{
		OnAgentRunStart: func(AgentRunStartParams) { starts.Add(1) },
		OnReplyStart:    func() { replies.Add(1) },
		OnTypingCleanup: func() { cleanups.Add(1) },
		OnBlockReply:    func(ReplyPayload) { blocks.Add(1) },
		OnToolResult:    func(ReplyPayload) { tools.Add(1) },
	}

	const workers = 64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			opts.OnAgentRunStart(AgentRunStartParams{RunID: fmt.Sprintf("run-%d", i)})
			opts.OnReplyStart()
			opts.OnTypingCleanup()
			opts.OnBlockReply(ReplyPayload{Text: "block"})
			opts.OnToolResult(ReplyPayload{Text: "tool"})
		}(i)
	}
	wg.Wait()
	for name, got := range map[string]int64{
		"starts":   starts.Load(),
		"replies":  replies.Load(),
		"cleanups": cleanups.Load(),
		"blocks":   blocks.Load(),
		"tools":    tools.Load(),
	} {
		if got != workers {
			t.Fatalf("%s calls = %d, want %d", name, got, workers)
		}
	}
}

func TestBoundarySessionModificationPointerTriState(t *testing.T) {
	truth := true
	falsehood := false
	empty := ""
	value := "value"
	tests := []struct {
		name string
		mod  SessionModification
	}{
		{name: "all omitted", mod: SessionModification{}},
		{name: "fast true", mod: SessionModification{FastMode: &truth}},
		{name: "fast false", mod: SessionModification{FastMode: &falsehood}},
		{name: "empty system prompt", mod: SessionModification{SystemPrompt: &empty}},
		{name: "nonempty system prompt", mod: SessionModification{SystemPrompt: &value}},
		{name: "empty label", mod: SessionModification{Label: &empty}},
		{name: "nonempty label", mod: SessionModification{Label: &value}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copy := tt.mod
			if !reflect.DeepEqual(copy, tt.mod) {
				t.Fatalf("value copy changed pointer state: %#v vs %#v", copy, tt.mod)
			}
		})
	}
}

func TestBoundaryEnumWireValuesRemainStable(t *testing.T) {
	want := map[string]string{
		"typing user":      "user_message",
		"typing system":    "system_event",
		"typing heartbeat": "heartbeat",
		"dispatch tool":    "tool",
		"dispatch block":   "block",
		"dispatch final":   "final",
		"reset none":       "",
		"reset command":    "command",
		"reset expired":    "expired",
		"reset freshness":  "freshness",
		"reset forced":     "forced",
		"verbose off":      "off",
		"verbose on":       "on",
		"verbose full":     "full",
		"elevated off":     "off",
		"elevated on":      "on",
		"elevated ask":     "ask",
		"elevated full":    "full",
		"reasoning off":    "off",
		"reasoning on":     "on",
		"reasoning stream": "stream",
		"usage off":        "off",
		"usage tokens":     "tokens",
		"usage full":       "full",
	}
	got := map[string]string{
		"typing user":      string(TypingPolicyUserMessage),
		"typing system":    string(TypingPolicySystemEvent),
		"typing heartbeat": string(TypingPolicyHeartbeat),
		"dispatch tool":    string(DispatchKindTool),
		"dispatch block":   string(DispatchKindBlock),
		"dispatch final":   string(DispatchKindFinal),
		"reset none":       string(ResetNone),
		"reset command":    string(ResetCommand),
		"reset expired":    string(ResetExpired),
		"reset freshness":  string(ResetFreshness),
		"reset forced":     string(ResetForced),
		"verbose off":      string(VerboseOff),
		"verbose on":       string(VerboseOn),
		"verbose full":     string(VerboseFull),
		"elevated off":     string(ElevatedOff),
		"elevated on":      string(ElevatedOn),
		"elevated ask":     string(ElevatedAsk),
		"elevated full":    string(ElevatedFull),
		"reasoning off":    string(ReasoningOff),
		"reasoning on":     string(ReasoningOn),
		"reasoning stream": string(ReasoningStream),
		"usage off":        string(UsageOff),
		"usage tokens":     string(UsageTokens),
		"usage full":       string(UsageFull),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire enum drift:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestBoundaryDistinctEnumValuesWithinEachDomain(t *testing.T) {
	groups := map[string][]string{
		"typing": {
			string(TypingPolicyUserMessage),
			string(TypingPolicySystemEvent),
			string(TypingPolicyHeartbeat),
		},
		"dispatch": {
			string(DispatchKindTool),
			string(DispatchKindBlock),
			string(DispatchKindFinal),
		},
		"reset": {
			string(ResetNone),
			string(ResetCommand),
			string(ResetExpired),
			string(ResetFreshness),
			string(ResetForced),
		},
		"reasoning": {
			string(ReasoningOff),
			string(ReasoningOn),
			string(ReasoningStream),
		},
		"usage": {
			string(UsageOff),
			string(UsageTokens),
			string(UsageFull),
		},
	}
	for name, values := range groups {
		t.Run(name, func(t *testing.T) {
			seen := make(map[string]struct{}, len(values))
			for _, value := range values {
				if _, duplicate := seen[value]; duplicate {
					t.Fatalf("duplicate %s value %q in %v", name, value, values)
				}
				seen[value] = struct{}{}
			}
		})
	}
}

func TestBoundaryZeroValueContextsRemainUsable(t *testing.T) {
	values := []any{
		SessionOrigin{},
		MediaContext{},
		SenderInfo{},
		CommandControl{},
		MsgContext{},
		TemplateContext{},
		BlockReplyContext{},
		ModelSelectedContext{},
		AgentRunStartParams{},
		GetReplyOptions{},
		ReplyPayload{},
		MessagingToolTarget{},
		BuildReplyPayloadsParams{},
		SessionState{},
		SessionResetPolicy{},
		SessionHintFlags{},
		SessionModification{},
	}
	for i, value := range values {
		t.Run(fmt.Sprintf("value_%02d_%T", i, value), func(t *testing.T) {
			typ := reflect.TypeOf(value)
			zero := reflect.Zero(typ).Interface()
			if !reflect.DeepEqual(value, zero) {
				t.Fatalf("%T literal is not its usable zero value: %#v", value, value)
			}
		})
	}
}

func TestBoundaryBuildReplyPayloadParamsKeepsRoutingCollectionsSeparate(t *testing.T) {
	params := BuildReplyPayloadsParams{
		Payloads: []ReplyPayload{
			{Text: "first"},
			{MediaURL: "https://example.test/image.png"},
		},
		IsHeartbeat:      true,
		CurrentMessageID: "message-9",
		MessageProvider:  "native",
		SentTexts:        []string{"already sent"},
		SentMediaURLs:    []string{"https://example.test/already.png"},
		SentTargets: []MessagingToolTarget{
			{Provider: "native", To: "device", AccountID: "account"},
		},
		OriginTo:  "origin-device",
		AccountID: "account",
	}

	copy := params
	copy.SentTexts = append([]string(nil), params.SentTexts...)
	copy.SentMediaURLs = append([]string(nil), params.SentMediaURLs...)
	copy.SentTargets = append([]MessagingToolTarget(nil), params.SentTargets...)
	copy.Payloads = append([]ReplyPayload(nil), params.Payloads...)
	copy.SentTexts[0] = "changed"
	copy.SentMediaURLs[0] = "changed"
	copy.SentTargets[0].To = "changed"
	copy.Payloads[0].Text = "changed"

	if params.SentTexts[0] != "already sent" ||
		params.SentMediaURLs[0] != "https://example.test/already.png" ||
		params.SentTargets[0].To != "device" ||
		params.Payloads[0].Text != "first" {
		t.Fatalf("independent collection copy mutated original: %#v", params)
	}
	if !params.IsHeartbeat || !strings.HasPrefix(params.CurrentMessageID, "message-") {
		t.Fatalf("routing scalars changed: %#v", params)
	}
}
