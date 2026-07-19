package notebook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestNewStoreDirectoryBoundary(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{"", " ", "\t\n"} {
		store, err := NewStore(dir)
		if store != nil || err == nil || !strings.Contains(err.Error(), "empty dir") {
			t.Errorf("NewStore(%q) = (%#v,%v), want empty-dir error", dir, store, err)
		}
	}
	root := t.TempDir()
	nested := filepath.Join(root, "private", "notebooks")
	store, err := NewStore("  " + nested + "  ")
	if err != nil {
		t.Fatalf("NewStore nested: %v", err)
	}
	if store.dir != nested {
		t.Fatalf("store dir = %q, want trimmed %q", store.dir, nested)
	}
	info, err := os.Stat(nested)
	if err != nil || !info.IsDir() {
		t.Fatalf("created directory = %#v err=%v", info, err)
	}
}

func TestNewStoreRejectsFileAsDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if store != nil || err == nil || !strings.Contains(err.Error(), "mkdir") {
		t.Fatalf("NewStore(file) = (%#v,%v)", store, err)
	}
}

func TestSlugifyBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "notebook"},
		{name: "spaces", in: "   ", want: "notebook"},
		{name: "punctuation only", in: "!@#$%^&*()", want: "notebook"},
		{name: "ascii lower", in: "Simple Name", want: "simple-name"},
		{name: "Korean", in: "태양광 프로젝트", want: "태양광-프로젝트"},
		{name: "separators", in: `a / b\c_d-e`, want: "a-b-c-d-e"},
		{name: "repeated separators", in: "a___///---   b", want: "a-b"},
		{name: "outer separators", in: "--- name ---", want: "name"},
		{name: "digits", in: "Deal 2026-07", want: "deal-2026-07"},
		{name: "emoji removed", in: "launch 🚀 plan", want: "launch-plan"},
		{name: "letters from scripts", in: "日本語 Español", want: "日本語-español"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := slugify(tc.in)
			if got != tc.want {
				t.Fatalf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, "--") || strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
				t.Fatalf("invalid slug shape %q", got)
			}
		})
	}
}

func TestSlugifyFortyRuneBoundaryPreservesUTF8(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
	}{
		{name: "ascii", in: strings.Repeat("a", 100)},
		{name: "Korean", in: strings.Repeat("한", 100)},
		{name: "mixed", in: strings.Repeat("한a", 100)},
		{name: "trailing separator at cutoff", in: strings.Repeat("a", 39) + "-" + strings.Repeat("b", 20)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := slugify(tc.in)
			if !utf8.ValidString(got) {
				t.Fatalf("slug is invalid UTF-8: %x", got)
			}
			if count := len([]rune(got)); count > 40 {
				t.Fatalf("slug rune count = %d, want <=40", count)
			}
			if strings.HasSuffix(got, "-") {
				t.Fatalf("slug ends in separator: %q", got)
			}
		})
	}
}

func TestCollapseDashesBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":             "",
		"-":            "-",
		"--":           "-",
		"---":          "-",
		"----":         "-",
		"a--b":         "a-b",
		"a---b---c":    "a-b-c",
		"--a--b--":     "-a-b-",
		"a-b-c":        "a-b-c",
		"no dash here": "no dash here",
	}
	for in, want := range tests {
		if got := collapseDashes(in); got != want {
			t.Errorf("collapseDashes(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseCiteBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want int
		ok   bool
	}{
		{in: "", want: 0, ok: false},
		{in: "S", want: 0, ok: false},
		{in: "S0", want: 0, ok: false},
		{in: "S-1", want: 0, ok: false},
		{in: "S1", want: 1, ok: true},
		{in: "S2", want: 2, ok: true},
		{in: "S0003", want: 3, ok: true},
		{in: "S999999", want: 999999, ok: true},
		{in: "s1", want: 0, ok: false},
		{in: " S1", want: 0, ok: false},
		{in: "S1 ", want: 0, ok: false},
		{in: "SS1", want: 0, ok: false},
		{in: "S1x", want: 0, ok: false},
		{in: "T1", want: 0, ok: false},
	}
	for _, tc := range tests {
		got, ok := parseCite(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseCite(%q) = (%d,%v), want (%d,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNextCiteIgnoresMalformedAndUsesMaximum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sources []Source
		want    string
	}{
		{name: "nil", sources: nil, want: "S1"},
		{name: "empty", sources: []Source{}, want: "S1"},
		{name: "one", sources: []Source{{Cite: "S1"}}, want: "S2"},
		{name: "gap", sources: []Source{{Cite: "S1"}, {Cite: "S3"}}, want: "S4"},
		{name: "out of order", sources: []Source{{Cite: "S9"}, {Cite: "S2"}, {Cite: "S5"}}, want: "S10"},
		{name: "duplicates", sources: []Source{{Cite: "S4"}, {Cite: "S4"}}, want: "S5"},
		{name: "malformed only", sources: []Source{{Cite: ""}, {Cite: "s9"}, {Cite: "S0"}, {Cite: "Sx"}}, want: "S1"},
		{name: "malformed and valid", sources: []Source{{Cite: "Sx"}, {Cite: "S7"}, {Cite: "S-1"}}, want: "S8"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := nextCite(tc.sources); got != tc.want {
				t.Fatalf("nextCite(%v) = %q, want %q", tc.sources, got, tc.want)
			}
		})
	}
}

func TestValidateSourceKindBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  Source
		ok   bool
		part string
	}{
		{name: "wiki ref", src: Source{Kind: KindWiki, Ref: "wiki/page.md"}, ok: true},
		{name: "wiki trims ref", src: Source{Kind: KindWiki, Ref: "  wiki/page.md  "}, ok: true},
		{name: "wiki empty", src: Source{Kind: KindWiki}, part: "requires ref"},
		{name: "wiki whitespace", src: Source{Kind: KindWiki, Ref: " \t "}, part: "requires ref"},
		{name: "note text", src: Source{Kind: KindNote, Text: "note"}, ok: true},
		{name: "file text", src: Source{Kind: KindFile, Text: "file"}, ok: true},
		{name: "url text", src: Source{Kind: KindURL, Text: "url snapshot"}, ok: true},
		{name: "mail text", src: Source{Kind: KindMail, Text: "mail snapshot"}, ok: true},
		{name: "diary text", src: Source{Kind: KindDiary, Text: "diary snapshot"}, ok: true},
		{name: "note empty", src: Source{Kind: KindNote}, part: "requires ingested text"},
		{name: "file whitespace", src: Source{Kind: KindFile, Text: " \n "}, part: "requires ingested text"},
		{name: "url ref without text", src: Source{Kind: KindURL, Ref: "https://example.com"}, part: "requires ingested text"},
		{name: "unknown", src: Source{Kind: "unknown", Text: "body"}, part: "unsupported source kind"},
		{name: "empty kind", src: Source{Kind: "", Text: "body"}, part: "unsupported source kind"},
		{name: "case sensitive kind", src: Source{Kind: "NOTE", Text: "body"}, part: "unsupported source kind"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateSource(tc.src)
			if tc.ok && err != nil {
				t.Fatalf("validateSource: %v", err)
			}
			if !tc.ok && (err == nil || !strings.Contains(err.Error(), tc.part)) {
				t.Fatalf("error = %v, want containing %q", err, tc.part)
			}
		})
	}
}

func TestCreateTrimsFieldsAndGeneratesUniqueSuffixes(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	var created []*Notebook
	for i := 0; i < 12; i++ {
		nb, err := store.Create("  Same Name  ", "  description  ")
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		created = append(created, nb)
		wantID := "same-name"
		if i > 0 {
			wantID += fmt.Sprintf("-%d", i+1)
		}
		if nb.ID != wantID || nb.Name != "Same Name" || nb.Description != "description" {
			t.Errorf("created %d = %#v, want ID %q", i, nb, wantID)
		}
		if nb.Created != nb.Updated || nb.Created <= 0 {
			t.Errorf("timestamps = %d/%d", nb.Created, nb.Updated)
		}
	}
	list := store.List()
	if len(list) != len(created) {
		t.Fatalf("List = %d, want %d", len(list), len(created))
	}
}

