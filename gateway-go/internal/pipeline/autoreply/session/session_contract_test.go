package session

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

func pointer[T any](value T) *T { return &value }

func TestAbortTriggerNormalizationMatrix(t *testing.T) {
	for _, input := range []string{
		"stop", " STOP ", "!stop!", "🛑 stop 🛑", "cancel", "abort", "halt",
		"quit", "exit", "end", "kill", "break", "esc",
		"중지", " 취소 ", "!!멈춰!!", "🛑그만🛑", "정지", "끝",
	} {
		if !IsAbortTrigger(input) {
			t.Errorf("IsAbortTrigger(%q) = false", input)
		}
	}
	for _, input := range []string{"", "   ", "stopped", "please stop", "중지해줘", "cancel later", "🛑"} {
		if IsAbortTrigger(input) {
			t.Errorf("IsAbortTrigger(%q) = true", input)
		}
	}
	for _, input := range []string{"/stop", " /STOP ", "/cancel", "/ABORT", "/quit", "stop", "중지"} {
		if !IsAbortRequestText(input) {
			t.Errorf("IsAbortRequestText(%q) = false", input)
		}
	}
	for _, input := range []string{"/stop now", "/cancel@bot", ""} {
		if IsAbortRequestText(input) {
			t.Errorf("IsAbortRequestText(%q) = true", input)
		}
	}
}

func TestAbortMemoryEvictionClearAndWindow(t *testing.T) {
	previousClock := currentTimeMs
	currentTimeMs = func() int64 { return 10_000 }
	t.Cleanup(func() { currentTimeMs = previousClock })

	memory := NewAbortMemory(2)
	memory.Record("old", 1_000)
	memory.Record("recent", 9_500)
	if memory.WasRecentlyAborted("old", 1_000) {
		t.Fatal("old abort reported recent")
	}
	if !memory.WasRecentlyAborted("recent", 1_000) {
		t.Fatal("recent abort missed")
	}
	memory.Record("new", 9_900)
	if _, exists := memory.entries["old"]; exists {
		t.Fatal("oldest entry was not evicted")
	}
	if len(memory.entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(memory.entries))
	}
	memory.Clear("recent")
	if memory.WasRecentlyAborted("recent", 1_000) {
		t.Fatal("cleared entry remains recent")
	}
	memory.Clear("missing")

	defaults := NewAbortMemory(0)
	if defaults.maxSize != 2000 {
		t.Fatalf("default maxSize = %d", defaults.maxSize)
	}
}

func TestAbortMemoryConcurrentRecordReadAndClear(t *testing.T) {
	previousClock := currentTimeMs
	currentTimeMs = func() int64 { return 1_000_000 }
	t.Cleanup(func() { currentTimeMs = previousClock })
	memory := NewAbortMemory(50)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				key := fmt.Sprintf("session-%d", (i+j)%80)
				switch i % 3 {
				case 0:
					memory.Record(key, int64(999_000+j))
				case 1:
					_ = memory.WasRecentlyAborted(key, 10_000)
				default:
					memory.Clear(key)
				}
			}
		}(i)
	}
	wg.Wait()
	if len(memory.entries) > memory.maxSize {
		t.Fatalf("entries = %d > maxSize %d", len(memory.entries), memory.maxSize)
	}
}

func TestAbortCutoffCopiesTimestampAndHandlesNilEntry(t *testing.T) {
	timestamp := int64(1234)
	entry := &SessionAbortCutoffEntry{}
	cutoff := &AbortCutoffContext{MessageSid: " sid ", Timestamp: &timestamp}
	ApplyAbortCutoffToSessionEntry(entry, cutoff)
	if entry.AbortCutoffMessageSid != " sid " || entry.AbortCutoffTimestamp == nil || *entry.AbortCutoffTimestamp != 1234 {
		t.Fatalf("applied entry = %+v", entry)
	}
	timestamp = 9999
	if *entry.AbortCutoffTimestamp != 1234 {
		t.Fatal("ApplyAbortCutoffToSessionEntry retained caller timestamp pointer")
	}
	read := ReadAbortCutoffFromSessionEntry(entry)
	if read == nil || read.MessageSid != "sid" || *read.Timestamp != 1234 {
		t.Fatalf("read cutoff = %+v", read)
	}
	*read.Timestamp = 7777
	if *entry.AbortCutoffTimestamp != 1234 {
		t.Fatal("ReadAbortCutoffFromSessionEntry returned internal timestamp pointer")
	}

	ApplyAbortCutoffToSessionEntry(nil, cutoff)
	ApplyAbortCutoffToSessionEntry(nil, nil)
	if ReadAbortCutoffFromSessionEntry(nil) != nil || HasAbortCutoff(nil) || ClearAbortCutoffInSession(nil) {
		t.Fatal("nil cutoff entry did not degrade safely")
	}
	emptyTimestamp := int64(0)
	if got := ReadAbortCutoffFromSessionEntry(&SessionAbortCutoffEntry{AbortCutoffTimestamp: &emptyTimestamp}); got == nil {
		t.Fatal("timestamp-only cutoff was discarded")
	}
}

