package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// Opt-in live timing against ~/.deneb/wiki: DENEB_KNOWLEDGE_LIVE=1.
func TestLiveKnowledgeRecallTiming(t *testing.T) {
	if os.Getenv("DENEB_KNOWLEDGE_LIVE") != "1" {
		t.Skip("set DENEB_KNOWLEDGE_LIVE=1 for live wiki recall timing")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	wikiDir := filepath.Join(home, ".deneb", "wiki")
	diaryDir := filepath.Join(home, ".deneb", "diary")
	if _, err := os.Stat(wikiDir); err != nil {
		t.Skip("no ~/.deneb/wiki")
	}
	store, err := wiki.NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	router := New(NewWikiAdapter(store))
	ctx := context.Background()
	q := "계약"
	if v := os.Getenv("DENEB_KNOWLEDGE_LIVE_Q"); v != "" {
		q = v
	}

	t0 := time.Now()
	cold := router.RecallPacket(ctx, q, 10, RecallOptions{}).Results
	coldMS := time.Since(t0)

	t1 := time.Now()
	warm := router.RecallPacket(ctx, q, 10, RecallOptions{}).Results
	warmMS := time.Since(t1)

	t.Logf("query=%q cold=%s hits=%d warm=%s hits=%d", q, coldMS, len(cold), warmMS, len(warm))
	if warmMS > coldMS*2 && warmMS > 500*time.Millisecond {
		// Soft signal only — environments vary; log for operators.
		t.Logf("warm recall unexpectedly slow relative to cold")
	}
}
