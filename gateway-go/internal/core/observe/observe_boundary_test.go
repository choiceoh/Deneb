package observe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewRingCapacityBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		size int
		want int
	}{
		{name: "negative", size: -100, want: DefaultRingSize},
		{name: "negative one", size: -1, want: DefaultRingSize},
		{name: "zero", size: 0, want: DefaultRingSize},
		{name: "one", size: 1, want: 1},
		{name: "two", size: 2, want: 2},
		{name: "default explicit", size: DefaultRingSize, want: DefaultRingSize},
		{name: "larger", size: DefaultRingSize + 1, want: DefaultRingSize + 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ring := NewRing(tc.size)
			if ring == nil {
				t.Fatal("NewRing returned nil")
			}
			if ring.Cap() != tc.want {
				t.Fatalf("Cap = %d, want %d", ring.Cap(), tc.want)
			}
			if ring.Len() != 0 {
				t.Fatalf("Len = %d, want 0", ring.Len())
			}
			if len(ring.buf) != tc.want || ring.next != 0 || ring.full {
				t.Fatalf("initial ring state = %#v", ring)
			}
		})
	}
}

func TestRingWrapBoundaryForManyCapacities(t *testing.T) {
	t.Parallel()

	for capacity := 1; capacity <= 32; capacity++ {
		t.Run(fmt.Sprintf("capacity-%02d", capacity), func(t *testing.T) {
			t.Parallel()
			ring := NewRing(capacity)
			writes := capacity*3 + 7
			for i := 0; i < writes; i++ {
				ring.append(LogLine{Ts: int64(i), Msg: fmt.Sprintf("line-%d", i), lvl: slog.LevelInfo})
				wantLen := min(i+1, capacity)
				if got := ring.Len(); got != wantLen {
					t.Fatalf("write %d Len = %d, want %d", i, got, wantLen)
				}
			}
			got := ring.Query(QueryOpts{MinLevel: slog.LevelDebug, Limit: capacity + 10})
			if len(got) != capacity {
				t.Fatalf("Query length = %d, want %d", len(got), capacity)
			}
			for i, line := range got {
				wantTS := int64(writes - 1 - i)
				if line.Ts != wantTS || line.Msg != fmt.Sprintf("line-%d", wantTS) {
					t.Errorf("result %d = %#v, want Ts=%d", i, line, wantTS)
				}
			}
		})
	}
}

func TestRingQueryLimitBoundary(t *testing.T) {
	t.Parallel()

	ring := NewRing(500)
	for i := 0; i < 300; i++ {
		ring.append(LogLine{Ts: int64(i), Msg: fmt.Sprintf("line-%03d", i), lvl: slog.LevelInfo})
	}
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "large negative defaults", limit: -100, want: defaultQueryLimit},
		{name: "negative one defaults", limit: -1, want: defaultQueryLimit},
		{name: "zero defaults", limit: 0, want: defaultQueryLimit},
		{name: "one", limit: 1, want: 1},
		{name: "below count", limit: 17, want: 17},
		{name: "exact count", limit: 300, want: 300},
		{name: "above count", limit: 301, want: 300},
		{name: "huge", limit: 1_000_000, want: 300},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ring.Query(QueryOpts{Limit: tc.limit})
			if len(got) != tc.want {
				t.Fatalf("Query limit %d length = %d, want %d", tc.limit, len(got), tc.want)
			}
			if len(got) > 0 && got[0].Ts != 299 {
				t.Fatalf("newest Ts = %d, want 299", got[0].Ts)
			}
		})
	}
}

