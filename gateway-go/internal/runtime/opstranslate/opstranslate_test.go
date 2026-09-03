package opstranslate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func useTempCache(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cache.json")
	store.mu.Lock()
	store.pathOverride = path
	store.loaded = false
	store.entries = nil
	store.order = nil
	store.dirty = false
	store.mu.Unlock()
	t.Cleanup(func() {
		store.mu.Lock()
		store.pathOverride = ""
		store.loaded = false
		store.entries = nil
		store.order = nil
		store.dirty = false
		store.mu.Unlock()
	})
	return path
}

func TestTranslatableAcceptsProseAndRefusesMachineText(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want bool
	}{
		{"english prose", "Guardrail working as designed — broad rewrite correctly rejected", true},
		{"short english sentence", "Promote rejected evolve into held-out validation", true},
		// Already Korean: translating it spends quota and can only lose fidelity.
		{"korean", "가드레일이 설계대로 작동했고 광범위한 재작성은 올바르게 거부됐다", false},
		// Korean prose naming English identifiers still reads as Korean.
		{"korean around identifiers", "이 후보는 `mail_archive` 도구의 지연을 다룬다", false},
		{"json blob", `{"candidate":"email-analysis-full","score":5.9}`, false},
		{"json array", `["a","b"]`, false},
		{"session key", "cron:morning-letter:1780959600105", false},
		{"single identifier", "self_improvement_coding", false},
		{"file path", "/home/choiceoh/.deneb/skills/email-analysis/SKILL.md", false},
		{"empty", "   ", false},
		{"numbers and arrows", "113 → 750 (2.0) == 2500", false},
		{"too big", strings.Repeat("word ", maxFieldBytes), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := translatable(tc.in); got != tc.want {
				t.Fatalf("translatable(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// Fields must return a slice the same length and order as its input, with every
// untranslated position holding its ORIGINAL — never empty, never a neighbour's
// text. A misalignment here puts one candidate's reason on another's title.
func TestFieldsPreservesLengthAndOrderWithoutATranslator(t *testing.T) {
	useTempCache(t)
	t.Setenv("DEEPL_API_KEY", "")

	in := []string{"Promote rejected evolve", "", "이미 한글", `{"a":1}`, "Do not auto-apply the rejected body"}
	out := Fields(t.Context(), in)
	if len(out) != len(in) {
		t.Fatalf("len=%d want %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Fatalf("index %d changed with no translator: %q → %q", i, in[i], out[i])
		}
	}
}

func TestFieldsServesRepeatedStringsFromOneCacheEntry(t *testing.T) {
	useTempCache(t)
	cachePut("Promote rejected evolve", "거절된 진화를 승격")

	in := []string{"Promote rejected evolve", "untouched", "Promote rejected evolve"}
	out := Fields(t.Context(), in)
	if out[0] != "거절된 진화를 승격" || out[2] != "거절된 진화를 승격" {
		t.Fatalf("both occurrences should resolve from cache: %q", out)
	}
	if out[1] != "untouched" {
		t.Fatalf("unrelated entry changed: %q", out[1])
	}
}

// The cache is what makes serving affordable across the several redeploys a day
// this gateway takes; an in-memory-only cache re-translates the whole backlog
// after each one.
func TestCacheSurvivesAProcessRestart(t *testing.T) {
	path := useTempCache(t)
	cachePut("Do not auto-apply the rejected body", "거절된 본문을 자동 적용하지 말 것")
	Flush()

	// A fresh process: same file, empty memory.
	store.mu.Lock()
	store.loaded = false
	store.entries = nil
	store.order = nil
	store.mu.Unlock()

	got, ok := cacheGet("Do not auto-apply the rejected body")
	if !ok || got != "거절된 본문을 자동 적용하지 말 것" {
		t.Fatalf("cache did not survive restart: %q ok=%v (file %s)", got, ok, path)
	}
}

// Only the Korean rendering is stored. The English original — which for the
// browser path is page text the operator read — must never land on disk here.
func TestCacheFileStoresHashesNotSourceText(t *testing.T) {
	path := useTempCache(t)
	cachePut("a distinctive source sentence about guardrails", "가드레일에 관한 독특한 원문")
	Flush()

	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "distinctive source sentence") {
		t.Fatal("the source text was written to the cache file")
	}
	var f cacheFile
	if err := json.Unmarshal(blob, &f); err != nil {
		t.Fatalf("cache file is not valid json: %v", err)
	}
	if f.Version != cacheFormat || len(f.Entries) != 1 {
		t.Fatalf("cache file = %+v", f)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != cacheFileMode {
		t.Fatalf("cache file mode = %v, want %v", perm, os.FileMode(cacheFileMode))
	}
}

func TestCorruptCacheFileReadsAsEmptyInsteadOfFailing(t *testing.T) {
	path := useTempCache(t)
	if err := os.WriteFile(path, []byte("{not json"), cacheFileMode); err != nil {
		t.Fatal(err)
	}
	if _, ok := cacheGet("anything"); ok {
		t.Fatal("a corrupt cache file must read as empty, not as a hit")
	}
	// And it must still accept new writes rather than staying wedged.
	cachePut("hello there friend", "안녕")
	if got, ok := cacheGet("hello there friend"); !ok || got != "안녕" {
		t.Fatalf("cache unusable after a corrupt file: %q ok=%v", got, ok)
	}
}
