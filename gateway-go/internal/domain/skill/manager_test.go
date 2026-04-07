package skill

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestInstallAndGetStatus(t *testing.T) {
	m := NewManager()

	result := m.Install("weather", "inst-1")
	if !result.OK {
		t.Fatal("expected OK")
	}

	status := m.Status("")
	if len(status.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(status.Skills))
	}
	if status.Skills[0].Key != "weather" {
		t.Fatalf("expected key 'weather', got %q", status.Skills[0].Key)
	}
	if !status.Skills[0].Installed {
		t.Fatal("expected installed=true")
	}
}

func TestInstallDuplicate(t *testing.T) {
	m := NewManager()
	m.Install("weather", "inst-1")
	result := m.Install("weather", "inst-2")
	if !result.OK {
		t.Fatal("expected OK even for duplicate")
	}
}

func TestUpdateSkill(t *testing.T) {
	m := NewManager()
	m.Install("coding", "inst-1")

	enabled := false
	updated, err := m.Update("coding", SkillPatch{
		Enabled: &enabled,
		APIKey:  "sk-123",
	})
	testutil.NoError(t, err)
	if updated.Enabled {
		t.Fatal("expected enabled=false")
	}
	if updated.Config["apiKey"] != "sk-123" {
		t.Fatal("expected apiKey to be set")
	}
}

func TestUpdateNotFound(t *testing.T) {
	m := NewManager()
	_, err := m.Update("unknown", SkillPatch{})
	if err == nil {
		t.Fatal("expected error for unknown skill")
	}
}

func TestListBins(t *testing.T) {
	m := NewManager()
	m.SetBins([]string{"ffmpeg", "yt-dlp"})
	bins := m.ListBins()
	if len(bins) != 2 {
		t.Fatalf("expected 2 bins, got %d", len(bins))
	}
}

func TestRegisterSkill(t *testing.T) {
	m := NewManager()
	m.RegisterSkill(SkillEntry{Key: "github", Name: "GitHub", Installed: true, Enabled: true})

	status := m.Status("")
	if len(status.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(status.Skills))
	}
}