func TestRingQueryFilterBoundaryMatrix(t *testing.T) {
	t.Parallel()

	ring := NewRing(20)
	lines := []LogLine{
		{Ts: 0, RunID: "", Session: "", Msg: "zero debug", lvl: slog.LevelDebug},
		{Ts: 1, RunID: "run-a", Session: "s-a", Msg: "alpha debug", lvl: slog.LevelDebug},
		{Ts: 2, RunID: "run-a", Session: "s-a", Msg: "alpha info", lvl: slog.LevelInfo},
		{Ts: 3, RunID: "run-b", Session: "s-a", Msg: "beta warn", lvl: slog.LevelWarn},
		{Ts: 4, RunID: "run-b", Session: "s-b", Msg: "beta error", lvl: slog.LevelError},
		{Ts: 5, RunID: "run-c", Session: "s-c", Msg: "CaseSensitive", lvl: slog.LevelError + 4},
	}
	for _, line := range lines {
		ring.append(line)
	}
	tests := []struct {
		name string
		opts QueryOpts
		want []int64
	}{
		{name: "zero opts defaults info", opts: QueryOpts{Limit: 20}, want: []int64{5, 4, 3, 2}},
		{name: "debug includes all", opts: QueryOpts{MinLevel: slog.LevelDebug, Limit: 20}, want: []int64{5, 4, 3, 2, 1, 0}},
		{name: "run exact", opts: QueryOpts{RunID: "run-a", MinLevel: slog.LevelDebug, Limit: 20}, want: []int64{2, 1}},
		{name: "run is case sensitive", opts: QueryOpts{RunID: "RUN-A", MinLevel: slog.LevelDebug, Limit: 20}, want: nil},
		{name: "session exact", opts: QueryOpts{Session: "s-a", MinLevel: slog.LevelDebug, Limit: 20}, want: []int64{3, 2, 1}},
		{name: "session and run", opts: QueryOpts{RunID: "run-b", Session: "s-a", MinLevel: slog.LevelDebug, Limit: 20}, want: []int64{3}},
		{name: "warn threshold", opts: QueryOpts{MinLevel: slog.LevelWarn, Limit: 20}, want: []int64{5, 4, 3}},
		{name: "error threshold", opts: QueryOpts{MinLevel: slog.LevelError, Limit: 20}, want: []int64{5, 4}},
		{name: "above error threshold", opts: QueryOpts{MinLevel: slog.LevelError + 1, Limit: 20}, want: []int64{5}},
		{name: "since disabled at zero", opts: QueryOpts{SinceMs: 0, MinLevel: slog.LevelDebug, Limit: 20}, want: []int64{5, 4, 3, 2, 1, 0}},
		{name: "since inclusive", opts: QueryOpts{SinceMs: 3, MinLevel: slog.LevelDebug, Limit: 20}, want: []int64{5, 4, 3}},
		{name: "since after newest", opts: QueryOpts{SinceMs: 6, MinLevel: slog.LevelDebug, Limit: 20}, want: nil},
		{name: "contains substring", opts: QueryOpts{Contains: "beta", MinLevel: slog.LevelDebug, Limit: 20}, want: []int64{4, 3}},
		{name: "contains case sensitive match", opts: QueryOpts{Contains: "Case", MinLevel: slog.LevelDebug, Limit: 20}, want: []int64{5}},
		{name: "contains case sensitive miss", opts: QueryOpts{Contains: "case", MinLevel: slog.LevelDebug, Limit: 20}, want: nil},
		{name: "all filters", opts: QueryOpts{RunID: "run-b", Session: "s-b", MinLevel: slog.LevelError, SinceMs: 4, Contains: "error", Limit: 20}, want: []int64{4}},
		{name: "limit after filtering", opts: QueryOpts{MinLevel: slog.LevelDebug, Contains: "alpha", Limit: 1}, want: []int64{2}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ring.Query(tc.opts)
			gotTS := make([]int64, len(got))
			for i := range got {
				gotTS[i] = got[i].Ts
			}
			if len(gotTS) != len(tc.want) {
				t.Fatalf("timestamps = %v, want %v", gotTS, tc.want)
			}
			for i := range gotTS {
				if gotTS[i] != tc.want[i] {
					t.Fatalf("timestamps = %v, want %v", gotTS, tc.want)
				}
			}
		})
	}
}

