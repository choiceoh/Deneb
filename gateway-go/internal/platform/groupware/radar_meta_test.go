package groupware

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadRadarDocMetaIndex_AgeAndEscalation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RadarStateFileName)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	first := now.Add(-6 * time.Hour).UnixMilli()
	state := radarState{
		Docs: map[string]radarDocState{
			"99178": {FirstSeenAt: first, LastSeenAt: now.UnixMilli(), EscalationLevel: RadarEscalationLevelFourHours, Notified: true},
			"fresh": {FirstSeenAt: now.Add(-30 * time.Minute).UnixMilli(), LastSeenAt: now.UnixMilli()},
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	got := LoadRadarDocMetaIndex(path, now)
	meta, ok := got["99178"]
	if !ok || meta.EscalationLevel != RadarEscalationLevelFourHours || meta.AgeHours < 5 {
		t.Fatalf("99178 meta = %+v", meta)
	}
	if meta.StaleLabel == "" {
		t.Fatalf("expected stale label, got %+v", meta)
	}
	if _, ok := got["fresh"]; !ok {
		t.Fatalf("fresh missing: %+v", got)
	}
}

func TestLoadRadarDocMetaIndex_MissingFileEmpty(t *testing.T) {
	got := LoadRadarDocMetaIndex(filepath.Join(t.TempDir(), "nope.json"), time.Now())
	if len(got) != 0 {
		t.Fatalf("want empty, got %+v", got)
	}
}
