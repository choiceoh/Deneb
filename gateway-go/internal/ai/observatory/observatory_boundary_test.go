package observatory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeObservatoryFile(t *testing.T, root, rel, content string, mtime time.Time) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", rel, err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatalf("Chtimes %s: %v", rel, err)
		}
	}
	return path
}

func TestMarkdownRendersAllStatusBranchesAndOptionalSections(t *testing.T) {
	now := time.Date(2026, 7, 11, 14, 30, 0, 0, time.FixedZone("KST", 9*60*60))
	r := Report{
		GeneratedAt: now,
		Liveness: []LoopStatus{
			{Name: "missing", Missing: true},
			{Name: "minutes", Fresh: true, AgeHours: 0.5},
			{Name: "hours", Fresh: true, AgeHours: 23.9},
			{Name: "days", Fresh: false, AgeHours: 72.2},
		},
		Skill: SkillSummary{NoOp: 7, Total: 7},
		Frontier: []FrontierItem{
			{Skill: "github", NoOps: 4},
			{Skill: "meeting-minutes", NoOps: 3},
		},
		Memory: MemoryStatus{
			DreamerConsumedThrough: "2026-07-08",
			LatestDiary:            "2026-07-11",
			BacklogDays:            3,
			PendingBytes:           1234,
			MemoryMDStamp:          "2026-06-30 10:00:00",
			SpilloverToday:         5,
		},
		Models: ModelSummary{WindowHours: 24, Models: []string{"a", "b"}, Down: []string{"vllm", "embed"}},
		Failures: []FailureCount{
			{Pattern: "unknown tool", Count: 2},
			{Pattern: "empty-args parse fail", Count: 1},
		},
	}
	got := r.Markdown()
	for _, want := range []string{
		"2026-07-11 14:30 KST",
		"missing MISSING",
		"minutes ok(30m)",
		"hours ok(23h)",
		"days STALE(3d)",
		"no-op 7 / evolve 0 / genesis 0 (saturated: no evolve/genesis)",
		"dreamer→2026-07-08 diary→2026-07-11 (backlog 3d) (pending 1234B)",
		"memoryMD→2026-06-30",
		"spillover 5 today",
		"2 in 24h window · DOWN: vllm, embed",
		"FRONTIER  github×4 · meeting-minutes×3",
		"FAILURES  unknown tool×2 · empty-args parse fail×1 (24h)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "\n") != 7 {
		t.Fatalf("markdown line count changed: %d\n%s", strings.Count(got, "\n"), got)
	}
}

func TestMarkdownRendersDashesForEmptyReportWithoutOptionalRows(t *testing.T) {
	r := Report{
		GeneratedAt: time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC),
		Memory:      MemoryStatus{},
		Models:      ModelSummary{},
	}
	got := r.Markdown()
	for _, want := range []string{
		"LIVENESS  ",
		"SKILL     no-op 0 / evolve 0 / genesis 0",
		"MEMORY    dreamer→— diary→— · spillover 0 today",
		"MODELS    0 in 0h window",
		"FAILURES  none (24h)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("minimal markdown missing %q:\n%s", want, got)
		}
	}
	for _, absent := range []string{"FRONTIER", "DOWN:", "backlog", "pending", "saturated"} {
		if strings.Contains(got, absent) {
			t.Errorf("minimal markdown unexpectedly contains %q", absent)
		}
	}
}