func TestRingQueryReturnsDeepCopiesOfAttrs(t *testing.T) {
	t.Parallel()

	ring := NewRing(3)
	ring.append(LogLine{
		Ts:    1,
		Msg:   "with attrs",
		Attrs: map[string]string{"key": "original"},
		lvl:   slog.LevelInfo,
	})
	one := ring.Query(QueryOpts{Limit: 1})
	two := ring.Query(QueryOpts{Limit: 1})
	one[0].Attrs["key"] = "changed"
	one[0].Attrs["new"] = "added"
	if !reflect.DeepEqual(two[0].Attrs, map[string]string{"key": "original"}) {
		t.Fatalf("second query shares attrs: %#v", two[0].Attrs)
	}
	three := ring.Query(QueryOpts{Limit: 1})
	if !reflect.DeepEqual(three[0].Attrs, map[string]string{"key": "original"}) {
		t.Fatalf("ring mutated through query: %#v", three[0].Attrs)
	}
}

func TestRingQueryPreservesNilAndEmptyAttrsShape(t *testing.T) {
	t.Parallel()

	ring := NewRing(2)
	ring.append(LogLine{Ts: 1, Attrs: nil, lvl: slog.LevelInfo})
	ring.append(LogLine{Ts: 2, Attrs: map[string]string{}, lvl: slog.LevelInfo})
	got := ring.Query(QueryOpts{Limit: 2})
	if got[0].Attrs == nil {
		t.Fatal("non-nil empty Attrs became nil")
	}
	if got[1].Attrs != nil {
		t.Fatalf("nil Attrs became non-nil: %#v", got[1].Attrs)
	}
}

func TestContainsSubMatchesStringsContainsBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		s   string
		sub string
	}{
		{s: "", sub: ""},
		{s: "", sub: "a"},
		{s: "a", sub: ""},
		{s: "a", sub: "a"},
		{s: "a", sub: "aa"},
		{s: "abc", sub: "a"},
		{s: "abc", sub: "b"},
		{s: "abc", sub: "c"},
		{s: "abc", sub: "abc"},
		{s: "abc", sub: "abcd"},
		{s: "aaaa", sub: "aa"},
		{s: "한글 문자열", sub: "문자"},
		{s: "한글 문자열", sub: "열"},
		{s: "emoji 🚀 launch", sub: "🚀"},
		{s: "CaseSensitive", sub: "case"},
	}
	for _, tc := range tests {
		if got, want := containsSub(tc.s, tc.sub), strings.Contains(tc.s, tc.sub); got != want {
			t.Errorf("containsSub(%q,%q) = %v, strings.Contains = %v", tc.s, tc.sub, got, want)
		}
	}
}

func TestParseLevelBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want slog.Level
	}{
		{in: "", want: slog.LevelDebug},
		{in: "debug", want: slog.LevelDebug},
		{in: "info", want: slog.LevelInfo},
		{in: "warn", want: slog.LevelWarn},
		{in: "warning", want: slog.LevelWarn},
		{in: "error", want: slog.LevelError},
		{in: "DEBUG", want: slog.LevelDebug},
		{in: "INFO", want: slog.LevelDebug},
		{in: "WARN", want: slog.LevelDebug},
		{in: "ERROR", want: slog.LevelDebug},
		{in: " info", want: slog.LevelDebug},
		{in: "info ", want: slog.LevelDebug},
		{in: "trace", want: slog.LevelDebug},
		{in: "fatal", want: slog.LevelDebug},
		{in: "bogus", want: slog.LevelDebug},
	}
	for _, tc := range tests {
		if got := ParseLevel(tc.in); got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestNewCaptureNilDelegateCapturesWithoutPanic(t *testing.T) {
	t.Parallel()

	ring := NewRing(4)
	capture := NewCapture(nil, ring)
	if capture == nil || capture.delegate == nil {
		t.Fatalf("NewCapture(nil) = %#v", capture)
	}
	logger := slog.New(capture)
	logger.Info("captured", "runId", "nil-delegate")
	got := ring.Query(QueryOpts{RunID: "nil-delegate", Limit: 1})
	if len(got) != 1 || got[0].Msg != "captured" {
		t.Fatalf("captured lines = %#v", got)
	}
}

func TestCaptureEnabledReturnsDelegateLevelDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		minimum slog.Level
		level   slog.Level
		want    bool
	}{
		{minimum: slog.LevelDebug, level: slog.LevelDebug, want: true},
		{minimum: slog.LevelDebug, level: slog.LevelInfo, want: true},
		{minimum: slog.LevelInfo, level: slog.LevelDebug, want: false},
		{minimum: slog.LevelInfo, level: slog.LevelInfo, want: true},
		{minimum: slog.LevelWarn, level: slog.LevelInfo, want: false},
		{minimum: slog.LevelWarn, level: slog.LevelWarn, want: true},
		{minimum: slog.LevelError, level: slog.LevelWarn, want: false},
		{minimum: slog.LevelError, level: slog.LevelError, want: true},
	}
	for _, tc := range tests {
		delegate := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: tc.minimum})
		capture := NewCapture(delegate, NewRing(1))
		if got := capture.Enabled(context.Background(), tc.level); got != tc.want {
			t.Errorf("minimum=%v level=%v Enabled=%v, want %v", tc.minimum, tc.level, got, tc.want)
		}
	}
}