func TestNumericMessageSIDBoundaries(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{input: " 000123 ", want: "123"},
		{input: strings.Repeat("9", 100), want: strings.Repeat("9", 100)},
		{input: "", want: ""},
		{input: "-1", want: ""},
		{input: "+1", want: ""},
		{input: "12a", want: ""},
		{input: "１２", want: ""},
	} {
		got := toNumericMessageSid(tc.input)
		if tc.want == "" {
			if got != nil {
				t.Errorf("toNumericMessageSid(%q) = %s", tc.input, got)
			}
			continue
		}
		if got == nil || got.String() != tc.want {
			t.Errorf("toNumericMessageSid(%q) = %v, want %q", tc.input, got, tc.want)
		}
	}
}

func TestShouldSkipAbortCutoffPrecedenceAndInvalidTimestamps(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cutoffSID string
		cutoffTS  *int64
		messageID string
		messageTS *int64
		want      bool
	}{
		{name: "numeric sid before ignores newer timestamp", cutoffSID: "100", cutoffTS: pointer[int64](1), messageID: "99", messageTS: pointer[int64](999), want: true},
		{name: "numeric sid after ignores older timestamp", cutoffSID: "100", cutoffTS: pointer[int64](999), messageID: "101", messageTS: pointer[int64](1), want: false},
		{name: "different opaque sid falls back timestamp", cutoffSID: "abc", cutoffTS: pointer[int64](100), messageID: "def", messageTS: pointer[int64](99), want: true},
		{name: "equal opaque sid", cutoffSID: " abc ", messageID: "abc", want: true},
		{name: "min cutoff ignored", cutoffTS: pointer[int64](-1 << 63), messageTS: pointer[int64](1), want: false},
		{name: "max message ignored", cutoffTS: pointer[int64](1), messageTS: pointer[int64](1<<63 - 1), want: false},
		{name: "one timestamp absent", cutoffTS: pointer[int64](10), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldSkipMessageByAbortCutoff(tc.cutoffSID, tc.cutoffTS, tc.messageID, tc.messageTS); got != tc.want {
				t.Fatalf("ShouldSkip = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestShouldPersistAbortCutoffWhitespaceMatrix(t *testing.T) {
	for _, tc := range []struct {
		command string
		target  string
		want    bool
	}{
		{command: " a ", target: "a", want: true},
		{command: "a", target: " b ", want: false},
		{command: "", target: "b", want: true},
		{command: "a", target: "", want: true},
		{command: " ", target: " ", want: true},
	} {
		if got := ShouldPersistAbortCutoff(tc.command, tc.target); got != tc.want {
			t.Errorf("ShouldPersist(%q,%q) = %v", tc.command, tc.target, got)
		}
	}
}

func TestRelativeAgeFormattingBoundaries(t *testing.T) {
	for _, tc := range []struct {
		duration time.Duration
		want     string
	}{
		{-time.Second, "<1s"},
		{0, "<1s"},
		{999 * time.Millisecond, "<1s"},
		{time.Second, "1s"},
		{59*time.Second + 999*time.Millisecond, "59s"},
		{time.Minute, "1m"},
		{59*time.Minute + 59*time.Second, "59m"},
		{time.Hour, "1h"},
		{23*time.Hour + 59*time.Minute, "23h"},
		{24 * time.Hour, "1d"},
		{49 * time.Hour, "2d"},
	} {
		if got := formatRelativeAge(tc.duration); got != tc.want {
			t.Errorf("formatRelativeAge(%v) = %q, want %q", tc.duration, got, tc.want)
		}
	}
	if got := FormatTimestampWithAge(0); got != "n/a" {
		t.Fatalf("zero timestamp = %q", got)
	}
	if got := FormatTimestampWithAge(-1); got != "n/a" {
		t.Fatalf("negative timestamp = %q", got)
	}
	if got := FormatTimestampWithAge(time.Now().Add(-2 * time.Second).UnixMilli()); !strings.Contains(got, "T") || !strings.Contains(got, "ago)") {
		t.Fatalf("formatted timestamp = %q", got)
	}
}

func TestHistoryTrackerAppendGetRecentAndClear(t *testing.T) {
	history := NewHistoryTracker()
	if history.Count() != 0 || history.Get("missing") != nil || history.Recent("missing", 3) != nil {
		t.Fatal("new history is not empty")
	}
	for i := 0; i < 5; i++ {
		history.Append("session", HistoryEntry{Role: "user", Text: fmt.Sprintf("message-%d", i), Timestamp: -1, SessionKey: "wrong"})
	}
	if history.Count() != 1 {
		t.Fatalf("Count = %d", history.Count())
	}
	all := history.Get("session")
	if len(all) != 5 || all[0].Text != "message-0" || all[4].Text != "message-4" {
		t.Fatalf("Get = %+v", all)
	}
	for _, entry := range all {
		if entry.Timestamp <= 0 || entry.SessionKey != "session" {
			t.Fatalf("Append metadata = %+v", entry)
		}
	}
	all[0].Text = "mutated"
	if history.Get("session")[0].Text != "message-0" {
		t.Fatal("Get returned internal slice")
	}
	if got := history.Recent("session", 2); len(got) != 2 || got[0].Text != "message-3" || got[1].Text != "message-4" {
		t.Fatalf("Recent 2 = %+v", got)
	}
	if got := history.Recent("session", 99); len(got) != 5 {
		t.Fatalf("Recent all = %+v", got)
	}
	if got := history.Recent("session", 0); got != nil {
		t.Fatalf("Recent zero = %+v", got)
	}
	history.Clear("session")
	if history.Count() != 0 || history.Get("session") != nil || len(history.order) != 0 {
		t.Fatalf("Clear left state: entries=%+v order=%+v", history.entries, history.order)
	}
	history.Clear("missing")
}

func TestHistoryTrackerEvictsLeastRecentlyUsedSession(t *testing.T) {
	history := NewHistoryTracker()
	for i := 0; i < maxHistoryKeys; i++ {
		history.Append(fmt.Sprintf("s-%04d", i), HistoryEntry{Text: "seed"})
	}
	// Reads and writes both make an existing session most-recently used.
	_ = history.Get("s-0000")
	_ = history.Recent("s-0001", 1)
	history.Append("s-0002", HistoryEntry{Text: "touch"})
	history.Append("overflow", HistoryEntry{Text: "new"})
	if history.Count() != maxHistoryKeys {
		t.Fatalf("Count after eviction = %d", history.Count())
	}
	if history.Get("s-0003") != nil {
		t.Fatal("least-recently used session s-0003 survived")
	}
	for _, key := range []string{"s-0000", "s-0001", "s-0002", "overflow"} {
		if history.Get(key) == nil {
			t.Fatalf("recent session %s was evicted", key)
		}
	}
}

func TestHistoryTrackerConcurrentUse(t *testing.T) {
	history := NewHistoryTracker()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("session-%d", (i+j)%20)
				switch i % 4 {
				case 0:
					history.Append(key, HistoryEntry{Role: "user", Text: "text"})
				case 1:
					_ = history.Get(key)
				case 2:
					_ = history.Recent(key, 3)
				default:
					history.Clear(key)
				}
			}
		}(i)
	}
	wg.Wait()
	if history.Count() > 20 || len(history.order) != history.Count() {
		t.Fatalf("history invariant: count=%d order=%d", history.Count(), len(history.order))
	}
}

