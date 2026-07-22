package agentlog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func appendTimedEntry(t *testing.T, w *Writer, ts int64, session, runID, typ string, data any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal %s: %v", typ, err)
	}
	if err := w.Append(LogEntry{Ts: ts, Type: typ, RunID: runID, Session: session, Data: raw}); err != nil {
		t.Fatalf("append %s: %v", typ, err)
	}
}

func TestReadContractFiltersOrderAndLimits(t *testing.T) {
	w := NewWriter(t.TempDir())
	fixtures := []LogEntry{
		{Ts: 10, Type: TypeRunStart, RunID: "run-a", Session: "client:main"},
		{Ts: 20, Type: TypeTurnTool, RunID: "run-a", Session: "client:main"},
		{Ts: 30, Type: TypeRunEnd, RunID: "run-a", Session: "client:main"},
		{Ts: 40, Type: TypeRunStart, RunID: "run-b", Session: "client:main"},
		{Ts: 50, Type: TypeTurnTool, RunID: "run-b", Session: "client:main"},
		{Ts: 60, Type: TypeRunEnd, RunID: "run-b", Session: "client:main"},
	}
	for _, entry := range fixtures {
		if err := w.Append(entry); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name      string
		opts      ReadOpts
		wantTotal int
		wantTs    []int64
	}{
		{
			name:      "default returns all when fewer than fifty",
			opts:      ReadOpts{SessionKey: "client:main"},
			wantTotal: 6,
			wantTs:    []int64{60, 50, 40, 30, 20, 10},
		},
		{
			name:      "positive limit slices newest first",
			opts:      ReadOpts{SessionKey: "client:main", Limit: 2},
			wantTotal: 6,
			wantTs:    []int64{60, 50},
		},
		{
			name:      "negative limit uses default",
			opts:      ReadOpts{SessionKey: "client:main", Limit: -1},
			wantTotal: 6,
			wantTs:    []int64{60, 50, 40, 30, 20, 10},
		},
		{
			name:      "run filter precedes total and limit",
			opts:      ReadOpts{SessionKey: "client:main", RunID: "run-a", Limit: 2},
			wantTotal: 3,
			wantTs:    []int64{30, 20},
		},
		{
			name:      "type filter spans runs",
			opts:      ReadOpts{SessionKey: "client:main", Type: TypeTurnTool},
			wantTotal: 2,
			wantTs:    []int64{50, 20},
		},
		{
			name: "combined filters are conjunctive",
			opts: ReadOpts{
				SessionKey: "client:main",
				RunID:      "run-b",
				Type:       TypeRunEnd,
			},
			wantTotal: 1,
			wantTs:    []int64{60},
		},
		{
			name:      "unknown run is empty",
			opts:      ReadOpts{SessionKey: "client:main", RunID: "missing"},
			wantTotal: 0,
			wantTs:    []int64{},
		},
		{
			name:      "unknown session is empty",
			opts:      ReadOpts{SessionKey: "other"},
			wantTotal: 0,
			wantTs:    []int64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := w.Read(tt.opts)
			if got.Total != tt.wantTotal {
				t.Fatalf("total = %d, want %d", got.Total, tt.wantTotal)
			}
			gotTs := make([]int64, len(got.Entries))
			for i, entry := range got.Entries {
				gotTs[i] = entry.Ts
			}
			if !reflect.DeepEqual(gotTs, tt.wantTs) {
				t.Fatalf("timestamps = %v, want %v", gotTs, tt.wantTs)
			}
		})
	}
}