func TestCaptureToLineUpdatesJoinKeysFromRecordAttrs(t *testing.T) {
	t.Parallel()

	capture := NewCapture(nil, NewRing(1))
	capture.attrs = []slog.Attr{
		slog.String("runId", "outer-run"),
		slog.String("session", "outer-session"),
		slog.String("outer", "value"),
	}
	record := slog.NewRecord(time.Unix(123, 456_000_000), slog.LevelWarn, "message", 0)
	record.AddAttrs(
		slog.String("runId", "record-run"),
		slog.String("sessionKey", "record-session-key"),
		slog.String("session", "record-session"),
		slog.Int("count", 7),
		slog.Bool("ok", true),
	)
	line := capture.toLine(record)
	if line.Ts != 123456 {
		t.Fatalf("Ts = %d, want 123456", line.Ts)
	}
	if line.Level != "WARN" || line.lvl != slog.LevelWarn || line.Msg != "message" {
		t.Fatalf("record scalars = %#v", line)
	}
	if line.RunID != "record-run" {
		t.Fatalf("record runId did not override With attr: %q", line.RunID)
	}
	if line.Session != "outer-session" {
		t.Fatalf("first session alias did not win: %q", line.Session)
	}
	wantAttrs := map[string]string{"outer": "value", "count": "7", "ok": "true"}
	if !reflect.DeepEqual(line.Attrs, wantAttrs) {
		t.Fatalf("Attrs = %#v, want %#v", line.Attrs, wantAttrs)
	}
}

func TestCaptureFormatsVariousAttributeKindsAsStrings(t *testing.T) {
	t.Parallel()

	capture := NewCapture(nil, NewRing(1))
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "kinds", 0)
	record.AddAttrs(
		slog.String("string", "value"),
		slog.Int64("int", -7),
		slog.Uint64("uint", 9),
		slog.Float64("float", 1.25),
		slog.Bool("bool", true),
		slog.Duration("duration", 1500*time.Millisecond),
		slog.Time("time", time.Unix(0, 0).UTC()),
		slog.Any("error", errors.New("boom")),
		slog.Group("group", slog.String("nested", "value")),
	)
	line := capture.toLine(record)
	checks := map[string]string{
		"string":   "value",
		"int":      "-7",
		"uint":     "9",
		"float":    "1.25",
		"bool":     "true",
		"duration": "1.5s",
		"error":    "boom",
	}
	for key, want := range checks {
		if got := line.Attrs[key]; got != want {
			t.Errorf("Attrs[%q] = %q, want %q", key, got, want)
		}
	}
	if line.Attrs["time"] == "" || line.Attrs["group"] == "" {
		t.Fatalf("complex attrs missing: %#v", line.Attrs)
	}
}

