package genesis

import (
	"log/slog"
	"testing"
)

// The usage ledger is keyed by skill name and the evolver resolves that key by
// exact catalog match; an empty/whitespace name can only ever be a phantom.
func TestRecordUsageRefusesEmptySkillName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "   ", "\t"} {
		if err := tr.RecordUsage(UsageRecord{SkillName: name, SessionKey: "client:main", Success: true}); err == nil {
			t.Fatalf("RecordUsage accepted name %q", name)
		}
	}
	if err := tr.RecordUsage(UsageRecord{SkillName: "  contract-review ", SessionKey: "client:main", Success: true}); err != nil {
		t.Fatal(err)
	}
	stats, err := tr.Stats("contract-review")
	if err != nil || stats == nil || stats.TotalUses != 1 {
		t.Fatalf("trimmed name must key the stats: stats=%+v err=%v", stats, err)
	}
}