func TestReadAllEntriesRecoveryContract(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.jsonl")
	content := strings.Join([]string{
		`{"ts":1,"type":"run.start","runId":"a","session":"s","data":{}}`,
		``,
		`not-json`,
		`{"ts":2,"type":"run.end","runId":"a","session":"s","data":{}} {"ts":3,"type":"run.start","runId":"b","session":"s","data":{}}`,
		`{"ts":4,"type":"run.end"`,
		`   `,
		`{"ts":5,"type":"turn.tool","runId":"b","session":"s","data":{"name":"exec"}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got := readAllEntries(path)
	if len(got) != 4 {
		t.Fatalf("valid entries = %d, want 4: %+v", len(got), got)
	}
	wantTs := []int64{1, 2, 3, 5}
	for i := range wantTs {
		if got[i].Ts != wantTs[i] {
			t.Fatalf("entry %d ts = %d, want %d", i, got[i].Ts, wantTs[i])
		}
	}
	if got := readAllEntries(filepath.Join(dir, "absent.jsonl")); got != nil {
		t.Fatalf("missing file = %#v, want nil", got)
	}
}

func TestReadRunAcrossSessionsAndMalformedLines(t *testing.T) {
	w := NewWriter(t.TempDir())
	appendTimedEntry(t, w, 10, "s1", "target", TypeRunStart, RunStartData{Model: "m1"})
	appendTimedEntry(t, w, 20, "s1", "other", TypeRunStart, RunStartData{Model: "m2"})
	appendTimedEntry(t, w, 30, "s2", "target", TypeTurnLLM, TurnLLMData{Turn: 1})
	appendTimedEntry(t, w, 40, "s2", "target", TypeRunEnd, RunEndData{Turns: 1})

	entries, session := w.ReadRun("target")
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3: %+v", len(entries), entries)
	}
	if session != "s1" && session != "s2" {
		t.Fatalf("session = %q, want one source session", session)
	}
	for _, entry := range entries {
		if entry.RunID != "target" {
			t.Fatalf("foreign run leaked: %+v", entry)
		}
	}
	if entries, session := w.ReadRun(""); entries != nil || session != "" {
		t.Fatalf("empty run = %v/%q, want nil/empty", entries, session)
	}
	if entries, session := (*Writer)(nil).ReadRun("target"); entries != nil || session != "" {
		t.Fatalf("nil writer = %v/%q, want nil/empty", entries, session)
	}
}

func TestToolProvenanceContractMatrix(t *testing.T) {
	w := NewWriter(t.TempDir())
	appendTimedEntry(t, w, 100, "s1", "r1", TypeTurnTool, TurnToolData{
		Turn:       1,
		Name:       "edit",
		ToolUseID:  "tool-a",
		DurationMs: 7,
		InputBytes: 12,
		InputHash:  "in-a",
		OutputLen:  20,
		OutputHash: "out-a",
		Targets:    []string{"SRC/Alpha.go"},
		FileEffects: []ToolFileEffect{{
			Path:         "src/alpha.go",
			ExistsBefore: true,
			ExistsAfter:  true,
			Changed:      true,
			AddedLines:   3,
		}},
	})
	appendTimedEntry(t, w, 200, "s2", "r2", TypeTurnTool, TurnToolData{
		Turn:      2,
		Name:      "exec",
		ToolUseID: "tool-b",
		Targets:   []string{"scripts/release.sh"},
		IsError:   true,
		Error:     "exit 1",
	})
	appendTimedEntry(t, w, 300, "s3", "r3", TypeTurnTool, TurnToolData{
		Turn:      3,
		Name:      "write",
		ToolUseID: "tool-c",
		FileEffects: []ToolFileEffect{{
			Path:        "generated/Beta.go",
			ExistsAfter: true,
			Changed:     true,
		}},
	})
	appendTimedEntry(t, w, 400, "s4", "r4", TypeRunEnd, RunEndData{Turns: 1})
	// A syntactically valid log entry with malformed TurnToolData must be ignored.
	if err := w.Append(LogEntry{
		Ts:      500,
		Type:    TypeTurnTool,
		RunID:   "bad",
		Session: "s5",
		Data:    json.RawMessage(`"not-an-object"`),
	}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		query   ToolProvenanceQuery
		wantIDs []string
	}{
		{
			name:    "empty query returns all tool calls newest first",
			query:   ToolProvenanceQuery{},
			wantIDs: []string{"r3", "r2", "r1"},
		},
		{
			name:    "target match is case insensitive",
			query:   ToolProvenanceQuery{Target: "alpha.GO"},
			wantIDs: []string{"r1"},
		},
		{
			name:    "effect path participates in target match",
			query:   ToolProvenanceQuery{Target: "beta.go"},
			wantIDs: []string{"r3"},
		},
		{
			name:    "target whitespace is trimmed",
			query:   ToolProvenanceQuery{Target: "  release.sh "},
			wantIDs: []string{"r2"},
		},
		{
			name:    "tool filter is exact",
			query:   ToolProvenanceQuery{Tool: "exec"},
			wantIDs: []string{"r2"},
		},
		{
			name:    "run filter is exact",
			query:   ToolProvenanceQuery{RunID: "r3"},
			wantIDs: []string{"r3"},
		},
		{
			name:    "since is inclusive",
			query:   ToolProvenanceQuery{SinceMs: 200},
			wantIDs: []string{"r3", "r2"},
		},
		{
			name:    "limit is applied after global timestamp sort",
			query:   ToolProvenanceQuery{Limit: 2},
			wantIDs: []string{"r3", "r2"},
		},
		{
			name:    "combined filters are conjunctive",
			query:   ToolProvenanceQuery{Tool: "edit", Target: "src/", RunID: "r1", SinceMs: 100},
			wantIDs: []string{"r1"},
		},
		{
			name:    "no match is nil or empty",
			query:   ToolProvenanceQuery{Target: "absent"},
			wantIDs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := w.ToolProvenance(tt.query)
			gotIDs := make([]string, len(got))
			for i, event := range got {
				gotIDs[i] = event.RunID
			}
			if !reflect.DeepEqual(gotIDs, tt.wantIDs) {
				t.Fatalf("run ids = %v, want %v", gotIDs, tt.wantIDs)
			}
		})
	}

	got := w.ToolProvenance(ToolProvenanceQuery{RunID: "r1"})
	if len(got) != 1 {
		t.Fatalf("r1 events = %d, want 1", len(got))
	}
	event := got[0]
	if event.Name != "edit" || event.ToolUseID != "tool-a" || event.DurationMs != 7 {
		t.Fatalf("identity fields lost: %+v", event)
	}
	if event.InputBytes != 12 || event.InputHash != "in-a" || event.OutputLen != 20 || event.OutputHash != "out-a" {
		t.Fatalf("I/O metadata lost: %+v", event)
	}
	if len(event.FileEffects) != 1 || !event.FileEffects[0].Changed {
		t.Fatalf("effects lost: %+v", event.FileEffects)
	}
	if got := (*Writer)(nil).ToolProvenance(ToolProvenanceQuery{}); got != nil {
		t.Fatalf("nil writer = %#v, want nil", got)
	}
}

func TestTargetMatchesContract(t *testing.T) {
	tests := []struct {
		name    string
		targets []string
		effects []ToolFileEffect
		query   string
		want    bool
	}{
		{
			name:    "empty query matches",
			targets: nil,
			effects: nil,
			query:   "",
			want:    true,
		},
		{
			name:    "whitespace query matches",
			targets: nil,
			effects: nil,
			query:   "  ",
			want:    true,
		},
		{
			name:    "target substring matches",
			targets: []string{"internal/core/agentlog/reader.go"},
			query:   "agentlog",
			want:    true,
		},
		{
			name:    "target case is folded",
			targets: []string{"README.MD"},
			query:   "readme.md",
			want:    true,
		},
		{
			name:    "effect substring matches",
			effects: []ToolFileEffect{{Path: "generated/report.json"}},
			query:   "REPORT",
			want:    true,
		},
		{
			name:    "unrelated path misses",
			targets: []string{"a.go"},
			effects: []ToolFileEffect{{Path: "b.go"}},
			query:   "c.go",
			want:    false,
		},
		{
			name:    "empty target entries do not match text",
			targets: []string{""},
			effects: []ToolFileEffect{{Path: ""}},
			query:   "x",
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := targetMatches(tt.targets, tt.effects, tt.query); got != tt.want {
				t.Fatalf("targetMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunLoggerAllEventShapesAndNilSafety(t *testing.T) {
	var nilLogger *RunLogger
	nilLogger.LogStart(RunStartData{})
	nilLogger.LogPrep(RunPrepData{})
	nilLogger.LogTurnLLM(TurnLLMData{})
	nilLogger.LogTurnTool(TurnToolData{})
	nilLogger.LogEnd(RunEndData{})
	nilLogger.LogError(RunErrorData{})
	nilLogger.LogCache(RunCacheData{})
	if got := NewRunLogger(nil, "session", "run"); got != nil {
		t.Fatalf("nil writer returned logger: %+v", got)
	}

	w := NewWriter(t.TempDir())
	rl := NewRunLogger(w, "session", "run")
	rl.LogStart(RunStartData{Model: "model", Provider: "provider", Message: "hello"})
	rl.LogPrep(RunPrepData{SystemPromptChars: 10, ContextMessages: 2, PrepMs: 3})
	rl.LogTurnLLM(TurnLLMData{Turn: 1, InputTokens: 11, OutputTokens: 4})
	rl.LogTurnTool(TurnToolData{Turn: 1, Name: "read", DurationMs: 2})
	rl.LogError(RunErrorData{Error: "transient", Aborted: true})
	rl.LogCache(RunCacheData{EngineHitTokens: 8, EngineQueryTokens: 10, MetricsURL: "http://engine/metrics"})
	rl.LogEnd(RunEndData{StopReason: "end_turn", Turns: 1})

	got := w.Read(ReadOpts{SessionKey: "session", Limit: 20})
	if got.Total != 7 {
		t.Fatalf("entries = %d, want 7", got.Total)
	}
	wantTypes := []string{
		TypeRunEnd,
		TypeRunCache,
		TypeRunError,
		TypeTurnTool,
		TypeTurnLLM,
		TypeRunPrep,
		TypeRunStart,
	}
	for i, typ := range wantTypes {
		if got.Entries[i].Type != typ {
			t.Fatalf("entry %d type = %q, want %q", i, got.Entries[i].Type, typ)
		}
		if got.Entries[i].RunID != "run" || got.Entries[i].Session != "session" {
			t.Fatalf("entry %d identity = %+v", i, got.Entries[i])
		}
		if !json.Valid(got.Entries[i].Data) {
			t.Fatalf("entry %d data invalid: %q", i, got.Entries[i].Data)
		}
	}
}

func TestRunLoggerTruncatesAtUTF8Boundary(t *testing.T) {
	w := NewWriter(t.TempDir())
	rl := NewRunLogger(w, "session", "run")
	// 4095 ASCII bytes followed by a three-byte Korean rune crosses the 4096
	// boundary. The old raw slice kept the first byte of the rune.
	message := strings.Repeat("a", maxMessageLen-1) + "한" + "tail"
	rl.LogStart(RunStartData{Message: message})

	result := w.Read(ReadOpts{SessionKey: "session"})
	if len(result.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(result.Entries))
	}
	var data RunStartData
	if err := json.Unmarshal(result.Entries[0].Data, &data); err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(data.Message) {
		t.Fatalf("truncated message is invalid UTF-8: %q", data.Message[len(data.Message)-16:])
	}
	if strings.ContainsRune(data.Message, utf8.RuneError) {
		t.Fatalf("truncated message contains replacement rune: %q", data.Message[len(data.Message)-16:])
	}
	if !strings.HasSuffix(data.Message, "...") {
		t.Fatalf("truncated message lacks suffix: %q", data.Message[len(data.Message)-16:])
	}
	if len(strings.TrimSuffix(data.Message, "...")) > maxMessageLen {
		t.Fatalf("truncated bytes exceed cap: %d", len(data.Message))
	}

	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "empty", in: "", max: 5, want: ""},
		{name: "zero", in: "abc", max: 0, want: ""},
		{name: "negative", in: "abc", max: -1, want: ""},
		{name: "under cap", in: "abc", max: 3, want: "abc"},
		{name: "ascii cut", in: "abcdef", max: 4, want: "abcd"},
		{name: "rune boundary", in: "가나다", max: 6, want: "가나"},
		{name: "inside rune", in: "가나다", max: 5, want: "가"},
		{name: "one byte before rune", in: "a가", max: 1, want: "a"},
		{name: "inside first rune", in: "가", max: 1, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateUTF8Bytes(tt.in, tt.max); got != tt.want {
				t.Fatalf("truncateUTF8Bytes(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}

func TestWriterFilesystemContract(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(filepath.Join(dir, "nested", "logs"))
	if err := w.Append(LogEntry{Ts: 1, Session: "client/main\\evil\x00", Type: TypeRunStart}); err != nil {
		t.Fatal(err)
	}
	path := w.logPath("client/main\\evil\x00")
	if filepath.Dir(path) != filepath.Join(dir, "nested", "logs") {
		t.Fatalf("log escaped base dir: %s", path)
	}
	if filepath.Base(path) != "client_main_evil.jsonl" {
		t.Fatalf("sanitized basename = %q", filepath.Base(path))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("log mode = %o, want 600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got&0o077 != 0 {
		t.Fatalf("log dir is accessible to group/other: %o", got)
	}
	if got := filepath.Base(w.logPath("/\\\x00")); got != "_invalid_.jsonl" {
		t.Fatalf("empty sanitized session path = %q", got)
	}
}

func TestWriterConcurrentAppendPreservesEveryEntry(t *testing.T) {
	w := NewWriter(t.TempDir())
	const goroutines = 12
	const perGoroutine = 40
	var wg sync.WaitGroup
	for worker := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perGoroutine {
				raw, _ := json.Marshal(map[string]int{"worker": worker, "index": i})
				if err := w.Append(LogEntry{
					Ts:      int64(worker*perGoroutine + i),
					Type:    TypeTurnLLM,
					RunID:   "concurrent",
					Session: "shared",
					Data:    raw,
				}); err != nil {
					t.Errorf("append worker=%d index=%d: %v", worker, i, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	got := w.Read(ReadOpts{SessionKey: "shared", RunID: "concurrent", Limit: goroutines * perGoroutine})
	if got.Total != goroutines*perGoroutine {
		t.Fatalf("total = %d, want %d", got.Total, goroutines*perGoroutine)
	}
	seen := make(map[int64]bool, got.Total)
	for _, entry := range got.Entries {
		if seen[entry.Ts] {
			t.Fatalf("duplicate timestamp %d", entry.Ts)
		}
		seen[entry.Ts] = true
	}
}

func TestWriterAppendErrorsAreActionable(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := NewWriter(blocker)
	err := w.Append(LogEntry{Session: "s", Type: TypeRunStart})
	if err == nil {
		t.Fatal("append through regular-file base dir succeeded")
	}
	if !strings.Contains(err.Error(), "create agent log dir") && !strings.Contains(err.Error(), "open agent log") {
		t.Fatalf("error lacks operation context: %v", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		// Either ENOTDIR or another platform-specific path error is acceptable;
		// the important contract is that it is returned, not swallowed.
		t.Logf("platform classified path failure as not-exist: %v", err)
	}
}

func TestAggregateContractMalformedAndSinceBoundaries(t *testing.T) {
	w := NewWriter(t.TempDir())
	appendTimedEntry(t, w, 99, "s", "old", TypeRunEnd, RunEndData{InputTokens: 999})
	appendTimedEntry(t, w, 100, "s", "edge", TypeRunEnd, RunEndData{
		InputTokens:       10,
		OutputTokens:      4,
		CacheReadTokens:   7,
		Proactive:         true,
		Compacted:         true,
		RepairedToolCalls: map[string]int{"read": 2},
	})
	appendTimedEntry(t, w, 101, "s", "new", TypeTurnTool, TurnToolData{
		Name:       "read",
		DurationMs: 9,
		OutputLen:  30,
		IsError:    true,
	})
	appendTimedEntry(t, w, 102, SessionProactive, "", TypeProactiveRelay, ProactiveRelayData{
		Decision: "suppressed",
		Reason:   "silent_token",
	})
	appendTimedEntry(t, w, 103, SessionBackground, "", TypeBackgroundJob, BackgroundJobData{
		Name:    "heartbeat",
		Outcome: "error",
	})
	for _, entry := range []LogEntry{
		{Ts: 104, Type: TypeRunEnd, Session: "bad", Data: json.RawMessage(`"x"`)},
		{Ts: 105, Type: TypeTurnTool, Session: "bad", Data: json.RawMessage(`"x"`)},
		{Ts: 106, Type: TypeProactiveRelay, Session: "bad", Data: json.RawMessage(`"x"`)},
		{Ts: 107, Type: TypeBackgroundJob, Session: "bad", Data: json.RawMessage(`"x"`)},
	} {
		if err := w.Append(entry); err != nil {
			t.Fatal(err)
		}
	}

	got := w.Aggregate(100)
	if got.Runs != 1 || got.TotalInputTokens != 10 || got.TotalOutputTokens != 4 {
		t.Fatalf("run totals = %+v", got)
	}
	if got.CacheReadTokens != 7 || got.ProactiveRuns != 1 || got.CompactedRuns != 1 {
		t.Fatalf("run flags = %+v", got)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("tools = %+v, want one", got.Tools)
	}
	tool := got.Tools[0]
	if tool.Name != "read" || tool.Calls != 1 || tool.Errors != 1 || tool.Repaired != 2 || tool.AvgMs != 9 {
		t.Fatalf("tool rollup = %+v", tool)
	}
	if got.ProactiveDecisions["suppressed:silent_token"] != 1 {
		t.Fatalf("proactive = %+v", got.ProactiveDecisions)
	}
	if got.BackgroundJobs["heartbeat"] != 1 || got.BackgroundErrors["heartbeat"] != 1 {
		t.Fatalf("background = jobs%v errors%v", got.BackgroundJobs, got.BackgroundErrors)
	}
}