func TestCaptureWithAttrsIsImmutableAcrossBranches(t *testing.T) {
	t.Parallel()

	ring := NewRing(10)
	root := NewCapture(nil, ring)
	branchA := root.WithAttrs([]slog.Attr{slog.String("runId", "run-a"), slog.String("branch", "a")})
	branchB := root.WithAttrs([]slog.Attr{slog.String("runId", "run-b"), slog.String("branch", "b")})
	slog.New(branchA).Info("from a")
	slog.New(branchB).Info("from b")
	slog.New(root).Info("from root")

	a := ring.Query(QueryOpts{RunID: "run-a", Limit: 10})
	b := ring.Query(QueryOpts{RunID: "run-b", Limit: 10})
	all := ring.Query(QueryOpts{MinLevel: slog.LevelDebug, Limit: 10})
	if len(a) != 1 || a[0].Attrs["branch"] != "a" {
		t.Fatalf("branch A = %#v", a)
	}
	if len(b) != 1 || b[0].Attrs["branch"] != "b" {
		t.Fatalf("branch B = %#v", b)
	}
	if len(all) != 3 || all[0].RunID != "" {
		t.Fatalf("root/branches leaked attrs: %#v", all)
	}
}

func TestCaptureWithEmptyAttrsAndGroupReturnsSameHandler(t *testing.T) {
	t.Parallel()

	capture := NewCapture(nil, NewRing(1))
	if got := capture.WithAttrs(nil); got != capture {
		t.Fatalf("WithAttrs(nil) = %T %p, want original %p", got, got, capture)
	}
	if got := capture.WithAttrs([]slog.Attr{}); got != capture {
		t.Fatalf("WithAttrs(empty) = %T %p, want original %p", got, got, capture)
	}
	if got := capture.WithGroup(""); got != capture {
		t.Fatalf("WithGroup(empty) = %T %p, want original %p", got, got, capture)
	}
}

func TestCaptureWithGroupPreservesCapturedFlatAttrsAndDelegateGrouping(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	ring := NewRing(2)
	capture := NewCapture(slog.NewJSONHandler(&output, nil), ring)
	logger := slog.New(capture.WithGroup("request")).With("runId", "run-g", "field", "value")
	logger.Info("grouped", "count", 3)
	lines := ring.Query(QueryOpts{RunID: "run-g", Limit: 1})
	if len(lines) != 1 || lines[0].Attrs["field"] != "value" || lines[0].Attrs["count"] != "3" {
		t.Fatalf("captured grouped line = %#v", lines)
	}
	text := output.String()
	if !strings.Contains(text, `"request"`) || !strings.Contains(text, `"field":"value"`) || !strings.Contains(text, `"count":3`) {
		t.Fatalf("delegate grouping lost: %s", text)
	}
}

type errorHandler struct {
	err     error
	handled atomic.Int64
}

func (h *errorHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *errorHandler) Handle(context.Context, slog.Record) error {
	h.handled.Add(1)
	return h.err
}
func (h *errorHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *errorHandler) WithGroup(string) slog.Handler      { return h }

func TestCapturePropagatesDelegateErrorAfterCapturing(t *testing.T) {
	t.Parallel()

	want := errors.New("delegate failed")
	delegate := &errorHandler{err: want}
	ring := NewRing(2)
	capture := NewCapture(delegate, ring)
	record := slog.NewRecord(time.Now(), slog.LevelError, "boom", 0)
	if err := capture.Handle(context.Background(), record); !errors.Is(err, want) {
		t.Fatalf("Handle error = %v, want %v", err, want)
	}
	if delegate.handled.Load() != 1 {
		t.Fatalf("delegate handled = %d", delegate.handled.Load())
	}
	got := ring.Query(QueryOpts{Limit: 1})
	if len(got) != 1 || got[0].Msg != "boom" {
		t.Fatalf("record not captured before delegate error: %#v", got)
	}
}

