package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

func TestBoundaryGenericTruncateRuneMatrix(t *testing.T) {
	tests := []struct {
		name string
		text string
		max  int
		want string
	}{
		{name: "empty zero", text: "", max: 0, want: ""},
		{name: "ascii below", text: "ab", max: 3, want: "ab"},
		{name: "ascii exact", text: "abc", max: 3, want: "abc"},
		{name: "ascii over", text: "abcd", max: 3, want: "abc..."},
		{name: "Korean below", text: "가나", max: 3, want: "가나"},
		{name: "Korean exact", text: "가나다", max: 3, want: "가나다"},
		{name: "Korean over", text: "가나다라", max: 3, want: "가나다..."},
		{name: "emoji rune", text: "A📎B", max: 2, want: "A📎..."},
		{name: "newline rune", text: "a\nb", max: 2, want: "a\n..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.text, tt.max); got != tt.want {
				t.Fatalf("truncate(%q, %d) = %q, want %q", tt.text, tt.max, got, tt.want)
			}
			if got := truncateRunes(tt.text, tt.max); !utf8.ValidString(got) {
				t.Fatalf("truncateRunes returned invalid UTF-8: %x", []byte(got))
			}
		})
	}
}

func TestBoundaryFormatBytesMatrix(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{bytes: -1, want: "-1 B"},
		{bytes: 0, want: "0 B"},
		{bytes: 1, want: "1 B"},
		{bytes: 999, want: "999 B"},
		{bytes: 1023, want: "1023 B"},
		{bytes: 1024, want: "1.0 KB"},
		{bytes: 1536, want: "1.5 KB"},
		{bytes: 1024 * 1024, want: "1.0 MB"},
		{bytes: 5 * 1024 * 1024, want: "5.0 MB"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("bytes_%d", tt.bytes), func(t *testing.T) {
			if got := formatBytes(tt.bytes); got != tt.want {
				t.Fatalf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestBoundaryFirstLineMatrix(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "empty", text: "", want: ""},
		{name: "one line", text: "one", want: "one"},
		{name: "one line preserves edges", text: "  one  ", want: "  one  "},
		{name: "newline trims first", text: "  one  \ntwo", want: "one"},
		{name: "empty first", text: "\ntwo", want: ""},
		{name: "CR retained before LF then trimmed", text: "one\r\ntwo", want: "one"},
		{name: "multiple lines", text: "first\nsecond\nthird", want: "first"},
		{name: "unicode", text: "  첫 줄 📎  \n둘째", want: "첫 줄 📎"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLine(tt.text); got != tt.want {
				t.Fatalf("firstLine(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestBoundaryPhoneActionAllowlistNormalization(t *testing.T) {
	valid := []string{"open_url", "open_app", "share", "message", "dial", "photo", "notify", "speak", "clipboard", "sync_state", "alarm", "timer"}
	for _, action := range valid {
		for _, raw := range []string{action, strings.ToUpper(action), " \t" + action + "\n"} {
			t.Run(raw, func(t *testing.T) {
				if !isPhoneAction(raw) {
					t.Fatalf("isPhoneAction(%q) = false", raw)
				}
			})
		}
	}
	for _, raw := range []string{"", " ", "exec", "shell", "open", "open-url", "timer!", "camera", "sms", "OPEN URL"} {
		t.Run("invalid_"+raw, func(t *testing.T) {
			if isPhoneAction(raw) {
				t.Fatalf("isPhoneAction(%q) = true", raw)
			}
		})
	}
}

func TestBoundaryParseAlarmTimeMatrix(t *testing.T) {
	tests := []struct {
		raw      string
		hour     int
		minute   int
		wantErr  bool
		errMatch string
	}{
		{raw: "0:0", hour: 0, minute: 0},
		{raw: "00:00", hour: 0, minute: 0},
		{raw: "7:5", hour: 7, minute: 5},
		{raw: "07:05", hour: 7, minute: 5},
		{raw: "12:30", hour: 12, minute: 30},
		{raw: "23:59", hour: 23, minute: 59},
		{raw: "24:00", wantErr: true, errMatch: "out of range"},
		{raw: "23:60", wantErr: true, errMatch: "out of range"},
		{raw: "99:99", wantErr: true, errMatch: "out of range"},
		{raw: "", wantErr: true, errMatch: "24h clock time"},
		{raw: "7", wantErr: true, errMatch: "24h clock time"},
		{raw: "7:", wantErr: true, errMatch: "24h clock time"},
		{raw: ":05", wantErr: true, errMatch: "24h clock time"},
		{raw: "007:05", wantErr: true, errMatch: "24h clock time"},
		{raw: "07:005", wantErr: true, errMatch: "24h clock time"},
		{raw: " 07:05", wantErr: true, errMatch: "24h clock time"},
		{raw: "07:05 ", wantErr: true, errMatch: "24h clock time"},
		{raw: "07:05:00", wantErr: true, errMatch: "24h clock time"},
		{raw: "오전 7:05", wantErr: true, errMatch: "24h clock time"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			hour, minute, err := parseAlarmTime(tt.raw)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), tt.errMatch) {
					t.Fatalf("parseAlarmTime(%q) = (%d,%d,%v)", tt.raw, hour, minute, err)
				}
				return
			}
			if err != nil || hour != tt.hour || minute != tt.minute {
				t.Fatalf("parseAlarmTime(%q) = (%d,%d,%v), want (%d,%d,nil)", tt.raw, hour, minute, err, tt.hour, tt.minute)
			}
		})
	}
}

func TestBoundaryParseTimerSecondsMatrix(t *testing.T) {
	tests := []struct {
		raw      string
		seconds  int
		wantErr  bool
		errMatch string
	}{
		{raw: "1s", seconds: 1},
		{raw: "59s", seconds: 59},
		{raw: "60s", seconds: 60},
		{raw: "1m", seconds: 60},
		{raw: "1m30s", seconds: 90},
		{raw: "90s", seconds: 90},
		{raw: "1h", seconds: 3600},
		{raw: "1h30m", seconds: 5400},
		{raw: "23h59m59s", seconds: 86399},
		{raw: "24h", seconds: 86400},
		{raw: "", wantErr: true, errMatch: "explicit unit"},
		{raw: "0", wantErr: true, errMatch: "out of range"},
		{raw: "90", wantErr: true, errMatch: "explicit unit"},
		{raw: "0s", wantErr: true, errMatch: "out of range"},
		{raw: "500ms", wantErr: true, errMatch: "out of range"},
		{raw: "-1s", wantErr: true, errMatch: "out of range"},
		{raw: "24h1s", wantErr: true, errMatch: "out of range"},
		{raw: "25h", wantErr: true, errMatch: "out of range"},
		{raw: "1d", wantErr: true, errMatch: "explicit unit"},
		{raw: " 1m", wantErr: true, errMatch: "explicit unit"},
		{raw: "1m ", wantErr: true, errMatch: "explicit unit"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			seconds, err := parseTimerSeconds(tt.raw)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), tt.errMatch) {
					t.Fatalf("parseTimerSeconds(%q) = (%d,%v)", tt.raw, seconds, err)
				}
				return
			}
			if err != nil || seconds != tt.seconds {
				t.Fatalf("parseTimerSeconds(%q) = (%d,%v), want (%d,nil)", tt.raw, seconds, err, tt.seconds)
			}
		})
	}
}

