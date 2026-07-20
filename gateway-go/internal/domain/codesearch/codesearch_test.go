package codesearch

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type symmetricCodeSearchEmbedder struct {
	calls []string
}

func (e *symmetricCodeSearchEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls = append(e.calls, "plain")
	return codeSearchTestVectors(texts), nil
}

type roleAwareCodeSearchEmbedder struct {
	calls []string
}

func (e *roleAwareCodeSearchEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls = append(e.calls, "passage")
	return codeSearchTestVectors(texts), nil
}

func (e *roleAwareCodeSearchEmbedder) EmbedKind(_ context.Context, kind string, texts []string) ([][]float32, error) {
	e.calls = append(e.calls, kind)
	return codeSearchTestVectors(texts), nil
}

func codeSearchTestVectors(texts []string) [][]float32 {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0}
	}
	return out
}

func TestFTSQueryQuotesTerms(t *testing.T) {
	got := ftsQuery(`retry "backoff" 로직`)
	want := `"retry" OR "backoff" OR "로직"`
	if got != want {
		t.Fatalf("ftsQuery = %q, want %q", got, want)
	}
}

func TestQueryJSONInitializesWALReadPathButRejectsWrites(t *testing.T) {
	db := filepath.Join(t.TempDir(), "codegraph.db")
	if out, err := exec.Command("sqlite3", db,
		"PRAGMA journal_mode=WAL; CREATE TABLE nodes(id TEXT); INSERT INTO nodes VALUES ('one');").CombinedOutput(); err != nil {
		t.Fatalf("create sqlite fixture: %v: %s", err, out)
	}
	// Reproduce the post-sync state: the DB exists but no connection-owned SHM
	// file is available yet. queryJSON must still open it successfully.
	_ = os.Remove(db + "-shm")
	_ = os.Remove(db + "-wal")
	type countRow struct {
		N int `json:"n"`
	}
	rows, err := queryJSON[countRow](context.Background(), db, "SELECT count(*) AS n FROM nodes")
	if err != nil || len(rows) != 1 || rows[0].N != 1 {
		t.Fatalf("queryJSON rows=%+v err=%v", rows, err)
	}
	if _, err := queryJSON[countRow](context.Background(), db, "INSERT INTO nodes VALUES ('two')"); err == nil {
		t.Fatal("query_only connection accepted a write")
	}
	rows, err = queryJSON[countRow](context.Background(), db, "SELECT count(*) AS n FROM nodes")
	if err != nil || rows[0].N != 1 {
		t.Fatalf("write guard changed database: rows=%+v err=%v", rows, err)
	}
}

func TestCosine(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	c := []float32{0, 1, 0}
	if v := cosine(a, b); math.Abs(v-1) > 1e-6 {
		t.Fatalf("identical vectors cosine = %v, want 1", v)
	}
	if v := cosine(a, c); math.Abs(v) > 1e-6 {
		t.Fatalf("orthogonal vectors cosine = %v, want 0", v)
	}
	if v := cosine(a, []float32{0, 0}); v != 0 {
		t.Fatalf("mismatched dims cosine = %v, want 0", v)
	}
}

func TestExpandQueryBridgesKorean(t *testing.T) {
	got := expandQuery("음성 전사 화자분리")
	for _, want := range []string{"transcribe", "diarize", "speech"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expandQuery(%q) = %q, missing %q", "음성 전사 화자분리", got, want)
		}
	}
	if got := expandQuery("plain english query"); got != "plain english query" {
		t.Fatalf("english query mutated: %q", got)
	}
	autoRecall := expandQuery("관련된 문서를 매 턴 자동으로 찾아 프롬프트에 넣는 코드")
	for _, want := range []string{"auto_recall", "preflight", "evidence", "context"} {
		if !strings.Contains(autoRecall, want) {
			t.Fatalf("auto-recall expansion = %q, missing %q", autoRecall, want)
		}
	}
}