func TestConcurrentRingAppendQueryAndSnapshotMutation(t *testing.T) {
	const (
		writers    = 32
		readers    = 16
		iterations = 600
	)
	ring := NewRing(1024)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for iteration := 0; iteration < iterations; iteration++ {
				ring.append(LogLine{
					Ts:      int64(writer*iterations + iteration),
					RunID:   fmt.Sprintf("run-%d", writer%4),
					Session: fmt.Sprintf("session-%d", writer%3),
					Msg:     fmt.Sprintf("writer=%d iteration=%d", writer, iteration),
					Attrs:   map[string]string{"writer": fmt.Sprint(writer), "iteration": fmt.Sprint(iteration)},
					lvl:     slog.Level(iteration%4*4 - 4),
				})
			}
		}()
	}
	for reader := 0; reader < readers; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for iteration := 0; iteration < iterations; iteration++ {
				lines := ring.Query(QueryOpts{
					RunID:    fmt.Sprintf("run-%d", reader%4),
					MinLevel: slog.LevelDebug,
					Limit:    50,
				})
				for i := range lines {
					if lines[i].RunID != fmt.Sprintf("run-%d", reader%4) {
						t.Errorf("query returned wrong run: %#v", lines[i])
						return
					}
					lines[i].Attrs["writer"] = "consumer mutation"
					lines[i].Attrs["consumer"] = fmt.Sprint(reader)
				}
				_ = ring.Len()
			}
		}()
	}
	close(start)
	wg.Wait()
	if ring.Len() != ring.Cap() {
		t.Fatalf("Len/Cap = %d/%d after %d writes", ring.Len(), ring.Cap(), writers*iterations)
	}
	for _, line := range ring.Query(QueryOpts{MinLevel: slog.LevelDebug, Limit: ring.Cap()}) {
		if line.Attrs["writer"] == "consumer mutation" || line.Attrs["consumer"] != "" {
			t.Fatalf("query mutation reached ring: %#v", line)
		}
	}
}

func TestPromLabelBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		key  string
		want string
	}{
		{name: "empty", body: "", key: "model_name", want: ""},
		{name: "first", body: `model_name="m",engine="0"`, key: "model_name", want: "m"},
		{name: "middle", body: `engine="0",model_name="m",rank="1"`, key: "model_name", want: "m"},
		{name: "after spaced comma", body: `engine="0", model_name="m"`, key: "model_name", want: "m"},
		{name: "last", body: `engine="0",model_name="m"`, key: "model_name", want: "m"},
		{name: "empty value", body: `model_name=""`, key: "model_name", want: ""},
		{name: "prefix decoy", body: `engine_model_name="decoy",model_name="real"`, key: "model_name", want: "real"},
		{name: "suffix decoy", body: `model_name_extra="decoy",model_name="real"`, key: "model_name", want: "real"},
		{name: "missing closing quote", body: `model_name="broken`, key: "model_name", want: ""},
		{name: "absent", body: `engine="0"`, key: "model_name", want: ""},
		{name: "different key", body: `engine="0",model_name="m"`, key: "engine", want: "0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := promLabel(tc.body, tc.key); got != tc.want {
				t.Fatalf("promLabel(%q,%q) = %q, want %q", tc.body, tc.key, got, tc.want)
			}
		})
	}
}