func TestStampLockedRemainsMonotonicWhenDiskClockIsAhead(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	store.lastStamp = time.Now().Add(24 * time.Hour).UnixMilli()
	previous := store.lastStamp
	for i := 0; i < 1000; i++ {
		got := store.stampLocked()
		if got != previous+1 {
			t.Fatalf("stamp %d = %d, want %d", i, got, previous+1)
		}
		previous = got
	}
}

func TestGetListAndDealLookupReturnDeepCopies(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	nb, err := store.EnsureForDeal("deal/ref", "deal", "description")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddSource(nb.ID, Source{Kind: KindNote, Text: "original"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StampProjectRefs("deal/ref", []string{"project/a", "project/b"}); err != nil {
		t.Fatal(err)
	}
	copies := []*Notebook{}
	if got, ok := store.Get(nb.ID); ok {
		copies = append(copies, got)
	}
	if got, ok := store.GetByDealRef("deal/ref"); ok {
		copies = append(copies, got)
	}
	copies = append(copies, store.List()[0])
	for i, got := range copies {
		got.Name = "changed"
		got.Sources[0].Text = "changed"
		got.ProjectRefs[0] = "changed"
		fresh, _ := store.Get(nb.ID)
		if fresh.Name != "deal" || fresh.Sources[0].Text != "original" || fresh.ProjectRefs[0] != "project/a" {
			t.Fatalf("copy %d mutated store: %#v", i, fresh)
		}
	}
}

func TestSetModeBoundaryAndNoOpTimestamp(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	nb, _ := store.Create("mode", "")
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{input: "", want: ModeSoft, ok: true},
		{input: "soft", want: ModeSoft, ok: true},
		{input: " soft ", want: ModeSoft, ok: true},
		{input: "strict", want: ModeStrict, ok: true},
		{input: " strict ", want: ModeStrict, ok: true},
		{input: "STRICT", ok: false},
		{input: "unknown", ok: false},
	}
	for _, tc := range tests {
		before, _ := store.Get(nb.ID)
		err := store.SetMode(nb.ID, tc.input)
		if !tc.ok {
			if err == nil || !strings.Contains(err.Error(), "invalid mode") {
				t.Errorf("SetMode(%q) error = %v", tc.input, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("SetMode(%q): %v", tc.input, err)
			continue
		}
		after, _ := store.Get(nb.ID)
		if after.Mode != tc.want {
			t.Errorf("SetMode(%q) mode = %q, want %q", tc.input, after.Mode, tc.want)
		}
		if before.Mode == tc.want && after.Updated != before.Updated {
			t.Errorf("SetMode(%q) no-op changed Updated %d -> %d", tc.input, before.Updated, after.Updated)
		}
	}
	if err := store.SetMode("missing", "strict"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing SetMode error = %v", err)
	}
}

func TestLoadAllSkipsNonJSONCorruptAndEmptyIDFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	valid := Notebook{ID: "valid", Name: "Valid", Created: 10, Updated: 20}
	data, _ := json.Marshal(valid)
	fixtures := map[string][]byte{
		"valid.json":        data,
		"corrupt.json":      []byte("{bad"),
		"empty-id.json":     []byte(`{"name":"No ID"}`),
		"ignored.txt":       []byte(`{"id":"ignored","name":"Ignored"}`),
		"also-ignored.JSON": []byte(`{"id":"upper","name":"Upper"}`),
	}
	for name, body := range fixtures {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "directory.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	list := store.List()
	if len(list) != 1 || list[0].ID != "valid" {
		t.Fatalf("loaded notebooks = %#v", list)
	}
	if store.lastStamp != 20 {
		t.Fatalf("lastStamp = %d, want 20", store.lastStamp)
	}
}

func TestConcurrentEnsureForDealCreatesExactlyOneNotebook(t *testing.T) {
	const workers = 128
	store := newTestStore(t)
	start := make(chan struct{})
	ids := make(chan string, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			nb, err := store.EnsureForDeal("deal/shared", fmt.Sprintf("name-%d", worker), "")
			if err != nil {
				t.Errorf("EnsureForDeal: %v", err)
				return
			}
			ids <- nb.ID
		}()
	}
	close(start)
	wg.Wait()
	close(ids)
	unique := make(map[string]bool)
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 || len(store.List()) != 1 {
		t.Fatalf("unique IDs=%v notebooks=%d", unique, len(store.List()))
	}
}

func TestConcurrentAddSourceAssignsUniqueMonotonicCites(t *testing.T) {
	const workers = 160
	store := newTestStore(t)
	nb, _ := store.Create("sources", "")
	start := make(chan struct{})
	cites := make(chan string, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			source, err := store.AddSource(nb.ID, Source{Kind: KindNote, Text: fmt.Sprintf("source-%d", worker)})
			if err != nil {
				t.Errorf("AddSource: %v", err)
				return
			}
			cites <- source.Cite
		}()
	}
	close(start)
	wg.Wait()
	close(cites)
	numbers := make([]int, 0, workers)
	for cite := range cites {
		n, ok := parseCite(cite)
		if !ok {
			t.Errorf("invalid cite %q", cite)
			continue
		}
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	for i, got := range numbers {
		if got != i+1 {
			t.Fatalf("cite sequence at %d = %d", i, got)
		}
	}
	got, _ := store.Get(nb.ID)
	if len(got.Sources) != workers {
		t.Fatalf("sources = %d, want %d", len(got.Sources), workers)
	}
}

func TestConcurrentPinUniqueDeduplicatesStableRef(t *testing.T) {
	const workers = 128
	store := newTestStore(t)
	start := make(chan struct{})
	var addedCount int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			added, err := store.PinUnique("deal/shared", "Deal", Source{Kind: KindMail, Ref: "mail:stable", Text: fmt.Sprintf("body-%d", worker)})
			if err != nil {
				t.Errorf("PinUnique: %v", err)
				return
			}
			if added {
				mu.Lock()
				addedCount++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if addedCount != 1 {
		t.Fatalf("added count = %d, want 1", addedCount)
	}
	nb, ok := store.GetByDealRef("deal/shared")
	if !ok || len(nb.Sources) != 1 || nb.Sources[0].Ref != "mail:stable" {
		t.Fatalf("notebook = %#v ok=%v", nb, ok)
	}
}

func TestPersistenceSavesWithRestrictedPermsAndCleansTempFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "notebooks"))
	if err != nil {
		t.Fatal(err)
	}
	nb, err := store.Create("private", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddSource(nb.ID, Source{Kind: KindNote, Text: "confidential"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.dir, nb.ID+".json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("notebook file mode = %o, want no group/other bits", info.Mode().Perm())
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file remains after save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !json.Valid(data) || !strings.Contains(string(data), "confidential") {
		t.Fatalf("saved JSON invalid: data=%q err=%v", data, err)
	}
}

func TestCloneNilAndEmptySliceShapes(t *testing.T) {
	t.Parallel()

	nilNB := &Notebook{ID: "nil"}
	nilClone := clone(nilNB)
	if nilClone == nil || nilClone.Sources != nil || nilClone.ProjectRefs != nil {
		t.Fatalf("nil clone = %#v", nilClone)
	}
	emptyNB := &Notebook{ID: "empty", Sources: []Source{}, ProjectRefs: []string{}}
	emptyClone := clone(emptyNB)
	// The wire contract permits nil for empty omitempty fields; focus on
	// isolation and semantic emptiness rather than backing-slice identity.
	if len(emptyClone.Sources) != 0 || len(emptyClone.ProjectRefs) != 0 {
		t.Fatalf("empty clone = %#v", emptyClone)
	}
	if !reflect.DeepEqual(nilClone, nilNB) {
		t.Fatalf("nil clone differs: got=%#v want=%#v", nilClone, nilNB)
	}
}
