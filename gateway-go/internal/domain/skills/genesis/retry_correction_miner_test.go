package genesis

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeRetryTranscript writes one session transcript with a tool_use/tool_result
// exchange sequence. Each step is (name, argsJSON, resultErr — "" for success).
func writeRetryTranscript(t *testing.T, dir, session string, steps [][3]string) {
	t.Helper()
	var lines string
	for i, s := range steps {
		id := fmt.Sprintf("tool-%s-%d", session, i)
		lines += fmt.Sprintf(`{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":%q,"input":%s}],"timestamp":%d}`+"\n",
			id, s[0], s[1], 1_700_000_000_000+int64(i)*1000)
		if s[2] == "" {
			lines += fmt.Sprintf(`{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":"ok"}],"timestamp":%d}`+"\n",
				id, 1_700_000_000_000+int64(i)*1000+500)
		} else {
			lines += fmt.Sprintf(`{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"is_error":true,"content":%q}],"timestamp":%d}`+"\n",
				id, s[2], 1_700_000_000_000+int64(i)*1000+500)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "transcripts", session+".jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
}

func newRetryTestMiner(t *testing.T) (*RetryCorrectionMiner, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "transcripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	return NewRetryCorrectionMiner(dir, slog.Default()), dir
}

// minerNow is far after the fixture timestamps but within the evidence TTL.
var minerNow = time.UnixMilli(1_700_000_100_000)

func TestRetryMinerExtractsFieldLevelCorrection(t *testing.T) {
	m, dir := newRetryTestMiner(t)
	writeRetryTranscript(t, dir, "client:main", [][3]string{
		{"gmail", `{"action":"read","id":"abc"}`, "unknown action for message id 12345"},
		{"gmail", `{"action":"thread","id":"abc"}`, ""},
	})

	clusters := m.MineAndClusters(5, minerNow)
	if len(clusters) != 1 {
		t.Fatalf("clusters = %d, want 1", len(clusters))
	}
	c := clusters[0]
	if c.Kind != "tool_retry" || c.Skill != "gmail" || c.Support != 1 {
		t.Errorf("cluster = %+v", c)
	}
	if want := "gmail|action|unknown action for message id #"; c.Signature != want {
		t.Errorf("signature = %q, want %q", c.Signature, want)
	}
	if c.Example == "" || c.LastAt == 0 {
		t.Errorf("example/lastAt missing: %+v", c)
	}
}

func TestRetryMinerExcludesTransientAndUnrelated(t *testing.T) {
	m, dir := newRetryTestMiner(t)
	writeRetryTranscript(t, dir, "client:main", [][3]string{
		// Identical args retried → transient flake, not a correction.
		{"web", `{"url":"https://a.example/x"}`, "HTTP 503"},
		{"web", `{"url":"https://a.example/x"}`, ""},
		// Different business entirely (no shared field, dissimilar args).
		{"read", `{"path":"/tmp/one.txt"}`, "no such file"},
		{"read", `{"path":"/completely/other/place.md"}`, ""},
	})
	if clusters := m.MineAndClusters(5, minerNow); len(clusters) != 0 {
		t.Fatalf("expected no clusters, got %+v", clusters)
	}
}

func TestRetryMinerFallsBackToTextSimilarityForSingleFieldTools(t *testing.T) {
	m, dir := newRetryTestMiner(t)
	writeRetryTranscript(t, dir, "client:main", [][3]string{
		{"web", `{"url":"https://a.example/page?id=1"}`, "blocked by challenge"},
		{"web", `{"url":"https://a.example/page?id=1&amp=1"}`, ""},
	})
	clusters := m.MineAndClusters(5, minerNow)
	if len(clusters) != 1 || clusters[0].Skill != "web" {
		t.Fatalf("clusters = %+v", clusters)
	}
	if len(clusters) == 1 && clusters[0].Signature != "web|args|blocked by challenge" {
		t.Errorf("signature = %q", clusters[0].Signature)
	}
}

func TestRetryMinerDedupesAcrossRescansAndPersists(t *testing.T) {
	m, dir := newRetryTestMiner(t)
	writeRetryTranscript(t, dir, "client:main", [][3]string{
		{"gmail", `{"action":"read","id":"abc"}`, "bad action"},
		{"gmail", `{"action":"thread","id":"abc"}`, ""},
	})
	first := m.MineAndClusters(5, minerNow)
	// Touch the file so the incremental scan re-reads it.
	path := filepath.Join(dir, "transcripts", "client:main.jsonl")
	future := minerNow.Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	second := m.MineAndClusters(5, minerNow.Add(2*time.Hour))
	if len(first) != 1 || len(second) != 1 || second[0].Support != 1 {
		t.Fatalf("rescan changed support: first=%+v second=%+v", first, second)
	}

	// A fresh instance reads the ledger without needing a rescan.
	reloaded := NewRetryCorrectionMiner(dir, slog.Default())
	if clusters := reloaded.MineAndClusters(5, minerNow.Add(3*time.Hour)); len(clusters) != 1 || clusters[0].Support != 1 {
		t.Fatalf("reloaded clusters = %+v", clusters)
	}
}

func TestRetryMinerAccumulatesSupportAcrossSessions(t *testing.T) {
	m, dir := newRetryTestMiner(t)
	for i := 0; i < 3; i++ {
		writeRetryTranscript(t, dir, fmt.Sprintf("client:s%d", i), [][3]string{
			{"gmail", `{"action":"read","id":"x"}`, "bad action"},
			{"gmail", `{"action":"thread","id":"x"}`, ""},
		})
	}
	clusters := m.MineAndClusters(5, minerNow)
	if len(clusters) != 1 || clusters[0].Support != 3 {
		t.Fatalf("clusters = %+v", clusters)
	}
}

func TestRetryMinerSkipsTestSessionsAndWindow(t *testing.T) {
	m, dir := newRetryTestMiner(t)
	// Excluded session name.
	writeRetryTranscript(t, dir, "client:lt-999", [][3]string{
		{"gmail", `{"action":"read","id":"x"}`, "bad action"},
		{"gmail", `{"action":"thread","id":"x"}`, ""},
	})
	// Success too far beyond the pairing window.
	steps := [][3]string{{"gmail", `{"action":"read","id":"y"}`, "bad action"}}
	for i := 0; i < retryPairWindow; i++ {
		steps = append(steps, [3]string{"wiki", `{"q":"filler"}`, ""})
	}
	steps = append(steps, [3]string{"gmail", `{"action":"thread","id":"y"}`, ""})
	writeRetryTranscript(t, dir, "client:main", steps)

	if clusters := m.MineAndClusters(5, minerNow); len(clusters) != 0 {
		t.Fatalf("expected no clusters, got %+v", clusters)
	}
}

func TestRetryMinerInertWithoutStateDir(t *testing.T) {
	m := NewRetryCorrectionMiner("", slog.Default())
	if got := m.MineAndClusters(5, minerNow); got != nil {
		t.Fatalf("inert miner returned %+v", got)
	}
	var nilMiner *RetryCorrectionMiner
	if got := nilMiner.MineAndClusters(5, minerNow); got != nil {
		t.Fatalf("nil miner returned %+v", got)
	}
}

func TestMergeFailureClustersRanksAndCaps(t *testing.T) {
	a := []FailureClusterSummary{{Signature: "a", Support: 2, LastAt: 10}}
	b := []FailureClusterSummary{
		{Signature: "b", Support: 5, LastAt: 5},
		{Signature: "c", Support: 2, LastAt: 20},
	}
	out := MergeFailureClusters(a, b, 2)
	if len(out) != 2 || out[0].Signature != "b" || out[1].Signature != "c" {
		t.Fatalf("merged = %+v", out)
	}
}
