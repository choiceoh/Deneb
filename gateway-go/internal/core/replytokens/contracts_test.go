package tokens

import (
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestExtractReplyTagsContract(t *testing.T) {
	for _, tt := range []struct {
		name string
		text string
		want []ReplyTag
	}{
		{name: "none", text: "hello"},
		{name: "empty", text: ""},
		{name: "simple", text: "[[reply_to_current]]", want: []ReplyTag{{Name: "reply_to_current"}}},
		{name: "value", text: "[[reply_to:message-123]]", want: []ReplyTag{{Name: "reply_to", Value: "message-123"}}},
		{name: "empty value", text: "[[reply_to:]]", want: []ReplyTag{{Name: "reply_to"}}},
		{name: "multiple", text: "a [[one]] b [[two:value]] c", want: []ReplyTag{{Name: "one"}, {Name: "two", Value: "value"}}},
		{name: "duplicate", text: "[[one:a]][[one:b]]", want: []ReplyTag{{Name: "one", Value: "a"}, {Name: "one", Value: "b"}}},
		{name: "unicode value", text: "[[tag:한국어 값]]", want: []ReplyTag{{Name: "tag", Value: "한국어 값"}}},
		{name: "uppercase invalid", text: "[[TAG:value]]"},
		{name: "hyphen invalid", text: "[[reply-to:value]]"},
		{name: "unclosed", text: "[[tag:value"},
		{name: "nested close invalid", text: "[[tag:a]b]]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractReplyTags(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tags = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestStripReplyTagsContract(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{in: "hello", want: "hello"},
		{in: "[[tag]]", want: ""},
		{in: "  [[tag]] text [[other:value]]  ", want: "text"},
		{in: "a [[tag]] b", want: "a  b"},
		{in: "[[UPPER]] remains", want: "[[UPPER]] remains"},
	} {
		if got := StripReplyTags(tt.in); got != tt.want {
			t.Errorf("StripReplyTags(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestReplyTagLookupAndThreadingContract(t *testing.T) {
	text := "[[reply_to:first]] body [[reply_to:second]] [[reply_to_current]]"
	if !HasReplyTag(text, "reply_to") || !HasReplyTag(text, "reply_to_current") || HasReplyTag(text, "missing") {
		t.Fatal("HasReplyTag")
	}
	if got := ReplyTagValue(text, "reply_to"); got != "first" {
		t.Fatalf("first value = %q", got)
	}
	if got := ReplyTagValue(text, "missing"); got != "" {
		t.Fatalf("missing value = %q", got)
	}
	if id, current := ApplyReplyThreading(text, "default"); id != "" || !current {
		t.Fatalf("current = %q/%v", id, current)
	}
	if id, current := ApplyReplyThreading("[[reply_to:specific]]", "default"); id != "specific" || current {
		t.Fatalf("specific = %q/%v", id, current)
	}
	if id, current := ApplyReplyThreading("[[reply_to:]]", "default"); id != "default" || current {
		t.Fatalf("empty = %q/%v", id, current)
	}
	if id, current := ApplyReplyThreading("plain", "default"); id != "default" || current {
		t.Fatalf("default = %q/%v", id, current)
	}
}

func TestHeartbeatContentEmptyContractAdditional(t *testing.T) {
	for _, tt := range []struct {
		content string
		want    bool
	}{
		{want: true}, {content: "\r\n\t", want: true}, {content: "#", want: true}, {content: "######", want: true},
		{content: "# Header", want: true}, {content: "-", want: true}, {content: "*", want: true}, {content: "+", want: true},
		{content: "- [ ]", want: true}, {content: "- [x]", want: true}, {content: "* [X]", want: true},
		{content: "- [ ] task"}, {content: "#hashtag"}, {content: "text"}, {content: "> quote"}, {content: "```"},
	} {
		if got := IsHeartbeatContentEffectivelyEmpty(tt.content); got != tt.want {
			t.Errorf("empty(%q) = %v, want %v", tt.content, got, tt.want)
		}
	}
}

func TestResolveHeartbeatPromptAdditional(t *testing.T) {
	for _, tt := range []struct{ in, want string }{{want: HeartbeatPrompt}, {in: " \n", want: HeartbeatPrompt}, {in: " custom \n", want: "custom"}} {
		if got := ResolveHeartbeatPrompt(tt.in); got != tt.want {
			t.Errorf("Resolve(%q) = %q", tt.in, got)
		}
	}
}

func TestStripTokenAtEdgesContractAdditional(t *testing.T) {
	for _, tt := range []struct {
		name     string
		in       string
		want     string
		stripped bool
	}{
		{name: "empty", in: " ", want: ""},
		{name: "absent", in: "hello", want: "hello"},
		{name: "only", in: HeartbeatToken, want: "", stripped: true},
		{name: "prefix", in: HeartbeatToken + " hello", want: "hello", stripped: true},
		{name: "suffix", in: "hello " + HeartbeatToken, want: "hello", stripped: true},
		{name: "both recursive", in: HeartbeatToken + " " + HeartbeatToken + " hello " + HeartbeatToken, want: "hello", stripped: true},
		{name: "suffix punctuation", in: "hello " + HeartbeatToken + "...", want: "hello...", stripped: true},
		{name: "five punctuation not edge", in: "hello " + HeartbeatToken + ".....", want: "hello " + HeartbeatToken + "....."},
		{name: "prefix collision", in: HeartbeatToken + "AY healthy", want: HeartbeatToken + "AY healthy"},
		{name: "suffix collision", in: "NOT" + HeartbeatToken, want: "NOT" + HeartbeatToken},
		{name: "middle", in: "hello " + HeartbeatToken + " world", want: "hello " + HeartbeatToken + " world"},
		{name: "whitespace collapse", in: HeartbeatToken + "  hello\n\tworld", want: "hello world", stripped: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, stripped := stripTokenAtEdges(tt.in)
			if got != tt.want || stripped != tt.stripped {
				t.Fatalf("got=%q/%v want=%q/%v", got, stripped, tt.want, tt.stripped)
			}
		})
	}
}

func TestTokenBoundaryHelpers(t *testing.T) {
	for _, tt := range []struct {
		b    byte
		want bool
	}{
		{b: 'a', want: true}, {b: 'Z', want: true}, {b: '0', want: true}, {b: '_', want: true},
		{b: '-', want: false}, {b: ' ', want: false}, {b: '.', want: false}, {b: 0, want: false},
	} {
		if got := isTokenWordByte(tt.b); got != tt.want {
			t.Errorf("word(%q) = %v", tt.b, got)
		}
	}
	if !tokenBoundaryAfter(HeartbeatToken, len(HeartbeatToken)) {
		t.Fatal("end not boundary")
	}
	if tokenBoundaryAfter(HeartbeatToken+"A", len(HeartbeatToken)) {
		t.Fatal("letter boundary accepted")
	}
	if !tokenBoundaryAfter(HeartbeatToken+".", len(HeartbeatToken)) {
		t.Fatal("punctuation boundary rejected")
	}
}

func TestStripHeartbeatTokenModesAndUnicodeChars(t *testing.T) {
	shortKorean := strings.Repeat("가", 200)
	longKorean := strings.Repeat("가", 301)
	for _, tt := range []struct {
		name string
		raw  string
		mode StripHeartbeatMode
		max  int
		want StripHeartbeatResult
	}{
		{name: "empty", raw: "", want: StripHeartbeatResult{ShouldSkip: true}},
		{name: "blank", raw: " \n", want: StripHeartbeatResult{ShouldSkip: true}},
		{name: "no token", raw: " text ", want: StripHeartbeatResult{Text: "text"}},
		{name: "message residue", raw: HeartbeatToken + " update", mode: StripModeMessage, want: StripHeartbeatResult{Text: "update", DidStrip: true}},
		{name: "default mode message", raw: HeartbeatToken + " update", want: StripHeartbeatResult{Text: "update", DidStrip: true}},
		{name: "token only", raw: HeartbeatToken, want: StripHeartbeatResult{ShouldSkip: true, DidStrip: true}},
		{name: "heartbeat short", raw: HeartbeatToken + " ok", mode: StripModeHeartbeat, max: 3, want: StripHeartbeatResult{ShouldSkip: true, DidStrip: true}},
		{name: "heartbeat over", raw: HeartbeatToken + " four", mode: StripModeHeartbeat, max: 3, want: StripHeartbeatResult{Text: "four", DidStrip: true}},
		{name: "unicode char count short", raw: HeartbeatToken + " " + shortKorean, mode: StripModeHeartbeat, max: 200, want: StripHeartbeatResult{ShouldSkip: true, DidStrip: true}},
		{name: "unicode char count long", raw: HeartbeatToken + " " + longKorean, mode: StripModeHeartbeat, max: 300, want: StripHeartbeatResult{Text: longKorean, DidStrip: true}},
		{name: "collision untouched", raw: HeartbeatToken + "AY", mode: StripModeHeartbeat, want: StripHeartbeatResult{Text: HeartbeatToken + "AY"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := StripHeartbeatToken(tt.raw, tt.mode, tt.max)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got=%+v want=%+v", got, tt.want)
			}
			if !utf8.ValidString(got.Text) {
				t.Fatalf("invalid UTF-8: %q", got.Text)
			}
		})
	}
}

func TestCollapseWhitespaceContract(t *testing.T) {
	for _, tt := range []struct{ in, want string }{{}, {in: "  a  b ", want: "a b"}, {in: "a\n\tb", want: "a b"}, {in: "가\u00a0나", want: "가 나"}} {
		if got := collapseWhitespace(tt.in); got != tt.want {
			t.Errorf("collapse(%q)=%q want=%q", tt.in, got, tt.want)
		}
	}
}

func TestSilentReplyExactRegexCacheConcurrent(t *testing.T) {
	silentExactMu.Lock()
	silentExactRe = map[string]*regexp.Regexp{}
	silentExactMu.Unlock()
	silentTrailingMu.Lock()
	silentTrailingRe = map[string]*regexp.Regexp{}
	silentTrailingMu.Unlock()
	const workers = 64
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if !IsSilentReplyText(" NO_REPLY ", "") {
					t.Error("exact false")
				}
				if got := StripSilentToken("hello NO_REPLY", ""); got != "hello" {
					t.Errorf("strip=%q", got)
				}
			}
		}()
	}
	wg.Wait()
	if len(silentExactRe) != 1 || len(silentTrailingRe) != 1 {
		t.Fatalf("cache sizes=%d/%d", len(silentExactRe), len(silentTrailingRe))
	}
}

func TestSilentReplyPrefixAdditionalMatrix(t *testing.T) {
	for _, tt := range []struct {
		text, token string
		want        bool
	}{
		{text: " NO", want: true}, {text: "\nNO_RE", want: true}, {text: "NO_REPLY", want: true},
		{text: "N"}, {text: "NO-"}, {text: "NO RE"}, {text: "No_RE"}, {text: "NO_REPLY extra"},
		{text: "CUSTOM_", token: "CUSTOM_TOKEN", want: true}, {text: "CU", token: "CUSTOM_TOKEN"},
		{text: "CUSTOM_TOKEN", token: "CUSTOM_TOKEN", want: true},
	} {
		if got := IsSilentReplyPrefixText(tt.text, tt.token); got != tt.want {
			t.Errorf("prefix(%q,%q)=%v want=%v", tt.text, tt.token, got, tt.want)
		}
	}
}
