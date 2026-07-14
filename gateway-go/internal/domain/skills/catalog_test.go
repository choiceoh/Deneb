package skills

import (
	"fmt"
	"sync"
	"testing"
)

func TestResolveSkillKeyPrefersMetadataSkillKeyWithNameFallback(t *testing.T) {
	tests := []struct {
		name     string
		entry    SkillEntry
		expected string
	}{
		{
			name:     "uses skill name when no metadata",
			entry:    SkillEntry{Skill: Skill{Name: "weather"}},
			expected: "weather",
		},
		{
			name: "uses metadata skillKey when present",
			entry: SkillEntry{
				Skill:    Skill{Name: "weather"},
				Metadata: &DenebSkillMetadata{SkillKey: "custom-weather"},
			},
			expected: "custom-weather",
		},
		{
			name: "falls back to name when skillKey is empty",
			entry: SkillEntry{
				Skill:    Skill{Name: "github"},
				Metadata: &DenebSkillMetadata{},
			},
			expected: "github",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveSkillKey(tt.entry)
			if got != tt.expected {
				t.Errorf("ResolveSkillKey() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCatalogListReturnsEntriesSortedByName(t *testing.T) {
	c := NewCatalog(nil)

	c.Register(SkillEntry{Skill: Skill{Name: "weather", Source: SourceBundled}})
	c.Register(SkillEntry{Skill: Skill{Name: "github", Source: SourceWorkspace}})
	c.Register(SkillEntry{Skill: Skill{Name: "coding", Source: SourceManaged}})

	entries := c.List()
	if len(entries) != 3 {
		t.Fatalf("got %d, want 3 entries", len(entries))
	}
	// Should be sorted alphabetically.
	if entries[0].Skill.Name != "coding" {
		t.Errorf("got %q, want first entry to be 'coding'", entries[0].Skill.Name)
	}
	if entries[1].Skill.Name != "github" {
		t.Errorf("got %q, want second entry to be 'github'", entries[1].Skill.Name)
	}
}

func TestCatalogBuildWorkspaceSnapshotReturnsFilteredEntriesByName(t *testing.T) {
	c := NewCatalog(nil)
	c.Register(SkillEntry{Skill: Skill{Name: "weather"}})
	c.Register(SkillEntry{Skill: Skill{Name: "github"}})
	c.Register(SkillEntry{Skill: Skill{Name: "coding"}})

	// nil filter = unrestricted.
	snap := c.BuildWorkspaceSnapshot(nil)
	if len(snap.Entries) != 3 {
		t.Errorf("nil filter should return all, got %d", len(snap.Entries))
	}

	// Empty filter = no skills.
	snap = c.BuildWorkspaceSnapshot([]string{})
	if len(snap.Entries) != 0 {
		t.Errorf("empty filter should return none, got %d", len(snap.Entries))
	}

	// Specific filter.
	snap = c.BuildWorkspaceSnapshot([]string{"weather", "coding"})
	if len(snap.Entries) != 2 {
		t.Errorf("filter [weather, coding] should return 2, got %d", len(snap.Entries))
	}
}

func TestParseFrontmatter(t *testing.T) {
	content := `---
name: test-skill
description: A test skill
user-invocable: true
---

# Test Skill
`
	fm := ParseFrontmatter(content)
	if fm["name"] != "test-skill" {
		t.Errorf("got %q, want name 'test-skill'", fm["name"])
	}
	if fm["description"] != "A test skill" {
		t.Errorf("got %q, want description", fm["description"])
	}
}

func TestResolveSkillInvocationPolicyAppliesOverridesWithDefaultFallback(t *testing.T) {
	fm := ParsedFrontmatter{
		"user-invocable":           "false",
		"disable-model-invocation": "true",
	}
	pol := ResolveSkillInvocationPolicy(fm)
	if pol.UserInvocable {
		t.Error("expected UserInvocable=false")
	}
	if !pol.DisableModelInvocation {
		t.Error("expected DisableModelInvocation=true")
	}

	// Defaults.
	pol = ResolveSkillInvocationPolicy(ParsedFrontmatter{})
	if !pol.UserInvocable {
		t.Error("default UserInvocable should be true")
	}
	if pol.DisableModelInvocation {
		t.Error("default DisableModelInvocation should be false")
	}
}

func TestCatalogRegisterPreservesDeepCopyAgainstCallerMutation(t *testing.T) {
	extract := true
	strip := 2
	original := SkillEntry{
		Skill:       Skill{Name: "deep-copy", Source: SourceWorkspace},
		Frontmatter: ParsedFrontmatter{"description": "original"},
		Metadata: &DenebSkillMetadata{
			Tags:      []string{"safe"},
			Triggers:  []string{"검토"},
			Requires:  &SkillRequires{Bins: []string{"git"}, Env: []string{"TOKEN"}},
			LocalExec: &SkillLocalExec{Command: "tool", Args: []string{"--safe"}},
			Install:   []SkillInstallSpec{{Kind: "download", Bins: []string{"tool"}, Extract: &extract, StripComponents: &strip}},
		},
		Invocation: &SkillInvocationPolicy{UserInvocable: true},
	}
	catalog := NewCatalog(nil)
	catalog.Register(original)

	original.Frontmatter["description"] = "caller mutation"
	original.Metadata.Tags[0] = "mutated"
	original.Metadata.Triggers[0] = "changed"
	original.Metadata.Requires.Bins[0] = "bad-bin"
	original.Metadata.LocalExec.Args[0] = "--unsafe"
	original.Metadata.Install[0].Bins[0] = "bad-install"
	*original.Metadata.Install[0].Extract = false
	*original.Metadata.Install[0].StripComponents = 9
	original.Invocation.UserInvocable = false

	got, ok := catalog.Get("deep-copy")
	if !ok {
		t.Fatal("registered entry missing")
	}
	if got.Frontmatter["description"] != "original" || got.Metadata.Tags[0] != "safe" || got.Metadata.Triggers[0] != "검토" {
		t.Fatalf("catalog retained caller-owned maps/slices: %#v", got)
	}
	if got.Metadata.Requires.Bins[0] != "git" || got.Metadata.LocalExec.Args[0] != "--safe" || got.Metadata.Install[0].Bins[0] != "tool" {
		t.Fatalf("catalog retained nested caller slices: %#v", got.Metadata)
	}
	if !*got.Metadata.Install[0].Extract || *got.Metadata.Install[0].StripComponents != 2 || !got.Invocation.UserInvocable {
		t.Fatalf("catalog retained nested caller pointers: %#v", got)
	}
}

func TestCatalogReadViewsCannotMutateStoredEntry(t *testing.T) {
	catalog := NewCatalog(nil)
	catalog.Register(SkillEntry{
		Skill:       Skill{Name: "immutable"},
		Frontmatter: ParsedFrontmatter{"key": "value"},
		Metadata:    &DenebSkillMetadata{Tags: []string{"tag"}, Requires: &SkillRequires{Bins: []string{"rg"}}},
	})

	fromGet, _ := catalog.Get("immutable")
	fromGet.Frontmatter["key"] = "get-mutated"
	fromGet.Metadata.Tags[0] = "get-mutated"
	fromGet.Metadata.Requires.Bins[0] = "get-mutated"
	fromList := catalog.List()
	fromList[0].Metadata.Tags[0] = "list-mutated"
	fromSnapshot := catalog.Snapshot()
	fromSnapshot.Entries[0].Frontmatter["key"] = "snapshot-mutated"

	fresh, _ := catalog.Get("immutable")
	if fresh.Frontmatter["key"] != "value" || fresh.Metadata.Tags[0] != "tag" || fresh.Metadata.Requires.Bins[0] != "rg" {
		t.Fatalf("stored entry mutated through a read view: %#v", fresh)
	}
}

func TestCatalogTracksVersionCountAndUnregisterDeletesEntry(t *testing.T) {
	catalog := NewCatalog(nil)
	catalog.SetVersion(42)
	catalog.Register(SkillEntry{Skill: Skill{Name: "one"}})
	catalog.Register(SkillEntry{Skill: Skill{Name: "two"}})
	if catalog.Version() != 42 || catalog.Snapshot().Version != 42 || catalog.Count() != 2 {
		t.Fatalf("catalog state = version %d, snapshot %d, count %d", catalog.Version(), catalog.Snapshot().Version, catalog.Count())
	}
	if !catalog.Unregister("one") || catalog.Unregister("one") || catalog.Count() != 1 {
		t.Fatalf("unexpected unregister behavior, count=%d", catalog.Count())
	}
	if _, ok := catalog.Get("one"); ok {
		t.Fatal("unregistered entry still returned")
	}
}

func TestCatalogConcurrentRegistrationAndSnapshots(t *testing.T) {
	catalog := NewCatalog(nil)
	const count = 64
	var writers sync.WaitGroup
	for i := range count {
		writers.Add(1)
		go func(i int) {
			defer writers.Done()
			catalog.Register(SkillEntry{Skill: Skill{Name: fmt.Sprintf("skill-%02d", i)}})
			_ = catalog.Snapshot()
			_ = catalog.List()
		}(i)
	}
	writers.Wait()
	if catalog.Count() != count {
		t.Fatalf("concurrent count = %d, want %d", catalog.Count(), count)
	}
	entries := catalog.List()
	for i, entry := range entries {
		if want := fmt.Sprintf("skill-%02d", i); entry.Skill.Name != want {
			t.Fatalf("entries[%d] = %q, want %q", i, entry.Skill.Name, want)
		}
	}
}