func TestParseVllmCounterNumericBoundaryMatrix(t *testing.T) {
	t.Parallel()

	const metric = vllmPrefixQueriesMetric
	tests := []struct {
		name  string
		value string
		want  float64
		ok    bool
	}{
		{name: "zero", value: "0", want: 0, ok: true},
		{name: "negative", value: "-1", want: -1, ok: true},
		{name: "integer", value: "42", want: 42, ok: true},
		{name: "decimal", value: "1.25", want: 1.25, ok: true},
		{name: "leading decimal", value: ".5", want: 0.5, ok: true},
		{name: "scientific positive", value: "1e3", want: 1000, ok: true},
		{name: "scientific negative", value: "2.5e-2", want: 0.025, ok: true},
		{name: "positive infinity", value: "+Inf", want: math.Inf(1), ok: true},
		{name: "negative infinity", value: "-Inf", want: math.Inf(-1), ok: true},
		{name: "nan", value: "NaN", want: math.NaN(), ok: true},
		{name: "empty", value: "", want: 0, ok: false},
		{name: "garbage", value: "abc", want: 0, ok: false},
		{name: "comma", value: "1,000", want: 0, ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			line := metric + `{model_name="m"} ` + tc.value
			model, got, ok := parseVllmCounter(line, metric)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (value=%v)", ok, tc.ok, got)
			}
			if !tc.ok {
				return
			}
			if model != "m" {
				t.Fatalf("model = %q", model)
			}
			if math.IsNaN(tc.want) {
				if !math.IsNaN(got) {
					t.Fatalf("value = %v, want NaN", got)
				}
			} else if got != tc.want {
				t.Fatalf("value = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestScrapeVllmCacheHTTPBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       string
		want       []VllmPrefixCache
		wantMethod string
		wantPath   string
	}{
		{name: "not found", status: http.StatusNotFound, body: "not found", want: nil, wantMethod: http.MethodGet, wantPath: "/metrics"},
		{name: "server error", status: http.StatusInternalServerError, body: "boom", want: nil, wantMethod: http.MethodGet, wantPath: "/metrics"},
		{name: "empty ok", status: http.StatusOK, body: "", want: []VllmPrefixCache{}, wantMethod: http.MethodGet, wantPath: "/metrics"},
		{name: "queries only", status: http.StatusOK, body: vllmPrefixQueriesMetric + "{model_name=\"m\"} 10\n", want: []VllmPrefixCache{{Model: "m", Queries: 10, Hits: 0, HitRatePct: 0}}, wantMethod: http.MethodGet, wantPath: "/metrics"},
		{name: "hits only", status: http.StatusOK, body: vllmPrefixHitsMetric + "{model_name=\"m\"} 7\n", want: []VllmPrefixCache{{Model: "m", Queries: 0, Hits: 7, HitRatePct: 0}}, wantMethod: http.MethodGet, wantPath: "/metrics"},
		{name: "both", status: http.StatusOK, body: vllmPrefixQueriesMetric + "{model_name=\"m\"} 8\n" + vllmPrefixHitsMetric + "{model_name=\"m\"} 3\n", want: []VllmPrefixCache{{Model: "m", Queries: 8, Hits: 3, HitRatePct: 37.5}}, wantMethod: http.MethodGet, wantPath: "/metrics"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var method, path string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method, path = r.Method, r.URL.Path
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			got := scrapeVllmPrefixCache(context.Background(), server.Client(), server.URL+"/v1/")
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("scrape = %#v, want %#v", got, tc.want)
			}
			if method != tc.wantMethod || path != tc.wantPath {
				t.Fatalf("request = %s %s, want %s %s", method, path, tc.wantMethod, tc.wantPath)
			}
		})
	}
}

func TestFetchVllmPrefixCachesPreservesEndpointOrderAndSortsWithinEndpoint(t *testing.T) {
	t.Parallel()

	server := func(prefix string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, "%s{model_name=\"%s-z\"} 10\n", vllmPrefixQueriesMetric, prefix)
			fmt.Fprintf(w, "%s{model_name=\"%s-a\"} 20\n", vllmPrefixQueriesMetric, prefix)
		}))
	}
	one := server("one")
	defer one.Close()
	two := server("two")
	defer two.Close()
	got := FetchVllmPrefixCaches(context.Background(), []string{two.URL + "/v1", one.URL + "/v1"})
	wantModels := []string{"two-a", "two-z", "one-a", "one-z"}
	models := make([]string, len(got))
	for i := range got {
		models[i] = got[i].Model
	}
	if !reflect.DeepEqual(models, wantModels) {
		t.Fatalf("models = %v, want %v", models, wantModels)
	}
}

func TestBuildTurnViewRingOnlyRecoversNewestNonEmptySession(t *testing.T) {
	t.Parallel()

	ring := NewRing(8)
	ring.append(LogLine{Ts: 1, RunID: "run", Session: "old-session", lvl: slog.LevelInfo})
	ring.append(LogLine{Ts: 2, RunID: "run", Session: "", lvl: slog.LevelInfo})
	ring.append(LogLine{Ts: 3, RunID: "other", Session: "other-session", lvl: slog.LevelInfo})
	ring.append(LogLine{Ts: 4, RunID: "run", Session: "new-session", lvl: slog.LevelInfo})
	view := BuildTurnView(nil, ring, "run")
	if view.Found {
		t.Fatal("ring-only view Found = true")
	}
	if view.Session != "new-session" {
		t.Fatalf("Session = %q, want newest non-empty session", view.Session)
	}
	if len(view.Logs) != 3 || view.Logs[0].Ts != 4 || view.Logs[1].Ts != 2 || view.Logs[2].Ts != 1 {
		t.Fatalf("Logs = %#v", view.Logs)
	}
}