func TestBoundaryBuildPhoneActionSuccessMatrix(t *testing.T) {
	tests := []struct {
		name string
		in   phoneWriteParams
		act  string
		args map[string]string
	}{
		{name: "open url", in: phoneWriteParams{To: " OPEN_URL ", Target: " https://example.test/a?q=1 "}, act: "open_url", args: map[string]string{"url": "https://example.test/a?q=1"}},
		{name: "open app", in: phoneWriteParams{To: "open_app", Target: " com.example.app "}, act: "open_app", args: map[string]string{"package": "com.example.app"}},
		{name: "share", in: phoneWriteParams{To: "share", Text: " share text ", Target: " recipient "}, act: "share", args: map[string]string{"text": " share text ", "to": "recipient"}},
		{name: "message", in: phoneWriteParams{To: "message", Text: " hello ", Target: " +8210 "}, act: "message", args: map[string]string{"text": " hello ", "to": "+8210"}},
		{name: "dial", in: phoneWriteParams{To: "dial", Target: " +82 10 1234 5678 "}, act: "dial", args: map[string]string{"number": "+82 10 1234 5678"}},
		{name: "photo", in: phoneWriteParams{To: "photo"}, act: "photo", args: map[string]string{}},
		{name: "notify default title", in: phoneWriteParams{To: "notify", Text: " notice "}, act: "notify", args: map[string]string{"title": "Deneb", "text": "notice"}},
		{name: "notify custom title", in: phoneWriteParams{To: "notify", Title: " Alert ", Text: " notice "}, act: "notify", args: map[string]string{"title": "Alert", "text": "notice"}},
		{name: "speak", in: phoneWriteParams{To: "speak", Text: " 말하기 "}, act: "speak", args: map[string]string{"text": "말하기"}},
		{name: "clipboard", in: phoneWriteParams{To: "clipboard", Text: " copy "}, act: "clipboard", args: map[string]string{"text": "copy"}},
		{name: "sync state", in: phoneWriteParams{To: "sync_state"}, act: "sync_state", args: map[string]string{}},
		{name: "alarm", in: phoneWriteParams{To: "alarm", Target: "07:05", Text: " wake "}, act: "alarm", args: map[string]string{"hour": "7", "minute": "5", "label": "wake"}},
		{name: "timer", in: phoneWriteParams{To: "timer", Target: "1m30s", Text: " break "}, act: "timer", args: map[string]string{"seconds": "90", "label": "break"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			act, args, err := buildPhoneAction(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if act != tt.act || !reflect.DeepEqual(args, tt.args) {
				t.Fatalf("buildPhoneAction = (%q,%#v), want (%q,%#v)", act, args, tt.act, tt.args)
			}
		})
	}
}

func TestBoundaryBuildPhoneActionValidationFailures(t *testing.T) {
	tests := []struct {
		name string
		in   phoneWriteParams
		want string
	}{
		{name: "unknown", in: phoneWriteParams{To: "exec"}, want: "not allowed"},
		{name: "empty action", in: phoneWriteParams{}, want: "not allowed"},
		{name: "relative URL", in: phoneWriteParams{To: "open_url", Target: "/relative"}, want: "valid absolute url"},
		{name: "empty URL", in: phoneWriteParams{To: "open_url"}, want: "valid absolute url"},
		{name: "empty app", in: phoneWriteParams{To: "open_app"}, want: "needs target"},
		{name: "empty share", in: phoneWriteParams{To: "share", Text: " "}, want: "needs text"},
		{name: "empty message", in: phoneWriteParams{To: "message"}, want: "needs text"},
		{name: "empty dial", in: phoneWriteParams{To: "dial", Target: " "}, want: "needs target"},
		{name: "empty notify", in: phoneWriteParams{To: "notify", Text: " "}, want: "needs text"},
		{name: "empty speak", in: phoneWriteParams{To: "speak"}, want: "needs text"},
		{name: "empty clipboard", in: phoneWriteParams{To: "clipboard"}, want: "needs text"},
		{name: "bad alarm", in: phoneWriteParams{To: "alarm", Target: "25:00"}, want: "out of range"},
		{name: "bad timer", in: phoneWriteParams{To: "timer", Target: "90"}, want: "explicit unit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, args, err := buildPhoneAction(tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("buildPhoneAction = (%q,%#v,%v), want error containing %q", action, args, err, tt.want)
			}
			if action != "" || args != nil {
				t.Fatalf("failure returned partial action: %q %#v", action, args)
			}
		})
	}
}