func TestKindHint(t *testing.T) {
	cases := map[string]string{
		"세션 상태 구조체":         "struct",
		"메일 파서 함수":          "function",
		"ChatViewModel 메서드": "method",
		"승인 반려 클래스":         "class",
		"배포 스크립트":           "file",
		"저장소 인터페이스":         "interface",
		"재시도 백오프":           "",
	}
	for q, want := range cases {
		if got := kindHint(q); got != want {
			t.Fatalf("kindHint(%q) = %q, want %q", q, got, want)
		}
	}
}

func TestRetrievalPriorKeepsDocsAncillaryForCodeIntent(t *testing.T) {
	markdown := Entry{Language: "markdown", File: "docs/guide.md"}
	if got := retrievalPrior("자동 recall 구현 코드", markdown); got >= 1 {
		t.Fatalf("code-intent markdown prior = %v, want penalty", got)
	}
	if got := retrievalPrior("CLAUDE 문서 파일", markdown); got <= 1 {
		t.Fatalf("explicit-doc prior = %v, want boost", got)
	}
	code := Entry{Language: "go", File: "recall.go"}
	if got := retrievalPrior("문서 주입 코드", code); got != 1 {
		t.Fatalf("source prior = %v, want 1", got)
	}
}

func TestLexicalTokensSplitIdentifiersWithoutFragmentingQuery(t *testing.T) {
	tokens := lexicalTokens("buildTailAdditions run_tail HTTPServer")
	for _, want := range []string{"buildtailadditions", "build", "tail", "additions", "run", "httpserver", "http", "server"} {
		if !contains(tokens, want) {
			t.Fatalf("lexicalTokens = %v, missing %q", tokens, want)
		}
	}
}

func TestLexicalRanksCoversRepositoryFileChunks(t *testing.T) {
	entries := []Entry{
		{ID: "symbol", File: "gateway-go/retry.go", Qualified: "retryProvider", Lexical: "provider retry exponential backoff"},
		{ID: "deploy", Kind: "file_chunk", File: "scripts/deploy/deploy.sh", Qualified: "scripts/deploy/deploy.sh", Lexical: "refresh semantic code index before restart codesearch index"},
	}
	ranks := lexicalRanks(entries, "semantic code index refresh", 5)
	if len(ranks) == 0 || entries[ranks[0].idx].ID != "deploy" {
		t.Fatalf("lexical ranks = %+v, want deploy first", ranks)
	}
}

