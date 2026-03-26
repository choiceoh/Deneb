package skill

import (
	"testing"
)

func TestBuildCoreToolCatalog(t *testing.T) {
	groups := buildCoreToolCatalog()
	if len(groups) == 0 {
		t.Fatal("expected non-empty catalog groups")
	}

	for _, g := range groups {
		if g.Source != "core" {
			t.Errorf("group %q source = %q, want 'core'", g.ID, g.Source)
		}
		if g.ID == "" {
			t.Error("group has empty ID")
		}
		if g.Label == "" {
			t.Error("group has empty Label")
		}
		if len(g.Tools) == 0 {
			t.Errorf("group %q has no tools (empty sections should be skipped)", g.ID)
		}

		for _, tool := range g.Tools {
			if tool.ID == "" {
				t.Errorf("tool in group %q has empty ID", g.ID)
			}
			if tool.Label != tool.ID {
				t.Errorf("tool %q label = %q, want label == ID", tool.ID, tool.Label)
			}
			if tool.Source != "core" {
				t.Errorf("tool %q source = %q, want 'core'", tool.ID, tool.Source)
			}
			if tool.Description == "" {
				t.Errorf("tool %q has empty description", tool.ID)
			}
		}
	}
}

func TestBuildCoreToolCatalog_KnownGroups(t *testing.T) {
	groups := buildCoreToolCatalog()

	expectedIDs := map[string]bool{
		"fs": true, "runtime": true, "web": true, "memory": true,
		"sessions": true, "messaging": true, "automation": true,
		"nodes": true, "media": true,
	}

	for _, g := range groups {
		delete(expectedIDs, g.ID)
	}
	for id := range expectedIDs {
		t.Errorf("expected group %q not found in catalog", id)
	}
}

func TestBuildCoreToolCatalog_ToolProfiles(t *testing.T) {
	groups := buildCoreToolCatalog()

	for _, g := range groups {
		for _, tool := range g.Tools {
			if tool.ID == "read" {
				found := false
				for _, p := range tool.DefaultProfiles {
					if p == ProfileCoding {
						found = true
					}
				}
				if !found {
					t.Errorf("tool 'read' should have 'coding' profile")
				}
				return
			}
		}
	}
	t.Error("tool 'read' not found in catalog")
}

func TestCatalogProfileOptions(t *testing.T) {
	if len(catalogProfileOptions) != 4 {
		t.Fatalf("expected 4 profile options, got %d", len(catalogProfileOptions))
	}

	expected := []string{ProfileMinimal, ProfileCoding, ProfileMessaging, ProfileFull}
	for i, opt := range catalogProfileOptions {
		if opt.ID != expected[i] {
			t.Errorf("profile[%d].ID = %q, want %q", i, opt.ID, expected[i])
		}
		if opt.Label == "" {
			t.Errorf("profile[%d] has empty label", i)
		}
	}
}