func TestBoundaryDispatchPhoneActionOutcomeMatrix(t *testing.T) {
	wantFailure := errors.New("device rejected")
	tests := []struct {
		name      string
		params    phoneWriteParams
		send      PhoneActionFunc
		wantText  string
		wantErr   string
		wantCalls int
	}{
		{name: "nil sender", params: phoneWriteParams{To: "photo"}, send: nil, wantErr: "unavailable", wantCalls: 0},
		{name: "confirmed", params: phoneWriteParams{To: "photo"}, send: func(context.Context, string, map[string]string) error { return nil }, wantText: "launched on device: photo", wantCalls: 1},
		{name: "unconfirmed", params: phoneWriteParams{To: "timer", Target: "1s"}, send: func(context.Context, string, map[string]string) error {
			return fmt.Errorf("wait: %w", ErrPhoneActionUnconfirmed)
		}, wantText: "Do NOT retry", wantCalls: 1},
		{name: "real failure", params: phoneWriteParams{To: "photo"}, send: func(context.Context, string, map[string]string) error { return wantFailure }, wantErr: "device rejected", wantCalls: 1},
		{name: "validation before sender", params: phoneWriteParams{To: "timer", Target: "90"}, send: func(context.Context, string, map[string]string) error { return nil }, wantErr: "explicit unit", wantCalls: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			send := tt.send
			if send != nil {
				original := send
				send = func(ctx context.Context, action string, args map[string]string) error {
					calls++
					return original(ctx, action, args)
				}
			}
			got, err := dispatchPhoneAction(context.Background(), send, tt.params)
			if calls != tt.wantCalls {
				t.Fatalf("sender calls = %d, want %d", calls, tt.wantCalls)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("result = %q, %v, want error %q", got, err, tt.wantErr)
				}
				return
			}
			if err != nil || !strings.Contains(got, tt.wantText) {
				t.Fatalf("result = %q, %v, want text %q", got, err, tt.wantText)
			}
		})
	}
}

