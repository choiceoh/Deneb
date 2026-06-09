package genesis

import (
	"log/slog"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

// TestGenesis_DailyCapPersistsAcrossRestart verifies the MaxSkillsPerDay cap
// survives a gateway restart. The counter was in-memory only, so the gateway's
// frequent SIGUSR1 restarts reset it to 0 — letting genesis exceed its daily
// cap simply by restarting.
func TestGenesis_DailyCapPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{OutputDir: dir, MaxSkillsPerDay: 10}
	logger := slog.Default()

	s1 := NewService(cfg, nil, skills.NewCatalog(logger), logger)
	today := time.Now().Format("2006-01-02")
	s1.mu.Lock()
	s1.dailyCount = 3
	s1.dailyCountDate = today
	s1.saveDailyCapLocked()
	s1.mu.Unlock()

	// New service = gateway restart. The count must persist, not reset to 0.
	s2 := NewService(cfg, nil, skills.NewCatalog(logger), logger)
	if s2.dailyCount != 3 || s2.dailyCountDate != today {
		t.Fatalf("expected dailyCount=3 date=%s restored across restart, got count=%d date=%s",
			today, s2.dailyCount, s2.dailyCountDate)
	}
}