func TestApplySessionUpdateSelectiveFields(t *testing.T) {
	session := &types.SessionState{
		Model:          "old-model",
		Provider:       "old-provider",
		FastMode:       false,
		VerboseLevel:   types.VerboseLevel("old-verbose"),
		ReasoningLevel: types.ReasoningLevel("old-reasoning"),
		ElevatedLevel:  types.ElevatedLevel("old-elevated"),
		UpdatedAt:      -1,
	}
	ApplySessionUpdate(session, SessionUpdate{})
	if session.Model != "old-model" || session.Provider != "old-provider" || session.FastMode || session.UpdatedAt <= 0 {
		t.Fatalf("empty update changed values: %+v", session)
	}
	before := session.UpdatedAt
	update := SessionUpdate{
		Model:          pointer("new-model"),
		Provider:       pointer("new-provider"),
		FastMode:       pointer(true),
		VerboseLevel:   pointer(types.VerboseLevel("new-verbose")),
		ReasoningLevel: pointer(types.ReasoningLevel("new-reasoning")),
		ElevatedLevel:  pointer(types.ElevatedLevel("new-elevated")),
	}
	ApplySessionUpdate(session, update)
	if session.Model != "new-model" || session.Provider != "new-provider" || !session.FastMode ||
		session.VerboseLevel != "new-verbose" || session.ReasoningLevel != "new-reasoning" ||
		session.ElevatedLevel != "new-elevated" || session.UpdatedAt < before {
		t.Fatalf("applied update = %+v", session)
	}
}

func TestSessionResetHelpersAndJSONAccounting(t *testing.T) {
	session := &types.SessionState{Model: "model", Provider: "provider", UpdatedAt: -1}
	ResetSessionModel(session)
	if session.Model != "" || session.Provider != "" || session.UpdatedAt <= 0 {
		t.Fatalf("ResetSessionModel = %+v", session)
	}
	session.UpdatedAt = -1
	ResetSessionPrompt(session)
	if session.UpdatedAt <= 0 {
		t.Fatalf("ResetSessionPrompt = %+v", session)
	}

	usage := SessionUsage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3, CacheReadTokens: 4, CacheWriteTokens: 5, RunCount: 6}
	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SessionUsage
	if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(decoded, usage) {
		t.Fatalf("SessionUsage round trip = %+v err=%v JSON=%s", decoded, err, data)
	}
	tokens := TokenUsage{}
	data, _ = json.Marshal(tokens)
	if string(data) != "{}" {
		t.Fatalf("zero TokenUsage JSON = %s", data)
	}
}