func TestBoundaryPolarisTimeRangeMatrix(t *testing.T) {
	location := time.FixedZone("KST", 9*60*60)
	now := time.Date(2026, 7, 11, 15, 30, 0, 0, location)
	nodes := []polaris.SummaryNode{
		{ID: 1, CreatedAt: now.AddDate(0, 0, -8).UnixMilli()},
		{ID: 2, CreatedAt: now.AddDate(0, 0, -7).UnixMilli()},
		{ID: 3, CreatedAt: time.Date(2026, 7, 11, 0, 0, 0, 0, location).UnixMilli()},
		{ID: 4, CreatedAt: now.UnixMilli()},
		{ID: 5, CreatedAt: now.Add(time.Hour).UnixMilli()},
	}
	tests := []struct {
		name      string
		rangeName string
		want      []int64
	}{
		{name: "empty means all", rangeName: "", want: []int64{1, 2, 3, 4, 5}},
		{name: "all", rangeName: "all", want: []int64{1, 2, 3, 4, 5}},
		{name: "unknown means all", rangeName: "month", want: []int64{1, 2, 3, 4, 5}},
		{name: "today inclusive midnight", rangeName: "today", want: []int64{3, 4, 5}},
		{name: "week inclusive cutoff", rangeName: "this_week", want: []int64{2, 3, 4, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterByTimeRange(nodes, tt.rangeName, now)
			ids := make([]int64, len(got))
			for i := range got {
				ids[i] = got[i].ID
			}
			if !reflect.DeepEqual(ids, tt.want) {
				t.Fatalf("IDs = %v, want %v", ids, tt.want)
			}
		})
	}
}

func TestBoundarySerializeExpandMessagesBudgetMatrix(t *testing.T) {
	msgs := []toolctx.ChatMessage{
		toolctx.NewTextChatMessage("user", "hello", 1),
		toolctx.NewTextChatMessage("assistant", "안녕하세요", 2),
		toolctx.NewTextChatMessage("tool", "result", 3),
	}
	full := "[user]: hello\n\n[assistant]: 안녕하세요\n\n[tool]: result\n\n"
	tests := []struct {
		name string
		max  int
		want string
	}{
		{name: "large budget", max: 1000, want: full},
		{name: "exact full budget", max: len(full), want: full},
		{name: "first only", max: len("[user]: hello\n\n"), want: "[user]: hello\n\n... (나머지 3건 생략)\n"},
		{name: "zero budget", max: 0, want: "... (나머지 3건 생략)\n"},
		{name: "negative budget", max: -1, want: "... (나머지 3건 생략)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serializeExpandMessages(msgs, tt.max); got != tt.want {
				t.Fatalf("serializeExpandMessages(max=%d) = %q, want %q", tt.max, got, tt.want)
			}
		})
	}
	if got := serializeExpandMessages(nil, 100); got != "" {
		t.Fatalf("nil messages = %q", got)
	}
}

