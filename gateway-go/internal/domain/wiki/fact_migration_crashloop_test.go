package wiki

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The legacy cutover must finish even when a bullet cannot be converted.
//
// This is the 2026-08-23 production outage. Two facts about the same subject+key
// disagreed on kind, upsertFact refused the second, and ImportLegacyFactFiles
// returned that error — which propagates to server init, which exits. The
// offending row is byte-identical on every boot, so the "resumes on the next
// clean startup" retry the caller relies on could never succeed: the gateway
// crash-looped ~200 times until the row was tolerated. Nothing is lost by
// skipping one, since the row stays in the preserved *.legacy.md copy.
func TestImportLegacyFactFilesCompletesOverAPopulatedStore(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	// Facts already committed by live corrections — the state that made the
	// production import collide.
	if _, err := store.UpsertFact(FactInput{
		Subject: "self", Key: "communication.response_length", Value: "답변은 짧게 해줘.",
		Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
		Sources: []string{"session:client:main#1"},
		At:      time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	workspace := t.TempDir()
	legacy := "# MEMORY\n\n" +
		"- 감정: 피곤·기분 저하를 솔직히 표현 — 그럴 땐 장황한 설명 대신 가벼운 유머·짧은 위로가 효과적.\n" +
		"- **주소**: 서울\n"
	if err := os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	imported, err := store.ImportLegacyFactFiles(workspace)
	if err != nil {
		t.Fatalf("cutover returned an error the caller turns into a failed startup: %v", err)
	}
	if imported == 0 {
		t.Fatal("cutover imported nothing")
	}
	// Whatever could not be converted is reported rather than thrown away silently.
	for _, skip := range store.LegacyFactImportSkips() {
		t.Logf("skipped: %s", skip)
	}
}

// A clean import reports no skips, so the accessor cannot be mistaken for a
// permanent warning.
func TestLegacyFactImportSkipsEmptyOnCleanImport(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte("# MEMORY\n\n- **주소**: 서울\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := store.ImportLegacyFactFiles(workspace); err != nil {
		t.Fatal(err)
	}
	if skips := store.LegacyFactImportSkips(); len(skips) != 0 {
		t.Fatalf("clean import reported skips: %v", skips)
	}
}
