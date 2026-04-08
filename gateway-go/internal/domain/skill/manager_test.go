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
		t.Fatalf("got %d, want 1 skill", len(status.Skills))
	}
	if status.Skills[0].Key != "weather" {
		t.Fatalf("got %q, want key 'weather'", status.Skills[0].Key)
	}
	if !status.Skills[0].Installed {
		t.Fatal("expected installed=true")
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


func TestListBins(t *testing.T) {
	m := NewManager()
	m.SetBins([]string{"ffmpeg", "yt-dlp"})
	bins := m.ListBins()
	if len(bins) != 2 {
		t.Fatalf("got %d, want 2 bins", len(bins))
	}
}

func TestRegisterSkill(t *testing.T) {
	m := NewManager()
	m.RegisterSkill(SkillEntry{Key: "github", Name: "GitHub", Installed: true, Enabled: true})

	status := m.Status("")
	if len(status.Skills) != 1 {
		t.Fatalf("got %d, want 1 skill", len(status.Skills))
	}
}