func TestBuildTurnViewTruncatesLogsKeepingNewestEntries(t *testing.T) {
	t.Parallel()

	ring := NewRing(turnLogLimit + 100)
	for i := 0; i < turnLogLimit+75; i++ {
		ring.append(LogLine{Ts: int64(i), RunID: "run", Msg: fmt.Sprint(i), lvl: slog.LevelDebug})
	}
	view := BuildTurnView(nil, ring, "run")
	if len(view.Logs) != turnLogLimit {
		t.Fatalf("Logs = %d, want cap %d", len(view.Logs), turnLogLimit)
	}
	if view.Logs[0].Ts != turnLogLimit+74 {
		t.Fatalf("newest Ts = %d", view.Logs[0].Ts)
	}
	if view.Logs[len(view.Logs)-1].Ts != 75 {
		t.Fatalf("oldest retained Ts = %d, want 75", view.Logs[len(view.Logs)-1].Ts)
	}
}

func TestCloneLogLineBoundary(t *testing.T) {
	t.Parallel()

	nilLine := LogLine{Msg: "nil", Attrs: nil}
	if got := cloneLogLine(nilLine); got.Attrs != nil || got.Msg != "nil" {
		t.Fatalf("clone nil line = %#v", got)
	}
	emptyLine := LogLine{Msg: "empty", Attrs: map[string]string{}}
	emptyClone := cloneLogLine(emptyLine)
	if emptyClone.Attrs == nil || len(emptyClone.Attrs) != 0 {
		t.Fatalf("clone empty line = %#v", emptyClone)
	}
	line := LogLine{Ts: 1, Level: "INFO", Msg: "msg", RunID: "run", Session: "session", Attrs: map[string]string{"a": "b"}, lvl: slog.LevelInfo}
	clone := cloneLogLine(line)
	if !reflect.DeepEqual(clone, line) {
		t.Fatalf("clone = %#v, want %#v", clone, line)
	}
	clone.Attrs["a"] = "changed"
	if line.Attrs["a"] != "b" {
		t.Fatalf("clone shares attrs: original=%#v clone=%#v", line.Attrs, clone.Attrs)
	}
}

func TestConcurrentCaptureHandlersShareRingSafely(t *testing.T) {
	const (
		workers    = 48
		iterations = 400
	)
	ring := NewRing(workers * iterations)
	root := NewCapture(nil, ring)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {

		handler := root.WithAttrs([]slog.Attr{
			slog.String("runId", fmt.Sprintf("run-%d", worker)),
			slog.String("session", fmt.Sprintf("session-%d", worker%3)),
			slog.Int("worker", worker),
		})
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			logger := slog.New(handler)
			for iteration := 0; iteration < iterations; iteration++ {
				logger.Info("message", "iteration", iteration)
			}
		}()
	}
	close(start)
	wg.Wait()
	if ring.Len() != workers*iterations {
		t.Fatalf("Len = %d, want %d", ring.Len(), workers*iterations)
	}
	for worker := 0; worker < workers; worker++ {
		runID := fmt.Sprintf("run-%d", worker)
		lines := ring.Query(QueryOpts{RunID: runID, Limit: iterations + 1})
		if len(lines) != iterations {
			t.Errorf("%s lines = %d, want %d", runID, len(lines), iterations)
			continue
		}
		seen := make([]int, 0, iterations)
		for _, line := range lines {
			if line.Attrs["worker"] != fmt.Sprint(worker) {
				t.Errorf("%s wrong attrs: %#v", runID, line.Attrs)
				break
			}
			var iteration int
			if _, err := fmt.Sscan(line.Attrs["iteration"], &iteration); err != nil {
				t.Errorf("parse iteration: %v", err)
				break
			}
			seen = append(seen, iteration)
		}
		sort.Ints(seen)
		for i, got := range seen {
			if got != i {
				t.Errorf("%s iterations missing at %d: got %d", runID, i, got)
				break
			}
		}
	}
}
