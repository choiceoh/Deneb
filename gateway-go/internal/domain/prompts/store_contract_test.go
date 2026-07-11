package prompts

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func promptTemplates() []Template {
	return []Template{
		{ID: " zeta ", Title: "Zulu", Category: "b", DefaultText: " zeta default ", Editable: true},
		{ID: "alpha", Title: "Zulu", Category: "a", DefaultText: " alpha default ", Editable: true},
		{ID: "beta", Title: "Alpha", Category: "a", DefaultText: "beta default", Editable: false},
		{ID: "", Title: "ignored", Editable: true},
		{ID: "alpha", Title: "duplicate ignored", Category: "z", DefaultText: "wrong", Editable: true},
	}
}

func TestNewStoreNormalizesDeduplicatesAndSortsTemplates(t *testing.T) {
	templates := promptTemplates()
	store := NewStore("", templates)
	entries, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got, want := len(entries), 3; got != want {
		t.Fatalf("entries = %d, want %d: %+v", got, want, entries)
	}
	ids := []string{entries[0].ID, entries[1].ID, entries[2].ID}
	if want := []string{"beta", "alpha", "zeta"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("order = %#v, want %#v", ids, want)
	}
	if entries[1].Title != "Zulu" || entries[1].Text != "alpha default" {
		t.Fatalf("duplicate replaced first template: %+v", entries[1])
	}
	if entries[2].ID != "zeta" || entries[2].DefaultText != "zeta default" {
		t.Fatalf("template normalization = %+v", entries[2])
	}

	// Constructor copies template values into its map; mutating the caller's
	// slice cannot alter the store's catalog after construction.
	templates[0].DefaultText = "mutated"
	if got := store.Text("zeta"); got != "zeta default" {
		t.Fatalf("caller mutation changed store text: %q", got)
	}
}

func TestGetTextAndOverrideTextMisses(t *testing.T) {
	store := NewStore("", promptTemplates())
	if _, ok, err := store.Get(" missing "); err != nil || ok {
		t.Fatalf("Get miss ok=%v err=%v", ok, err)
	}
	if got := store.Text("missing"); got != "" {
		t.Fatalf("Text miss = %q", got)
	}
	if got, ok := store.OverrideText("alpha"); ok || got != "" {
		t.Fatalf("OverrideText default = %q,%v", got, ok)
	}
	if _, err := store.Set(" alpha ", " custom "); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, ok := store.OverrideText(" alpha "); !ok || got != "custom" {
		t.Fatalf("OverrideText = %q,%v", got, ok)
	}
	if got := store.Text("alpha"); got != "custom" {
		t.Fatalf("Text override = %q", got)
	}
}

func TestSetRuneLimitBoundary(t *testing.T) {
	store := NewStore("", []Template{{ID: "editable", Editable: true}})
	within := strings.Repeat("가", maxPromptRunes)
	entry, err := store.Set("editable", within)
	if err != nil {
		t.Fatalf("Set at rune limit: %v", err)
	}
	if got := len([]rune(entry.Text)); got != maxPromptRunes {
		t.Fatalf("stored runes = %d", got)
	}
	tooLarge := within + "나"
	if _, err := store.Set("editable", tooLarge); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Set over rune limit err = %v", err)
	}
	if got := store.Text("editable"); got != within {
		t.Fatal("rejected oversized Set changed effective text")
	}
}

func TestLoadFiltersUnknownAndEmptyOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overrides.json")
	wire := overrideFile{Prompts: map[string]overrideEntry{
		" alpha ": {Text: " custom alpha ", UpdatedAtMs: 17},
		"beta":    {Text: "  ", UpdatedAtMs: 18},
		"unknown": {Text: "hidden", UpdatedAtMs: 19},
	}}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path, promptTemplates())
	if got, ok := store.OverrideText("alpha"); !ok || got != "custom alpha" {
		t.Fatalf("alpha override = %q,%v", got, ok)
	}
	if got, ok := store.OverrideText("beta"); ok || got != "" {
		t.Fatalf("empty override = %q,%v", got, ok)
	}
	if got, ok := store.OverrideText("unknown"); ok || got != "" {
		t.Fatalf("unknown override = %q,%v", got, ok)
	}
	entry, ok, err := store.Get("alpha")
	if err != nil || !ok || entry.UpdatedAtMs != 17 || !entry.Overridden {
		t.Fatalf("loaded entry = %+v ok=%v err=%v", entry, ok, err)
	}
}

func TestLoadErrorsAndTextFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path, []Template{{ID: "alpha", DefaultText: " default "}})
	if _, err := store.List(); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("List parse error = %v", err)
	}
	if _, ok, err := store.Get("alpha"); err == nil || ok {
		t.Fatalf("Get parse error ok=%v err=%v", ok, err)
	}
	if got := store.Text("alpha"); got != "default" {
		t.Fatalf("Text did not degrade to template: %q", got)
	}
	if got := store.Text("missing"); got != "" {
		t.Fatalf("Text missing on error = %q", got)
	}
	if got, ok := store.OverrideText("alpha"); ok || got != "" {
		t.Fatalf("OverrideText on parse error = %q,%v", got, ok)
	}

	dirStore := NewStore(t.TempDir(), []Template{{ID: "alpha", DefaultText: "fallback"}})
	if _, err := dirStore.List(); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("directory read error = %v", err)
	}
	if got := dirStore.Text("alpha"); got != "fallback" {
		t.Fatalf("directory Text fallback = %q", got)
	}
}

func TestPersistencePermissionsBackupAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "prompts.json")
	templates := []Template{{ID: "alpha", DefaultText: "default", Editable: true}}
	store := NewStore(path, templates)
	if _, err := store.Set("alpha", "first"); err != nil {
		t.Fatalf("first Set: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file permissions = %o, want 600", perm)
	}
	if _, err := store.Set("alpha", "second"); err != nil {
		t.Fatalf("second Set: %v", err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	reloaded := NewStore(path, templates)
	if got := reloaded.Text("alpha"); got != "second" {
		t.Fatalf("reloaded text = %q", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("persisted JSON lacks trailing newline: %q", data)
	}
}

func TestFailedSetAndResetAreTransactional(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(parentFile, "prompts.json")
	store := NewStore(badPath, []Template{{ID: "alpha", DefaultText: "default", Editable: true}})
	if _, err := store.Set("alpha", "uncommitted"); err == nil {
		t.Fatal("Set unexpectedly succeeded")
	}
	if got := store.Text("alpha"); got != "default" {
		t.Fatalf("failed Set leaked into memory: %q", got)
	}
	if got, ok := store.OverrideText("alpha"); ok || got != "" {
		t.Fatalf("failed Set left override = %q,%v", got, ok)
	}

	goodPath := filepath.Join(t.TempDir(), "prompts.json")
	good := NewStore(goodPath, []Template{{ID: "alpha", DefaultText: "default", Editable: true}})
	if _, err := good.Set("alpha", "committed"); err != nil {
		t.Fatalf("seed Set: %v", err)
	}
	// Force subsequent persistence to fail while retaining the loaded state.
	good.path = badPath
	if _, err := good.Reset("alpha"); err == nil {
		t.Fatal("Reset unexpectedly succeeded")
	}
	if got := good.Text("alpha"); got != "committed" {
		t.Fatalf("failed Reset discarded committed override: %q", got)
	}
}

func TestStoreConcurrentReadersAndWriters(t *testing.T) {
	store := NewStore("", []Template{{ID: "alpha", DefaultText: "default", Editable: true}})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if i%4 == 0 {
					_, _ = store.Set("alpha", strings.Repeat("x", i+j+1))
				} else {
					_, _, _ = store.Get("alpha")
					_, _ = store.List()
					_ = store.Text("alpha")
					_, _ = store.OverrideText("alpha")
				}
			}
		}(i)
	}
	wg.Wait()
	if text := store.Text("alpha"); text == "" {
		t.Fatal("concurrent operations produced empty effective text")
	}
}
