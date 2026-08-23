package wiki

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFactIdentitySeparatesColonBearingTupleMembersAcrossRestart(t *testing.T) {
	root := t.TempDir()
	wikiDir, diaryDir := filepath.Join(root, "wiki"), filepath.Join(root, "diary")
	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	inputs := []FactInput{
		{Subject: "project:alpha", Key: "quote.amount", Value: "alpha project amount", At: base},
		{Subject: "project", Key: "alpha:quote.amount", Value: "project alpha-key amount", At: base.Add(time.Minute)},
	}
	for _, input := range inputs {
		if result, upsertErr := store.UpsertFact(input); upsertErr != nil || !result.Committed {
			t.Fatalf("UpsertFact(%+v) = %+v, err=%v", input, result, upsertErr)
		}
	}
	assertSeparated := func(label string, current *Store) {
		t.Helper()
		for _, input := range inputs {
			history := current.FactHistory(input.Subject, input.Key)
			if len(history) != 1 || history[0].Value != input.Value || history[0].Status != FactStatusCurrent {
				t.Fatalf("%s history(%q, %q)=%+v", label, input.Subject, input.Key, history)
			}
		}
		if left, right := factIdentity(inputs[0].Subject, inputs[0].Key), factIdentity(inputs[1].Subject, inputs[1].Key); left == right {
			t.Fatalf("%s canonical identities still collide: %q", label, left)
		}
		if facts := current.SnapshotFacts().Facts; len(facts) != 2 {
			t.Fatalf("%s snapshot identities=%d, want 2: %+v", label, len(facts), facts)
		}
	}
	assertSeparated("live", store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	assertSeparated("reopened", reopened)
}

func TestFactPlaneReplaysLegacyIdentityAndRekeysSnapshot(t *testing.T) {
	root := t.TempDir()
	wikiDir, diaryDir := filepath.Join(root, "wiki"), filepath.Join(root, "diary")
	empty, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.Close(); err != nil {
		t.Fatal(err)
	}

	const value = "한국어로 답변"
	const atMs = int64(1_777_075_200_000)
	legacyIdentity := legacyFactIdentity("self", "communication.language")
	claim := FactClaim{
		Subject: "self", Key: "communication.language", Value: value,
		Kind: FactKindPreference, Authority: FactAuthorityDirectUser, Status: FactStatusCurrent,
		RecordedAtMs: atMs, Revision: 1,
	}
	claim.ID = newFactOperationID(1, legacyIdentity, value, atMs)
	mutation := FactMutation{
		SchemaVersion: factSchemaVersion, Revision: 1, OperationID: claim.ID, AtMs: atMs,
		Op: "assert", Identity: legacyIdentity, Claim: &claim, Resolution: "create",
	}
	mutationRaw, err := json.Marshal(mutation)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wikiDir, factJournalFile), append(mutationRaw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	legacySnapshot := FactSnapshot{
		SchemaVersion: factSchemaVersion, Revision: 1,
		Facts: map[string][]FactClaim{legacyIdentity: {claim}},
	}
	snapshotRaw, err := json.MarshalIndent(legacySnapshot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wikiDir, factStateFile), append(snapshotRaw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("replay legacy identity: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	history := store.FactHistory("self", "communication.language")
	if len(history) != 1 || history[0].ID != claim.ID || history[0].Value != value {
		t.Fatalf("legacy history=%+v", history)
	}
	canonicalIdentity := factIdentity("self", "communication.language")
	checkpoint := store.SnapshotFacts()
	if _, ok := checkpoint.Facts[canonicalIdentity]; !ok {
		t.Fatalf("canonical snapshot key %q missing: %+v", canonicalIdentity, checkpoint.Facts)
	}
	if _, ok := checkpoint.Facts[legacyIdentity]; ok {
		t.Fatalf("legacy snapshot key %q was not rekeyed", legacyIdentity)
	}

	if _, err := store.UpsertFact(FactInput{
		Subject: "self", Key: "communication.language", Value: "영어로 답변",
		Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
		At: time.UnixMilli(atMs).Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	journalRaw, err := os.ReadFile(filepath.Join(wikiDir, factJournalFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(journalRaw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("journal lines=%d, want 2", len(lines))
	}
	var latest FactMutation
	if err := json.Unmarshal([]byte(lines[1]), &latest); err != nil {
		t.Fatal(err)
	}
	if latest.Identity != canonicalIdentity || latest.Identity == legacyIdentity {
		t.Fatalf("new mutation identity=%q, canonical=%q legacy=%q", latest.Identity, canonicalIdentity, legacyIdentity)
	}
}

func TestFactPlaneReplaysCrossTupleStatusUpdateFromCollidingLegacyIdentity(t *testing.T) {
	root := t.TempDir()
	wikiDir, diaryDir := filepath.Join(root, "wiki"), filepath.Join(root, "diary")
	empty, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.Close(); err != nil {
		t.Fatal(err)
	}

	const (
		firstSubject  = "project:alpha"
		firstKey      = "quote.amount"
		secondSubject = "project"
		secondKey     = "alpha:quote.amount"
	)
	sharedLegacyIdentity := legacyFactIdentity(firstSubject, firstKey)
	if other := legacyFactIdentity(secondSubject, secondKey); other != sharedLegacyIdentity {
		t.Fatalf("test fixture no longer collides: %q != %q", sharedLegacyIdentity, other)
	}
	baseMs := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC).UnixMilli()
	first := FactClaim{
		Subject: firstSubject, Key: firstKey, Value: "alpha project amount",
		Kind: FactKindGeneric, Authority: FactAuthorityLegacyImport, Status: FactStatusCurrent,
		RecordedAtMs: baseMs, Revision: 1,
	}
	first.ID = newFactOperationID(1, sharedLegacyIdentity, first.Value, baseMs)
	second := FactClaim{
		Subject: secondSubject, Key: secondKey, Value: "project alpha-key amount",
		Kind: FactKindGeneric, Authority: FactAuthorityLegacyImport, Status: FactStatusCurrent,
		RecordedAtMs: baseMs + 1, Revision: 2,
	}
	second.ID = newFactOperationID(2, sharedLegacyIdentity, second.Value, baseMs+1)
	mutations := []FactMutation{
		{
			SchemaVersion: factSchemaVersion, Revision: 1, OperationID: first.ID, AtMs: baseMs,
			Op: "assert", Identity: sharedLegacyIdentity, Claim: &first, Resolution: "create",
		},
		{
			SchemaVersion: factSchemaVersion, Revision: 2, OperationID: second.ID, AtMs: baseMs + 1,
			Op: "assert", Identity: sharedLegacyIdentity, Claim: &second,
			StatusUpdates: map[string]FactStatus{first.ID: FactStatusSuperseded}, Resolution: "replace",
		},
	}
	var journal []byte
	for _, mutation := range mutations {
		raw, marshalErr := json.Marshal(mutation)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		journal = append(journal, raw...)
		journal = append(journal, '\n')
	}
	if err := os.WriteFile(filepath.Join(wikiDir, factJournalFile), journal, 0o600); err != nil {
		t.Fatal(err)
	}

	assertSeparatedHistory := func(label string, store *Store) {
		t.Helper()
		firstHistory := store.FactHistory(firstSubject, firstKey)
		if len(firstHistory) != 1 || firstHistory[0].ID != first.ID || firstHistory[0].Status != FactStatusCurrent {
			t.Fatalf("%s first history=%+v", label, firstHistory)
		}
		secondHistory := store.FactHistory(secondSubject, secondKey)
		if len(secondHistory) != 1 || secondHistory[0].ID != second.ID || secondHistory[0].Status != FactStatusCurrent {
			t.Fatalf("%s second history=%+v", label, secondHistory)
		}
		snapshot := store.SnapshotFacts()
		if len(snapshot.Facts) != 2 {
			t.Fatalf("%s snapshot identities=%d, want 2: %+v", label, len(snapshot.Facts), snapshot.Facts)
		}
		if _, ok := snapshot.Facts[factIdentity(firstSubject, firstKey)]; !ok {
			t.Fatalf("%s first canonical identity missing: %+v", label, snapshot.Facts)
		}
		if _, ok := snapshot.Facts[factIdentity(secondSubject, secondKey)]; !ok {
			t.Fatalf("%s second canonical identity missing: %+v", label, snapshot.Facts)
		}
		if _, ok := snapshot.Facts[sharedLegacyIdentity]; ok {
			t.Fatalf("%s shared legacy identity was retained", label)
		}
	}

	firstOpen, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("first replay: %v", err)
	}
	assertSeparatedHistory("first replay", firstOpen)
	if err := firstOpen.Close(); err != nil {
		t.Fatal(err)
	}

	secondOpen, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("second replay: %v", err)
	}
	t.Cleanup(func() { _ = secondOpen.Close() })
	assertSeparatedHistory("second replay", secondOpen)
}