func TestHumanAgeAndDashBoundaryFormatting(t *testing.T) {
	cases := []struct {
		hours float64
		want  string
	}{
		{0, "0m"},
		{0.016, "0m"},
		{0.5, "30m"},
		{0.999, "59m"},
		{1, "1h"},
		{47.999, "47h"},
		{48, "2d"},
		{71.9, "2d"},
		{72, "3d"},
	}
	for _, tc := range cases {
		if got := humanAge(tc.hours); got != tc.want {
			t.Errorf("humanAge(%v) = %q, want %q", tc.hours, got, tc.want)
		}
	}
	for input, want := range map[string]string{"": "—", "   ": "—", "\t": "—", "value": "value", " value ": " value "} {
		if got := dash(input); got != want {
			t.Errorf("dash(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLoopStatusMissingExactThresholdStaleAndFuture(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	sp := loopSpec{name: "state", rel: "data/state.json", thresh: 24}
	missing := loopStatus(root, sp, now)
	if missing.Name != "state" || !missing.Missing || missing.Fresh || missing.AgeHours != 0 {
		t.Fatalf("missing loop = %#v", missing)
	}
	path := writeObservatoryFile(t, root, sp.rel, `{}`, now.Add(-24*time.Hour))
	exact := loopStatus(root, sp, now)
	if exact.Missing || !exact.Fresh || exact.AgeHours != 24 {
		t.Fatalf("exact threshold loop = %#v", exact)
	}
	if err := os.Chtimes(path, now.Add(-24*time.Hour-time.Second), now.Add(-24*time.Hour-time.Second)); err != nil {
		t.Fatal(err)
	}
	stale := loopStatus(root, sp, now)
	if stale.Fresh || stale.Missing || stale.AgeHours <= 24 {
		t.Fatalf("stale loop = %#v", stale)
	}
	if err := os.Chtimes(path, now.Add(time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	future := loopStatus(root, sp, now)
	if !future.Fresh || future.AgeHours != -1 {
		t.Fatalf("future clock-skew loop = %#v", future)
	}
}

func TestLoopStatusDiaryLoadsNewestValidFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "memory", "diary")
	if err := os.MkdirAll(filepath.Join(dir, "diary-9999-99-99.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	writeObservatoryFile(t, root, "memory/diary/notes.txt", "ignore", now)
	writeObservatoryFile(t, root, "memory/diary/diary-2026-07-10.md", "old", now.Add(-30*time.Hour))
	writeObservatoryFile(t, root, "memory/diary/diary-2026-07-11.md", "new", now.Add(-time.Hour))
	got := loopStatus(root, loopSpec{name: "diary", rel: "memory/diary", thresh: 48}, now)
	if got.Missing || !got.Fresh || got.AgeHours != 1 {
		t.Fatalf("diary loop = %#v", got)
	}
}

func TestSkillAndFrontierNormalizesRoutesSkipsMalformedAndSorts(t *testing.T) {
	root := t.TempDir()
	path := writeObservatoryFile(t, root, "genesis.jsonl", strings.Join([]string{
		`{"route":" NO-OP ","skillName":" github "}`,
		`{"route":"noop","skillName":"github"}`,
		`{"route":"no-op","skillName":""}`,
		`{"route":"no-op","skillName":"alpha"}`,
		`{"route":"no-op","skillName":"beta"}`,
		`{"route":"evolve","skillName":"x"}`,
		`{"route":"GENESIS","skillName":"y"}`,
		`{"route":"ignored","skillName":"z"}`,
		`not-json`,
		`{"route":`,
	}, "\n")+"\n", time.Time{})
	summary, frontier := skillAndFrontier(path)
	if summary != (SkillSummary{NoOp: 5, Evolve: 1, Genesis: 1, Total: 7}) {
		t.Fatalf("skill summary = %#v", summary)
	}
	want := []FrontierItem{
		{Skill: "github", NoOps: 2},
		{Skill: "(unmatched)", NoOps: 1},
		{Skill: "alpha", NoOps: 1},
		{Skill: "beta", NoOps: 1},
	}
	if !reflect.DeepEqual(frontier, want) {
		t.Fatalf("frontier = %#v, want %#v", frontier, want)
	}
	if missingSummary, missingFrontier := skillAndFrontier(filepath.Join(root, "missing")); missingSummary != (SkillSummary{}) || missingFrontier != nil {
		t.Fatalf("missing log = %#v %#v", missingSummary, missingFrontier)
	}
}

func TestTopFrontierBreaksTiesAndPreservesInputMap(t *testing.T) {
	input := map[string]int{"z": 3, "a": 3, "b": 2, "c": 1}
	got := topFrontier(input, 3)
	want := []FrontierItem{{Skill: "a", NoOps: 3}, {Skill: "z", NoOps: 3}, {Skill: "b", NoOps: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("top frontier = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(input, map[string]int{"z": 3, "a": 3, "b": 2, "c": 1}) {
		t.Fatalf("topFrontier mutated input: %#v", input)
	}
	if got := topFrontier(nil, 5); len(got) != 0 {
		t.Fatalf("nil frontier = %#v", got)
	}
}

func TestRecentFailuresCountsMultiplePatternsAndSkipsInvalidSources(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Hour).UnixMilli()
	old := now.Add(-25 * time.Hour).UnixMilli()
	writeObservatoryFile(t, root, "recent.log", strings.Join([]string{
		fmt.Sprintf(`{"ts":%d,"error":"cannot unmarshal string into; unexpected end of JSON input; unknown tool"}`, recent),
		fmt.Sprintf(`{"ts":%d,"error":"unknown tool"}`, recent),
		fmt.Sprintf(`{"ts":%d,"error":"unknown tool"}`, old),
		`{"error":"unknown tool"}`,
		`not json unknown tool`,
	}, "\n"), now)
	writeObservatoryFile(t, root, "stale-file.log", fmt.Sprintf(`{"ts":%d,"error":"unknown tool"}`, recent), now.Add(-25*time.Hour))
	if err := os.Mkdir(filepath.Join(root, "directory.log"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := recentFailures(root, now)
	want := []FailureCount{
		{Pattern: "type-coercion drop", Count: 1},
		{Pattern: "empty-args parse fail", Count: 1},
		{Pattern: "unknown tool", Count: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recent failures = %#v, want %#v", got, want)
	}
	if got := recentFailures(filepath.Join(root, "missing"), now); got != nil {
		t.Fatalf("missing failure dir = %#v", got)
	}
}

func TestHitPatternsReturnsEveryMatchInDefinitionOrder(t *testing.T) {
	patterns := []struct{ label, needle string }{
		{"first", "alpha"},
		{"second", "beta"},
		{"third", "alpha beta"},
	}
	if got := hitPatterns("alpha beta", patterns); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("hits = %#v", got)
	}
	if got := hitPatterns("none", patterns); got != nil {
		t.Fatalf("no hits = %#v", got)
	}
}

func TestReadTailCappedMissingUnderOverAndDirectoryErrors(t *testing.T) {
	root := t.TempDir()
	path := writeObservatoryFile(t, root, "data.log", "0123456789", time.Time{})
	if got := string(readTailCapped(path, 4)); got != "6789" {
		t.Fatalf("tail cap = %q", got)
	}
	if got := string(readTailCapped(path, 100)); got != "0123456789" {
		t.Fatalf("tail under cap = %q", got)
	}
	if got := readTailCapped(filepath.Join(root, "missing"), 4); got != nil {
		t.Errorf("missing read = %q", got)
	}
	if got := readTailCapped(root, 4); got != nil {
		t.Errorf("directory read = %q", got)
	}
}

func TestMemoryStatusMalformedLedgerOffsetsAndSpillFilesOnly(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	writeObservatoryFile(t, root, "wiki/.diary-process-state.json", `{broken`, now)
	writeObservatoryFile(t, root, "memory/diary/diary-2026-07-11.md", "today", now)
	writeObservatoryFile(t, root, "spillover/today.txt", "x", now)
	writeObservatoryFile(t, root, "spillover/yesterday.txt", "x", now.Add(-24*time.Hour))
	dir := filepath.Join(root, "spillover", "today-directory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, now, now); err != nil {
		t.Fatal(err)
	}
	m := memoryStatus(root, now)
	if m.LatestDiary != "2026-07-11" || m.PendingBytes != 0 || m.BacklogDays != 0 || m.MemoryMDStamp != "" {
		t.Fatalf("malformed ledger memory = %#v", m)
	}
	if m.SpilloverToday != 1 {
		t.Fatalf("spillover today counts directories/stale files: %#v", m)
	}

	writeObservatoryFile(t, root, "wiki/.diary-process-state.json", `{
  "memoryConsumedThrough":"2026-07-01",
  "files":{"diary-2026-07-11.md":{"offset":999}}
}`, now)
	m = memoryStatus(root, now)
	if m.DreamerConsumedThrough != "2026-07-11" || m.PendingBytes != 0 || m.MemoryMDStamp != "2026-07-01" {
		t.Fatalf("oversized offset handling = %#v", m)
	}
}

func TestModelSummaryDeterministicMalformedAndDownParsing(t *testing.T) {
	root := t.TempDir()
	writeObservatoryFile(t, root, "model-stats.json", `{"windowHours":48,"models":{"zeta":{},"alpha":{},"middle":{}}}`, time.Time{})
	writeObservatoryFile(t, root, "logs/sparkfleet.log", strings.Join([]string{
		"old down=old-one",
		"latest status=degraded down=vllm,embed,,reranker other=x",
	}, "\n"), time.Time{})
	got := modelSummary(root)
	if got.WindowHours != 48 || !reflect.DeepEqual(got.Models, []string{"alpha", "middle", "zeta"}) ||
		!reflect.DeepEqual(got.Down, []string{"vllm", "embed", "reranker"}) {
		t.Fatalf("model summary = %#v", got)
	}

	writeObservatoryFile(t, root, "model-stats.json", `{broken`, time.Time{})
	got = modelSummary(root)
	if got.WindowHours != 0 || got.Models != nil || len(got.Down) != 3 {
		t.Fatalf("malformed model summary = %#v", got)
	}
	if got := fleetDownBackends(filepath.Join(root, "missing")); got != nil {
		t.Fatalf("missing fleet log = %#v", got)
	}
}

func TestNewestDiaryDateAndBacklogBoundaryCases(t *testing.T) {
	root := t.TempDir()
	if name, mt := newestDiary(filepath.Join(root, "missing")); name != "" || !mt.IsZero() {
		t.Fatalf("missing newest diary = %q %s", name, mt)
	}
	dir := filepath.Join(root, "diary")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeObservatoryFile(t, root, "diary/diary-2026-01-02.md", "a", time.Unix(10, 0))
	writeObservatoryFile(t, root, "diary/diary-2026-12-31.md", "b", time.Unix(20, 0))
	writeObservatoryFile(t, root, "diary/diary-2026-99-99.txt", "ignore", time.Unix(30, 0))
	if err := os.Mkdir(filepath.Join(dir, "diary-9999-12-31.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	name, mt := newestDiary(dir)
	if name != "diary-2026-12-31.md" || !mt.Equal(time.Unix(20, 0)) {
		t.Fatalf("newest diary = %q %s", name, mt)
	}

	for input, want := range map[string]string{
		"diary-2026-07-11.md":     "2026-07-11",
		"diary-short.md":          "",
		"2026-07-11":              "2026-07-11",
		"diary-2026-07-11.md.bak": "",
	} {
		if got := diaryDate(input); got != want {
			t.Errorf("diaryDate(%q) = %q, want %q", input, got, want)
		}
	}
	for _, tc := range []struct {
		consumed, latest string
		want             int
	}{
		{"2026-07-01", "2026-07-11", 10},
		{"2026-07-11", "2026-07-11", 0},
		{"2026-07-12", "2026-07-11", 0},
		{"bad", "2026-07-11", 0},
		{"2026-07-01T10:00", "2026-07-03", 2},
	} {
		if got := backlogDays(tc.consumed, tc.latest); got != tc.want {
			t.Errorf("backlogDays(%q,%q) = %d, want %d", tc.consumed, tc.latest, got, tc.want)
		}
	}
	for input, want := range map[string]string{" 2026-07-11T10:00 ": "2026-07-11", "short": "short", "": ""} {
		if got := firstTenDate(input); got != want {
			t.Errorf("firstTenDate(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLastLineContainingTailWindowAndMissingNeedle(t *testing.T) {
	root := t.TempDir()
	content := strings.Repeat("padding without marker\n", 600) +
		"recent down=first\nintermediate\nlatest down=second\n"
	path := writeObservatoryFile(t, root, "large.log", content, time.Time{})
	if got := lastLineContaining(path, "down="); got != "latest down=second" {
		t.Fatalf("last matching line = %q", got)
	}
	if got := lastLineContaining(path, "missing"); got != "" {
		t.Fatalf("missing needle = %q", got)
	}
	if got := lastLineContaining(filepath.Join(root, "missing"), "x"); got != "" {
		t.Fatalf("missing file = %q", got)
	}
}

func TestSnapshotGeneratedAtAndFiveLoopContractOnPartialState(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	writeObservatoryFile(t, root, "regression-baseline.json", `{}`, now)
	r := Snapshot(root, now)
	if !r.GeneratedAt.Equal(now) || len(r.Liveness) != 5 {
		t.Fatalf("partial snapshot = %#v", r)
	}
	names := make([]string, 0, len(r.Liveness))
	for _, l := range r.Liveness {
		names = append(names, l.Name)
	}
	want := []string{"dreamer", "skill-review", "regression-baseline", "model-stats", "diary"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("loop names = %#v, want %#v", names, want)
	}
	for _, l := range r.Liveness {
		if l.Name == "regression-baseline" {
			if !l.Fresh || l.Missing {
				t.Errorf("present loop = %#v", l)
			}
		} else if !l.Missing {
			t.Errorf("absent loop not missing: %#v", l)
		}
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatalf("report JSON marshal: %v", err)
	}
}