func TestBoundaryTranslateInputEnvelopeMatrix(t *testing.T) {
	prefix := translateSegmentEnvelopePrefix
	tests := []struct {
		name string
		raw  string
		want translateInput
	}{
		{name: "plain", raw: "plain", want: translateInput{Text: "plain"}},
		{name: "empty plain", raw: "", want: translateInput{Text: ""}},
		{name: "malformed envelope", raw: prefix + `{`, want: translateInput{Text: prefix + `{`}},
		{name: "empty envelope fallback", raw: prefix + `{}`, want: translateInput{Text: prefix + `{}`}},
		{name: "text envelope", raw: prefix + `{"text":"hello"}`, want: translateInput{Text: "hello"}},
		{name: "metadata trimmed", raw: prefix + `{"text":"hello","context":" ctx ","role":" heading "}`, want: translateInput{Text: "hello", Context: "ctx", Role: "heading"}},
		{name: "parts joined", raw: prefix + `{"parts":["one","two"]}`, want: translateInput{Text: "onetwo", Parts: []string{"one", "two"}}},
		{name: "empty parts dropped", raw: prefix + `{"parts":["one","","two"]}`, want: translateInput{Text: "onetwo", Parts: []string{"one", "two"}}},
		{name: "parts beat text", raw: prefix + `{"text":"ignored","parts":["one","two"]}`, want: translateInput{Text: "onetwo", Parts: []string{"one", "two"}}},
		{name: "whitespace part retained", raw: prefix + `{"parts":[" ","x"]}`, want: translateInput{Text: " x", Parts: []string{" ", "x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseTranslateInput(tt.raw); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseTranslateInput = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBoundaryTranslateBatchRangeLimits(t *testing.T) {
	inputs := make([]translateInput, 45)
	for i := range inputs {
		inputs[i] = translateInput{Text: "x"}
	}
	want := []translateBatchRange{{Start: 0, End: 20}, {Start: 20, End: 40}, {Start: 40, End: 45}}
	if got := translateBatchRanges(inputs); !reflect.DeepEqual(got, want) {
		t.Fatalf("short input ranges = %#v, want %#v", got, want)
	}
	large := []translateInput{
		{Text: strings.Repeat("a", 700)},
		{Text: strings.Repeat("b", 500)},
		{Text: "c"},
	}
	want = []translateBatchRange{{Start: 0, End: 2}, {Start: 2, End: 3}}
	if got := translateBatchRanges(large); !reflect.DeepEqual(got, want) {
		t.Fatalf("char-bound ranges = %#v, want %#v", got, want)
	}
	if got := translateBatchRanges(nil); len(got) != 0 {
		t.Fatalf("nil ranges = %#v", got)
	}
}

func TestBoundaryTranslateInputCostContextDiscount(t *testing.T) {
	tests := []struct {
		in   translateInput
		want int
	}{
		{in: translateInput{}, want: 0},
		{in: translateInput{Text: "abcd"}, want: 4},
		{in: translateInput{Context: "abcd"}, want: 1},
		{in: translateInput{Text: "abcd", Context: "abcdefgh"}, want: 6},
		{in: translateInput{Text: "가"}, want: len("가")},
	}
	for _, tt := range tests {
		if got := translateInputCost(tt.in); got != tt.want {
			t.Fatalf("translateInputCost(%#v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestBoundaryTranslateRangeFailureBisectsAndPreservesOrder(t *testing.T) {
	original := translateBatchFn
	t.Cleanup(func() { translateBatchFn = original })
	var mu sync.Mutex
	var calls [][]string
	translateBatchFn = func(_ context.Context, batch []translateInput, _ string) ([]string, bool) {
		texts := make([]string, len(batch))
		for i := range batch {
			texts[i] = batch[i].Text
		}
		mu.Lock()
		calls = append(calls, texts)
		mu.Unlock()
		if len(batch) > 1 {
			return nil, false
		}
		return []string{"T:" + batch[0].Text}, true
	}
	inputs := []translateInput{{Text: "a"}, {Text: "b"}, {Text: "c"}, {Text: "d"}}
	out := []string{"a", "b", "c", "d"}
	translateRange(context.Background(), inputs, out, 0, len(inputs), "Korean")
	if !reflect.DeepEqual(out, []string{"T:a", "T:b", "T:c", "T:d"}) {
		t.Fatalf("translated output = %v", out)
	}
	if len(calls) != 7 {
		t.Fatalf("bisection calls = %d, want 7: %#v", len(calls), calls)
	}
}

func TestBoundaryNormalizeSourceURLMatrix(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
		err  string
	}{
		{name: "empty", raw: "", err: "빈 URL"},
		{name: "spaces", raw: "  ", err: "빈 URL"},
		{name: "unsupported ftp", raw: "ftp://example.com/a", err: "지원하지 않는 스킴"},
		{name: "unsupported relative", raw: "/relative", err: "지원하지 않는 스킴"},
		{name: "HTTP preserved", raw: "http://EXAMPLE.COM/a", want: "http://example.com/a"},
		{name: "HTTPS host lower", raw: "https://EXAMPLE.COM/A", want: "https://example.com/A"},
		{name: "fragment removed", raw: "https://example.com/a#section", want: "https://example.com/a"},
		{name: "utm removed", raw: "https://example.com/a?utm_source=x&keep=y", want: "https://example.com/a?keep=y"},
		{name: "query sorted", raw: "https://example.com/a?z=2&a=1", want: "https://example.com/a?a=1&z=2"},
		{name: "youtu be", raw: "https://youtu.be/abc123?t=30", want: "https://www.youtube.com/watch?v=abc123"},
		{name: "youtube watch", raw: "https://www.youtube.com/watch?v=abc123&list=PL", want: "https://www.youtube.com/watch?v=abc123"},
		{name: "youtube shorts", raw: "https://youtube.com/shorts/abc123?feature=share", want: "https://www.youtube.com/watch?v=abc123"},
		{name: "youtube embed", raw: "https://m.youtube.com/embed/abc123", want: "https://www.youtube.com/watch?v=abc123"},
		{name: "youtube live", raw: "https://music.youtube.com/live/abc123", want: "https://www.youtube.com/watch?v=abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeSourceURL(tt.raw)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("normalizeSourceURL = %q, %v", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("normalizeSourceURL = %q, %v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestBoundaryYouTubeVideoIDMatrix(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "https://youtube.com/watch?v=watch-id", want: "watch-id"},
		{raw: "https://www.youtube.com/watch?v=www-id", want: "www-id"},
		{raw: "https://m.youtube.com/watch?v=mobile-id", want: "mobile-id"},
		{raw: "https://music.youtube.com/watch?v=music-id", want: "music-id"},
		{raw: "https://youtube.com/shorts/short-id", want: "short-id"},
		{raw: "https://youtube.com/embed/embed-id/more", want: "embed-id"},
		{raw: "https://youtube.com/live/live-id", want: "live-id"},
		{raw: "https://youtu.be/share-id", want: "share-id"},
		{raw: "https://youtu.be/share-id/extra", want: "share-id"},
		{raw: "https://example.com/watch?v=no", want: ""},
		{raw: "https://notyoutube.com/watch?v=no", want: ""},
		{raw: "https://youtube.com/watch", want: ""},
		{raw: "https://youtu.be/", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			u, err := url.Parse(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := youtubeVideoID(u); got != tt.want {
				t.Fatalf("youtubeVideoID(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestBoundarySlugifyTitleMatrix(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "자료"},
		{name: "punctuation only", in: "!!!", want: "자료"},
		{name: "ascii lower", in: "Hello World", want: "hello-world"},
		{name: "Korean", in: "태양광 계약 검토", want: "태양광-계약-검토"},
		{name: "mixed", in: "2026 Q3 태양광", want: "2026-q3-태양광"},
		{name: "collapse punctuation", in: "a --- b///c", want: "a-b-c"},
		{name: "trim dashes", in: " -- title -- ", want: "title"},
		{name: "underscore folds", in: "one_two", want: "one-two"},
		{name: "emoji folds", in: "report 📎 final", want: "report-final"},
		{name: "Hangul jamo", in: "ㄱㄴ test", want: "ㄱㄴ-test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slugifyTitle(tt.in); got != tt.want {
				t.Fatalf("slugifyTitle(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
	long := strings.Repeat("가", ingestSlugMaxRunes+20)
	got := slugifyTitle(long)
	if utf8.RuneCountInString(got) != ingestSlugMaxRunes || strings.Contains(got, "...") {
		t.Fatalf("long slug length=%d value=%q", utf8.RuneCountInString(got), got)
	}
}

func TestBoundaryWorkFeedPriorityMatrix(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{raw: "", want: 0},
		{raw: "unknown", want: 0},
		{raw: "urgent", want: workfeed.PriorityUrgent},
		{raw: " URGENT ", want: workfeed.PriorityUrgent},
		{raw: "긴급", want: workfeed.PriorityUrgent},
		{raw: "high", want: workfeed.PriorityHigh},
		{raw: "높음", want: workfeed.PriorityHigh},
		{raw: "normal", want: workfeed.PriorityNormal},
		{raw: "보통", want: workfeed.PriorityNormal},
		{raw: "low", want: workfeed.PriorityLow},
		{raw: "낮음", want: workfeed.PriorityLow},
		{raw: "highest", want: 0},
	}
	for _, tt := range tests {
		if got := workFeedPriority(tt.raw); got != tt.want {
			t.Fatalf("workFeedPriority(%q) = %d, want %d", tt.raw, got, tt.want)
		}
	}
}

func TestBoundaryWorkFeedTitleFallbackMatrix(t *testing.T) {
	tests := []struct {
		name string
		item workfeed.Item
		want string
	}{
		{name: "title wins", item: workfeed.Item{Title: " Title ", Summary: "summary", Body: "body"}, want: "Title"},
		{name: "summary fallback", item: workfeed.Item{Summary: " summary line \nsecond", Body: "body"}, want: "summary line "},
		{name: "body fallback", item: workfeed.Item{Body: " body line \nsecond"}, want: "body line "},
		{name: "empty", item: workfeed.Item{}, want: ""},
		{name: "blank title skipped", item: workfeed.Item{Title: " ", Summary: "summary"}, want: "summary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workFeedTitle(tt.item); got != tt.want {
				t.Fatalf("workFeedTitle = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBoundaryTranslateSegmentsConcurrentBatchLimit(t *testing.T) {
	original := translateBatchFn
	t.Cleanup(func() { translateBatchFn = original })
	var active atomic.Int32
	var maxActive atomic.Int32
	translateBatchFn = func(_ context.Context, batch []translateInput, _ string) ([]string, bool) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			old := maxActive.Load()
			if current <= old || maxActive.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		out := make([]string, len(batch))
		for i := range batch {
			out[i] = "T:" + batch[i].Text
		}
		return out, true
	}
	segments := make([]string, translateMaxSegmentsPerBatch*8)
	for i := range segments {
		segments[i] = fmt.Sprintf("segment-%03d", i)
	}
	got, err := TranslateSegments(context.Background(), segments, "Korean")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(segments) {
		t.Fatalf("translated length = %d", len(got))
	}
	for i := range got {
		if got[i] != "T:"+segments[i] {
			t.Fatalf("output %d = %q", i, got[i])
		}
	}
	if maxActive.Load() > translateMaxConcurrentBatches {
		t.Fatalf("max concurrent batches = %d", maxActive.Load())
	}
}

func TestBoundaryToolPolarisRejectsMalformedAndUnknownActions(t *testing.T) {
	tool := ToolPolaris(nil, nil)
	if _, err := tool(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed input accepted")
	}
	for _, input := range []string{`{}`, `{"action":""}`, `{"action":"unknown"}`, `{"action":"SEARCH"}`} {
		got, err := tool(context.Background(), json.RawMessage(input))
		if err != nil {
			t.Fatalf("input %s: %v", input, err)
		}
		if !strings.Contains(got, "search, describe, expand") {
			t.Fatalf("input %s output = %q", input, got)
		}
	}
}
