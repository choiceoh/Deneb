package skills

import (
	"fmt"
	"sync"
	"testing"
)

func boolPtr(v bool) *bool { return &v }

func TestRegistryInstallUpdateAndStatus(t *testing.T) {
	registry := NewRegistry()
	if ack := registry.Install("  github  ", ""); !ack.OK {
		t.Fatalf("Install failed: %#v", ack)
	}
	if ack := registry.Install("github", ""); !ack.OK || ack.Message != `skill "github" already installed` {
		t.Fatalf("idempotent Install = %#v", ack)
	}

	updated, err := registry.Update("github", ConfigPatch{
		Enabled: boolPtr(false),
		APIKey:  "secret",
		Env:     map[string]string{"REGION": "kr", "MODE": "safe"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Enabled || updated.Config["apiKey"] != "secret" || updated.Config["REGION"] != "kr" {
		t.Fatalf("updated skill = %#v", updated)
	}
	status := registry.Status("")
	if len(status.Skills) != 1 || status.Skills[0].Enabled {
		t.Fatalf("Status = %#v", status)
	}
}

func TestRegistryRejectsMissingSkillAndBlankInstall(t *testing.T) {
	registry := NewRegistry()
	if ack := registry.Install("  ", ""); ack.OK || ack.Message == "" {
		t.Fatalf("blank Install = %#v, want rejected", ack)
	}
	if got, err := registry.Update("missing", ConfigPatch{}); err == nil || got != nil {
		t.Fatalf("Update(missing) = (%#v, %v)", got, err)
	}
}

func TestRegistryReadResultsCannotMutateInternalState(t *testing.T) {
	registry := NewRegistry()
	registry.bins = []string{"git", "rg"}
	registry.Install("github", "")
	updated, err := registry.Update("github", ConfigPatch{Env: map[string]string{"TOKEN": "original"}})
	if err != nil {
		t.Fatal(err)
	}

	updated.Config["TOKEN"] = "mutated-return"
	status := registry.Status("")
	status.Skills[0].Config["TOKEN"] = "mutated-status"
	status.RequiredBins[0] = "mutated-bin"
	bins := registry.ListBins()
	bins[1] = "mutated-list"

	fresh := registry.Status("")
	if got := fresh.Skills[0].Config["TOKEN"]; got != "original" {
		t.Fatalf("internal Config mutated through returned value: %q", got)
	}
	if got := fresh.RequiredBins; len(got) != 2 || got[0] != "git" || got[1] != "rg" {
		t.Fatalf("internal bins mutated: %v", got)
	}
}

func TestRegistryStatusReturnsSkillsSortedByKey(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"zeta", "alpha", "middle"} {
		registry.Install(name, "")
	}
	got := registry.Status("").Skills
	if len(got) != 3 || got[0].Key != "alpha" || got[1].Key != "middle" || got[2].Key != "zeta" {
		t.Fatalf("Status order = %#v", got)
	}
}

func TestRegistryConcurrentInstallAndUpdate(t *testing.T) {
	registry := NewRegistry()
	const count = 64
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("skill-%02d", i)
			registry.Install(name, "")
			if _, err := registry.Update(name, ConfigPatch{Env: map[string]string{"INDEX": fmt.Sprint(i)}}); err != nil {
				t.Errorf("Update(%s): %v", name, err)
			}
		}(i)
	}
	wg.Wait()

	status := registry.Status("")
	if len(status.Skills) != count {
		t.Fatalf("concurrent skill count = %d, want %d", len(status.Skills), count)
	}
	for i, skill := range status.Skills {
		want := fmt.Sprintf("skill-%02d", i)
		if skill.Key != want || skill.Config["INDEX"] != fmt.Sprint(i) {
			t.Errorf("skill[%d] = %#v, want %s", i, skill, want)
		}
	}
}