func TestRepositoryChunksAddsUnparsedTrackedText(t *testing.T) {
	repo := t.TempDir()
	mustWrite := func(rel, body string) {
		t.Helper()
		full := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("scripts/deploy.sh", "#!/bin/bash\nrefresh_semantic_index() {\n  echo refresh\n}\n")
	mustWrite("docs/guide.md", "# Retrieval\n\nInject applicable documentation sections.\n")
	mustWrite("pkg/known.go", "package pkg\nfunc Known() {}\n")
	mustWrite("pkg/known_test.go", "package pkg\nfunc TestKnown() {}\n")
	mustWrite("secrets/private.key", "definitely not indexed")

	chunks := repositoryChunks(
		repo,
		[]string{"scripts/deploy.sh", "docs/guide.md", "pkg/known.go", "pkg/known_test.go", "secrets/private.key"},
		map[string]bool{"pkg/known.go": true},
	)
	files := make(map[string]bool)
	for _, chunk := range chunks {
		files[chunk.File] = true
		if chunk.Kind != "file_chunk" || chunk.Text == "" || chunk.Lexical == "" || chunk.UpdatedAt == 0 {
			t.Fatalf("incomplete repository chunk: %+v", chunk)
		}
	}
	if !files["scripts/deploy.sh"] || !files["docs/guide.md"] {
		t.Fatalf("repository chunk files = %v, want shell+markdown", files)
	}
	for _, excluded := range []string{"pkg/known.go", "pkg/known_test.go", "secrets/private.key"} {
		if files[excluded] {
			t.Fatalf("excluded file %q was indexed", excluded)
		}
	}
}

func TestBuildContextPackIncludesSourceAndRelevantRules(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "gateway"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := "package gateway\n\nfunc injectTail() {\n\t// append recall to the last user message\n}\n"
	if err := os.WriteFile(filepath.Join(repo, "gateway", "tail.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	rules := "# Gateway rules\n\n## Prompt cache\nInject dynamic recall into the last user-message tail.\n\n## Unrelated release\nPublish mobile artifacts.\n"
	if err := os.WriteFile(filepath.Join(repo, "gateway", "CLAUDE.md"), []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := []Hit{{Entry: Entry{
		ID: "injectTail", Kind: "function", Language: "go", Qualified: "injectTail",
		File: "gateway/tail.go", StartLine: 3, EndLine: 5,
	}, Cosine: 0.72}}

	got := BuildContextPack(context.Background(), repo, filepath.Join(repo, ".codegraph"), "prompt cache recall tail injection", hits)
	for _, want := range []string{"3 | func injectTail", "gateway/CLAUDE.md", "## Prompt cache", "last user-message tail"} {
		if !strings.Contains(got, want) {
			t.Fatalf("context pack missing %q:\n%s", want, got)
		}
	}
}

func TestRelatedForHitsKeepsSafeEdgesAndDropsCrossFileCallNoise(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "codegraph.db")
	sql := `
CREATE TABLE nodes(id TEXT PRIMARY KEY, qualified_name TEXT, file_path TEXT);
CREATE TABLE edges(source TEXT, target TEXT, kind TEXT);
INSERT INTO nodes VALUES
 ('hit','pkg.Run','pkg/run.go'),
 ('same','pkg.helper','pkg/run.go'),
 ('remote','other.helper','other/helper.go'),
 ('iface','pkg.Runner','pkg/runner.go');
INSERT INTO edges VALUES
 ('hit','same','calls'),
 ('hit','remote','calls'),
 ('hit','iface','implements');`
	if out, err := exec.Command("sqlite3", db, sql).CombinedOutput(); err != nil {
		t.Fatalf("sqlite fixture: %v: %s", err, out)
	}
	related := relatedForHits(context.Background(), dir, []Hit{{Entry: Entry{ID: "hit"}}})["hit"]
	joined := ""
	for _, relation := range related {
		joined += relation.Kind + ":" + relation.Qualified + "\n"
	}
	if !strings.Contains(joined, "calls:pkg.helper") || !strings.Contains(joined, "implements:pkg.Runner") {
		t.Fatalf("safe relations missing:\n%s", joined)
	}
	if strings.Contains(joined, "other.helper") {
		t.Fatalf("cross-file ambiguous call leaked into context:\n%s", joined)
	}
}

func TestApplicableRepositoryDocsRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	reads := 0
	docs := ApplicableRepositoryDocs(root, "query", []string{"pkg/../../etc/passwd.go"}, func(string) ([]byte, error) {
		reads++
		return []byte("SECRET"), nil
	}, 3, 1000)
	if len(docs) != 0 || reads != 0 {
		t.Fatalf("traversal docs=%+v reads=%d, want none", docs, reads)
	}
}

func TestRelevantMarkdownSectionsKeepsSafetyAndDropsUnrelatedSection(t *testing.T) {
	doc := "# Rules\n\n## 안전 (항상 적용)\nNever edit generated files.\n\n## Prompt cache\nPut dynamic recall at the message tail.\n\n## Mobile release\nPublish the APK.\n"
	got, score := relevantMarkdownSections(doc, "prompt cache recall tail", 2000)
	if score == 0 || !strings.Contains(got, "## 안전") || !strings.Contains(got, "## Prompt cache") {
		t.Fatalf("projected rules missing safety or relevant section (score=%d):\n%s", score, got)
	}
	if strings.Contains(got, "Mobile release") {
		t.Fatalf("unrelated section leaked into projection:\n%s", got)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type fakeReranker struct{ scores []float64 }

func (f fakeReranker) Rerank(_ context.Context, _ string, docs []string) ([]float64, error) {
	return f.scores[:len(docs)], nil
}
func (f fakeReranker) Identity() string { return "fake" }

func TestRerankHitsBlendsWithRetrievalOrder(t *testing.T) {
	hits := []Hit{
		{Entry: Entry{ID: "a", Qualified: "A"}},
		{Entry: Entry{ID: "b", Qualified: "B"}},
		{Entry: Entry{ID: "c", Qualified: "C"}},
	}
	out := rerankHits(context.Background(), t.TempDir(), fakeReranker{scores: []float64{0.1, 0.9, 0.5}}, "q", hits)
	if out[0].ID != "b" || out[1].ID != "a" || out[2].ID != "c" {
		t.Fatalf("rerank blend = %s,%s,%s want b,a,c", out[0].ID, out[1].ID, out[2].ID)
	}
	// nil reranker → unchanged
	same := rerankHits(context.Background(), t.TempDir(), nil, "q", hits)
	if same[0].ID != "a" {
		t.Fatalf("nil reranker mutated order")
	}
}

func TestRerankHitsFallsBackWhenAllScoresAreNegative(t *testing.T) {
	hits := []Hit{{Entry: Entry{ID: "dense-a"}}, {Entry: Entry{ID: "dense-b"}}}
	out := rerankHits(context.Background(), t.TempDir(), fakeReranker{scores: []float64{-4, -1}}, "한국어 코드 질의", hits)
	if out[0].ID != "dense-a" || out[1].ID != "dense-b" {
		t.Fatalf("negative reranker displaced retrieval: %s,%s", out[0].ID, out[1].ID)
	}
	if out[0].RerankScore != -4 || out[1].RerankScore != -1 {
		t.Fatalf("diagnostic scores not retained: %+v", out)
	}
}

func TestSearchUsesQueryRoleWithSymmetricFallback(t *testing.T) {
	dir := t.TempDir()
	meta := Meta{
		Model: "test", Dim: 2,
		Entries: []Entry{{ID: "one", Kind: "function", Qualified: "mail.Search", File: "mail.go", StartLine: 1, EndLine: 2}},
	}
	if err := saveIndex(dir, meta, [][]float32{{1, 0}}); err != nil {
		t.Fatalf("save index: %v", err)
	}

	t.Run("role aware", func(t *testing.T) {
		embedder := &roleAwareCodeSearchEmbedder{}
		hits, err := Search(context.Background(), dir, embedder, "mail lookup", 1)
		if err != nil || len(hits) != 1 || hits[0].ID != "one" {
			t.Fatalf("Search hits = %+v, err %v", hits, err)
		}
		if want := []string{"query"}; !reflect.DeepEqual(embedder.calls, want) {
			t.Fatalf("embedding roles = %v, want %v", embedder.calls, want)
		}
	})

	t.Run("symmetric", func(t *testing.T) {
		embedder := &symmetricCodeSearchEmbedder{}
		hits, err := Search(context.Background(), dir, embedder, "mail lookup", 1)
		if err != nil || len(hits) != 1 || hits[0].ID != "one" {
			t.Fatalf("Search hits = %+v, err %v", hits, err)
		}
		if want := []string{"plain"}; !reflect.DeepEqual(embedder.calls, want) {
			t.Fatalf("embedding fallback = %v, want %v", embedder.calls, want)
		}
	})
}

func TestIndexSnapshotCachesAndInvalidatesOnSave(t *testing.T) {
	dir := t.TempDir()
	meta := Meta{Model: "test", Dim: 2, Entries: []Entry{{ID: "one", Lexical: "mail search"}}}
	if err := saveIndex(dir, meta, [][]float32{{1, 0}}); err != nil {
		t.Fatal(err)
	}
	first, err := loadIndexSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadIndexSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("unchanged sidecar should reuse the in-memory snapshot")
	}
	if err := saveIndex(dir, meta, [][]float32{{0, 1}}); err != nil {
		t.Fatal(err)
	}
	third, err := loadIndexSnapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if third == first || third.vecs[0][0] != 0 || third.vecs[0][1] != 1 {
		t.Fatalf("save did not invalidate snapshot: %+v", third.vecs[0])
	}
}

func TestRemovedEntryCountDoesNotCallChangedEntryRemoved(t *testing.T) {
	previous := []Entry{{ID: "changed", UpdatedAt: 1}, {ID: "deleted", UpdatedAt: 1}}
	current := []Entry{{ID: "changed", UpdatedAt: 2}, {ID: "new", UpdatedAt: 1}}
	if got := removedEntryCount(previous, current); got != 1 {
		t.Fatalf("removedEntryCount = %d, want only the deleted ID", got)
	}
}
